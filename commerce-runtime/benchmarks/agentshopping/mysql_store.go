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
)

// MySQLStore can connect only to the dedicated loopback simulation schema.
// Its credentials are supplied independently from every StablePay .env/DSN.
type MySQLStore struct {
	db     *gorm.DB
	tenant string
	now    func() time.Time
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
	Product   Product      `json:"product"`
	Inventory OverlayEntry `json:"inventory"`
}

type Taxonomy struct {
	Categories     []string            `json:"categories"`
	Items          []string            `json:"items"`
	Formats        []string            `json:"formats"`
	Styles         []string            `json:"styles"`
	Regions        []string            `json:"regions"`
	ItemCategories map[string][]string `json:"item_categories,omitempty"`
	FormatAliases  map[string][]string `json:"format_aliases,omitempty"`
	StyleAliases   map[string][]string `json:"style_aliases,omitempty"`
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
	var rows []struct {
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
		Region      string
		OnHand      int
		Reserved    int
		Eligible    bool
		PriceMinor  int64
		Revision    int64
	}
	err := s.db.WithContext(ctx).Table("sim_products AS p").
		Select("p.sku_id, p.merchant_id, p.category, p.title, p.description, p.image_url, p.base_price AS price, p.currency, p.tags_json, p.attributes_json AS attributes, p.item, p.format, p.style, i.region, i.on_hand, i.reserved, i.eligible, i.price_minor, i.revision").
		Joins("JOIN sim_inventory AS i ON i.tenant_id = p.tenant_id AND i.merchant_id = p.merchant_id AND i.sku_id = p.sku_id").
		Where("p.tenant_id = ? AND i.tenant_id = ? AND p.status = 'active' AND p.currency = ? AND p.category = ? AND p.item = ? AND p.format = ? AND i.region = ? AND i.price_minor <= ? AND i.eligible = ? AND i.on_hand > i.reserved",
			s.tenant, s.tenant, "USDC", intent.Category, intent.Item, intent.RequiredFormat, intent.Region, intent.MaxPriceMinor, true).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		var tags []string
		var attributes map[string]any
		_ = json.Unmarshal(row.TagsJSON, &tags)
		_ = json.Unmarshal(row.Attributes, &attributes)
		result = append(result, Candidate{
			Product: Product{ID: StableID("product", row.MerchantID, row.SKUID), SKUID: row.SKUID, MerchantID: row.MerchantID,
				Category: row.Category, Title: row.Title, Description: row.Description, ImageURL: row.ImageURL,
				Price: FormatMinorUnits(row.Price), PriceMinor: row.Price, Currency: row.Currency, Tags: tags,
				Attributes: attributes, Item: row.Item, Format: row.Format, Style: row.Style},
			Inventory: OverlayEntry{MerchantID: row.MerchantID, SKUID: row.SKUID, Region: row.Region,
				OnHand: row.OnHand, Eligible: row.Eligible, PriceMinor: row.PriceMinor, Revision: row.Revision},
		})
	}
	sort.Slice(result, func(i, j int) bool {
		leftScore := stylePreferenceScore(result[i].Product, intent.PreferredStyle)
		rightScore := stylePreferenceScore(result[j].Product, intent.PreferredStyle)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if result[i].Inventory.PriceMinor != result[j].Inventory.PriceMinor {
			return result[i].Inventory.PriceMinor < result[j].Inventory.PriceMinor
		}
		return result[i].Product.SKUID < result[j].Product.SKUID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
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
	if request.TaskID == "" || request.BuyerID == "" || request.MerchantID == "" || request.SKUID == "" || request.Region == "" || request.Currency != "USDC" || request.IdempotencyKey == "" {
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
		var inventory InventoryRecord
		key := map[string]any{"tenant_id": s.tenant, "merchant_id": request.MerchantID, "sku_id": request.SKUID, "region": request.Region}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(key).First(&inventory).Error; err != nil {
			return ErrNotFound
		}
		if !inventory.Eligible || inventory.OnHand-inventory.Reserved < 1 {
			return ErrUnavailable
		}
		if inventory.Currency != request.Currency || inventory.PriceMinor > request.MaxMinor {
			return ErrBudgetExceeded
		}
		var product ProductRecord
		if err := tx.Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND status = ?", s.tenant, request.MerchantID, request.SKUID, "active").First(&product).Error; err != nil {
			return ErrNotFound
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
			request.Region, strconv.FormatInt(version, 10), strconv.FormatInt(inventory.PriceMinor, 10),
			request.Currency, strconv.FormatInt(inventory.Revision, 10), expires.UTC().Format(time.RFC3339Nano))
		row := QuoteRecord{QuoteID: quoteID, TenantID: s.tenant, TaskID: request.TaskID, BuyerID: request.BuyerID,
			MerchantID: request.MerchantID, SKUID: request.SKUID, Region: request.Region, Version: version,
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
}

func (s *MySQLStore) ConfirmBuyer(ctx context.Context, request PersistentConfirmRequest) (Confirmation, error) {
	if request.BuyerID == "" || request.QuoteID == "" || request.IdempotencyKey == "" {
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
		confirmationID := StableID("confirmation", s.tenant, request.IdempotencyKey)
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
		var old OrderRecord
		if err := tx.Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&old).Error; err == nil {
			if old.ConfirmationID != confirmationID {
				return ErrIdempotencyConflict
			}
			var payment MockPaymentRecord
			if err := tx.Where("tenant_id = ? AND order_id = ?", s.tenant, old.OrderID).First(&payment).Error; err != nil {
				return err
			}
			result = checkoutFromRecords(old, payment)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var confirmation BuyerConfirmationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND confirmation_id = ?", s.tenant, confirmationID).First(&confirmation).Error; err != nil {
			return ErrNotFound
		}
		if !confirmation.Accepted {
			return ErrConfirmationRequired
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
		{"simulated_payments", &MockPaymentRecord{}}, {"audit_events", &SimulationAuditRecord{}}, {"intent_parse_traces", &IntentParseTraceRecord{}},
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

func (s *MySQLStore) CreateTask(ctx context.Context, buyerID, prompt, idempotencyKey string) (ShoppingTaskRecord, error) {
	if buyerID == "" || strings.TrimSpace(prompt) == "" || idempotencyKey == "" || len(prompt) > 8000 {
		return ShoppingTaskRecord{}, ErrInvalidSimulationData
	}
	var buyer BuyerRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND buyer_id = ?", s.tenant, buyerID).First(&buyer).Error; err != nil {
		return ShoppingTaskRecord{}, ErrNotFound
	}
	promptBytes, _ := json.Marshal(map[string]any{})
	requestHash := hashValue(buyerID, prompt)
	row := ShoppingTaskRecord{TaskID: StableID("task", s.tenant, idempotencyKey), TenantID: s.tenant,
		BuyerID: buyerID, IdempotencyKey: idempotencyKey, Prompt: prompt, IntentJSON: promptBytes,
		Provider: "pending", Model: "pending", ParseSource: "live", Status: TaskStatusIntentPending, CreatedAt: s.now(), DataOrigin: syntheticOrigin}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var existing ShoppingTaskRecord
			if lookupErr := s.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", s.tenant, idempotencyKey).First(&existing).Error; lookupErr == nil {
				if existing.BuyerID == buyerID && hashValue(existing.BuyerID, existing.Prompt) == requestHash {
					return existing, nil
				}
				return ShoppingTaskRecord{}, ErrIdempotencyConflict
			}
		}
		return ShoppingTaskRecord{}, err
	}
	return row, nil
}

func (s *MySQLStore) SaveParsedIntent(ctx context.Context, taskID, provider, model string, intent Intent, latency time.Duration, parseErr error, usage ParseUsage) error {
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
		if err := tx.Where("tenant_id = ? AND task_id = ?", s.tenant, taskID).First(&task).Error; err != nil {
			return ErrNotFound
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
			task.IntentJSON = output
			task.Provider = provider
			task.Model = model
			task.Status = TaskStatusDiscovered
		}
		if err := tx.Save(&task).Error; err != nil {
			return err
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
		Version: row.Version, MerchantID: row.MerchantID, SKUID: row.SKUID, Region: row.Region,
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
