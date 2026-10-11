package agentshopping

import (
	"strings"
	"testing"
)

func TestRankCandidatesOffersOnlyCuratedSubstitutesAndRanksExactFirst(t *testing.T) {
	products := []Product{
		{SKUID: "exact", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Style: "花果香", Currency: "USDC"},
		{SKUID: "origin-substitute", Category: "coffee", Item: "曼特宁", Format: "单品豆", Style: "坚果可可", Currency: "USDC"},
		{SKUID: "wrong-format", Category: "coffee", Item: "曼特宁", Format: "拼配豆", Style: "坚果可可", Currency: "USDC"},
		{SKUID: "unrelated-item", Category: "coffee", Item: "耶加雪菲风味课程", Format: "单品豆", Style: "花果香", Currency: "USDC"},
	}
	state := map[string]OverlayEntry{
		OverlayKey("exact", "CN-EAST"):             {SKUID: "exact", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 900},
		OverlayKey("origin-substitute", "CN-EAST"): {SKUID: "origin-substitute", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500},
		OverlayKey("wrong-format", "CN-EAST"):      {SKUID: "wrong-format", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 400},
		OverlayKey("unrelated-item", "CN-EAST"):    {SKUID: "unrelated-item", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 300},
	}
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆", Region: "CN-EAST", MaxPriceMinor: 1000}
	withoutPermission := RankCandidates(products, intent, state, true, 20)
	if len(withoutPermission) != 1 || withoutPermission[0].SKUID != "exact" {
		t.Fatalf("substitutes must be excluded unless explicitly allowed: %+v", withoutPermission)
	}
	intent.AllowSubstitution = true
	withPermission := RankCandidates(products, intent, state, true, 20)
	if len(withPermission) != 2 || withPermission[0].SKUID != "exact" || withPermission[1].SKUID != "origin-substitute" {
		t.Fatalf("only curated same-format substitutes should appear, after exact items: %+v", withPermission)
	}
	if got := candidateMatchType(withPermission[0], intent); got != MatchTypeExact {
		t.Fatalf("exact result match_type=%q, want %q", got, MatchTypeExact)
	}
	if got := candidateMatchType(withPermission[1], intent); got != MatchTypeSubstitute {
		t.Fatalf("substitute result match_type=%q, want %q", got, MatchTypeSubstitute)
	}
}

func TestCandidateMatchTypeFailsClosedForUncuratedCategoryOrFormat(t *testing.T) {
	intent := Intent{Category: "books", Item: "城市漫游手记", RequiredFormat: "电子版", AllowSubstitution: true}
	for _, product := range []Product{
		{Category: "books", Item: "世界建筑图册", Format: "电子版"},
		{Category: "coffee", Item: "曼特宁", Format: "单品豆"},
		{Category: "coffee", Item: "曼特宁", Format: "挂耳装"},
	} {
		if got := candidateMatchType(product, intent); got != "" {
			t.Fatalf("unrelated product unexpectedly matched as %q: %+v", got, product)
		}
	}
}

func TestSubstituteQuoteNoticeNamesBothRequestedAndSelectedItems(t *testing.T) {
	got := substitutionNotice("耶加雪菲", "曼特宁")
	if got == "" || !containsAll(got, "耶加雪菲", "曼特宁", "明确确认") {
		t.Fatalf("substitution notice does not clearly bind the change to renewed buyer consent: %q", got)
	}
}

func TestRequiredAttributeFilteringIsHardAndCategoryScoped(t *testing.T) {
	products := []Product{
		{SKUID: "light", Status: "active", Category: "coffee", Item: "beans", Format: "bag", Style: "floral",
			Currency: "USDC", Attributes: map[string]any{"roast_level": "light", "grind_options": []any{"whole bean", "filter"}}},
		{SKUID: "medium", Status: "active", Category: "coffee", Item: "beans", Format: "bag", Style: "floral",
			Currency: "USDC", Attributes: map[string]any{"roast_level": "medium", "grind_options": []string{"whole bean", "espresso"}}},
		{SKUID: "book", Status: "active", Category: "books", Item: "beans", Format: "bag", Style: "floral",
			Currency: "USDC", Attributes: map[string]any{"roast_level": "light"}},
	}
	taxonomy := TaxonomyFromProducts(products)
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
		RequiredAttributes: map[string]string{"roast_level": "light", "grind_options": "filter"}}
	if !requiredAttributesAllowed(intent.Category, intent.RequiredAttributes, taxonomy) {
		t.Fatal("attributes observed in the selected category were not whitelisted")
	}
	if candidateMatchType(products[0], intent) != MatchTypeExact {
		t.Fatal("product satisfying all hard attributes must remain a candidate")
	}
	if candidateMatchType(products[1], intent) != "" {
		t.Fatal("a product failing one hard attribute must be excluded")
	}
	if requiredAttributesAllowed("books", intent.RequiredAttributes, taxonomy) {
		t.Fatal("category-specific attributes must not be accepted for another category")
	}
	if requiredAttributesMatch(products[0].Attributes, map[string]string{"grind_options": "espresso"}) {
		t.Fatal("array-valued attributes must match only a contained value")
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
