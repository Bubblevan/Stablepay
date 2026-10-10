package agentshopping

import (
	"reflect"
	"testing"
)

func TestParseMinorUnits(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		bad   bool
	}{
		{input: "0", want: 0},
		{input: "12.3", want: 1230},
		{input: "12.34", want: 1234},
		{input: " 105 ", want: 10500},
		{input: "-1", bad: true},
		{input: "1.234", bad: true},
		{input: "1.2.3", bad: true},
	}
	for _, tt := range tests {
		got, err := ParseMinorUnits(tt.input)
		if tt.bad {
			if err == nil {
				t.Errorf("ParseMinorUnits(%q) = %d, want error", tt.input, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseMinorUnits(%q) = (%d, %v), want %d", tt.input, got, err, tt.want)
		}
	}
}

func TestGenerateTasksIsDeterministicBalancedAndUnique(t *testing.T) {
	products := make([]Product, 0, 8)
	for merchantIndex, merchantID := range []string{"merchant-b", "merchant-a"} {
		category := "coffee"
		if merchantIndex == 1 {
			category = "books"
		}
		for i := 0; i < 4; i++ {
			products = append(products, Product{
				SKUID: merchantID + "-sku-" + string(rune('1'+i)), MerchantID: merchantID,
				Category: category, Item: "sample-" + string(rune('a'+i)), Format: "digital", Style: "plain",
				Currency: "USDC", PriceMinor: int64(1000 + i*10),
			})
		}
	}
	overlay := make([]OverlayEntry, 0, len(products)*len(Regions))
	for _, product := range products {
		for _, region := range Regions {
			overlay = append(overlay, OverlayEntry{MerchantID: product.MerchantID, SKUID: product.SKUID,
				Region: region, OnHand: 2, Eligible: true, PriceMinor: product.PriceMinor, Revision: 1})
		}
	}
	first, err := GenerateTasks(products, overlay, 20261010, 8)
	if err != nil {
		t.Fatalf("GenerateTasks: %v", err)
	}
	second, err := GenerateTasks(products, overlay, 20261010, 8)
	if err != nil {
		t.Fatalf("GenerateTasks repeat: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same seed/input produced different task suite")
	}
	seenTask, seenTarget := map[string]bool{}, map[string]bool{}
	byMerchant := map[string]int{}
	for _, task := range first {
		if seenTask[task.TaskID] || seenTarget[task.UniqueTarget] {
			t.Fatalf("duplicate task or gold target: %+v", task)
		}
		seenTask[task.TaskID], seenTarget[task.UniqueTarget] = true, true
		if !contains(task.GoldSKUs, task.UniqueTarget) || task.Intent.ConfirmationNeeded != true {
			t.Fatalf("invalid gold target or missing confirmation requirement: %+v", task)
		}
		for _, product := range products {
			if product.SKUID == task.UniqueTarget {
				byMerchant[product.MerchantID]++
				break
			}
		}
	}
	if byMerchant["merchant-a"] != 4 || byMerchant["merchant-b"] != 4 {
		t.Fatalf("tasks not balanced per merchant: %#v", byMerchant)
	}
}

func TestStockAwareRankingFiltersUnavailableOffers(t *testing.T) {
	products := []Product{
		{SKUID: "cheap-out-of-stock", MerchantID: "m1", Category: "c", Item: "i", Format: "f", Style: "s", Currency: "USDC"},
		{SKUID: "available", MerchantID: "m2", Category: "c", Item: "i", Format: "f", Style: "s", Currency: "USDC"},
	}
	intent := Intent{Category: "c", Item: "i", RequiredFormat: "f", PreferredStyle: "s", Region: "CN-EAST", MaxPriceMinor: 2000}
	state := map[string]OverlayEntry{
		OverlayKey("cheap-out-of-stock", "CN-EAST"): {SKUID: "cheap-out-of-stock", Region: "CN-EAST", OnHand: 0, Eligible: true, PriceMinor: 100},
		OverlayKey("available", "CN-EAST"):          {SKUID: "available", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 200},
	}
	baseline := RankCandidates(products, intent, state, false, 20)
	treatment := RankCandidates(products, intent, state, true, 20)
	if len(baseline) != 2 || baseline[0].SKUID != "cheap-out-of-stock" {
		t.Fatalf("catalog-only baseline = %+v", baseline)
	}
	if len(treatment) != 1 || treatment[0].SKUID != "available" {
		t.Fatalf("stock-aware treatment = %+v", treatment)
	}
}

func TestSyntheticBuyerBudgetsStayWithinDocumentedRange(t *testing.T) {
	buyers := BuildSyntheticBuyers(20261010, 2000)
	if len(buyers) != 2000 {
		t.Fatalf("buyer count = %d, want 2000", len(buyers))
	}
	for _, buyer := range buyers {
		if buyer.BudgetMinor < 15000 || buyer.BudgetMinor > 53250 {
			t.Fatalf("buyer %s has budget %d minor units outside $150-$532.50", buyer.BuyerID, buyer.BudgetMinor)
		}
	}
}

func TestIntentAliasHintsReturnIndependentCopies(t *testing.T) {
	formats, styles := IntentAliasHints()
	if len(formats) == 0 || len(styles) == 0 {
		t.Fatal("expected non-empty alias dictionaries")
	}
	formats["挂耳装"][0] = "mutated"
	styles["花果香"][0] = "mutated"
	formats["new-format"] = []string{"new-alias"}
	styles["new-style"] = []string{"new-alias"}

	formatsAgain, stylesAgain := IntentAliasHints()
	if formatsAgain["挂耳装"][0] == "mutated" || stylesAgain["花果香"][0] == "mutated" {
		t.Fatal("alias hint slices must not expose mutable package state")
	}
	if _, ok := formatsAgain["new-format"]; ok {
		t.Fatal("alias hint map must not expose mutable package state")
	}
	if _, ok := stylesAgain["new-style"]; ok {
		t.Fatal("alias hint map must not expose mutable package state")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
