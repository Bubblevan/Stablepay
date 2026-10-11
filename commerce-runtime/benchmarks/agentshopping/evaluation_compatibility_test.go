package agentshopping

import "testing"

func TestLegacyProductCatalogHashV1OmitsBenchmarkDerivedMetadata(t *testing.T) {
	product := Product{ID: "p-1", SKUID: "sku-1", MerchantID: "m-1", Category: "coffee",
		Title: "beans", Description: "whole bean", Price: "1.00", PriceMinor: 100, Currency: "USDC",
		Tags: []string{"organic"}, Attributes: map[string]any{"roast": "light"}, Status: "active",
		Item: "beans", Format: "bag", Style: "floral", SubstitutionGroup: "coffee-beans"}
	legacy := product
	legacy.Attributes = nil
	legacy.SubstitutionGroup = ""
	want, err := hashJSON([]Product{legacy})
	if err != nil {
		t.Fatal(err)
	}
	got, err := legacyProductCatalogHashV1([]Product{product})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("legacy projection hash = %s, want %s", got, want)
	}
	product.Attributes["roast_level"] = "dark"
	derivedMetadataChanged, err := legacyProductCatalogHashV1([]Product{product})
	if err != nil {
		t.Fatal(err)
	}
	if derivedMetadataChanged != want {
		t.Fatal("legacy projection unexpectedly included benchmark-generated attributes")
	}
	product.Title = "different legacy fact"
	changed, err := legacyProductCatalogHashV1([]Product{product})
	if err != nil {
		t.Fatal(err)
	}
	if changed == want {
		t.Fatal("legacy projection failed to bind a pre-v2 product field")
	}
}

func TestValidateRecordedIntentHashesV1RequiresExactNonCatalogHashes(t *testing.T) {
	product := Product{ID: "p-1", SKUID: "sku-1", MerchantID: "m-1", Category: "coffee",
		Title: "beans", Price: "1.00", PriceMinor: 100, Currency: "USDC", Status: "active",
		Item: "beans", Format: "bag", Style: "floral", SubstitutionGroup: "coffee-beans"}
	catalog := ImportedCatalog{Products: []Product{product}}
	recordedCatalogHash, err := legacyProductCatalogHashV1(catalog.Products)
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"fixture":"locked"}`)
	current, err := ComputeEvaluationHashes(catalog, nil, manifest)
	if err != nil {
		t.Fatal(err)
	}
	recorded := current
	recorded.CatalogSHA256 = recordedCatalogHash
	got, compatibilityHash, err := validateRecordedIntentHashesV1(catalog, nil, manifest, recorded)
	if err != nil {
		t.Fatalf("valid v1 projection replay rejected: %v", err)
	}
	if got != current || compatibilityHash != recordedCatalogHash {
		t.Fatalf("unexpected compatibility result: hashes=%+v projection=%s", got, compatibilityHash)
	}
	recorded.OverlaySHA256 = "sha256:wrong"
	if _, _, err := validateRecordedIntentHashesV1(catalog, nil, manifest, recorded); err == nil {
		t.Fatal("compatibility accepted an inventory hash mismatch")
	}
}
