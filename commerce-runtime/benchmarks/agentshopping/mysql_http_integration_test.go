package agentshopping

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

const mysqlIntegrationAPIKey = "local-mysql-http-integration-key-only"

type mysqlIntegrationParser struct{}

func (mysqlIntegrationParser) Provider() string { return "integration-test" }
func (mysqlIntegrationParser) Model() string    { return "deterministic-stub" }
func (mysqlIntegrationParser) Parse(_ context.Context, _ string, region string, budget int64, _ Taxonomy) (Intent, error) {
	return Intent{Category: "coffee", Item: "beans", RequiredFormat: "whole-bean", PreferredStyle: "floral",
		Region: region, MaxPriceMinor: budget, ConfirmationNeeded: true}, nil
}

type mysqlIntegrationFixture struct {
	merchantID string
	skuID      string
	buyerID    string
	taskID     string
	parseKey   string
}

type mysqlIntegrationAPI struct {
	client *http.Client
	server *httptest.Server
	key    string
}

func TestMySQLHTTPShoppingLifecycleRequiresFreshConsent(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_INTEGRATION") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_INTEGRATION=1 to run against the isolated shopping simulation MySQL")
	}

	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required when SHOP_SIM_MYSQL_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated simulation MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("isolated MySQL ping failed: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("isolated MySQL schema migration failed: %v", err)
	}

	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-it-" + suffix
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated integration tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify isolated integration tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("integration tenant still has %d rows in %s after cleanup", count, table)
			}
		}
	})

	api, err := NewHTTPAPI(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}

	ready := client.do(t, http.MethodGet, "/readyz", nil)
	if ready.status != http.StatusOK || !strings.Contains(string(ready.body), `"status":"ready"`) {
		t.Fatalf("simulation API readiness status=%d body=%s", ready.status, ready.body)
	}
	dataset := client.do(t, http.MethodGet, "/v1/dataset", nil)
	var datasetInfo struct {
		TenantID string `json:"tenant_id"`
	}
	if dataset.status != http.StatusOK || json.Unmarshal(dataset.body, &datasetInfo) != nil || datasetInfo.TenantID != tenantID {
		t.Fatalf("API escaped the isolated integration tenant: status=%d response=%s", dataset.status, dataset.body)
	}

	allScenariosPass := true
	allScenariosPass = t.Run("price revision blocks old consent and settles only after new consent", func(t *testing.T) {
		fixture := createMySQLIntegrationFixture(t, ctx, store, client, suffix+"-price")
		client.assertTaskStatus(t, fixture.taskID, TaskStatusDiscovered)
		firstQuote := client.issueAndCheckQuote(t, fixture)
		client.assertTaskStatus(t, fixture.taskID, TaskStatusQuoted)
		newPrice := int64(1200)
		client.adjustInventory(t, fixture, "price-change", "PRICE_CHANGED", &newPrice, nil)

		stale := client.confirm(t, firstQuote, true, "stale-price-confirm", http.StatusConflict)
		if stale.errorCode != "STALE_QUOTE" {
			t.Fatalf("old quote confirmation error=%q, want STALE_QUOTE", stale.errorCode)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusReconfirmationRequired)
		unchanged, status := client.getQuote(t, firstQuote.ID)
		if status != http.StatusOK || unchanged.AmountMinor != 1000 {
			t.Fatalf("issued quote was mutated after repricing: status=%d quote=%+v", status, unchanged)
		}

		secondQuote := client.issueQuote(t, fixture, "fresh-price-quote", 2000, http.StatusCreated)
		if secondQuote.ID == firstQuote.ID || secondQuote.Version <= firstQuote.Version || secondQuote.AmountMinor != newPrice {
			t.Fatalf("new quote does not represent a new immutable price revision: old=%+v new=%+v", firstQuote, secondQuote)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusQuoted)
		confirmation := client.confirm(t, secondQuote, true, "fresh-price-confirm", http.StatusCreated).confirmation
		client.assertTaskStatus(t, fixture.taskID, TaskStatusBuyerConfirmed)
		checkoutKey := settledCheckoutKey("price-" + suffix)
		result := client.checkout(t, confirmation.ID, checkoutKey, http.StatusCreated)
		if result.Order.Status != OrderPaid || result.Payment.Status != PaymentSettled || result.Payment.Provider != "simulation-only" ||
			!result.Payment.SimulationOnly || !result.SimulationOnly || result.RealSettlement || result.ChainTransaction != nil {
			t.Fatalf("price lifecycle did not finish through isolated mock settlement: %+v", result)
		}
		replay := client.checkout(t, confirmation.ID, checkoutKey, http.StatusCreated)
		if replay.Order.ID != result.Order.ID || replay.Payment.ID != result.Payment.ID {
			t.Fatalf("checkout idempotent replay changed transaction identity: first=%+v replay=%+v", result, replay)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusPaid)
		if replayStatus := client.replayIntent(t, fixture); replayStatus != TaskStatusPaid {
			t.Fatalf("idempotent intent replay changed task terminal state: got=%s", replayStatus)
		}
		assertMySQLIntegrationLifecycle(t, ctx, store, fixture.taskID, fixture, 1, 1, 1, 1, 3,
			[]string{"INVENTORY_REVISED", "BUYER_CONFIRMATION_BLOCKED_STALE_QUOTE", "BUYER_CONFIRMED", "MOCK_PAYMENT_SETTLED", OrderPaid})
	}) && allScenariosPass

	allScenariosPass = t.Run("stock restoration also requires a new quote and confirmation", func(t *testing.T) {
		fixture := createMySQLIntegrationFixture(t, ctx, store, client, suffix+"-restock")
		firstQuote := client.issueAndCheckQuote(t, fixture)
		zero, restored := 0, 3
		client.adjustInventory(t, fixture, "stock-out", "STOCK_UNAVAILABLE", nil, &zero)
		stale := client.confirm(t, firstQuote, true, "stale-stock-confirm", http.StatusConflict)
		if stale.errorCode != "STALE_QUOTE" {
			t.Fatalf("old quote after stock loss error=%q, want STALE_QUOTE", stale.errorCode)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusReconfirmationRequired)
		client.adjustInventory(t, fixture, "stock-restored", "STOCK_REPLENISHED", nil, &restored)
		secondQuote := client.issueQuote(t, fixture, "fresh-stock-quote", 2000, http.StatusCreated)
		if secondQuote.ID == firstQuote.ID || secondQuote.Version <= firstQuote.Version {
			t.Fatalf("restocked inventory did not issue a later quote version: old=%+v new=%+v", firstQuote, secondQuote)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusQuoted)
		confirmation := client.confirm(t, secondQuote, true, "fresh-stock-confirm", http.StatusCreated).confirmation
		checkoutKey := settledCheckoutKey("restock-" + suffix)
		result := client.checkout(t, confirmation.ID, checkoutKey, http.StatusCreated)
		if result.Order.Status != OrderPaid || result.Payment.Status != PaymentSettled || result.RealSettlement || result.ChainTransaction != nil {
			t.Fatalf("restocked lifecycle did not complete via mock payment: %+v", result)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusPaid)
		assertMySQLIntegrationLifecycle(t, ctx, store, fixture.taskID, fixture, 1, 1, 1, 1, 2,
			[]string{"INVENTORY_REVISED", "BUYER_CONFIRMATION_BLOCKED_STALE_QUOTE", "BUYER_CONFIRMED", "MOCK_PAYMENT_SETTLED", OrderPaid})
	}) && allScenariosPass

	allScenariosPass = t.Run("last-moment stock loss blocks checkout without order or payment", func(t *testing.T) {
		fixture := createMySQLIntegrationFixture(t, ctx, store, client, suffix+"-last-moment")
		quote := client.issueAndCheckQuote(t, fixture)
		confirmation := client.confirm(t, quote, true, "confirmed-before-stock-loss", http.StatusCreated).confirmation
		client.assertTaskStatus(t, fixture.taskID, TaskStatusBuyerConfirmed)
		zero := 0
		client.adjustInventory(t, fixture, "last-moment-stock-loss", "STOCK_LOST_AFTER_CONSENT", nil, &zero)
		blocked := client.checkoutError(t, confirmation.ID, "checkout-after-stock-loss", http.StatusConflict)
		if blocked != "STALE_QUOTE" {
			t.Fatalf("checkout error=%q, want STALE_QUOTE", blocked)
		}
		client.assertTaskStatus(t, fixture.taskID, TaskStatusReconfirmationRequired)
		assertMySQLIntegrationLifecycle(t, ctx, store, fixture.taskID, fixture, 0, 0, 0, 0, 0,
			[]string{"INVENTORY_REVISED", "BUYER_CONFIRMED", "CHECKOUT_BLOCKED_STALE_QUOTE"})
	}) && allScenariosPass

	if allScenariosPass {
		t.Logf("MYSQL_HTTP_LIFECYCLE_PASS tenant=%s scenarios=3 price_requote=PASS stock_restock_reconfirm=PASS stock_loss_no_side_effect=PASS real_payment_adapter=NOT_PRESENT",
			tenantID)
	}
}

func TestMySQLHTTPMerchantSnapshotCandidateSearch(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_INTEGRATION") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_INTEGRATION=1 to run against the isolated shopping simulation MySQL")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required when SHOP_SIM_MYSQL_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated Merchant catalog integration MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-catalog-it-" + suffix
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated catalog integration tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify catalog integration tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("catalog integration tenant retained %d rows in %s", count, table)
			}
		}
	})

	now := time.Now().UTC()
	merchantID, skuID, buyerID := "sim-m01", "sim-m01-catalog-integration", "sim-buyer-catalog-integration"
	merchant := MerchantRecord{TenantID: tenantID, MerchantID: merchantID, Name: "Synthetic Merchant",
		Category: "coffee", DID: "did:sim:merchant:" + merchantID, DataOrigin: syntheticOrigin, CreatedAt: now}
	product := Product{ID: StableID("product", merchantID, skuID), MerchantID: merchantID, SKUID: skuID,
		Category: "coffee", Item: "beans", Format: "whole-bean", Style: "floral",
		Title: "beans · whole-bean · floral · source integration fixture", Description: "synthetic source listing",
		Price: "10.00", PriceMinor: 1000, Currency: "USDC", Tags: []string{"synthetic"}, Status: "active"}
	tagsJSON, err := json.Marshal(product.Tags)
	if err != nil {
		t.Fatal(err)
	}
	productRow := ProductRecord{TenantID: tenantID, MerchantID: merchantID, SKUID: skuID, Category: product.Category,
		Item: product.Item, Format: product.Format, Style: product.Style, Title: product.Title,
		Description: product.Description, BasePrice: product.PriceMinor, Currency: product.Currency,
		Status: product.Status, TagsJSON: tagsJSON, AttributesJSON: []byte(`{"data_origin":"SYNTHETIC_SIMULATION"}`),
		DataOrigin: syntheticOrigin, CreatedAt: now, UpdatedAt: now}
	inventory := InventoryRecord{TenantID: tenantID, MerchantID: merchantID, SKUID: skuID, Region: "CN-EAST",
		OnHand: 4, Eligible: true, PriceMinor: 1000, Currency: "USDC", Revision: 1, UpdatedAt: now, DataOrigin: syntheticOrigin}
	buyer := BuyerRecord{TenantID: tenantID, BuyerID: buyerID, DisplayName: "Synthetic Catalog Buyer",
		HomeRegion: "CN-EAST", Segment: "integration", BudgetMinor: 2000, PreferencesJSON: []byte(`{}`),
		DataOrigin: syntheticOrigin, CreatedAt: now}
	if err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range []any{&merchant, &productRow, &inventory, &buyer} {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed synthetic Merchant HTTP catalog integration tenant: %v", err)
	}

	catalog := ImportedCatalog{Merchants: []MerchantRecord{merchant}, Products: []Product{product}}
	if err := store.ValidateMerchantCatalogSnapshot(ctx, catalog); err != nil {
		t.Fatalf("matching Merchant and MySQL snapshots should validate: %v", err)
	}
	drifted := catalog
	drifted.Products = append([]Product(nil), catalog.Products...)
	drifted.Products[0].Description = "stale Merchant description"
	if err := store.ValidateMerchantCatalogSnapshot(ctx, drifted); err == nil {
		t.Fatal("Merchant/MySQL snapshot validator accepted product metadata drift")
	}
	if err := store.db.WithContext(ctx).Model(&ProductRecord{}).
		Where("tenant_id = ? AND merchant_id = ? AND sku_id = ?", tenantID, merchantID, skuID).
		Update("title", "stale MySQL title after startup").Error; err != nil {
		t.Fatal(err)
	}

	api, err := NewHTTPAPIWithMerchantCatalog(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey, catalog)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	response := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": buyerID, "prompt": "Buy one floral whole-bean coffee within budget", "idempotency_key": "catalog-search-" + suffix,
	})
	if response.status != http.StatusOK {
		t.Fatalf("Merchant-backed intent search status=%d body=%s", response.status, response.body)
	}
	var result struct {
		TaskStatus string      `json:"task_status"`
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != TaskStatusDiscovered || len(result.Candidates) != 1 {
		t.Fatalf("Merchant-backed candidate search result=%+v body=%s", result, response.body)
	}
	candidate := result.Candidates[0]
	if candidate.Product.SKUID != skuID || candidate.Product.Title != product.Title ||
		candidate.Inventory.MerchantID != merchantID || candidate.Inventory.PriceMinor != inventory.PriceMinor ||
		candidate.Inventory.OnHand != inventory.OnHand || !candidate.Inventory.Eligible {
		t.Fatalf("candidate did not combine Merchant product facts with isolated MySQL inventory: %+v", candidate)
	}
	t.Logf("MYSQL_MERCHANT_HTTP_CANDIDATE_PASS tenant=%s products=%d source=%s", tenantID, len(catalog.Products), "merchant_http_snapshot")
}

func TestMySQLMerchantCatalogCandidateSearchAtScale(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_SCALE_INTEGRATION") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_SCALE_INTEGRATION=1 to exercise candidate search against the full isolated catalog")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required when SHOP_SIM_MYSQL_SCALE_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	var merchants []MerchantRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", SimulationTenantID).Find(&merchants).Error; err != nil {
		t.Fatal(err)
	}
	var rows []ProductRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", SimulationTenantID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(merchants) != 12 || len(rows) != 24000 {
		t.Fatalf("full-catalog source tenant dimensions merchants=%d products=%d, want 12/24000", len(merchants), len(rows))
	}
	catalogProducts := make([]Product, 0, len(rows))
	productsByKey := make(map[string]Product, len(rows))
	for _, row := range rows {
		var tags []string
		if err := json.Unmarshal(row.TagsJSON, &tags); err != nil {
			t.Fatalf("decode source tags for %s/%s: %v", row.MerchantID, row.SKUID, err)
		}
		product := Product{ID: StableID("product", row.MerchantID, row.SKUID), MerchantID: row.MerchantID, SKUID: row.SKUID,
			Category: row.Category, Title: row.Title, Description: row.Description, ImageURL: row.ImageURL,
			Price: FormatMinorUnits(row.BasePrice), PriceMinor: row.BasePrice, Currency: row.Currency,
			Tags: tags, Status: row.Status, Item: row.Item, Format: row.Format, Style: row.Style}
		catalogProducts = append(catalogProducts, product)
		productsByKey[catalogProductKey(row.MerchantID, row.SKUID)] = product
	}

	type availableInventory struct {
		MerchantID string `gorm:"column:merchant_id"`
		SKUID      string `gorm:"column:sku_id"`
		Region     string `gorm:"column:region"`
		OnHand     int    `gorm:"column:on_hand"`
		Eligible   bool   `gorm:"column:eligible"`
		PriceMinor int64  `gorm:"column:price_minor"`
	}
	var available availableInventory
	err = store.db.WithContext(ctx).Table("sim_inventory AS i").
		Select("i.merchant_id, i.sku_id, i.region, i.on_hand, i.eligible, i.price_minor").
		Joins("JOIN sim_products AS p ON p.tenant_id = i.tenant_id AND p.merchant_id = i.merchant_id AND p.sku_id = i.sku_id").
		Where("i.tenant_id = ? AND p.tenant_id = ? AND p.status = 'active' AND i.eligible = ? AND i.on_hand > i.reserved",
			SimulationTenantID, SimulationTenantID, true).
		Order("i.merchant_id, i.sku_id, i.region").Take(&available).Error
	if err != nil {
		t.Fatalf("find one in-stock source offer in the full catalog: %v", err)
	}
	product, ok := productsByKey[catalogProductKey(available.MerchantID, available.SKUID)]
	if !ok {
		t.Fatalf("in-stock offer %s/%s is absent from the product snapshot", available.MerchantID, available.SKUID)
	}
	intent := Intent{Category: product.Category, Item: product.Item, RequiredFormat: product.Format,
		PreferredStyle: product.Style, Region: available.Region, MaxPriceMinor: available.PriceMinor}
	if _, err := store.FindCandidatesFromMerchantCatalog(ctx, catalogProducts, intent, 20); err != nil {
		t.Fatalf("warm full-catalog Merchant snapshot search: %v", err)
	}
	latencies := make([]time.Duration, 25)
	var candidateCount int
	for index := range latencies {
		started := time.Now()
		candidates, err := store.FindCandidatesFromMerchantCatalog(ctx, catalogProducts, intent, 20)
		latencies[index] = time.Since(started)
		if err != nil {
			t.Fatalf("full-catalog Merchant snapshot search %d: %v", index+1, err)
		}
		if index == 0 {
			candidateCount = len(candidates)
		}
		foundOffer := false
		for _, candidate := range candidates {
			if candidate.Product.MerchantID == available.MerchantID && candidate.Product.SKUID == available.SKUID &&
				candidate.Inventory.Region == available.Region {
				foundOffer = true
				break
			}
		}
		if !foundOffer {
			t.Fatalf("full-catalog search omitted its exact eligible in-stock target %s/%s", available.MerchantID, available.SKUID)
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("MYSQL_SEEDED_CATALOG_SEARCH_SCALE_PASS merchants=%d products=%d search_repeats=%d candidates=%d p50_ms=%.3f p95_ms=%.3f",
		len(merchants), len(rows), len(latencies), candidateCount,
		float64(latencies[len(latencies)/2].Microseconds())/1000,
		float64(latencies[int(float64(len(latencies)-1)*0.95)].Microseconds())/1000)
}

func TestCleanupMySQLIntegrationTenant(t *testing.T) {
	tenant := os.Getenv("SHOP_SIM_MYSQL_CLEANUP_TENANT")
	if tenant == "" {
		t.Skip("set SHOP_SIM_MYSQL_CLEANUP_TENANT to an exact shopping-it-<16 hex> tenant for cleanup")
	}
	suffix := strings.TrimPrefix(tenant, "shopping-it-")
	if len(suffix) != 16 {
		t.Fatal("cleanup tenant must match shopping-it-<16 hex>")
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		t.Fatal("cleanup tenant must match shopping-it-<16 hex>")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required for explicit integration-tenant cleanup")
	}
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cleanupMySQLIntegrationTenant(ctx, store, tenant); err != nil {
		t.Fatal(err)
	}
	t.Logf("cleaned only explicitly named synthetic tenant %s in the isolated simulation database", tenant)
}

type mysqlIntegrationResponse struct {
	status int
	body   []byte
}

type mysqlIntegrationQuoteResponse struct {
	Quote Quote `json:"quote"`
}

type mysqlIntegrationConfirmationResponse struct {
	Confirmation Confirmation `json:"confirmation"`
}

type mysqlIntegrationCheckoutResponse struct {
	Order            Order       `json:"order"`
	Payment          MockPayment `json:"payment"`
	SimulationOnly   bool        `json:"simulation_only"`
	RealSettlement   bool        `json:"real_settlement"`
	ChainTransaction *string     `json:"chain_transaction"`
}

func (api *mysqlIntegrationAPI) do(t *testing.T, method, path string, input any) mysqlIntegrationResponse {
	t.Helper()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, api.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Simulation-API-Key", api.key)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := api.client.Do(request)
	if err != nil {
		t.Fatalf("HTTP %s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return mysqlIntegrationResponse{status: response.StatusCode, body: responseBody}
}

func (api *mysqlIntegrationAPI) parseIntent(t *testing.T, buyerID, key, expectedSKU string) string {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": buyerID, "prompt": "Buy one floral whole-bean coffee within budget", "idempotency_key": key,
	})
	if response.status != http.StatusOK {
		t.Fatalf("parse intent status=%d body=%s", response.status, response.body)
	}
	var result struct {
		TaskID     string      `json:"task_id"`
		TaskStatus string      `json:"task_status"`
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range result.Candidates {
		if candidate.Product.SKUID == expectedSKU {
			found = true
			break
		}
	}
	if result.TaskID == "" || !found || result.TaskStatus != TaskStatusDiscovered {
		t.Fatalf("stub intent parse did not discover fixture product %q: task=%q candidates=%d", expectedSKU, result.TaskID, len(result.Candidates))
	}
	return result.TaskID
}

func (api *mysqlIntegrationAPI) assertTaskStatus(t *testing.T, taskID, expected string) {
	t.Helper()
	response := api.do(t, http.MethodGet, "/v1/tasks/"+taskID, nil)
	var result struct {
		TaskStatus string `json:"task_status"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	if response.status != http.StatusOK || result.TaskStatus != expected {
		t.Fatalf("task state=%q want=%q status=%d body=%s", result.TaskStatus, expected, response.status, response.body)
	}
}

func (api *mysqlIntegrationAPI) replayIntent(t *testing.T, fixture mysqlIntegrationFixture) string {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": fixture.buyerID, "prompt": "Buy one floral whole-bean coffee within budget", "idempotency_key": fixture.parseKey,
	})
	var result struct {
		TaskStatus       string `json:"task_status"`
		IdempotentReplay bool   `json:"idempotent_replay"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	if response.status != http.StatusOK || !result.IdempotentReplay {
		t.Fatalf("intent replay status=%d body=%s", response.status, response.body)
	}
	return result.TaskStatus
}

func (api *mysqlIntegrationAPI) issueAndCheckQuote(t *testing.T, fixture mysqlIntegrationFixture) Quote {
	t.Helper()
	return api.issueQuote(t, fixture, "initial-quote", 2000, http.StatusCreated)
}

func (api *mysqlIntegrationAPI) issueQuote(t *testing.T, fixture mysqlIntegrationFixture, key string, maxMinor int64, expectedStatus int) Quote {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/quotes", map[string]any{
		"task_id": fixture.taskID, "buyer_id": fixture.buyerID, "merchant_id": fixture.merchantID,
		"sku_id": fixture.skuID, "region": "CN-EAST", "max_price_minor": maxMinor,
		"currency": "USDC", "idempotency_key": key + "-" + fixture.taskID,
	})
	if response.status != expectedStatus {
		t.Fatalf("issue quote status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	var result mysqlIntegrationQuoteResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Quote
}

func (api *mysqlIntegrationAPI) getQuote(t *testing.T, quoteID string) (Quote, int) {
	t.Helper()
	response := api.do(t, http.MethodGet, "/v1/quotes/"+quoteID, nil)
	var result mysqlIntegrationQuoteResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Quote, response.status
}

func (api *mysqlIntegrationAPI) confirm(t *testing.T, quote Quote, accepted bool, key string, expectedStatus int) struct {
	confirmation Confirmation
	errorCode    string
} {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/quotes/"+quote.ID+"/confirm", map[string]any{
		"buyer_id": quote.BuyerID, "accepted": accepted, "amount_minor": quote.AmountMinor,
		"idempotency_key": key + "-" + quote.ID,
	})
	if response.status != expectedStatus {
		t.Fatalf("confirm quote status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	if response.status == http.StatusConflict {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.body, &envelope); err != nil {
			t.Fatal(err)
		}
		return struct {
			confirmation Confirmation
			errorCode    string
		}{errorCode: envelope.Error.Code}
	}
	var result mysqlIntegrationConfirmationResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return struct {
		confirmation Confirmation
		errorCode    string
	}{confirmation: result.Confirmation}
}

func (api *mysqlIntegrationAPI) adjustInventory(t *testing.T, fixture mysqlIntegrationFixture, suffix, reason string, price *int64, onHand *int) {
	t.Helper()
	request := map[string]any{"task_id": fixture.taskID, "merchant_id": fixture.merchantID, "sku_id": fixture.skuID,
		"region": "CN-EAST", "reason": reason, "idempotency_key": suffix + "-" + fixture.taskID}
	if price != nil {
		request["price_minor"] = *price
	}
	if onHand != nil {
		request["on_hand"] = *onHand
	}
	response := api.do(t, http.MethodPost, "/v1/admin/inventory-adjustments", request)
	if response.status != http.StatusOK {
		t.Fatalf("inventory adjustment status=%d body=%s", response.status, response.body)
	}
}

func (api *mysqlIntegrationAPI) checkout(t *testing.T, confirmationID, key string, expectedStatus int) mysqlIntegrationCheckoutResponse {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/orders", map[string]any{"confirmation_id": confirmationID, "idempotency_key": key})
	if response.status != expectedStatus {
		t.Fatalf("checkout status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	var result mysqlIntegrationCheckoutResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func (api *mysqlIntegrationAPI) checkoutError(t *testing.T, confirmationID, key string, expectedStatus int) string {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/orders", map[string]any{"confirmation_id": confirmationID, "idempotency_key": key})
	if response.status != expectedStatus {
		t.Fatalf("blocked checkout status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Error.Code
}

func createMySQLIntegrationFixture(t *testing.T, ctx context.Context, store *MySQLStore, client *mysqlIntegrationAPI, suffix string) mysqlIntegrationFixture {
	t.Helper()
	fixture := mysqlIntegrationFixture{merchantID: "it-merchant-" + suffix, skuID: "it-sku-" + suffix,
		buyerID: "it-buyer-" + suffix, parseKey: "task-" + suffix}
	now := time.Now().UTC()
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		merchant := MerchantRecord{TenantID: store.tenant, MerchantID: fixture.merchantID, Name: "Integration Fixture Merchant",
			Category: "coffee", DID: "did:example:shopping-it", DataOrigin: syntheticOrigin, CreatedAt: now}
		if err := tx.Create(&merchant).Error; err != nil {
			return err
		}
		product := ProductRecord{TenantID: store.tenant, SKUID: fixture.skuID, MerchantID: fixture.merchantID,
			Category: "coffee", Item: "beans", Format: "whole-bean", Style: "floral", Title: "beans · whole-bean · floral · integration fixture",
			Description: "Synthetic benchmark-only product", BasePrice: 1000, Currency: "USDC", Status: "active",
			TagsJSON: []byte(`[]`), AttributesJSON: []byte(`{}`), DataOrigin: syntheticOrigin, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&product).Error; err != nil {
			return err
		}
		inventory := InventoryRecord{TenantID: store.tenant, MerchantID: fixture.merchantID, SKUID: fixture.skuID,
			Region: "CN-EAST", OnHand: 4, Eligible: true, PriceMinor: 1000, Currency: "USDC", Revision: 1,
			UpdatedAt: now, DataOrigin: syntheticOrigin}
		if err := tx.Create(&inventory).Error; err != nil {
			return err
		}
		buyer := BuyerRecord{TenantID: store.tenant, BuyerID: fixture.buyerID, DisplayName: "Synthetic Integration Buyer",
			HomeRegion: "CN-EAST", Segment: "integration", BudgetMinor: 2000, PreferencesJSON: []byte(`{}`),
			DataOrigin: syntheticOrigin, CreatedAt: now}
		return tx.Create(&buyer).Error
	})
	if err != nil {
		t.Fatalf("create synthetic integration fixture: %v", err)
	}
	fixture.taskID = client.parseIntent(t, fixture.buyerID, fixture.parseKey, fixture.skuID)
	return fixture
}

func assertMySQLIntegrationLifecycle(t *testing.T, ctx context.Context, store *MySQLStore, taskID string,
	fixture mysqlIntegrationFixture, orders, payments, reservations, staleAttempts int64, expectedOnHand int, requiredActions []string) {
	t.Helper()
	for model, expected := range map[any]int64{&OrderRecord{}: orders, &MockPaymentRecord{}: payments, &InventoryReservationRecord{}: reservations} {
		query := store.db.WithContext(ctx).Model(model).Where("tenant_id = ? AND task_id = ?", store.tenant, taskID)
		if _, ok := model.(*MockPaymentRecord); ok {
			query = store.db.WithContext(ctx).Model(model).Joins("JOIN sim_orders ON sim_orders.order_id = simulated_payments.order_id").
				Where("simulated_payments.tenant_id = ? AND sim_orders.task_id = ?", store.tenant, taskID)
		}
		if _, ok := model.(*InventoryReservationRecord); ok {
			query = store.db.WithContext(ctx).Model(model).Joins("JOIN sim_orders ON sim_orders.order_id = sim_inventory_reservations.order_id").
				Where("sim_inventory_reservations.tenant_id = ? AND sim_orders.task_id = ?", store.tenant, taskID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			t.Fatalf("count %T rows: %v", model, err)
		}
		if count != expected {
			t.Errorf("%T rows=%d want=%d for task %s", model, count, expected, taskID)
		}
	}
	var inventory InventoryRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
		store.tenant, fixture.merchantID, fixture.skuID, "CN-EAST").First(&inventory).Error; err != nil {
		t.Fatal(err)
	}
	if inventory.Reserved != 0 {
		t.Errorf("inventory reservation leaked after terminal outcome: %+v", inventory)
	}
	if inventory.OnHand != expectedOnHand {
		t.Errorf("inventory on-hand=%d want=%d after terminal outcome", inventory.OnHand, expectedOnHand)
	}
	var staleAttemptCount int64
	if err := store.db.WithContext(ctx).Model(&BuyerConfirmationAttemptRecord{}).
		Where("tenant_id = ? AND quote_id IN (SELECT quote_id FROM sim_quotes WHERE tenant_id = ? AND task_id = ?) AND status = ?",
			store.tenant, store.tenant, taskID, "STALE_QUOTE").Count(&staleAttemptCount).Error; err != nil {
		t.Fatal(err)
	}
	if staleAttemptCount != staleAttempts {
		t.Errorf("stale confirmation attempts=%d want=%d", staleAttemptCount, staleAttempts)
	}
	var events []string
	if err := store.db.WithContext(ctx).Model(&SimulationAuditRecord{}).Where("tenant_id = ? AND task_id = ?", store.tenant, taskID).
		Order("id ASC").Pluck("action", &events).Error; err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event] = true
	}
	for _, action := range requiredActions {
		if !seen[action] {
			t.Errorf("missing audit action %q; events=%v", action, events)
		}
	}
}

func cleanupMySQLIntegrationTenant(ctx context.Context, store *MySQLStore, tenant string) error {
	models := []any{&InventoryReservationRecord{}, &MockPaymentRecord{}, &OrderRecord{}, &BuyerConfirmationRecord{},
		&BuyerConfirmationAttemptRecord{}, &QuoteRecord{}, &InventoryAdjustmentRecord{}, &SimulationAuditRecord{},
		&IntentParseTraceRecord{}, &ShoppingTaskRecord{}, &InventoryRecord{}, &ProductRecord{}, &BuyerRecord{}, &MerchantRecord{}, &DatasetRunRecord{}}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range models {
			if err := tx.Where("tenant_id = ?", tenant).Delete(model).Error; err != nil {
				return fmt.Errorf("delete %T: %w", model, err)
			}
		}
		return nil
	})
}

func mysqlIntegrationSuffix(t *testing.T) string {
	t.Helper()
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data[:])
}

func settledCheckoutKey(prefix string) string {
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%s-%d", prefix, i)
		if simulatedPaymentStatus(key) == PaymentSettled {
			return key
		}
	}
	return prefix + "-fallback"
}
