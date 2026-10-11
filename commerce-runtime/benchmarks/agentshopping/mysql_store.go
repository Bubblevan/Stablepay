package agentshopping

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	SimulationDatabaseName = "stablepay_shopping_sim"
	SimulationDatabaseUser = "stablepay_sim"
	SimulationDatabaseHost = "127.0.0.1"
	SimulationDatabasePort = "13327"
	syntheticOrigin        = "SYNTHETIC_SIMULATION"
)

var (
	ErrUnsafeDatabaseTarget = errors.New("refusing non-isolated shopping simulation database target")
	ErrDatasetAlreadyExists = errors.New("simulation dataset already exists")
	ErrIntentAlreadyParsed  = errors.New("shopping task intent is no longer pending")
)

// MySQLStore can connect only to the dedicated loopback simulation schema.
// Its credentials are supplied independently from every StablePay .env/DSN.
type MySQLStore struct {
	db                    *gorm.DB
	tenant                string
	now                   func() time.Time
	mockFulfillmentFaults map[string]MockFulfillmentFaultPlan
}

func OpenLocalMySQLStore(password string) (*MySQLStore, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("SHOP_SIM_DB_PASSWORD is required")
	}
	config := mysqlDriver.Config{
		User:                 SimulationDatabaseUser,
		Passwd:               password,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(SimulationDatabaseHost, SimulationDatabasePort),
		DBName:               SimulationDatabaseName,
		ParseTime:            true,
		Loc:                  time.UTC,
		Timeout:              5 * time.Second,
		ReadTimeout:          10 * time.Second,
		WriteTimeout:         10 * time.Second,
		AllowNativePasswords: true,
		Params:               map[string]string{"charset": "utf8mb4"},
	}
	return OpenMySQLStore(config.FormatDSN())
}

// OpenMySQLStore validates the complete target before opening a socket. This
// prevents accidentally pointing the simulator at the existing production or
// development StablePay MySQL instances.
func OpenMySQLStore(dsn string) (*MySQLStore, error) {
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse isolated simulation DSN: %w", err)
	}
	host, port, splitErr := net.SplitHostPort(parsed.Addr)
	if splitErr != nil || parsed.Net != "tcp" || parsed.DBName != SimulationDatabaseName ||
		parsed.User != SimulationDatabaseUser || host != SimulationDatabaseHost || port != SimulationDatabasePort {
		return nil, ErrUnsafeDatabaseTarget
	}
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open isolated simulation MySQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get isolated simulation SQL handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(24)
	sqlDB.SetMaxIdleConns(8)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	var databaseName string
	if err := db.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("verify isolated simulation schema: %w", err)
	}
	if databaseName != SimulationDatabaseName {
		_ = sqlDB.Close()
		return nil, ErrUnsafeDatabaseTarget
	}
	return &MySQLStore{db: db, tenant: SimulationTenantID, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *MySQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func (s *MySQLStore) Ping(ctx context.Context) error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

func (s *MySQLStore) Migrate(ctx context.Context) error {
	// GORM's default initialism splitting maps the Go field SKUID to sk_uid.
	// Preserve any benchmark rows already written by early builds while moving
	// those isolated tables to the API/database contract column name sku_id.
	for _, table := range []string{"sim_products", "sim_inventory", "sim_quotes", "sim_inventory_reservations", "sim_inventory_adjustments"} {
		var oldCount, newCount int64
		if err := s.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = 'sk_uid'`, table).Scan(&oldCount).Error; err != nil {
			return fmt.Errorf("inspect legacy SKU column in %s: %w", table, err)
		}
		if err := s.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = 'sku_id'`, table).Scan(&newCount).Error; err != nil {
			return fmt.Errorf("inspect SKU column in %s: %w", table, err)
		}
		if oldCount > 0 && newCount > 0 {
			return fmt.Errorf("both legacy sk_uid and canonical sku_id columns exist in %s; refusing an ambiguous data migration", table)
		}
		if oldCount > 0 {
			if err := s.db.WithContext(ctx).Exec(fmt.Sprintf("ALTER TABLE `%s` RENAME COLUMN `sk_uid` TO `sku_id`", table)).Error; err != nil {
				return fmt.Errorf("rename isolated SKU column in %s: %w", table, err)
			}
		}
	}
	return s.db.WithContext(ctx).AutoMigrate(
		&MerchantRecord{}, &ProductRecord{}, &InventoryRecord{}, &BuyerRecord{},
		&ShoppingTaskRecord{}, &QuoteRecord{}, &BuyerConfirmationRecord{}, &BuyerConfirmationAttemptRecord{},
		&OrderRecord{}, &InventoryReservationRecord{}, &InventoryAdjustmentRecord{}, &MockPaymentRecord{},
		&MockFulfillmentRecord{}, &MockFulfillmentAttemptRecord{}, &MockRefundRecord{},
		&SimulationAuditRecord{}, &IntentParseTraceRecord{}, &DatasetRunRecord{},
	)
}

type SimulationSeed struct {
	DatasetID string
	Seed      int64
	Manifest  string
	Merchants []MerchantRecord
	Products  []ProductRecord
	Inventory []InventoryRecord
	Buyers    []BuyerRecord
	Tasks     []ShoppingTaskRecord
}

func (s *MySQLStore) SeedDataset(ctx context.Context, seed SimulationSeed) error {
	if seed.DatasetID == "" || len(seed.Merchants) != 12 || len(seed.Products) != 24000 || len(seed.Inventory) != 96000 || len(seed.Buyers) == 0 {
		return fmt.Errorf("invalid simulation seed dimensions: merchants=%d products=%d inventory=%d buyers=%d", len(seed.Merchants), len(seed.Products), len(seed.Inventory), len(seed.Buyers))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&DatasetRunRecord{}).Where("tenant_id = ?", s.tenant).Count(&existing).Error; err != nil {
			return err
		}
		if existing != 0 {
			return ErrDatasetAlreadyExists
		}
		for i := range seed.Merchants {
			seed.Merchants[i].TenantID = s.tenant
			seed.Merchants[i].DataOrigin = syntheticOrigin
			seed.Merchants[i].CreatedAt = s.now()
		}
		for i := range seed.Products {
			seed.Products[i].TenantID = s.tenant
			seed.Products[i].DataOrigin = syntheticOrigin
			if seed.Products[i].CreatedAt.IsZero() {
				seed.Products[i].CreatedAt = s.now()
			}
			seed.Products[i].UpdatedAt = seed.Products[i].CreatedAt
		}
		for i := range seed.Inventory {
			seed.Inventory[i].TenantID = s.tenant
			seed.Inventory[i].DataOrigin = syntheticOrigin
			seed.Inventory[i].UpdatedAt = s.now()
		}
		for i := range seed.Buyers {
			seed.Buyers[i].TenantID = s.tenant
			seed.Buyers[i].DataOrigin = syntheticOrigin
			if seed.Buyers[i].CreatedAt.IsZero() {
				seed.Buyers[i].CreatedAt = s.now()
			}
		}
		for i := range seed.Tasks {
			seed.Tasks[i].TenantID = s.tenant
			seed.Tasks[i].DataOrigin = syntheticOrigin
			if seed.Tasks[i].CreatedAt.IsZero() {
				seed.Tasks[i].CreatedAt = s.now()
			}
		}
		for _, batch := range []struct {
			value any
			size  int
		}{
			{seed.Merchants, 100}, {seed.Products, 500}, {seed.Inventory, 1000},
			{seed.Buyers, 500}, {seed.Tasks, 500},
		} {
			if err := tx.CreateInBatches(batch.value, batch.size).Error; err != nil {
				return err
			}
		}
		run := DatasetRunRecord{
			DatasetID: seed.DatasetID, TenantID: s.tenant, Seed: seed.Seed,
			MerchantCount: len(seed.Merchants), ProductCount: len(seed.Products),
			InventoryCount: len(seed.Inventory), BuyerCount: len(seed.Buyers),
			TaskCount: len(seed.Tasks), SourceManifest: seed.Manifest, SeededAt: s.now(),
			DataOrigin: syntheticOrigin,
		}
		return tx.Create(&run).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: false})
}

type Candidate struct {
	Product            Product      `json:"product"`
	Inventory          OverlayEntry `json:"inventory"`
	MatchType          string       `json:"match_type"`
	SubstitutionNotice string       `json:"substitution_notice,omitempty"`
}

type Taxonomy struct {
	Categories              []string                       `json:"categories"`
	Items                   []string                       `json:"items"`
	Formats                 []string                       `json:"formats"`
	Styles                  []string                       `json:"styles"`
	Regions                 []string                       `json:"regions"`
	ItemCategories          map[string][]string            `json:"item_categories,omitempty"`
	CategoryAttributeValues map[string]map[string][]string `json:"category_attribute_values,omitempty"`
	FormatAliases           map[string][]string            `json:"format_aliases,omitempty"`
	StyleAliases            map[string][]string            `json:"style_aliases,omitempty"`
}

func (s *MySQLStore) ReadTaxonomy(ctx context.Context) (Taxonomy, error) {
	var taxonomy Taxonomy
	for _, field := range []struct {
		column string
		dest   *[]string
	}{
		{"category", &taxonomy.Categories}, {"item", &taxonomy.Items},
		{"format", &taxonomy.Formats}, {"style", &taxonomy.Styles},
	} {
		if err := s.db.WithContext(ctx).Model(&ProductRecord{}).Where("tenant_id = ?", s.tenant).
			Distinct(field.column).Order(field.column).Pluck(field.column, field.dest).Error; err != nil {
			return Taxonomy{}, err
		}
	}
	var itemCategoryRows []struct {
		Item     string
		Category string
	}
	if err := s.db.WithContext(ctx).Model(&ProductRecord{}).Where("tenant_id = ?", s.tenant).
		Select("item, category").Distinct().
		Order("item").Order("category").Scan(&itemCategoryRows).Error; err != nil {
		return Taxonomy{}, err
	}
	taxonomy.ItemCategories = make(map[string][]string)
	for _, row := range itemCategoryRows {
		taxonomy.ItemCategories[row.Item] = append(taxonomy.ItemCategories[row.Item], row.Category)
	}
	var attributeRows []struct {
		Category       string
		Status         string
		AttributesJSON []byte
	}
	if err := s.db.WithContext(ctx).Model(&ProductRecord{}).Where("tenant_id = ? AND status = ?", s.tenant, "active").
		Select("category, status, attributes_json").Scan(&attributeRows).Error; err != nil {
		return Taxonomy{}, err
	}
	products := make([]Product, 0, len(attributeRows))
	for _, row := range attributeRows {
		var attributes map[string]any
		if err := json.Unmarshal(row.AttributesJSON, &attributes); err != nil {
			return Taxonomy{}, fmt.Errorf("decode active product attribute taxonomy: %w", err)
		}
		products = append(products, Product{Category: row.Category, Status: row.Status, Attributes: attributes})
	}
	taxonomy.CategoryAttributeValues = CategoryAttributeValues(products)
	taxonomy.FormatAliases, taxonomy.StyleAliases = IntentAliasHints()
	taxonomy.Regions = append([]string(nil), Regions...)
	sort.Strings(taxonomy.Regions)
	return taxonomy, nil
}

func (s *MySQLStore) ChangeInventory(ctx context.Context, taskID, merchantID, skuID, region, reason, idempotencyKey string, priceMinor *int64, onHand *int) error {
	if merchantID == "" || skuID == "" || !containsRegion(region) || strings.TrimSpace(reason) == "" || len(reason) > 120 || idempotencyKey == "" || (priceMinor == nil && onHand == nil) {
		return ErrInvalidSimulationData
	}
	priceKey, stockKey := "", ""
	if priceMinor != nil {
		priceKey = strconv.FormatInt(*priceMinor, 10)
	}
	if onHand != nil {
		stockKey = strconv.Itoa(*onHand)
	}
	requestHash := hashValue(taskID, merchantID, skuID, region, reason, priceKey, stockKey)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing InventoryAdjustmentRecord
		if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&existing).Error; err == nil {
			if existing.RequestHash != requestHash {
				return ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var row InventoryRecord
		key := map[string]any{"tenant_id": s.tenant, "merchant_id": merchantID, "sku_id": skuID, "region": region}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(key).First(&row).Error; err != nil {
			return ErrNotFound
		}
		previousPrice, previousOnHand := row.PriceMinor, row.OnHand
		changed := false
		if priceMinor != nil {
			if *priceMinor <= 0 {
				return ErrInvalidSimulationData
			}
			row.PriceMinor = *priceMinor
			changed = true
		}
		if onHand != nil {
			if *onHand < row.Reserved || *onHand < 0 {
				return ErrInvalidSimulationData
			}
			row.OnHand = *onHand
			changed = true
		}
		if !changed {
			return nil
		}
		row.Revision++
		row.UpdatedAt = s.now()
		if err := tx.Model(&InventoryRecord{}).Where(key).Updates(map[string]any{
			"price_minor": row.PriceMinor, "on_hand": row.OnHand, "revision": row.Revision, "updated_at": row.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		adjustment := InventoryAdjustmentRecord{AdjustmentID: StableID("inventory-adjustment", s.tenant, idempotencyKey),
			TenantID: s.tenant, IdempotencyKey: idempotencyKey, RequestHash: requestHash, TaskID: taskID,
			MerchantID: merchantID, SKUID: skuID, Region: region, Reason: reason,
			PreviousOnHand: previousOnHand, NewOnHand: row.OnHand, PreviousPrice: previousPrice,
			NewPrice: row.PriceMinor, Revision: row.Revision, CreatedAt: row.UpdatedAt, DataOrigin: syntheticOrigin}
		if err := tx.Create(&adjustment).Error; err != nil {
			return err
		}
		// Keep the indexed audit action bounded; the full operator-supplied
		// reason is already preserved on the immutable adjustment record.
		return auditTx(tx, s.tenant, taskID, adjustment.AdjustmentID, "INVENTORY_REVISED", row.UpdatedAt)
	})
}

func (s *MySQLStore) UpdateDatasetRunSummary(ctx context.Context, datasetID string) error {
	if datasetID == "" {
		return ErrInvalidSimulationData
	}
	counts, err := s.Counts(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&DatasetRunRecord{}).Where("tenant_id = ? AND dataset_id = ?", s.tenant, datasetID).
		Updates(map[string]any{"order_count": counts["orders"], "payment_count": counts["simulated_payments"]}).Error
}

func (s *MySQLStore) GetDatasetRun(ctx context.Context, datasetID string) (DatasetRunRecord, error) {
	var row DatasetRunRecord
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND dataset_id = ?", s.tenant, datasetID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DatasetRunRecord{}, ErrNotFound
	}
	return row, err
}

func (s *MySQLStore) FindCandidates(ctx context.Context, intent Intent, limit int) ([]Candidate, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var productRows []struct {
		SKUID       string `gorm:"column:sku_id"`
		MerchantID  string
		Category    string
		Title       string
		Description string
		ImageURL    string
		Price       int64
		Currency    string
		TagsJSON    []byte
		Attributes  []byte
		Item        string
		Format      string
		Style       string
	}
	if err := s.db.WithContext(ctx).Table("sim_products").
		Select("sku_id, merchant_id, category, title, description, image_url, base_price AS price, currency, tags_json, attributes_json AS attributes, item, format, style").
		Where("tenant_id = ? AND status = 'active' AND currency = ? AND category = ? AND format = ?",
			s.tenant, "USDC", intent.Category, intent.RequiredFormat).Scan(&productRows).Error; err != nil {
		return nil, err
	}
	products := make([]Product, 0, len(productRows))
	for _, row := range productRows {
		var tags []string
		if len(row.TagsJSON) > 0 {
			if err := json.Unmarshal(row.TagsJSON, &tags); err != nil {
				return nil, fmt.Errorf("decode product tags for %s/%s: %w", row.MerchantID, row.SKUID, err)
			}
		}
		var attributes map[string]any
		if len(row.Attributes) > 0 {
			if err := json.Unmarshal(row.Attributes, &attributes); err != nil {
				return nil, fmt.Errorf("decode product attributes for %s/%s: %w", row.MerchantID, row.SKUID, err)
			}
		}
		group, _ := attributes["substitution_group"].(string)
		products = append(products, Product{ID: StableID("product", row.MerchantID, row.SKUID), SKUID: row.SKUID, MerchantID: row.MerchantID,
			Category: row.Category, Title: row.Title, Description: row.Description, ImageURL: row.ImageURL,
			Price: FormatMinorUnits(row.Price), PriceMinor: row.Price, Currency: row.Currency, Tags: tags,
			Attributes: attributes, Item: row.Item, Format: row.Format, Style: row.Style, Status: "active", SubstitutionGroup: group})
	}
	discovery := rankDiscoveryProducts(products, intent, DefaultDiscoveryCandidateWindow)
	if len(discovery) == 0 {
		return []Candidate{}, nil
	}
	productByKey := make(map[string]Product, len(discovery))
	discoveryOrder := make(map[string]int, len(discovery))
	skuSet := make(map[string]bool, len(discovery))
	for index, product := range discovery {
		key := catalogProductKey(product.MerchantID, product.SKUID)
		productByKey[key] = product
		discoveryOrder[key] = index
		skuSet[product.SKUID] = true
	}
	skuIDs := make([]string, 0, len(skuSet))
	for skuID := range skuSet {
		skuIDs = append(skuIDs, skuID)
	}
	sort.Strings(skuIDs)
	var inventoryRows []struct {
		SKUID      string `gorm:"column:sku_id"`
		MerchantID string `gorm:"column:merchant_id"`
		Region     string `gorm:"column:region"`
		OnHand     int    `gorm:"column:on_hand"`
		Reserved   int    `gorm:"column:reserved"`
		Eligible   bool   `gorm:"column:eligible"`
		PriceMinor int64  `gorm:"column:price_minor"`
		Revision   int64  `gorm:"column:revision"`
	}
	if err := s.db.WithContext(ctx).Table("sim_inventory").
		Select("sku_id, merchant_id, region, on_hand, reserved, eligible, price_minor, revision").
		Where("tenant_id = ? AND region = ? AND sku_id IN ?", s.tenant, intent.Region, skuIDs).
		Scan(&inventoryRows).Error; err != nil {
		return nil, err
	}
	result := make([]Candidate, 0, len(discovery))
	for _, row := range inventoryRows {
		key := catalogProductKey(row.MerchantID, row.SKUID)
		product, ok := productByKey[key]
		if !ok || !row.Eligible || row.OnHand-row.Reserved < 1 || row.PriceMinor > intent.MaxPriceMinor {
			continue
		}
		matchType := candidateMatchType(product, intent)
		if matchType == "" {
			continue
		}
		candidate := Candidate{Product: product,
			Inventory: OverlayEntry{MerchantID: row.MerchantID, SKUID: row.SKUID, Region: row.Region,
				OnHand: row.OnHand, Eligible: row.Eligible, PriceMinor: row.PriceMinor, Revision: row.Revision},
			MatchType: matchType}
		if matchType == MatchTypeSubstitute {
			candidate.SubstitutionNotice = substitutionNotice(intent.Item, product.Item)
		}
		result = append(result, candidate)
	}
	sortCandidatesByDiscoveryOrder(result, discoveryOrder)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// FindCandidatesFromMerchantCatalog uses the Merchant HTTP snapshot as the
// product source of truth and the isolated MySQL tenant only for inventory,
// regional eligibility, and synthetic product enrichment.
type merchantInventoryKey struct {
	merchantID string
	skuID      string
}

func merchantInventoryPairPredicate(keys []merchantInventoryKey) (string, []any) {
	if len(keys) == 0 {
		return "", nil
	}
	var tuples strings.Builder
	tuples.Grow(len(keys) * len("(?,?)"))
	args := make([]any, 0, len(keys)*2)
	for index, key := range keys {
		if index > 0 {
			tuples.WriteByte(',')
		}
		tuples.WriteString("(?,?)")
		args = append(args, key.merchantID, key.skuID)
	}
	return "(i.merchant_id, i.sku_id) IN (" + tuples.String() + ")", args
}

func (s *MySQLStore) FindCandidatesFromMerchantCatalog(ctx context.Context, catalog []Product, intent Intent, limit int) ([]Candidate, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	productsByMerchant := make(map[string]map[string]Product)
	discoveryOrder := make(map[string]int, len(catalog))
	// Search first over merchant-owned listing facts. Hard attributes and live
	// inventory/price are deliberately deferred until the bounded discovery
	// window has been selected, so a miss at this boundary is measurable.
	discovery := rankDiscoveryProducts(catalog, intent, DefaultDiscoveryCandidateWindow)
	for index, product := range discovery {
		discoveryOrder[catalogProductKey(product.MerchantID, product.SKUID)] = index
		if productsByMerchant[product.MerchantID] == nil {
			productsByMerchant[product.MerchantID] = make(map[string]Product)
		}
		productsByMerchant[product.MerchantID][product.SKUID] = product
	}
	if len(productsByMerchant) == 0 {
		return []Candidate{}, nil
	}

	type inventoryCandidateRow struct {
		SKUID      string `gorm:"column:sku_id"`
		MerchantID string `gorm:"column:merchant_id"`
		Region     string `gorm:"column:region"`
		OnHand     int    `gorm:"column:on_hand"`
		Reserved   int    `gorm:"column:reserved"`
		Eligible   bool   `gorm:"column:eligible"`
		PriceMinor int64  `gorm:"column:price_minor"`
		Revision   int64  `gorm:"column:revision"`
		Attributes []byte `gorm:"column:attributes"`
	}
	merchantIDs := make([]string, 0, len(productsByMerchant))
	for merchantID := range productsByMerchant {
		merchantIDs = append(merchantIDs, merchantID)
	}
	sort.Strings(merchantIDs)
	productByKey := make(map[string]Product, len(discovery))
	inventoryKeys := make([]merchantInventoryKey, 0, len(discovery))
	for _, merchantID := range merchantIDs {
		products := productsByMerchant[merchantID]
		skuIDs := make([]string, 0, len(products))
		for skuID := range products {
			skuIDs = append(skuIDs, skuID)
		}
		sort.Strings(skuIDs)
		for _, skuID := range skuIDs {
			productByKey[catalogProductKey(merchantID, skuID)] = products[skuID]
			inventoryKeys = append(inventoryKeys, merchantInventoryKey{merchantID: merchantID, skuID: skuID})
		}
	}
	pairPredicate, pairArgs := merchantInventoryPairPredicate(inventoryKeys)
	if pairPredicate == "" {
		return []Candidate{}, nil
	}
	queryArgs := make([]any, 0, len(pairArgs)+5)
	queryArgs = append(queryArgs, s.tenant, s.tenant)
	queryArgs = append(queryArgs, pairArgs...)
	queryArgs = append(queryArgs, intent.Region, true, intent.MaxPriceMinor)
	var rows []inventoryCandidateRow
	if err := s.db.WithContext(ctx).Table("sim_inventory AS i").
		Select("i.sku_id, i.merchant_id, i.region, i.on_hand, i.reserved, i.eligible, i.price_minor, i.revision, p.attributes_json AS attributes").
		Joins("JOIN sim_products AS p ON p.tenant_id = i.tenant_id AND p.merchant_id = i.merchant_id AND p.sku_id = i.sku_id").
		Where("i.tenant_id = ? AND p.tenant_id = ? AND p.status = 'active' AND "+pairPredicate+" AND i.region = ? AND i.eligible = ? AND i.on_hand > i.reserved AND i.price_minor <= ?", queryArgs...).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]Candidate, 0)
	for _, row := range rows {
		key := catalogProductKey(row.MerchantID, row.SKUID)
		product, ok := productByKey[key]
		if !ok {
			return nil, fmt.Errorf("Merchant catalog inventory returned an unknown SKU %s/%s", row.MerchantID, row.SKUID)
		}
		var attributes map[string]any
		if len(row.Attributes) > 0 {
			if err := json.Unmarshal(row.Attributes, &attributes); err != nil {
				return nil, fmt.Errorf("decode synthetic attributes for %s/%s: %w", row.MerchantID, row.SKUID, err)
			}
		}
		product.Tags = append([]string(nil), product.Tags...)
		product.Attributes = attributes
		matchType := candidateMatchType(product, intent)
		if matchType == "" {
			continue
		}
		candidate := Candidate{Product: product, MatchType: matchType,
			Inventory: OverlayEntry{MerchantID: row.MerchantID, SKUID: row.SKUID, Region: row.Region,
				OnHand: row.OnHand, Eligible: row.Eligible, PriceMinor: row.PriceMinor, Revision: row.Revision},
		}
		if matchType == MatchTypeSubstitute {
			candidate.SubstitutionNotice = substitutionNotice(intent.Item, product.Item)
		}
		result = append(result, candidate)
	}
	sortCandidatesByDiscoveryOrder(result, discoveryOrder)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// sortCandidatesByDiscoveryOrder restores the product relevance order after
// the unordered MySQL inventory overlay lookup. Price and SKU are not allowed
// to silently replace the ranker's BM25/hard-attribute ordering.
func sortCandidatesByDiscoveryOrder(candidates []Candidate, discoveryOrder map[string]int) {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftKey := catalogProductKey(candidates[i].Product.MerchantID, candidates[i].Product.SKUID)
		rightKey := catalogProductKey(candidates[j].Product.MerchantID, candidates[j].Product.SKUID)
		leftRank, leftFound := discoveryOrder[leftKey]
		rightRank, rightFound := discoveryOrder[rightKey]
		if leftFound != rightFound {
			return leftFound
		}
		if leftFound && leftRank != rightRank {
			return leftRank < rightRank
		}
		if candidates[i].MatchType != candidates[j].MatchType {
			return matchTypePriority(candidates[i].MatchType) > matchTypePriority(candidates[j].MatchType)
		}
		if candidates[i].Inventory.PriceMinor != candidates[j].Inventory.PriceMinor {
			return candidates[i].Inventory.PriceMinor < candidates[j].Inventory.PriceMinor
		}
		if candidates[i].Product.MerchantID != candidates[j].Product.MerchantID {
			return candidates[i].Product.MerchantID < candidates[j].Product.MerchantID
		}
		return candidates[i].Product.SKUID < candidates[j].Product.SKUID
	})
}

func catalogProductMatchesIntent(product Product, intent Intent) bool {
	return product.Status == "active" && product.Currency == "USDC" &&
		candidateMatchType(product, intent) != ""
}

// ValidateMerchantCatalogSnapshot rejects a mixed catalog where candidate
// metadata comes from one Merchant snapshot but quotes would read a different
// product seed from isolated MySQL.
func (s *MySQLStore) ValidateMerchantCatalogSnapshot(ctx context.Context, catalog ImportedCatalog) error {
	if len(catalog.Merchants) == 0 || len(catalog.Products) == 0 {
		return errors.New("Merchant HTTP catalog snapshot is empty")
	}
	var merchants []MerchantRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", s.tenant).Find(&merchants).Error; err != nil {
		return fmt.Errorf("read isolated Merchant seed: %w", err)
	}
	var products []ProductRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", s.tenant).Find(&products).Error; err != nil {
		return fmt.Errorf("read isolated product seed: %w", err)
	}
	return validateMerchantCatalogSnapshotRecords(catalog.Merchants, catalog.Products, merchants, products)
}

func validateMerchantCatalogSnapshotRecords(sourceMerchants []MerchantRecord, sourceProducts []Product,
	seedMerchants []MerchantRecord, seedProducts []ProductRecord) error {
	if len(sourceMerchants) != len(seedMerchants) {
		return fmt.Errorf("Merchant count mismatch: HTTP snapshot=%d isolated seed=%d", len(sourceMerchants), len(seedMerchants))
	}
	if len(sourceProducts) != len(seedProducts) {
		return fmt.Errorf("product count mismatch: HTTP snapshot=%d isolated seed=%d", len(sourceProducts), len(seedProducts))
	}
	sourceMerchantIDs := make(map[string]struct{}, len(sourceMerchants))
	for _, merchant := range sourceMerchants {
		if _, exists := sourceMerchantIDs[merchant.MerchantID]; exists {
			return fmt.Errorf("duplicate Merchant %s in HTTP snapshot", merchant.MerchantID)
		}
		sourceMerchantIDs[merchant.MerchantID] = struct{}{}
	}
	seedMerchantByID := make(map[string]MerchantRecord, len(seedMerchants))
	for _, merchant := range seedMerchants {
		if _, exists := seedMerchantByID[merchant.MerchantID]; exists {
			return fmt.Errorf("duplicate Merchant %s in isolated seed", merchant.MerchantID)
		}
		seedMerchantByID[merchant.MerchantID] = merchant
	}
	for _, source := range sourceMerchants {
		seed, exists := seedMerchantByID[source.MerchantID]
		if !exists || seed.Name != source.Name || seed.Category != source.Category || seed.DID != source.DID {
			return fmt.Errorf("Merchant HTTP profile %s differs from isolated seed", source.MerchantID)
		}
	}
	seedProductByKey := make(map[string]ProductRecord, len(seedProducts))
	for _, product := range seedProducts {
		key := catalogProductKey(product.MerchantID, product.SKUID)
		if _, exists := seedProductByKey[key]; exists {
			return fmt.Errorf("duplicate product %s/%s in isolated seed", product.MerchantID, product.SKUID)
		}
		seedProductByKey[key] = product
	}
	sourceProductKeys := make(map[string]struct{}, len(sourceProducts))
	for _, source := range sourceProducts {
		key := catalogProductKey(source.MerchantID, source.SKUID)
		if _, exists := sourceProductKeys[key]; exists {
			return fmt.Errorf("duplicate product %s/%s in HTTP snapshot", source.MerchantID, source.SKUID)
		}
		sourceProductKeys[key] = struct{}{}
		seed, exists := seedProductByKey[key]
		if !exists {
			return fmt.Errorf("Merchant HTTP product %s/%s is missing from isolated seed", source.MerchantID, source.SKUID)
		}
		var seedTags []string
		if err := json.Unmarshal(seed.TagsJSON, &seedTags); err != nil {
			return fmt.Errorf("decode isolated product tags for %s/%s: %w", source.MerchantID, source.SKUID, err)
		}
		if seed.Category != source.Category || seed.Item != source.Item || seed.Format != source.Format ||
			seed.Style != source.Style || seed.Title != source.Title || seed.Description != source.Description ||
			seed.ImageURL != source.ImageURL || seed.BasePrice != source.PriceMinor || seed.Currency != source.Currency ||
			seed.Status != source.Status || !equalStringSlices(seedTags, source.Tags) {
			return fmt.Errorf("Merchant HTTP product %s/%s differs from isolated seed", source.MerchantID, source.SKUID)
		}
	}
	return nil
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type PersistentQuoteRequest struct {
	TaskID         string
	BuyerID        string
	MerchantID     string
	SKUID          string
	Region         string
	MaxMinor       int64
	Currency       string
	IdempotencyKey string
}

func (s *MySQLStore) IssueQuote(ctx context.Context, request PersistentQuoteRequest) (Quote, error) {
	if request.TaskID == "" || request.BuyerID == "" || request.MerchantID == "" || request.SKUID == "" || request.Region == "" ||
		request.MaxMinor <= 0 || request.Currency != "USDC" || request.IdempotencyKey == "" {
		return Quote{}, ErrInvalidSimulationData
	}
	requestHash := hashValue(request.TaskID, request.BuyerID, request.MerchantID, request.SKUID, request.Region, strconv.FormatInt(request.MaxMinor, 10), request.Currency)
	var result Quote
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old QuoteRecord
		if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&old).Error; err == nil {
			if old.RequestHash != requestHash {
				return ErrIdempotencyConflict
			}
			result = quoteFromRecord(old)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var task ShoppingTaskRecord
		if err := tx.Where("task_id = ? AND tenant_id = ? AND buyer_id = ?", request.TaskID, s.tenant, request.BuyerID).First(&task).Error; err != nil {
			return ErrNotFound
		}
		var intent Intent
		if err := json.Unmarshal(task.IntentJSON, &intent); err != nil || intent.ClarificationRequired || intent.Item == "" || intent.Category == "" ||
			intent.RequiredFormat == "" || intent.MaxPriceMinor <= 0 {
			return ErrCandidateNotAllowed
		}
		if request.Region != intent.Region {
			return ErrCandidateNotAllowed
		}
		if request.MaxMinor > intent.MaxPriceMinor {
			return ErrBudgetExceeded
		}
		var buyer BuyerRecord
		if err := tx.Where("tenant_id = ? AND buyer_id = ?", s.tenant, request.BuyerID).First(&buyer).Error; err != nil {
			return ErrNotFound
		}
		var inventory InventoryRecord
		key := map[string]any{"tenant_id": s.tenant, "merchant_id": request.MerchantID, "sku_id": request.SKUID, "region": request.Region}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(key).First(&inventory).Error; err != nil {
			return ErrNotFound
		}
		if !inventory.Eligible || inventory.OnHand-inventory.Reserved < 1 {
			return ErrUnavailable
		}
		if inventory.Currency != request.Currency || inventory.PriceMinor > request.MaxMinor || inventory.PriceMinor > buyer.BudgetMinor {
			return ErrBudgetExceeded
		}
		var product ProductRecord
		if err := tx.Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND status = ?", s.tenant, request.MerchantID, request.SKUID, "active").First(&product).Error; err != nil {
			return ErrNotFound
		}
		var attributes map[string]any
		if err := json.Unmarshal(product.AttributesJSON, &attributes); err != nil {
			return ErrCandidateNotAllowed
		}
		group, _ := attributes["substitution_group"].(string)
		selected := Product{SKUID: product.SKUID, Category: product.Category, Item: product.Item, Format: product.Format,
			Status: product.Status, Currency: product.Currency, Title: product.Title, Attributes: attributes, SubstitutionGroup: group}
		matchType := candidateMatchType(selected, intent)
		if matchType == "" {
			return ErrCandidateNotAllowed
		}
		notice := ""
		if matchType == MatchTypeSubstitute {
			notice = substitutionNotice(intent.Item, selected.Item)
		}
		var version int64
		if err := tx.Model(&QuoteRecord{}).Select("COALESCE(MAX(version), 0)").Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?", s.tenant, request.MerchantID, request.SKUID, request.Region).Scan(&version).Error; err != nil {
			return err
		}
		version++
		now := s.now()
		expires := now.Add(15 * time.Minute)
		quoteID := StableID("quote", s.tenant, request.TaskID, request.MerchantID, request.SKUID, request.Region, strconv.FormatInt(version, 10))
		snapshotHash := hashValue(quoteID, request.TaskID, request.BuyerID, request.MerchantID, request.SKUID,
			intent.Item, selected.Item, selected.Title, matchType, notice,
			request.Region, strconv.FormatInt(version, 10), strconv.FormatInt(inventory.PriceMinor, 10),
			request.Currency, strconv.FormatInt(inventory.Revision, 10), expires.UTC().Format(time.RFC3339Nano))
		row := QuoteRecord{QuoteID: quoteID, TenantID: s.tenant, TaskID: request.TaskID, BuyerID: request.BuyerID,
			MerchantID: request.MerchantID, SKUID: request.SKUID, RequestedItem: intent.Item,
			ProductItem: selected.Item, ProductTitle: selected.Title, MatchType: matchType, SubstitutionNotice: notice,
			Region: request.Region, Version: version,
			AmountMinor: inventory.PriceMinor, Currency: request.Currency, StockRevision: inventory.Revision,
			SnapshotHash: snapshotHash, IdempotencyKey: request.IdempotencyKey, RequestHash: requestHash,
			ExpiresAt: expires, CreatedAt: now, DataOrigin: syntheticOrigin}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := auditTx(tx, s.tenant, request.TaskID, quoteID, "QUOTE_ISSUED", now); err != nil {
			return err
		}
		if err := updateTaskStatusTx(tx, s.tenant, request.TaskID, TaskStatusQuoted); err != nil {
			return err
		}
		result = quoteFromRecord(row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var old QuoteRecord
		if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&old).Error; lookupErr == nil {
			if old.RequestHash == requestHash {
				return quoteFromRecord(old), nil
			}
			return Quote{}, ErrIdempotencyConflict
		}
	}
	return result, err
}

type PersistentConfirmRequest struct {
	BuyerID        string
	QuoteID        string
	Accepted       bool
	AmountMinor    int64
	IdempotencyKey string
	ConfirmationID string
}

func (s *MySQLStore) ConfirmBuyer(ctx context.Context, request PersistentConfirmRequest) (Confirmation, error) {
	if request.BuyerID == "" || request.QuoteID == "" || request.IdempotencyKey == "" || len(request.ConfirmationID) > 96 {
		return Confirmation{}, ErrInvalidSimulationData
	}
	requestHash := hashValue(request.BuyerID, request.QuoteID, strconv.FormatBool(request.Accepted), strconv.FormatInt(request.AmountMinor, 10))
	var result Confirmation
	var rejected error
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var priorAttempt BuyerConfirmationAttemptRecord
		if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&priorAttempt).Error; err == nil {
			if priorAttempt.RequestHash != requestHash {
				return ErrIdempotencyConflict
			}
			if priorAttempt.Status == "STALE_QUOTE" {
				rejected = ErrStaleQuote
				return nil
			}
			var confirmation BuyerConfirmationRecord
			if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&confirmation).Error; err != nil {
				return err
			}
			result = confirmationFromRecord(confirmation)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var old BuyerConfirmationRecord
		if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&old).Error; err == nil {
			if old.RequestHash != requestHash {
				return ErrIdempotencyConflict
			}
			result = confirmationFromRecord(old)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var quote QuoteRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND quote_id = ? AND buyer_id = ?", s.tenant, request.QuoteID, request.BuyerID).First(&quote).Error; err != nil {
			return ErrNotFound
		}
		now := s.now()
		if !quote.ExpiresAt.After(now) {
			attempt := BuyerConfirmationAttemptRecord{AttemptID: StableID("confirmation-attempt", s.tenant, request.IdempotencyKey),
				TenantID: s.tenant, IdempotencyKey: request.IdempotencyKey, RequestHash: requestHash,
				BuyerID: request.BuyerID, QuoteID: quote.QuoteID, Status: "STALE_QUOTE", CreatedAt: now, DataOrigin: syntheticOrigin}
			if err := tx.Create(&attempt).Error; err != nil {
				return err
			}
			if err := auditTx(tx, s.tenant, quote.TaskID, quote.QuoteID, "BUYER_CONFIRMATION_BLOCKED_EXPIRED_QUOTE", now); err != nil {
				return err
			}
			if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, TaskStatusReconfirmationRequired); err != nil {
				return err
			}
			rejected = ErrStaleQuote
			return nil
		}
		if request.AmountMinor != quote.AmountMinor {
			return ErrQuoteAmountMismatch
		}
		if request.Accepted {
			var inventory InventoryRecord
			key := map[string]any{"tenant_id": s.tenant, "merchant_id": quote.MerchantID, "sku_id": quote.SKUID, "region": quote.Region}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(key).First(&inventory).Error; err != nil {
				return ErrNotFound
			}
			if inventory.Revision != quote.StockRevision || !inventory.Eligible || inventory.OnHand-inventory.Reserved < 1 || inventory.PriceMinor != quote.AmountMinor {
				attempt := BuyerConfirmationAttemptRecord{AttemptID: StableID("confirmation-attempt", s.tenant, request.IdempotencyKey),
					TenantID: s.tenant, IdempotencyKey: request.IdempotencyKey, RequestHash: requestHash,
					BuyerID: request.BuyerID, QuoteID: quote.QuoteID, Status: "STALE_QUOTE", CreatedAt: now, DataOrigin: syntheticOrigin}
				if err := tx.Create(&attempt).Error; err != nil {
					return err
				}
				if err := auditTx(tx, s.tenant, quote.TaskID, quote.QuoteID, "BUYER_CONFIRMATION_BLOCKED_STALE_QUOTE", now); err != nil {
					return err
				}
				if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, TaskStatusReconfirmationRequired); err != nil {
					return err
				}
				rejected = ErrStaleQuote
				return nil
			}
		}
		confirmationID := request.ConfirmationID
		if confirmationID == "" {
			confirmationID = StableID("confirmation", s.tenant, request.IdempotencyKey)
		}
		row := BuyerConfirmationRecord{ConfirmationID: confirmationID, TenantID: s.tenant, BuyerID: request.BuyerID,
			QuoteID: quote.QuoteID, QuoteVersion: quote.Version, QuoteHash: quote.SnapshotHash,
			AmountMinor: request.AmountMinor, Accepted: request.Accepted, IdempotencyKey: request.IdempotencyKey,
			RequestHash: requestHash, ConfirmedAt: now, DataOrigin: syntheticOrigin}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		attempt := BuyerConfirmationAttemptRecord{AttemptID: StableID("confirmation-attempt", s.tenant, request.IdempotencyKey),
			TenantID: s.tenant, IdempotencyKey: request.IdempotencyKey, RequestHash: requestHash,
			BuyerID: request.BuyerID, QuoteID: quote.QuoteID, Status: "RECORDED", CreatedAt: now, DataOrigin: syntheticOrigin}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		action := "BUYER_REJECTED"
		if request.Accepted {
			action = "BUYER_CONFIRMED"
		}
		if err := auditTx(tx, s.tenant, quote.TaskID, confirmationID, action, now); err != nil {
			return err
		}
		status := TaskStatusBuyerRejected
		if request.Accepted {
			status = TaskStatusBuyerConfirmed
		}
		if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, status); err != nil {
			return err
		}
		result = confirmationFromRecord(row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var attempt BuyerConfirmationAttemptRecord
		if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&attempt).Error; lookupErr == nil {
			if attempt.RequestHash != requestHash {
				return Confirmation{}, ErrIdempotencyConflict
			}
			if attempt.Status == "STALE_QUOTE" {
				return Confirmation{}, ErrStaleQuote
			}
		}
		var old BuyerConfirmationRecord
		if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, request.IdempotencyKey).First(&old).Error; lookupErr == nil {
			if old.RequestHash == requestHash {
				return confirmationFromRecord(old), nil
			}
			return Confirmation{}, ErrIdempotencyConflict
		}
	}
	if err == nil && rejected != nil {
		return Confirmation{}, rejected
	}
	return result, err
}

func (s *MySQLStore) Checkout(ctx context.Context, tenant, confirmationID, idempotencyKey string) (CheckoutResult, error) {
	if tenant != s.tenant || confirmationID == "" || idempotencyKey == "" {
		return CheckoutResult{}, ErrInvalidSimulationData
	}
	var result CheckoutResult
	var staleBlocked bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		replay, found, err := checkoutReplayInTx(tx, s.tenant, confirmationID, idempotencyKey, false)
		if err != nil {
			return err
		}
		if found {
			result = replay
			return nil
		}
		var confirmation BuyerConfirmationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND confirmation_id = ?", s.tenant, confirmationID).First(&confirmation).Error; err != nil {
			return ErrNotFound
		}
		if !confirmation.Accepted {
			return ErrConfirmationRequired
		}
		// Serialize same-confirmation checkouts, then perform a locking/current
		// read of the idempotency key. A concurrent request may have missed the
		// initial lookup before it waited on this row; it must replay that order
		// before observing the inventory revision changed by the first commit.
		replay, found, err = checkoutReplayInTx(tx, s.tenant, confirmationID, idempotencyKey, true)
		if err != nil {
			return err
		}
		if found {
			result = replay
			return nil
		}
		var quote QuoteRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND quote_id = ? AND buyer_id = ?", s.tenant, confirmation.QuoteID, confirmation.BuyerID).First(&quote).Error; err != nil {
			return ErrStaleQuote
		}
		now := s.now()
		if quote.Version != confirmation.QuoteVersion || quote.SnapshotHash != confirmation.QuoteHash ||
			!quote.ExpiresAt.After(now) || quote.AmountMinor != confirmation.AmountMinor {
			if err := auditTx(tx, s.tenant, quote.TaskID, quote.QuoteID, "CHECKOUT_BLOCKED_STALE_QUOTE", now); err != nil {
				return err
			}
			if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, TaskStatusReconfirmationRequired); err != nil {
				return err
			}
			staleBlocked = true
			return nil
		}
		var inventory InventoryRecord
		key := map[string]any{"tenant_id": s.tenant, "merchant_id": quote.MerchantID, "sku_id": quote.SKUID, "region": quote.Region}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(key).First(&inventory).Error; err != nil {
			return ErrNotFound
		}
		if inventory.Revision != quote.StockRevision || !inventory.Eligible || inventory.OnHand-inventory.Reserved < 1 || inventory.PriceMinor != quote.AmountMinor {
			if err := auditTx(tx, s.tenant, quote.TaskID, quote.QuoteID, "CHECKOUT_BLOCKED_STALE_QUOTE", now); err != nil {
				return err
			}
			if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, TaskStatusReconfirmationRequired); err != nil {
				return err
			}
			staleBlocked = true
			return nil
		}
		orderID := StableID("order", s.tenant, idempotencyKey)
		reservationID := StableID("reservation", s.tenant, orderID)
		paymentID := StableID("mock-payment", s.tenant, orderID)
		order := OrderRecord{OrderID: orderID, TenantID: s.tenant, TaskID: quote.TaskID, BuyerID: confirmation.BuyerID,
			QuoteID: quote.QuoteID, ConfirmationID: confirmation.ConfirmationID, IdempotencyKey: idempotencyKey,
			AmountMinor: quote.AmountMinor, Currency: quote.Currency, Status: "PAYMENT_PENDING", CreatedAt: now,
			CompletedAt: now, DataOrigin: syntheticOrigin}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		reservation := InventoryReservationRecord{ReservationID: reservationID, TenantID: s.tenant, OrderID: orderID,
			MerchantID: quote.MerchantID, SKUID: quote.SKUID, Region: quote.Region, Quantity: 1,
			Status: "RESERVED", CreatedAt: now, CompletedAt: now, DataOrigin: syntheticOrigin}
		inventory.Reserved++
		if err := tx.Model(&InventoryRecord{}).Where(key).Updates(map[string]any{"reserved": inventory.Reserved, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&reservation).Error; err != nil {
			return err
		}
		if err := auditTx(tx, s.tenant, quote.TaskID, orderID, "ORDER_RESERVED", now); err != nil {
			return err
		}

		paymentStatus := simulatedPaymentStatus(idempotencyKey)
		payment := MockPaymentRecord{PaymentID: paymentID, TenantID: s.tenant, OrderID: orderID,
			AmountMinor: quote.AmountMinor, Currency: quote.Currency, Provider: "simulation-only",
			Status: paymentStatus, IdempotencyKey: StableID("payment-idempotency", s.tenant, orderID),
			CreatedAt: now, DataOrigin: syntheticOrigin}
		if paymentStatus == PaymentSettled {
			payment.SettlementReference = "SIM-SETTLEMENT-" + strings.ToUpper(paymentID[:12])
			inventory.OnHand--
			order.Status = OrderPaid
			reservation.Status = "CONSUMED"
			if err := auditTx(tx, s.tenant, quote.TaskID, paymentID, "MOCK_PAYMENT_SETTLED", now); err != nil {
				return err
			}
		} else {
			order.Status = OrderPaymentError
			reservation.Status = "RELEASED"
			payment.FailureCode = "SIMULATED_DECLINE"
			if err := auditTx(tx, s.tenant, quote.TaskID, paymentID, "MOCK_PAYMENT_DECLINED", now); err != nil {
				return err
			}
		}
		inventory.Reserved--
		if paymentStatus == PaymentSettled {
			inventory.Revision++
		}
		inventory.UpdatedAt = now
		order.CompletedAt = now
		reservation.CompletedAt = now
		if err := tx.Model(&InventoryRecord{}).Where(key).Updates(map[string]any{
			"on_hand": inventory.OnHand, "reserved": inventory.Reserved, "revision": inventory.Revision, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := tx.Save(&reservation).Error; err != nil {
			return err
		}
		if err := tx.Create(&payment).Error; err != nil {
			return err
		}
		if err := auditTx(tx, s.tenant, quote.TaskID, orderID, order.Status, now); err != nil {
			return err
		}
		if err := updateTaskStatusTx(tx, s.tenant, quote.TaskID, order.Status); err != nil {
			return err
		}
		result = CheckoutResult{Order: Order{ID: order.OrderID, TenantID: order.TenantID, TaskID: order.TaskID,
			BuyerID: order.BuyerID, QuoteID: order.QuoteID, ConfirmationID: order.ConfirmationID,
			IdempotencyKey: order.IdempotencyKey, AmountMinor: order.AmountMinor, Currency: order.Currency,
			Status: order.Status, CreatedAt: order.CreatedAt, CompletedAt: order.CompletedAt},
			Payment: paymentFromRecord(payment)}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var old OrderRecord
		if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&old).Error; lookupErr == nil {
			if old.ConfirmationID != confirmationID {
				return CheckoutResult{}, ErrIdempotencyConflict
			}
			var payment MockPaymentRecord
			if lookupErr = s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, old.OrderID).First(&payment).Error; lookupErr == nil {
				return checkoutFromRecords(old, payment), nil
			}
		}
	}
	if err == nil && staleBlocked {
		return CheckoutResult{}, ErrStaleQuote
	}
	return result, err
}

func checkoutReplayInTx(tx *gorm.DB, tenant, confirmationID, idempotencyKey string, currentRead bool) (CheckoutResult, bool, error) {
	query := tx
	if currentRead {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var order OrderRecord
	if err := query.Where("tenant_id = ? AND idempotency_key = ?", tenant, idempotencyKey).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckoutResult{}, false, nil
		}
		return CheckoutResult{}, false, err
	}
	if order.ConfirmationID != confirmationID {
		return CheckoutResult{}, false, ErrIdempotencyConflict
	}
	var payment MockPaymentRecord
	if err := tx.Where("tenant_id = ? AND order_id = ?", tenant, order.OrderID).First(&payment).Error; err != nil {
		return CheckoutResult{}, false, err
	}
	return checkoutFromRecords(order, payment), true, nil
}

func simulatedPaymentStatus(idempotencyKey string) string {
	digest := sha256.Sum256([]byte("simulation-payment|" + idempotencyKey))
	if int(digest[0])%100 >= 95 {
		return PaymentDeclined
	}
	return PaymentSettled
}

func (s *MySQLStore) GetOrder(ctx context.Context, orderID string) (Order, MockPayment, error) {
	var order OrderRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&order).Error; err != nil {
		return Order{}, MockPayment{}, ErrNotFound
	}
	var payment MockPaymentRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&payment).Error; err != nil {
		return Order{}, MockPayment{}, ErrNotFound
	}
	result := checkoutFromRecords(order, payment)
	return result.Order, result.Payment, nil
}

func (s *MySQLStore) Counts(ctx context.Context) (map[string]int64, error) {
	models := []struct {
		name  string
		model any
	}{
		{"merchants", &MerchantRecord{}}, {"products", &ProductRecord{}}, {"inventory", &InventoryRecord{}},
		{"buyers", &BuyerRecord{}}, {"tasks", &ShoppingTaskRecord{}}, {"quotes", &QuoteRecord{}},
		{"confirmations", &BuyerConfirmationRecord{}}, {"confirmation_attempts", &BuyerConfirmationAttemptRecord{}},
		{"orders", &OrderRecord{}}, {"reservations", &InventoryReservationRecord{}}, {"inventory_adjustments", &InventoryAdjustmentRecord{}},
		{"simulated_payments", &MockPaymentRecord{}}, {"simulated_fulfillment_attempts", &MockFulfillmentAttemptRecord{}},
		{"audit_events", &SimulationAuditRecord{}}, {"intent_parse_traces", &IntentParseTraceRecord{}},
	}
	counts := make(map[string]int64, len(models))
	for _, entry := range models {
		var count int64
		if err := s.db.WithContext(ctx).Model(entry.model).Where("tenant_id = ?", s.tenant).Count(&count).Error; err != nil {
			return nil, err
		}
		counts[entry.name] = count
	}
	return counts, nil
}

func (s *MySQLStore) CreateTask(ctx context.Context, buyerID, prompt, idempotencyKey string, clarifiesTaskIDs ...string) (ShoppingTaskRecord, error) {
	return s.createTask(ctx, nil, buyerID, prompt, idempotencyKey, clarifiesTaskIDs...)
}

func (s *MySQLStore) createTaskForKnownBuyer(ctx context.Context, buyer BuyerRecord, prompt, idempotencyKey string,
	clarifiesTaskIDs ...string) (ShoppingTaskRecord, error) {
	if buyer.TenantID != s.tenant || buyer.BuyerID == "" {
		return ShoppingTaskRecord{}, ErrNotFound
	}
	return s.createTask(ctx, &buyer, buyer.BuyerID, prompt, idempotencyKey, clarifiesTaskIDs...)
}

func (s *MySQLStore) createTask(ctx context.Context, knownBuyer *BuyerRecord, buyerID, prompt, idempotencyKey string,
	clarifiesTaskIDs ...string) (ShoppingTaskRecord, error) {
	if buyerID == "" || strings.TrimSpace(prompt) == "" || idempotencyKey == "" || len(prompt) > 8000 || len(clarifiesTaskIDs) > 1 {
		return ShoppingTaskRecord{}, ErrInvalidSimulationData
	}
	clarifiesTaskID := ""
	if len(clarifiesTaskIDs) == 1 {
		clarifiesTaskID = strings.TrimSpace(clarifiesTaskIDs[0])
	}
	if knownBuyer == nil {
		var buyer BuyerRecord
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND buyer_id = ?", s.tenant, buyerID).First(&buyer).Error; err != nil {
			return ShoppingTaskRecord{}, ErrNotFound
		}
	} else if knownBuyer.TenantID != s.tenant || knownBuyer.BuyerID != buyerID {
		return ShoppingTaskRecord{}, ErrNotFound
	}
	requestHash := hashValue(buyerID, prompt, clarifiesTaskID)
	if clarifiesTaskID != "" {
		var existing ShoppingTaskRecord
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&existing).Error; err == nil {
			if existing.BuyerID == buyerID && hashValue(existing.BuyerID, existing.Prompt, existing.ClarifiesTaskID) == requestHash {
				return existing, nil
			}
			return ShoppingTaskRecord{}, ErrIdempotencyConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return ShoppingTaskRecord{}, err
		}
		var parent ShoppingTaskRecord
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND task_id = ?", s.tenant, clarifiesTaskID).First(&parent).Error; err != nil {
			return ShoppingTaskRecord{}, ErrNotFound
		}
		if parent.BuyerID != buyerID || parent.Status != TaskStatusClarificationRequired {
			return ShoppingTaskRecord{}, ErrClarificationNotOpen
		}
	}
	promptBytes, _ := json.Marshal(map[string]any{})
	row := ShoppingTaskRecord{TaskID: StableID("task", s.tenant, idempotencyKey), TenantID: s.tenant,
		BuyerID: buyerID, ClarifiesTaskID: clarifiesTaskID, IdempotencyKey: idempotencyKey, Prompt: prompt, IntentJSON: promptBytes,
		Provider: "pending", Model: "pending", ParseSource: "live", Status: TaskStatusIntentPending, CreatedAt: s.now(), DataOrigin: syntheticOrigin}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var concurrent ShoppingTaskRecord
			if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&concurrent).Error; lookupErr == nil {
				if concurrent.BuyerID == buyerID && hashValue(concurrent.BuyerID, concurrent.Prompt, concurrent.ClarifiesTaskID) == requestHash {
					return concurrent, nil
				}
				return ShoppingTaskRecord{}, ErrIdempotencyConflict
			}
		}
		return ShoppingTaskRecord{}, err
	}
	return row, nil
}

func (s *MySQLStore) SaveParsedIntent(ctx context.Context, taskID, provider, model string, intent Intent, latency time.Duration, parseErr error, usage ParseUsage) error {
	return s.saveParsedIntent(ctx, nil, taskID, provider, model, intent, latency, parseErr, usage)
}

func (s *MySQLStore) saveParsedIntentForKnownTask(ctx context.Context, task ShoppingTaskRecord, provider, model string,
	intent Intent, latency time.Duration, parseErr error, usage ParseUsage) error {
	if task.TenantID != s.tenant || task.TaskID == "" || task.Status != TaskStatusIntentPending {
		return ErrIntentAlreadyParsed
	}
	return s.saveParsedIntent(ctx, &task, task.TaskID, provider, model, intent, latency, parseErr, usage)
}

func (s *MySQLStore) saveParsedIntent(ctx context.Context, knownTask *ShoppingTaskRecord, taskID, provider, model string,
	intent Intent, latency time.Duration, parseErr error, usage ParseUsage) error {
	var output []byte
	if parseErr == nil {
		var err error
		output, err = json.Marshal(intent)
		if err != nil {
			return err
		}
	} else {
		output = []byte(`{}`)
	}
	if usage.UsageStatus == "" {
		usage.UsageStatus = "TOKEN_METRIC_UNAVAILABLE"
	}
	if usage.Reasoning == "" {
		usage.Reasoning = "none"
	}
	modelConfig, err := json.Marshal(map[string]any{
		"provider": provider, "model": model, "reasoning": usage.Reasoning,
		"temperature": "UNREPORTED_BY_HERMES_CLI", "top_p": "UNREPORTED_BY_HERMES_CLI",
		"max_tokens": "UNREPORTED_BY_HERMES_CLI", "prompt_template_sha256": usage.TemplateSHA256,
	})
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task ShoppingTaskRecord
		if knownTask != nil {
			task = *knownTask
		} else if err := tx.Where("tenant_id = ? AND task_id = ?", s.tenant, taskID).First(&task).Error; err != nil {
			return ErrNotFound
		}
		if task.TenantID != s.tenant || task.TaskID != taskID || task.Status != TaskStatusIntentPending {
			return ErrIntentAlreadyParsed
		}
		inputHash := hashValue(task.Prompt)
		traceID := StableID("intent-parse", taskID, provider, model, strconv.FormatInt(s.now().UnixNano(), 10))
		trace := IntentParseTraceRecord{TraceID: traceID, TenantID: s.tenant, TaskID: taskID, InputHash: inputHash,
			TemplateHash: usage.TemplateSHA256, Provider: provider, Model: model, Reasoning: usage.Reasoning,
			UsageStatus: usage.UsageStatus, UsageFile: usage.UsageFile, InputTokens: usage.InputTokens,
			OutputTokens: usage.OutputTokens, AuxInputTokens: usage.AuxInputTokens,
			AuxOutputTokens: usage.AuxOutputTokens, APICalls: usage.APICalls, ModelConfigJSON: modelConfig,
			OutputJSON: output, LatencyMS: latency.Milliseconds(),
			Succeeded: parseErr == nil, CreatedAt: s.now(), DataOrigin: syntheticOrigin}
		if parseErr != nil {
			trace.ErrorCode = TaskStatusIntentParseFailed
			task.Status = TaskStatusIntentParseFailed
		} else {
			if intent.ClarificationRequired {
				task.Status = TaskStatusClarificationRequired
			} else {
				task.Status = TaskStatusDiscovered
			}
		}
		updates := map[string]any{"status": task.Status}
		if parseErr == nil {
			updates["intent_json"] = output
			updates["provider"] = provider
			updates["model"] = model
		}
		updated := tx.Model(&ShoppingTaskRecord{}).
			Where("tenant_id = ? AND task_id = ? AND status = ?", s.tenant, taskID, TaskStatusIntentPending).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrIntentAlreadyParsed
		}
		if task.ClarifiesTaskID != "" && task.BuyerID == "" {
			return ErrInvalidSimulationData
		}
		if parseErr == nil && !intent.ClarificationRequired && task.ClarifiesTaskID != "" {
			var parent ShoppingTaskRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND task_id = ?", s.tenant, task.ClarifiesTaskID).First(&parent).Error; err != nil {
				return ErrNotFound
			}
			if parent.BuyerID != task.BuyerID || parent.Status != TaskStatusClarificationRequired {
				return ErrClarificationNotOpen
			}
			parent.Status = TaskStatusClarificationResolved
			parent.ResolvedByTaskID = task.TaskID
			if err := tx.Save(&parent).Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "trace_id"}}, DoNothing: true}).Create(&trace).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *MySQLStore) GetBuyer(ctx context.Context, buyerID string) (BuyerRecord, error) {
	var row BuyerRecord
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND buyer_id = ?", s.tenant, buyerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BuyerRecord{}, ErrNotFound
	}
	return row, err
}

// SnapshotBuyerProfiles loads the immutable synthetic buyer profiles for an
// API startup snapshot. The simulation API has no buyer-profile mutation
// endpoint; if the tenant changes after startup, HTTPAPI falls back to MySQL.
func (s *MySQLStore) SnapshotBuyerProfiles(ctx context.Context) (map[string]BuyerRecord, error) {
	var rows []BuyerRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", s.tenant).Order("buyer_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	profiles := make(map[string]BuyerRecord, len(rows))
	for _, row := range rows {
		if row.TenantID != s.tenant || row.BuyerID == "" {
			return nil, errors.New("buyer-profile snapshot contains an invalid or cross-tenant row")
		}
		if _, exists := profiles[row.BuyerID]; exists {
			return nil, errors.New("buyer-profile snapshot contains a duplicate buyer ID")
		}
		profiles[row.BuyerID] = row
	}
	return profiles, nil
}

func (s *MySQLStore) GetTask(ctx context.Context, taskID string) (ShoppingTaskRecord, error) {
	var row ShoppingTaskRecord
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND task_id = ?", s.tenant, taskID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ShoppingTaskRecord{}, ErrNotFound
	}
	return row, err
}

func (s *MySQLStore) SetTaskStatus(ctx context.Context, taskID, status string) error {
	if taskID == "" || status == "" {
		return ErrInvalidSimulationData
	}
	return s.db.WithContext(ctx).Model(&ShoppingTaskRecord{}).Where("tenant_id = ? AND task_id = ?", s.tenant, taskID).
		Update("status", status).Error
}

func (s *MySQLStore) ListBuyers(ctx context.Context, limit int) ([]BuyerRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var rows []BuyerRecord
	err := s.db.WithContext(ctx).Where("tenant_id = ?", s.tenant).Order("buyer_id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *MySQLStore) GetQuote(ctx context.Context, quoteID string) (Quote, error) {
	var row QuoteRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND quote_id = ?", s.tenant, quoteID).First(&row).Error; err != nil {
		return Quote{}, ErrNotFound
	}
	return quoteFromRecord(row), nil
}

func updateTaskStatusTx(tx *gorm.DB, tenant, taskID, status string) error {
	return tx.Model(&ShoppingTaskRecord{}).Where("tenant_id = ? AND task_id = ?", tenant, taskID).
		Update("status", status).Error
}

func auditTx(tx *gorm.DB, tenant, taskID, entityID, action string, at time.Time) error {
	return tx.Create(&SimulationAuditRecord{TenantID: tenant, TaskID: taskID, EntityID: entityID,
		Action: action, OccurredAt: at, DataOrigin: syntheticOrigin}).Error
}

func quoteFromRecord(row QuoteRecord) Quote {
	return Quote{ID: row.QuoteID, TenantID: row.TenantID, TaskID: row.TaskID, BuyerID: row.BuyerID,
		Version: row.Version, MerchantID: row.MerchantID, SKUID: row.SKUID,
		RequestedItem: row.RequestedItem, ProductItem: row.ProductItem, ProductTitle: row.ProductTitle,
		MatchType: row.MatchType, SubstitutionNotice: row.SubstitutionNotice, Region: row.Region,
		AmountMinor: row.AmountMinor, Currency: row.Currency, StockRevision: row.StockRevision,
		SnapshotHash: row.SnapshotHash, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
}

func confirmationFromRecord(row BuyerConfirmationRecord) Confirmation {
	return Confirmation{ID: row.ConfirmationID, TenantID: row.TenantID, BuyerID: row.BuyerID,
		QuoteID: row.QuoteID, QuoteVersion: row.QuoteVersion, QuoteHash: row.QuoteHash,
		AmountMinor: row.AmountMinor, Accepted: row.Accepted, IdempotencyKey: row.IdempotencyKey, CreatedAt: row.ConfirmedAt}
}

func paymentFromRecord(row MockPaymentRecord) MockPayment {
	return MockPayment{ID: row.PaymentID, TenantID: row.TenantID, OrderID: row.OrderID,
		AmountMinor: row.AmountMinor, Currency: row.Currency, Provider: row.Provider, Status: row.Status,
		SimulationOnly: true, SettlementReference: row.SettlementReference, FailureCode: row.FailureCode, CreatedAt: row.CreatedAt}
}

func checkoutFromRecords(order OrderRecord, payment MockPaymentRecord) CheckoutResult {
	return CheckoutResult{Order: Order{ID: order.OrderID, TenantID: order.TenantID, TaskID: order.TaskID,
		BuyerID: order.BuyerID, QuoteID: order.QuoteID, ConfirmationID: order.ConfirmationID,
		IdempotencyKey: order.IdempotencyKey, AmountMinor: order.AmountMinor, Currency: order.Currency,
		Status: order.Status, CreatedAt: order.CreatedAt, CompletedAt: order.CompletedAt}, Payment: paymentFromRecord(payment)}
}

func hashValue(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func containsRegion(region string) bool {
	for _, candidate := range Regions {
		if region == candidate {
			return true
		}
	}
	return false
}
