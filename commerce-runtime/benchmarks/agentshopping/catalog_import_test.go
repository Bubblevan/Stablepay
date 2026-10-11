package agentshopping

import (
	"encoding/json"
	"testing"
)

func TestImportedSyntheticAttributesDriveTaxonomyAndCandidateFiltering(t *testing.T) {
	product := Product{SKUID: "sim-m01-000001", MerchantID: "sim-m01", Category: "coffee",
		Item: "耶加雪菲", Format: "单品豆", Style: "花果香", Currency: "USDC", Status: "active"}
	attributesJSON, err := attachSyntheticAttributes(&product, 20261010)
	if err != nil {
		t.Fatalf("attach synthetic attributes: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(attributesJSON, &persisted); err != nil {
		t.Fatalf("decode persisted attributes: %v", err)
	}
	for _, key := range []string{"origin", "roast_level", "grind_options"} {
		if product.Attributes[key] == nil || persisted[key] == nil {
			t.Fatalf("attribute %q missing from imported product or persisted row: memory=%v persisted=%v", key, product.Attributes, persisted)
		}
	}

	roast, ok := product.Attributes["roast_level"].(string)
	if !ok {
		t.Fatalf("synthetic roast level has unexpected type %T", product.Attributes["roast_level"])
	}
	intent := Intent{Category: product.Category, Item: product.Item, RequiredFormat: product.Format,
		RequiredAttributes: map[string]string{"roast_level": roast}}
	taxonomy := TaxonomyFromProducts([]Product{product})
	if !requiredAttributesAllowed(product.Category, intent.RequiredAttributes, taxonomy) ||
		candidateMatchType(product, intent) != MatchTypeExact {
		t.Fatalf("imported attributes were not available to taxonomy and candidate filtering: taxonomy=%+v product=%+v", taxonomy, product)
	}
	intent.RequiredAttributes["roast_level"] = "unsupported-value"
	if candidateMatchType(product, intent) != "" {
		t.Fatal("candidate filter ignored a hard attribute that the imported product does not satisfy")
	}
}

func TestSyntheticCoffeeOriginIsConsistentWithProductItem(t *testing.T) {
	expected := map[string]string{
		"耶加雪菲": "Ethiopia / Yirgacheffe", "曼特宁": "Indonesia / Sumatra",
		"哥伦比亚": "Colombia / Huila", "危地马拉": "Guatemala / Huehuetenango",
		"云南日晒": "Yunnan / Baoshan", "巴西黄波旁": "Brazil / Cerrado",
	}
	for item, want := range expected {
		t.Run(item, func(t *testing.T) {
			attributes := BuildSyntheticAttributes(Product{SKUID: "origin-check", Category: "coffee", Item: item}, 20261010)
			if got := attributes["origin"]; got != want {
				t.Fatalf("coffee item %q has synthetic origin %v, want %q", item, got, want)
			}
		})
	}
}

func TestSyntheticCoffeeGrindOptionsMatchProductFormat(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{format: "挂耳装", want: "filter"},
		{format: "冷萃包", want: "coarse-ground"},
		{format: "拼配豆", want: "whole bean"},
		{format: "单品豆", want: "whole bean"},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			product := Product{SKUID: "grind-check", Category: "coffee", Item: "哥伦比亚", Format: test.format}
			attributes := BuildSyntheticAttributes(product, 20261010)
			if got := attributeValueStrings(attributes["grind_options"]); len(got) == 0 || !containsString(got, test.want) {
				t.Fatalf("format %q has grind options %v, want %q", test.format, got, test.want)
			}
		})
	}
}
