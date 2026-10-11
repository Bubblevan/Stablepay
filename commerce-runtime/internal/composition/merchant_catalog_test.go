package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

func TestHTTPMerchantCatalogReaderMapsExplicitMetadataAndSyncsLifecycle(t *testing.T) {
	active := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/products":
			if r.Method != http.MethodGet || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("size") != "100" {
				t.Errorf("unexpected product list request: %s %s", r.Method, r.URL.String())
			}
			page := merchantCatalogPage{OK: true}
			page.Pagination.Page = 1
			page.Pagination.Size = merchantCatalogPageSize
			if active {
				page.Pagination.Total = 1
				page.Products = []merchantCatalogProduct{catalogTestProduct("sku-active", "active", "1.25", "2026-10-10T12:00:00Z")}
			}
			writeCatalogTestJSON(t, w, page)
		case "/api/v1/products/sku-active":
			status, price, updatedAt := "inactive", "2.00", "2026-10-10T12:00:00Z"
			if active {
				status, price, updatedAt = "active", "1.25", "2026-10-10T12:00:00Z"
			}
			writeCatalogTestJSON(t, w, map[string]any{
				"ok": true, "product": catalogTestProduct("sku-active", status, price, updatedAt),
			})
		case "/api/v1/products/sku-paused":
			writeCatalogTestJSON(t, w, map[string]any{
				"ok": true, "product": catalogTestProduct("sku-paused", "draft", "0.50", "2026-10-10T11:00:00Z"),
			})
		case "/api/v1/products/sku-deleted":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manifest := MerchantCatalogManifest{
		SchemaVersion: merchantCatalogManifestVersion,
		Sources: []MerchantCatalogSource{{
			SourceID: "merchant-test", MerchantDID: "did:merchant:explicit",
			PayeeDID: "did:solana:explicit-payee", ProductAPIURL: server.URL + "/api/v1/products",
			Capabilities: []MerchantProductCapability{
				catalogTestMetadata("sku-active", "audio.transcribe"),
				catalogTestMetadata("sku-paused", "audio.transcribe.paused"),
				{
					SKUID: "sku-deleted", CapabilityID: "audio.transcribe.deleted", MetadataVersion: 1, AllowMissingSKU: true,
					TaskTypes: []string{"transcription"}, InputSchemaRef: "schema://audio/input", OutputSchemaRef: "schema://text/output",
					InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"},
					SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed",
				},
			},
		}},
	}
	reader, err := NewHTTPMerchantCatalogReader(server.Client(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := reader.ReadCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != 2 {
		t.Fatalf("expected active and paused declared capabilities, got %d", len(capabilities))
	}
	var imported *catalog.MerchantCapability
	for _, capability := range capabilities {
		if capability.CapabilityID == "audio.transcribe" {
			imported = capability
		}
	}
	if imported == nil {
		t.Fatal("active SKU was not mapped")
	}
	initialVersion := imported.CatalogVersion
	if imported.MerchantDID != "did:merchant:explicit" || imported.PayeeDID != "did:solana:explicit-payee" {
		t.Fatalf("identity was not taken from explicit operator metadata: %#v", imported)
	}
	if imported.PriceHintMinor == nil || *imported.PriceHintMinor != 125 || imported.PriceHintCurrency != "USDC" {
		t.Fatalf("display price was not converted to Runtime business minor units: %#v", imported.PriceHintMinor)
	}
	if imported.InvokeEndpoint.Endpoint != server.URL+"/api/v1/products/sku-active/execute" {
		t.Fatalf("unexpected invocation endpoint: %s", imported.InvokeEndpoint.Endpoint)
	}
	if len(imported.SemanticTags) != 1 || imported.SemanticTags[0] != "operator-audio" {
		t.Fatalf("product free-text tags were incorrectly inferred as capability metadata: %#v", imported.SemanticTags)
	}
	if !imported.IsActiveAt(time.Now()) {
		t.Fatal("active Merchant product did not become an available Runtime capability")
	}

	store := repository.NewInMemoryStore()
	if err := SyncMerchantCatalog(context.Background(), store, reader, manifest); err != nil {
		t.Fatalf("initial catalog sync: %v", err)
	}
	activeCapabilities, err := store.ListActiveCapabilities(context.Background())
	if err != nil || len(activeCapabilities) != 1 || activeCapabilities[0].CapabilityID != "audio.transcribe" {
		t.Fatalf("unexpected active Runtime catalog after import: capabilities=%#v err=%v", activeCapabilities, err)
	}

	// Repeating an unchanged startup sync must be idempotent.
	if err := SyncMerchantCatalog(context.Background(), store, reader, manifest); err != nil {
		t.Fatalf("idempotent catalog sync: %v", err)
	}

	// A Merchant state/price update is imported as a new immutable version and
	// removes the product from the active discovery view.
	active = false
	if err := SyncMerchantCatalog(context.Background(), store, reader, manifest); err != nil {
		t.Fatalf("catalog lifecycle sync: %v", err)
	}
	activeCapabilities, err = store.ListActiveCapabilities(context.Background())
	if err != nil || len(activeCapabilities) != 0 {
		t.Fatalf("inactive Merchant product remained discoverable: capabilities=%#v err=%v", activeCapabilities, err)
	}
	current, err := store.GetCurrentActiveCapability(context.Background(), "did:merchant:explicit", "audio.transcribe")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != catalog.StatusInactive || current.Availability != catalog.AvailabilityUnavailable || current.PriceHintMinor == nil || *current.PriceHintMinor != 200 {
		t.Fatalf("changed Merchant facts were not persisted as inactive latest version: %#v", current)
	}
	if catalog.CompareVersions(current.CatalogVersion, initialVersion) <= 0 {
		t.Fatalf("same-timestamp Merchant edits did not produce a newer immutable version: old=%s new=%s", initialVersion, current.CatalogVersion)
	}
}

func TestMerchantCatalogManifestRejectsUnsafeSKUPathSegments(t *testing.T) {
	manifest := MerchantCatalogManifest{
		SchemaVersion: merchantCatalogManifestVersion,
		Sources: []MerchantCatalogSource{{
			SourceID: "merchant-test", MerchantDID: "did:merchant:test", PayeeDID: "did:payee:test",
			ProductAPIURL: "https://merchant.example/api/v1/products",
			Capabilities:  []MerchantProductCapability{catalogTestMetadata("../other", "audio.transcribe")},
		}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("path traversal SKU was accepted")
	}
}

func TestSyncMerchantCatalogRetiresSourcesRemovedFromManifest(t *testing.T) {
	now := time.Now().UTC().Round(time.Millisecond)
	source := MerchantCatalogSource{
		SourceID: "merchant-old", MerchantDID: "did:merchant:old", PayeeDID: "did:payee:old",
		ProductAPIURL: "https://old.example/api/v1/products",
	}
	capability, err := capabilityFromMerchantProduct(source, catalogTestMetadata("sku-old", "old.service"),
		merchantCatalogProduct{SKUID: "sku-old", Title: "Old service", Description: "Old service",
			Price: "1.00", Currency: "USDC", Status: "active",
			CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewInMemoryStore()
	if err := store.SaveCapabilityVersion(context.Background(), capability); err != nil {
		t.Fatal(err)
	}

	manifest := MerchantCatalogManifest{
		SchemaVersion: merchantCatalogManifestVersion,
		Sources: []MerchantCatalogSource{{
			SourceID: "merchant-new", MerchantDID: "did:merchant:new", PayeeDID: "did:payee:new",
			ProductAPIURL: "https://new.example/api/v1/products",
		}},
	}
	if err := SyncMerchantCatalog(context.Background(), store, staticMerchantCatalogReader{}, manifest); err != nil {
		t.Fatal(err)
	}
	active, err := store.ListActiveCapabilities(context.Background())
	if err != nil || len(active) != 0 {
		t.Fatalf("removed Merchant source remained active: capabilities=%#v err=%v", active, err)
	}
	current, err := store.GetCurrentActiveCapability(context.Background(), "did:merchant:old", "old.service")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != catalog.StatusInactive {
		t.Fatalf("removed Merchant source was not tombstoned: status=%s", current.Status)
	}
}

type staticMerchantCatalogReader struct{}

func (staticMerchantCatalogReader) ReadCapabilities(context.Context) ([]*catalog.MerchantCapability, error) {
	return nil, nil
}

func catalogTestMetadata(skuID, capabilityID string) MerchantProductCapability {
	return MerchantProductCapability{
		SKUID: skuID, CapabilityID: capabilityID, MetadataVersion: 1, TaskTypes: []string{"transcription"},
		SemanticTags: []string{"operator-audio"}, InputSchemaRef: "schema://audio/input",
		OutputSchemaRef: "schema://text/output", InputContentTypes: []string{"audio/mpeg"},
		OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v2"},
		SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed",
	}
}

func catalogTestProduct(skuID, status, price, updatedAt string) merchantCatalogProduct {
	return merchantCatalogProduct{
		SKUID: skuID, Title: "Audio transcription", Description: "A catalog product.",
		Price: price, Currency: "USDC", Status: status, CreatedAt: "2026-10-10T10:00:00Z",
		UpdatedAt: updatedAt, Tags: []string{"operator-audio", "not-capability-metadata"},
	}
}

func writeCatalogTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}
