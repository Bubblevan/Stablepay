package agentshopping

import (
	"reflect"
	"testing"
)

func TestMerchantInventoryPairPredicateUsesBoundExactTuples(t *testing.T) {
	keys := []merchantInventoryKey{
		{merchantID: "merchant-a", skuID: "sku-1"},
		{merchantID: "merchant-b", skuID: "sku-2'); DROP TABLE sim_inventory;--"},
	}

	predicate, args := merchantInventoryPairPredicate(keys)
	if want := "(i.merchant_id, i.sku_id) IN ((?,?),(?,?))"; predicate != want {
		t.Fatalf("tuple predicate = %q, want %q", predicate, want)
	}
	wantArgs := []any{"merchant-a", "sku-1", "merchant-b", "sku-2'); DROP TABLE sim_inventory;--"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("tuple args = %#v, want %#v", args, wantArgs)
	}
}

func TestMerchantInventoryPairPredicateEmptyInputIsEmpty(t *testing.T) {
	predicate, args := merchantInventoryPairPredicate(nil)
	if predicate != "" || args != nil {
		t.Fatalf("empty tuple predicate = %q, args %#v; want empty and nil", predicate, args)
	}
}

func TestCandidateSortingPreservesDiscoveryRankAfterInventoryLookup(t *testing.T) {
	relevantButExpensive := Product{MerchantID: "merchant-a", SKUID: "bm25-first"}
	cheapButLessRelevant := Product{MerchantID: "merchant-b", SKUID: "bm25-second"}
	candidates := []Candidate{
		{Product: cheapButLessRelevant, MatchType: MatchTypeExact, Inventory: OverlayEntry{PriceMinor: 500}},
		{Product: relevantButExpensive, MatchType: MatchTypeExact, Inventory: OverlayEntry{PriceMinor: 1500}},
	}
	discoveryOrder := map[string]int{
		catalogProductKey(relevantButExpensive.MerchantID, relevantButExpensive.SKUID): 0,
		catalogProductKey(cheapButLessRelevant.MerchantID, cheapButLessRelevant.SKUID): 1,
	}

	sortCandidatesByDiscoveryOrder(candidates, discoveryOrder)
	if got, want := candidates[0].Product.SKUID, relevantButExpensive.SKUID; got != want {
		t.Fatalf("inventory price ordering displaced the ranker's top result: first SKU=%s want=%s", got, want)
	}
}
