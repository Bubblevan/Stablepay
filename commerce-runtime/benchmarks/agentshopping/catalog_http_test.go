package agentshopping

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type catalogHTTPTestParser struct{}

func (catalogHTTPTestParser) Provider() string { return "test" }
func (catalogHTTPTestParser) Model() string    { return "stub" }
func (catalogHTTPTestParser) Parse(context.Context, string, string, int64, Taxonomy) (Intent, error) {
	return Intent{}, nil
}

func TestLoadMerchantCatalogReadsAllHTTPPages(t *testing.T) {
	const productsPerMerchant = 2000
	servers := make([]*httptest.Server, 0, 12)
	manifest := sourceManifest{Seed: 20261010, MerchantCount: 12, ProductCount: 24000}
	var requestCount atomic.Int64
	for merchantIndex := 1; merchantIndex <= 12; merchantIndex++ {
		merchantID := fmt.Sprintf("sim-m%02d", merchantIndex)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			size, _ := strconv.Atoi(r.URL.Query().Get("size"))
			if page < 1 || size != 100 {
				http.Error(w, "invalid pagination", http.StatusBadRequest)
				return
			}
			products := make([]map[string]any, 0, size)
			first := (page - 1) * size
			for offset := first; offset < first+size && offset < productsPerMerchant; offset++ {
				products = append(products, map[string]any{
					"sku_id":      fmt.Sprintf("%s-%05d", merchantID, offset+1),
					"title":       "beans · whole-bean · floral · synthetic listing",
					"description": "isolated test product", "price": "12.34", "currency": "USDC",
					"tags": []string{"synthetic"}, "status": "active",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "products": products,
				"pagination": map[string]any{"total": productsPerMerchant, "page": page, "size": size},
			})
		}))
		servers = append(servers, server)
		t.Cleanup(server.Close)
		manifest.Merchants = append(manifest.Merchants, merchantManifest{MerchantID: merchantID,
			Name: merchantID, Category: "coffee", Port: 8780 + merchantIndex,
			ProductCount: productsPerMerchant, ProductsURL: server.URL + "/api/v1/products"})
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog, err := LoadMerchantCatalog(ctx, manifestPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Merchants) != 12 || len(catalog.Products) != 24000 || len(catalog.Inventory) != 96000 {
		t.Fatalf("unexpected HTTP catalog dimensions: merchants=%d products=%d inventory=%d",
			len(catalog.Merchants), len(catalog.Products), len(catalog.Inventory))
	}
	if got := requestCount.Load(); got != 240 {
		t.Fatalf("Merchant HTTP pagination made %d requests, want 240", got)
	}
}

func TestTaxonomyFromProductsUsesOnlyActiveProductsAndStableOrdering(t *testing.T) {
	taxonomy := TaxonomyFromProducts([]Product{
		{Category: "books", Item: "guide", Format: "pdf", Style: "minimal", Status: "active"},
		{Category: "coffee", Item: "beans", Format: "bag", Style: "floral", Status: "active"},
		{Category: "ignored", Item: "hidden", Format: "draft", Style: "draft", Status: "inactive"},
	})
	if strings.Join(taxonomy.Categories, ",") != "books,coffee" || strings.Join(taxonomy.Items, ",") != "beans,guide" {
		t.Fatalf("taxonomy should contain sorted active vocabulary: %+v", taxonomy)
	}
	if strings.Join(taxonomy.ItemCategories["beans"], ",") != "coffee" || len(taxonomy.ItemCategories["hidden"]) != 0 {
		t.Fatalf("taxonomy item/category map includes invalid products: %+v", taxonomy.ItemCategories)
	}
	if len(taxonomy.Regions) != len(Regions) || len(taxonomy.FormatAliases) == 0 || len(taxonomy.StyleAliases) == 0 {
		t.Fatalf("taxonomy omitted configured regions or explicit aliases: %+v", taxonomy)
	}
}

func TestMerchantCatalogHTTPAPIReportsSourceAndRejectsDuplicateSKU(t *testing.T) {
	product := Product{MerchantID: "sim-m01", SKUID: "sim-m01-00001", Category: "coffee", Item: "beans",
		Format: "whole-bean", Style: "floral", Title: "beans · whole-bean · floral · sample", Price: "12.34",
		PriceMinor: 1234, Currency: "USDC", Status: "active"}
	catalog := ImportedCatalog{Merchants: []MerchantRecord{{MerchantID: "sim-m01"}}, Products: []Product{product}}
	api, err := NewHTTPAPIWithMerchantCatalog(&MySQLStore{tenant: "isolated-test"}, catalogHTTPTestParser{},
		"local-catalog-http-test-key-long-enough", catalog)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/catalog/source", nil)
	request.Header.Set("X-Simulation-API-Key", "local-catalog-http-test-key-long-enough")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	var status struct {
		Source        string `json:"source"`
		ProductCount  int    `json:"product_count"`
		MerchantCount int    `json:"merchant_count"`
		Simulation    bool   `json:"simulation_only"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil ||
		status.Source != "merchant_http_snapshot" || status.ProductCount != 1 || status.MerchantCount != 1 || !status.Simulation {
		t.Fatalf("catalog source status mismatch: code=%d body=%s status=%+v", response.Code, response.Body.String(), status)
	}
	catalog.Products = append(catalog.Products, product)
	if _, err := NewHTTPAPIWithMerchantCatalog(&MySQLStore{}, catalogHTTPTestParser{},
		"local-catalog-http-test-key-long-enough", catalog); err == nil {
		t.Fatal("duplicate Merchant/SKU key should be rejected")
	}
}

func TestCatalogProductIntentFilter(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "whole-bean"}
	product := Product{Category: "coffee", Item: "beans", Format: "whole-bean", Currency: "USDC", Status: "active"}
	if !catalogProductMatchesIntent(product, intent) {
		t.Fatal("matching active catalog product should pass the intent filter")
	}
	product.Status = "inactive"
	if catalogProductMatchesIntent(product, intent) {
		t.Fatal("inactive Merchant product must not be discoverable")
	}
	product.Status, product.Currency = "active", "SOL"
	if catalogProductMatchesIntent(product, intent) {
		t.Fatal("non-USDC product must not be discoverable by this shopping simulation")
	}
}

func TestValidateMerchantCatalogSnapshotRecordsRejectsSeedDrift(t *testing.T) {
	merchant := MerchantRecord{MerchantID: "sim-m01", Name: "merchant", Category: "coffee", DID: "did:sim:merchant:sim-m01"}
	product := Product{MerchantID: "sim-m01", SKUID: "sim-m01-00001", Category: "coffee", Item: "beans",
		Format: "whole-bean", Style: "floral", Title: "beans · whole-bean · floral · sample", Description: "description",
		ImageURL: "https://example.invalid/product.png", PriceMinor: 1234, Currency: "USDC", Status: "active", Tags: []string{"fresh", "synthetic"}}
	tagsJSON, err := json.Marshal(product.Tags)
	if err != nil {
		t.Fatal(err)
	}
	seedProduct := ProductRecord{MerchantID: product.MerchantID, SKUID: product.SKUID, Category: product.Category,
		Item: product.Item, Format: product.Format, Style: product.Style, Title: product.Title,
		Description: product.Description, ImageURL: product.ImageURL, BasePrice: product.PriceMinor,
		Currency: product.Currency, Status: product.Status, TagsJSON: tagsJSON}
	if err := validateMerchantCatalogSnapshotRecords([]MerchantRecord{merchant}, []Product{product},
		[]MerchantRecord{merchant}, []ProductRecord{seedProduct}); err != nil {
		t.Fatalf("identical Merchant and seed snapshots should validate: %v", err)
	}
	seedProduct.Title = "stale title"
	if err := validateMerchantCatalogSnapshotRecords([]MerchantRecord{merchant}, []Product{product},
		[]MerchantRecord{merchant}, []ProductRecord{seedProduct}); err == nil {
		t.Fatal("catalog validator accepted product metadata drift")
	}
}
