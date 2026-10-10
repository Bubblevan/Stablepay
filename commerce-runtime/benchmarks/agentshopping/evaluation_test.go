package agentshopping

import (
	"reflect"
	"testing"
)

func TestStructuredEvaluationSeparatesStaticBaselineFromCurrentOfferFilter(t *testing.T) {
	products := make([]Product, 0, 21)
	offers := make([]Inventory, 0, 21)
	for i := 0; i < 20; i++ {
		sku := "unavailable-" + string(rune('a'+i))
		products = append(products, Product{SKUID: sku, MerchantID: "m1", Category: "coffee", Item: "beans",
			Format: "bag", Style: "floral", Currency: "USDC", PriceMinor: int64(100 + i),
			Title: "beans · bag · floral · sample"})
		offers = append(offers, Inventory{TenantID: SimulationTenantID, MerchantID: "m1", SKUID: sku,
			Region: "CN-EAST", OnHand: 0, Eligible: true, PriceMinor: int64(100 + i), Revision: 1})
	}
	products = append(products, Product{SKUID: "available-target", MerchantID: "m1", Category: "coffee", Item: "beans",
		Format: "bag", Style: "floral", Currency: "USDC", PriceMinor: 500, Title: "beans · bag · floral · target"})
	offers = append(offers, Inventory{TenantID: SimulationTenantID, MerchantID: "m1", SKUID: "available-target",
		Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1})
	catalog := ImportedCatalog{Seed: 20261010, Products: products, Inventory: offers}
	task := Task{TaskID: "locked-001", SourceMerchantID: "m1", Scenario: "STOCK_CHANGE",
		Intent: Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
			Region: "CN-EAST", MaxPriceMinor: 1000, ConfirmationNeeded: true},
		GoldSKUs: []string{"available-target"}, UniqueTarget: "available-target"}
	report, rows, err := EvaluateStructuredTasks(catalog, []Task{task}, 20, 1000, 7)
	if err != nil {
		t.Fatalf("EvaluateStructuredTasks: %v", err)
	}
	if len(rows) != 1 || report.TaskCount != 1 {
		t.Fatalf("unexpected evaluation dimensions: report=%+v rows=%d", report, len(rows))
	}
	if rows[0].Baseline.RecallHits != 0 || rows[0].Baseline.PlanValid {
		t.Fatalf("static baseline should miss the unavailable/expensive gold SKU at K=20: %+v", rows[0].Baseline)
	}
	if rows[0].Treatment.RecallHits != 1 || !rows[0].Treatment.PlanValid || rows[0].Treatment.TopSKU != "available-target" {
		t.Fatalf("treatment should retrieve and select the eligible offer: %+v", rows[0].Treatment)
	}
	if report.Baseline.ValidPlans != 0 || report.Treatment.ValidPlans != 1 || report.Treatment.ValidPlanRate != 1 {
		t.Fatalf("unexpected paired summaries: baseline=%+v treatment=%+v", report.Baseline, report.Treatment)
	}
}

func TestEvaluationHashesAreStableAcrossInputOrdering(t *testing.T) {
	first := ImportedCatalog{Products: []Product{
		{SKUID: "b", MerchantID: "m", Title: "two"}, {SKUID: "a", MerchantID: "m", Title: "one"},
	}, Inventory: []Inventory{
		{MerchantID: "m", SKUID: "b", Region: "CN-WEST"}, {MerchantID: "m", SKUID: "a", Region: "CN-EAST"},
	}}
	second := ImportedCatalog{Products: []Product{first.Products[1], first.Products[0]},
		Inventory: []Inventory{first.Inventory[1], first.Inventory[0]}}
	tasks := []Task{{TaskID: "t1", GoldSKUs: []string{"a"}}, {TaskID: "t2", GoldSKUs: []string{"b"}}}
	one, err := ComputeEvaluationHashes(first, tasks, []byte("manifest"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := ComputeEvaluationHashes(second, tasks, []byte("manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("hashes depend on source ordering: first=%+v second=%+v", one, two)
	}
}

func TestRankCandidatesTreatsStyleAsPreferenceNotHardFilter(t *testing.T) {
	products := []Product{
		{SKUID: "cheap-alternative", Category: "coffee", Item: "beans", Format: "bag", Style: "cocoa", Currency: "USDC"},
		{SKUID: "preferred-style", Category: "coffee", Item: "beans", Format: "bag", Style: "floral", Currency: "USDC"},
	}
	state := map[string]OverlayEntry{
		OverlayKey("cheap-alternative", "CN-EAST"): {SKUID: "cheap-alternative", Region: "CN-EAST", OnHand: 2, Eligible: true, PriceMinor: 100},
		OverlayKey("preferred-style", "CN-EAST"):   {SKUID: "preferred-style", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500},
	}
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral", Region: "CN-EAST", MaxPriceMinor: 1000}
	got := RankCandidates(products, intent, state, true, 20)
	if len(got) != 2 || got[0].SKUID != "preferred-style" || got[1].SKUID != "cheap-alternative" {
		t.Fatalf("preferred style should rank first without excluding alternatives: %+v", got)
	}
}

func TestIntentEvaluationPopulatesFieldLevelMetrics(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
		Region: "CN-EAST", MaxPriceMinor: 1000, AllowSubstitution: true, ConfirmationNeeded: true}
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "sku-1", MerchantID: "m1", Category: intent.Category, Item: intent.Item,
			Format: intent.RequiredFormat, Style: intent.PreferredStyle, Currency: "USDC", PriceMinor: 900, Title: "beans bag floral"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "m1", SKUID: "sku-1", Region: intent.Region,
			OnHand: 1, Eligible: true, PriceMinor: 900, Revision: 1}},
	}
	tasks := []Task{
		{TaskID: "t1", Intent: intent, GoldSKUs: []string{"sku-1"}, UniqueTarget: "sku-1"},
		{TaskID: "t2", Intent: intent, GoldSKUs: []string{"sku-1"}, UniqueTarget: "sku-1"},
	}
	wrongCategory := intent
	wrongCategory.Category = "education"
	results := []BatchIntentResult{{TaskID: "t1", Intent: intent}, {TaskID: "t2", Intent: wrongCategory}}
	report, _, err := EvaluateParsedIntentBatch(catalog, tasks, results, 20, 1000, 7)
	if err != nil {
		t.Fatalf("EvaluateParsedIntentBatch: %v", err)
	}
	if report.Category.Total != 2 || report.Category.Correct != 1 || report.Category.Accuracy != 0.5 {
		t.Fatalf("category field metric missing or incorrect: %+v", report.Category)
	}
	for name, metric := range map[string]IntentFieldMetric{
		"item": report.Item, "required_format": report.RequiredFormat, "preferred_style": report.PreferredStyle,
		"region": report.Region, "max_price_minor": report.MaxPriceMinor, "allow_substitution": report.AllowSubstitution,
	} {
		if metric.Total != 2 || metric.Correct != 2 || metric.Accuracy != 1 {
			t.Errorf("%s field metric missing or incorrect: %+v", name, metric)
		}
	}
}

func TestIntentEvaluationSeparatesRawModelFromCanonicalPipelineAccuracy(t *testing.T) {
	intent := Intent{Category: "education", Item: "安全编码训练", RequiredFormat: "视频课", PreferredStyle: "基础路线",
		Region: "CN-SOUTH", MaxPriceMinor: 3390, AllowSubstitution: true, ConfirmationNeeded: true}
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "course-1", MerchantID: "m1", Category: intent.Category, Item: intent.Item,
			Format: intent.RequiredFormat, Style: intent.PreferredStyle, Currency: "USDC", PriceMinor: 3390,
			Title: "安全编码训练 · 视频课 · 基础路线 · sample"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "m1", SKUID: "course-1", Region: intent.Region,
			OnHand: 1, Eligible: true, PriceMinor: 3390, Revision: 1}},
	}
	raw := intent
	raw.RequiredFormat = "录播课"
	task := Task{TaskID: "alias-1", Intent: intent, GoldSKUs: []string{"course-1"}, UniqueTarget: "course-1"}
	result := BatchIntentResult{TaskID: task.TaskID, RawIntent: &raw, Intent: intent,
		CanonicalizedFields: []string{"required_format_alias"}}
	report, trials, err := EvaluateParsedIntentBatch(catalog, []Task{task}, []BatchIntentResult{result}, 20, 1000, 7)
	if err != nil {
		t.Fatalf("EvaluateParsedIntentBatch: %v", err)
	}
	if report.ExactIntentMatches != 1 || report.RawModelExactMatches != 0 || report.RawModelExactRate != 0 {
		t.Fatalf("raw and normalized exact metrics conflated: %+v", report)
	}
	if report.Canonicalizations["required_format_alias"] != 1 || len(trials) != 1 ||
		!reflect.DeepEqual(trials[0].CanonicalizedFields, []string{"required_format_alias"}) {
		t.Fatalf("canonicalization audit missing: report=%+v trial=%+v", report.Canonicalizations, trials)
	}
}
