package agentshopping

import (
	"fmt"
	"reflect"
	"testing"
)

func TestMerchantCatalogDiscoveryIndexPreservesRankingSemantics(t *testing.T) {
	products := []Product{
		{SKUID: "exact", MerchantID: "m1", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Style: "花果香", Currency: "USDC", Status: "active"},
		{SKUID: "substitute", MerchantID: "m2", Category: "coffee", Item: "曼特宁", Format: "单品豆", Style: "坚果", Currency: "USDC", Status: "active"},
		{SKUID: "wrong-format", MerchantID: "m3", Category: "coffee", Item: "耶加雪菲", Format: "挂耳装", Style: "花果香", Currency: "USDC", Status: "active"},
		{SKUID: "wrong-category", MerchantID: "m4", Category: "tea", Item: "耶加雪菲", Format: "单品豆", Style: "花果香", Currency: "USDC", Status: "active"},
		{SKUID: "inactive", MerchantID: "m5", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Style: "花果香", Currency: "USDC", Status: "inactive"},
	}
	index := newMerchantCatalogDiscoveryIndex(products)
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆", PreferredStyle: "花果香"}
	indexedProducts := index.productsForIntent(intent)
	if len(indexedProducts) != 3 || len(indexedProducts) >= len(products) {
		t.Fatalf("index did not narrow the catalog by category and format: bucket=%d full=%d", len(indexedProducts), len(products))
	}
	if got, want := rankDiscoveryProducts(indexedProducts, intent, 100), rankDiscoveryProducts(products, intent, 100); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed exact-item ranking differs from full-scan semantics: indexed=%+v full=%+v", got, want)
	}
	intent = intent.WithSearchText("floral whole bean coffee")
	intent.discoveryDocs = index.documentsForIntent(intent)
	if got, want := rankDiscoveryProducts(index.productsForIntent(intent), intent, 100), rankDiscoveryProducts(products, intent, 100); !reflect.DeepEqual(got, want) {
		t.Fatalf("pre-tokenized catalog ranking differs from uncached BM25 ranking: indexed=%+v full=%+v", got, want)
	}

	intent.AllowSubstitution = true
	if got, want := rankDiscoveryProducts(index.productsForIntent(intent), intent, 100), rankDiscoveryProducts(products, intent, 100); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed substitution ranking differs from full-scan semantics: indexed=%+v full=%+v", got, want)
	}

	unknown := Intent{Category: "unknown", RequiredFormat: "unknown-format"}
	if got := index.productsForIntent(unknown); len(got) != 0 {
		t.Fatalf("unknown category/format should have no indexed candidates: %+v", got)
	}
}

func TestDiscoveryTitleCoverageBoostIsOptInAndUsesOriginalQuery(t *testing.T) {
	products := []Product{
		{SKUID: "description-heavy", Category: "audio", Item: "headphones", Format: "wired", Title: "Classic Headphones",
			Description: "blue wire blue wire blue wire headphones", Currency: "USDC", Status: "active"},
		{SKUID: "title-match", Category: "audio", Item: "headphones", Format: "wired", Title: "Blue Wire Headphones",
			Currency: "USDC", Status: "active"},
	}
	intent := Intent{Category: "audio", Item: "headphones", RequiredFormat: "wired"}.WithSearchText("blue wire headphones")
	baseline := rankDiscoveryProductsWithTitleCoverageWeight(products, intent, DefaultRecallCutoff, 0)
	defaultRanking := rankDiscoveryProducts(products, intent, DefaultRecallCutoff)
	expectedDefault := rankDiscoveryProductsWithWeights(products, intent, DefaultRecallCutoff,
		defaultDiscoveryTitleCoverageWeight, defaultDiscoveryTitlePhraseWeight)
	if !reflect.DeepEqual(defaultRanking, expectedDefault) {
		t.Fatalf("default ranker does not use its pinned title-coverage weight: got=%v want=%v", defaultRanking, expectedDefault)
	}
	boosted := rankDiscoveryProductsWithTitleCoverageWeight(products, intent, DefaultRecallCutoff, 100)
	if len(boosted) != len(products) || boosted[0].SKUID != "title-match" {
		t.Fatalf("title coverage did not favor the title matching the original query: got=%v", boosted)
	}
	if len(baseline) != len(products) {
		t.Fatalf("zero-weight BM25 baseline omitted candidates: %v", baseline)
	}
}

func TestDiscoveryTitlePhraseBoostPrefersOrderedQueryBigrams(t *testing.T) {
	products := []Product{
		{SKUID: "a-reordered", MerchantID: "m1", Category: "audio", Item: "headphones", Format: "wireless",
			Title: "Wireless Blue Headphones", Currency: "USDC", Status: "active"},
		{SKUID: "z-phrase", MerchantID: "m1", Category: "audio", Item: "headphones", Format: "wireless",
			Title: "Blue Wireless Headphones", Currency: "USDC", Status: "active"},
	}
	intent := Intent{Category: "audio", Item: "headphones", RequiredFormat: "wireless"}.WithSearchText("blue wireless headphones")

	withoutPhrase := rankDiscoveryProductsWithWeights(products, intent, DefaultRecallCutoff, 0, 0)
	withPhrase := rankDiscoveryProductsWithWeights(products, intent, DefaultRecallCutoff, 0, 100)
	if withoutPhrase[0].SKUID != "a-reordered" {
		t.Fatalf("control ranking should resolve equal lexical scores by SKU: %+v", withoutPhrase)
	}
	if withPhrase[0].SKUID != "z-phrase" {
		t.Fatalf("ordered title bigrams should favor the title preserving query phrase order: %+v", withPhrase)
	}
	if production := rankDiscoveryProducts(products, intent, DefaultRecallCutoff); production[0].SKUID != "z-phrase" {
		t.Fatalf("default discovery ranker should apply the validated title-phrase weight: %+v", production)
	}
	index := newMerchantCatalogDiscoveryIndex(products)
	indexedIntent := intent
	indexedIntent.discoveryDocs = index.documentsForIntent(indexedIntent)
	indexed := rankDiscoveryProductsWithWeights(index.productsForIntent(indexedIntent), indexedIntent,
		DefaultRecallCutoff, 0, 100)
	if !reflect.DeepEqual(indexed, withPhrase) {
		t.Fatalf("pre-tokenized title phrase scores differ from uncached scores: indexed=%+v uncached=%+v", indexed, withPhrase)
	}

	if got, want := discoveryTitleBigramCoverage(tokenizeDiscoveryText("blue wireless headphones"),
		tokenizeDiscoveryText("blue wireless overhead headphones")), 0.5; got != want {
		t.Fatalf("partial ordered-bigram coverage = %.2f, want %.2f", got, want)
	}
}

func TestRankDiscoveryProductsPrioritizesFullHardAttributeMatches(t *testing.T) {
	products := []Product{
		{SKUID: "a-decoy-1", Category: "coffee", Item: "beans", Format: "bag", Style: "floral",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"roast_level": "dark", "origin": "Colombia"}},
		{SKUID: "a-decoy-2", Category: "coffee", Item: "beans", Format: "bag", Style: "floral",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"roast_level": "light", "origin": "Brazil"}},
		{SKUID: "z-target", Category: "coffee", Item: "beans", Format: "bag", Style: "",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"roast_level": "light", "origin": "Ethiopia"}},
	}
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag",
		RequiredAttributes: map[string]string{"roast_level": "light", "origin": "Ethiopia"}}

	ranked := rankDiscoveryProducts(products, intent, 2)
	if len(ranked) != 2 || ranked[0].SKUID != "z-target" {
		t.Fatalf("full hard-attribute match should rank before same-item decoys even without a soft style: %+v", ranked)
	}
	if ranked[1].SKUID == "z-target" {
		t.Fatal("hard-attribute ranking must not be implemented by removing other broad-retrieval candidates")
	}
}

func TestRankDiscoveryProductsUsesBuyerTextOverGenericLexicalTie(t *testing.T) {
	products := []Product{
		{SKUID: "generic", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Title: "耶加雪菲 单品豆",
			Description: "普通烘焙咖啡豆", Currency: "USDC", Status: "active"},
		{SKUID: "text-match", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Title: "耶加雪菲 单品豆",
			Description: "埃塞俄比亚 花果香 浅烘焙 适合手冲", Tags: []string{"新鲜烘焙"},
			Attributes: map[string]any{"origin": "埃塞俄比亚", "roast_level": "浅烘焙"}, Currency: "USDC", Status: "active"},
	}
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆"}.
		WithSearchText("想要埃塞俄比亚产地、花果香、浅烘焙，适合手冲的耶加雪菲单品豆")

	ranked := rankDiscoveryProducts(products, intent, 2)
	if len(ranked) != 2 || ranked[0].SKUID != "text-match" {
		t.Fatalf("buyer wording and product descriptions should break an otherwise equal lexical tie: %+v", ranked)
	}
}

func TestRankDiscoveryProductsBM25DoesNotOverrideBusinessPriority(t *testing.T) {
	products := []Product{
		{SKUID: "exact-generic", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Title: "单品豆",
			Description: "普通咖啡", Currency: "USDC", Status: "active"},
		{SKUID: "substitute-rich", Category: "coffee", Item: "曼特宁", Format: "单品豆", Title: "花果香 浅烘焙 埃塞俄比亚",
			Currency: "USDC", Status: "active"},
	}
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆", AllowSubstitution: true}.
		WithSearchText("埃塞俄比亚 花果香 浅烘焙")

	ranked := rankDiscoveryProducts(products, intent, 2)
	if len(ranked) != 2 || ranked[0].SKUID != "exact-generic" {
		t.Fatalf("BM25 must not rank a substitute above an eligible exact item: %+v", ranked)
	}
}

func TestTokenizeDiscoveryTextHandlesChineseBigramsAndEnglishWords(t *testing.T) {
	got := tokenizeDiscoveryText("Ethiopia 浅烘焙")
	want := []string{"ethiopia", "浅烘", "烘焙"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected discovery tokens: got=%v want=%v", got, want)
	}
}

func BenchmarkRankDiscoveryCatalogIndex(b *testing.B) {
	products := make([]Product, 0, 24000)
	for index := 0; index < 24000; index++ {
		products = append(products, Product{SKUID: fmt.Sprintf("sku-%05d", index), MerchantID: fmt.Sprintf("merchant-%02d", index/2000),
			Category: fmt.Sprintf("category-%02d", index/2000), Item: fmt.Sprintf("item-%04d", index%2000),
			Title:  fmt.Sprintf("Studio headphones product item %04d", index%2000),
			Format: "standard", Currency: "USDC", Status: "active"})
	}
	intent := Intent{Category: "category-03", Item: "item-0001", RequiredFormat: "standard"}.
		WithSearchText("studio headphones product item")
	index := newMerchantCatalogDiscoveryIndex(products)
	partition := index.productsForIntent(intent)
	intent.discoveryDocs = index.documentsForIntent(intent)
	b.Run("full-snapshot-production", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = rankDiscoveryProducts(products, intent, DefaultDiscoveryCandidateWindow)
		}
	})
	b.Run("category-format-production", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = rankDiscoveryProducts(partition, intent, DefaultDiscoveryCandidateWindow)
		}
	})
	b.Run("category-format-bm25-baseline", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = rankDiscoveryProductsWithTitleCoverageWeight(partition, intent, DefaultDiscoveryCandidateWindow, 0)
		}
	})
}
