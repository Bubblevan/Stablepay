package agentshopping

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestChallengeHashesAreStableForCatalogOrderingAndBindTaskLabels(t *testing.T) {
	tasks := []ChallengeTask{{TaskID: "t1", Scenario: "FEASIBLE", Prompt: "buy one", ExpectedDisposition: ChallengePlan}}
	first := ImportedCatalog{
		Products:  []Product{{SKUID: "b", MerchantID: "m"}, {SKUID: "a", MerchantID: "m"}},
		Inventory: []Inventory{{MerchantID: "m", SKUID: "b", Region: "CN-WEST"}, {MerchantID: "m", SKUID: "a", Region: "CN-EAST"}},
	}
	second := ImportedCatalog{Products: []Product{first.Products[1], first.Products[0]},
		Inventory: []Inventory{first.Inventory[1], first.Inventory[0]}}
	one, err := ComputeChallengeHashes(first, tasks, []byte("source manifest"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := ComputeChallengeHashes(second, tasks, []byte("source manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("catalog input ordering changed canonical evidence hashes: first=%+v second=%+v", one, two)
	}
	changed := append([]ChallengeTask(nil), tasks...)
	changed[0].ExpectedDisposition = ChallengeNoPlan
	three, err := ComputeChallengeHashes(second, changed, []byte("source manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if three.TasksSHA256 == one.TasksSHA256 {
		t.Fatal("task hash did not change after changing its expected disposition")
	}
	changedRelevance := append([]ChallengeTask(nil), tasks...)
	changedRelevance[0].RelevantSKUs = []string{"a"}
	four, err := ComputeChallengeHashes(second, changedRelevance, []byte("source manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if four.TasksSHA256 == one.TasksSHA256 {
		t.Fatal("task hash did not bind the independent relevance labels")
	}
}

func TestChallengeEvaluationKeepsFeasibleNoPlanAndClarificationDenominatorsSeparate(t *testing.T) {
	fullIntent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
		Region: "CN-EAST", MaxPriceMinor: 1000, ConfirmationNeeded: true}
	missingItemIntent := fullIntent
	missingItemIntent.Item = ""
	missingItemIntent.ClarificationRequired = true
	missingItemIntent.ClarificationQuestion = "您想买哪种咖啡？"

	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Style: "floral", Currency: "USDC", PriceMinor: 500, Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 2, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	tasks := []ChallengeTask{
		{TaskID: "plan-good", Scenario: "FEASIBLE", Prompt: "买一袋咖啡豆", ExpectedDisposition: ChallengePlan,
			Intent: fullIntent, GoldSKUs: []string{"beans-1"}},
		{TaskID: "plan-parse-fail", Scenario: "FEASIBLE", Prompt: "买一袋咖啡豆", ExpectedDisposition: ChallengePlan,
			Intent: fullIntent, GoldSKUs: []string{"beans-1"}},
		{TaskID: "plan-wrong-parse", Scenario: "FEASIBLE", Prompt: "买一袋咖啡豆", ExpectedDisposition: ChallengePlan,
			Intent: fullIntent, GoldSKUs: []string{"beans-1"}},
		{TaskID: "no-plan-correct", Scenario: "BUDGET_INFEASIBLE", Prompt: "预算不超过 1 元", ExpectedDisposition: ChallengeNoPlan,
			Intent: Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
				Region: "CN-EAST", MaxPriceMinor: 100, ConfirmationNeeded: true}},
		{TaskID: "no-plan-false-positive", Scenario: "BUDGET_INFEASIBLE", Prompt: "预算不超过 1 元", ExpectedDisposition: ChallengeNoPlan,
			Intent: Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
				Region: "CN-EAST", MaxPriceMinor: 100, ConfirmationNeeded: true}},
		{TaskID: "clarify-correct", Scenario: "AMBIGUOUS", Prompt: "买点咖啡", ExpectedDisposition: ChallengeClarify,
			Intent: missingItemIntent, RequiredMissingFields: []string{"item"}},
		{TaskID: "clarify-false-plan", Scenario: "AMBIGUOUS", Prompt: "买点咖啡", ExpectedDisposition: ChallengeClarify,
			Intent: missingItemIntent, RequiredMissingFields: []string{"item"}},
		{TaskID: "clarify-generic-question", Scenario: "AMBIGUOUS", Prompt: "买点咖啡", ExpectedDisposition: ChallengeClarify,
			Intent: missingItemIntent, RequiredMissingFields: []string{"item"}},
	}
	wrongCategory := fullIntent
	wrongCategory.Category = "books"
	tooHighBudget := tasks[4].Intent
	tooHighBudget.MaxPriceMinor = 1000
	filledMissingIntent := fullIntent
	genericQuestionWithCorrectIntent := missingItemIntent
	genericQuestionWithCorrectIntent.ClarificationQuestion = "请补充完整购物要求。"
	results := []BatchIntentResult{
		{TaskID: "plan-good", Intent: fullIntent},
		{TaskID: "plan-parse-fail", Error: "invalid JSON"},
		{TaskID: "plan-wrong-parse", Intent: wrongCategory},
		{TaskID: "no-plan-correct", Intent: tasks[3].Intent},
		{TaskID: "no-plan-false-positive", Intent: tooHighBudget},
		{TaskID: "clarify-correct", Intent: missingItemIntent},
		{TaskID: "clarify-false-plan", Intent: filledMissingIntent},
		{TaskID: "clarify-generic-question", Intent: genericQuestionWithCorrectIntent},
	}

	report, trials, err := EvaluateChallengeTasks(catalog, tasks, results, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks: %v", err)
	}
	if report.TaskCount != 8 || report.PlanTasks != 3 || report.NoPlanTasks != 2 || report.ClarificationTasks != 3 {
		t.Fatalf("expected denominators must include parse failures and stay disposition-specific: %+v", report)
	}
	if report.CorrectDecisionRate.Numerator != 3 || report.CorrectDecisionRate.Denominator != 8 || report.CorrectDecisionRate.Rate != 3.0/8 {
		t.Fatalf("overall business-decision accuracy must include every disposition and count parse failures as misses: %+v", report.CorrectDecisionRate)
	}
	if report.ByScenario["FEASIBLE"].CorrectDecisionRate.Numerator != 1 || report.ByScenario["FEASIBLE"].CorrectDecisionRate.Denominator != 3 ||
		report.ByScenario["BUDGET_INFEASIBLE"].CorrectDecisionRate.Numerator != 1 || report.ByScenario["BUDGET_INFEASIBLE"].CorrectDecisionRate.Denominator != 2 ||
		report.ByScenario["AMBIGUOUS"].CorrectDecisionRate.Numerator != 1 || report.ByScenario["AMBIGUOUS"].CorrectDecisionRate.Denominator != 3 {
		t.Fatalf("scenario decision accuracy must preserve each scenario denominator: %+v", report.ByScenario)
	}
	if report.PlanTaskHitAtK.Numerator != 1 || report.PlanTaskHitAtK.Denominator != 3 ||
		report.ValidPlanRate.Numerator != 1 || report.ValidPlanRate.Denominator != 3 {
		t.Fatalf("feasible-task rates should preserve failed parses in denominator: task_hit=%+v valid=%+v", report.PlanTaskHitAtK, report.ValidPlanRate)
	}
	if report.PlanComparison.BaselineValidPlanRate.Numerator != 0 || report.PlanComparison.BaselineValidPlanRate.Denominator != 3 ||
		report.PlanComparison.TreatmentMinusBaselineValidPlanRate != 1.0/3 ||
		report.PlanComparison.PairedOutcomes.TreatmentOnly != 1 || report.PlanComparison.PairedOutcomes.NeitherValid != 2 {
		t.Fatalf("baseline/treatment comparison must be paired on all feasible tasks: %+v", report.PlanComparison)
	}
	if report.FeasibleSKURecallAtK.TaskCount != 3 || report.FeasibleSKURecallAtK.GoldSKUCount != 3 ||
		report.FeasibleSKURecallAtK.RetrievedGoldSKUCount != 1 || report.FeasibleSKURecallAtK.MacroRecallAtK != 1.0/3 ||
		report.FeasibleSKURecallAtK.MicroRecallAtK != 1.0/3 {
		t.Fatalf("SKU-level retrieval recall must include parse failures as misses: %+v", report.FeasibleSKURecallAtK)
	}
	if report.TreatmentFeasibleOfferSKURecallAtK.TaskCount != 3 || report.TreatmentFeasibleOfferSKURecallAtK.GoldSKUCount != 3 ||
		report.TreatmentFeasibleOfferSKURecallAtK.RetrievedGoldSKUCount != 1 ||
		report.TreatmentFeasibleOfferTaskHitAtK.Numerator != 1 || report.TreatmentFeasibleOfferTaskHitAtK.Denominator != 3 {
		t.Fatalf("post-filter offered SKU metrics must retain parse failures in the plan-task denominator: recall=%+v hit=%+v",
			report.TreatmentFeasibleOfferSKURecallAtK, report.TreatmentFeasibleOfferTaskHitAtK)
	}
	if report.TargetProductRecallAtK.Status != "NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS" ||
		report.TargetProductRecallAtK.LabeledPlanTaskCount != 0 || report.TargetProductRecallAtK.RelevantSKUCount != 0 ||
		report.TargetProductTaskHitAtK.Denominator != 0 {
		t.Fatalf("generated feasible labels must not be reported as independent product relevance: recall=%+v task_hit=%+v",
			report.TargetProductRecallAtK, report.TargetProductTaskHitAtK)
	}
	if report.CorrectNoPlanRate.Numerator != 1 || report.CorrectNoPlanRate.Denominator != 2 ||
		report.CorrectClarifyRate.Numerator != 1 || report.CorrectClarifyRate.Denominator != 3 ||
		report.ClarificationQuestionRate.Numerator != 2 || report.ClarificationQuestionRate.Denominator != 3 ||
		report.ClarificationQuestionCueRate.Numerator != 1 || report.ClarificationQuestionCueRate.Denominator != 3 {
		t.Fatalf("negative and clarification rates should use their own denominators: no-plan=%+v clarify=%+v", report.CorrectNoPlanRate, report.CorrectClarifyRate)
	}
	if report.UnexpectedPlanCount != 2 || report.ParseFailures != 1 ||
		report.ExactIntentRate.Numerator != 2 || report.ExactIntentRate.Denominator != 5 {
		t.Fatalf("unexpected plans, parse failures, or complete-intent accuracy were miscounted: %+v", report)
	}
	if len(trials) != len(tasks) || trials[1].ActualDisposition != ChallengeParseFail ||
		!trials[0].ValidPlan || trials[4].ActualDisposition != ChallengePlan || trials[6].CorrectDecision ||
		trials[7].CorrectDecision || !trials[5].ClarificationQuestionTargetsMissingFields ||
		trials[7].ClarificationQuestionTargetsMissingFields {
		t.Fatalf("per-task decision trace is incomplete or incorrect: %+v", trials)
	}
	for _, rate := range []ChallengeRate{report.CorrectDecisionRate, report.PlanTaskHitAtK, report.ValidPlanRate, report.CorrectNoPlanRate, report.CorrectClarifyRate} {
		if rate.CI95[0] < 0 || rate.CI95[1] > 1 || rate.CI95[0] > rate.Rate || rate.CI95[1] < rate.Rate {
			t.Errorf("invalid Wilson interval for rate %+v", rate)
		}
	}

	// The clarification-question metric is conditional on expected CLARIFY
	// tasks. Over-clarifying PLAN/NO_PLAN tasks must not inflate its numerator
	// beyond that denominator (which would make the Wilson interval NaN).
	overClarifiedResults := append([]BatchIntentResult(nil), results...)
	for i := range overClarifiedResults {
		if tasks[i].ExpectedDisposition == ChallengeClarify || overClarifiedResults[i].Error != "" {
			continue
		}
		intent := fullIntent
		intent.ClarificationRequired = true
		intent.ClarificationQuestion = "请补充更多信息。"
		overClarifiedResults[i].Intent = intent
	}
	overClarified, _, err := EvaluateChallengeTasks(catalog, tasks, overClarifiedResults, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks with over-clarification: %v", err)
	}
	if overClarified.ClarificationQuestionRate.Numerator != 2 || overClarified.ClarificationQuestionRate.Denominator != 3 ||
		overClarified.ClarificationQuestionRate.Rate != 2.0/3 || overClarified.ClarificationQuestionRate.CI95[1] > 1 {
		t.Fatalf("clarification-question rate must count only expected clarification tasks: %+v", overClarified.ClarificationQuestionRate)
	}
}

func TestClarificationQuestionMustTargetEveryDeclaredMissingField(t *testing.T) {
	tests := []struct {
		name     string
		question string
		fields   []string
		want     bool
	}{
		{name: "Chinese item question", question: "您想买哪种咖啡豆？", fields: []string{"item"}, want: true},
		{name: "English category and item question", question: "What kind of product are you looking for?", fields: []string{"category", "item"}, want: true},
		{name: "English item and format question", question: "Which product and package size do you need?", fields: []string{"item", "required_format"}, want: true},
		{name: "optional style is not a required clarification target", question: "您想买哪种商品？", fields: []string{"item"}, want: true},
		{name: "style is not a blocking question by itself", question: "您偏好什么风味？", fields: []string{"preferred_style"}, want: false},
		{name: "generic filler", question: "请补充完整购物要求。", fields: []string{"item"}, want: false},
		{name: "missing one of two fields", question: "您想买哪种商品？", fields: []string{"item", "required_format"}, want: false},
		{name: "declarative mention is not a question", question: "商品规格尚未提供。", fields: []string{"required_format"}, want: false},
		{name: "empty question", question: " ", fields: []string{"item"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clarificationQuestionTargetsMissingFields(test.question, test.fields); got != test.want {
				t.Fatalf("clarificationQuestionTargetsMissingFields(%q, %v)=%t, want %t", test.question, test.fields, got, test.want)
			}
		})
	}
}

func TestClarificationCannotBeBlockedOnlyByOptionalStyle(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 1000, ConfirmationNeeded: true, ClarificationRequired: true}
	task := ChallengeTask{TaskID: "style-only", Scenario: "STYLE_ONLY", Prompt: "买咖啡，偏好尚未说明",
		ExpectedDisposition: ChallengeClarify, Intent: intent, RequiredMissingFields: []string{"preferred_style"}}
	if err := ValidateChallengeTaskSet(ImportedCatalog{}, []ChallengeTask{task}); err == nil {
		t.Fatal("a soft-style-only omission must not be accepted as a blocking clarification task")
	}
	task.RequiredMissingFields = []string{"item", "preferred_style"}
	intent.Item = ""
	task.Intent = intent
	if err := ValidateChallengeTaskSet(ImportedCatalog{}, []ChallengeTask{task}); err == nil {
		t.Fatal("optional style must not be listed as a required missing field, even alongside a required item")
	}
	task.RequiredMissingFields = []string{"item"}
	if err := ValidateChallengeTaskSet(ImportedCatalog{}, []ChallengeTask{task}); err != nil {
		t.Fatalf("a missing required item may be clarified while style remains optional: %v", err)
	}
}

func TestChallengeEvaluationMeasuresDiscoveryRecallBeforeHardFiltering(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 1000, ConfirmationNeeded: true, RequiredAttributes: map[string]string{"roast_level": "light"}}
	products := make([]Product, 0, 21)
	offers := make([]Inventory, 0, 21)
	for index := 0; index < 20; index++ {
		sku := fmt.Sprintf("a-%02d", index)
		attributes := map[string]any{"roast_level": "dark"}
		if index == 0 || index == 1 {
			attributes["roast_level"] = "light"
		}
		products = append(products, Product{SKUID: sku, MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active", Attributes: attributes})
		price := int64(500)
		if index == 1 {
			price = 1500 // relevant listing, but outside the buyer's budget snapshot
		}
		offers = append(offers, Inventory{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: sku,
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: price, Revision: 1})
	}
	products = append(products, Product{SKUID: "z-target", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
		Format: "bag", Currency: "USDC", Status: "active", Attributes: map[string]any{"roast_level": "light"}})
	offers = append(offers, Inventory{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "z-target",
		Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1})
	catalog := ImportedCatalog{Products: products, Inventory: offers}
	task := ChallengeTask{TaskID: "partial-recall", Scenario: "DISCOVERY_CUTOFF", Prompt: "买一袋浅烘咖啡豆",
		ExpectedDisposition: ChallengePlan, Intent: intent, GoldSKUs: []string{"a-00", "z-target"},
		RelevantSKUs: []string{"a-00", "a-01", "z-target"}}
	report, trials, err := EvaluateChallengeTasks(catalog, []ChallengeTask{task}, []BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks: %v", err)
	}
	if report.SchemaVersion != "stablepay.agent-shopping-challenge.v9" || report.DiscoveryCandidateWindow != DefaultDiscoveryCandidateWindow {
		t.Fatalf("separate discovery/feasible-offer/relevance metrics and recall ceiling must use the v8 schema, report=%+v", report)
	}
	if len(trials) != 1 || trials[0].DiscoveryCandidateCount != 21 || trials[0].DiscoveryTopKCount != 20 || trials[0].CandidateCount != 2 ||
		trials[0].RetrievedGoldSKUCount != 2 || trials[0].RetrievedRelevantSKUCount != 3 ||
		trials[0].RetrievedTreatmentFeasibleGoldSKUCount != 2 || len(trials[0].TreatmentFeasibleOfferSKUs) != 2 ||
		!trials[0].GoldHitAtK || !trials[0].TargetProductHitAtK || !trials[0].ValidPlan {
		t.Fatalf("runtime-sized discovery window, Recall@K prefix, and feasibility filtering were not separated: trial=%+v", trials)
	}
	if report.TargetProductRecallAtK.Status != "AVAILABLE_UNVERIFIED_RELEVANCE_LABELS" || report.TargetProductRecallAtK.PlanTaskCount != 1 ||
		report.TargetProductRecallAtK.LabeledPlanTaskCount != 1 ||
		report.TargetProductRecallAtK.DeclaredDoubleReviewedTaskCount != 0 ||
		report.TargetProductRecallAtK.UnverifiedLabeledPlanTaskCount != 1 ||
		report.TargetProductTaskHitAtK.Status != report.TargetProductRecallAtK.Status ||
		report.FeasibleSKURecallAtK.GoldSKUCount != 2 || report.FeasibleSKURecallAtK.RetrievedGoldSKUCount != 2 ||
		report.FeasibleSKURecallAtK.MicroRecallAtK != 1 || report.FeasibleSKURecallAtK.MacroRecallAtK != 1 ||
		report.PlanTaskHitAtK.Rate != 1 || report.TreatmentFeasibleOfferTaskHitAtK.Rate != 1 ||
		report.TreatmentFeasibleOfferSKURecallAtK.MicroRecallAtK != 1 || report.TargetProductRecallAtK.RelevantSKUCount != 3 ||
		report.TargetProductRecallAtK.RetrievedRelevantSKUCount != 3 || report.TargetProductRecallAtK.MicroRecallAtK != 1 ||
		report.TargetProductTaskHitAtK.Rate != 1 {
		t.Fatalf("feasible-plan recall, human relevance recall, and task-hit should remain distinct: feasible=%+v target=%+v target_hit=%+v",
			report.FeasibleSKURecallAtK, report.TargetProductRecallAtK, report.TargetProductTaskHitAtK)
	}
	if report.TargetProductRecallAtK.MaxPossibleRetrievedRelevantSKUCount != 3 ||
		report.TargetProductRecallAtK.MaxPossibleMicroRecallAtK != 1 ||
		report.TargetProductRecallAtK.MaxPossibleMacroRecallAtK != 1 ||
		report.TargetProductRecallAtK.PlanTasksWithRelevantSKUCountAboveK != 0 {
		t.Fatalf("target recall report must expose its attainable ceiling at K=20: %+v", report.TargetProductRecallAtK)
	}
	if report.ByScenario[task.Scenario].TargetProductTaskHitAtK.Status != "AVAILABLE_UNVERIFIED_RELEVANCE_LABELS" {
		t.Fatalf("scenario target-product task-hit omitted its relevance-label status: %+v", report.ByScenario[task.Scenario].TargetProductTaskHitAtK)
	}
	encodedReport, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal challenge report: %v", err)
	}
	var serializedReport struct {
		TargetProductTaskHitAtK struct {
			Status string `json:"status"`
		} `json:"target_product_task_hit_at_k"`
	}
	if err := json.Unmarshal(encodedReport, &serializedReport); err != nil {
		t.Fatalf("unmarshal challenge report status: %v", err)
	}
	if serializedReport.TargetProductTaskHitAtK.Status != "AVAILABLE_UNVERIFIED_RELEVANCE_LABELS" {
		t.Fatalf("serialized target-product task-hit lost its provenance status: %s", encodedReport)
	}
	if _, _, err := EvaluateChallengeTasks(catalog, []ChallengeTask{task},
		[]BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, DefaultDiscoveryCandidateWindow+1); err == nil {
		t.Fatal("recall cutoff must not exceed the runtime discovery candidate window")
	}
	reviewedWithoutRelevance := task
	reviewedWithoutRelevance.Provenance = &ChallengeTaskProvenance{AcceptedSubstituteLabelsExplicit: true}
	reviewedWithoutRelevance.RelevantSKUs = nil
	if err := ValidateChallengeTaskSet(catalog, []ChallengeTask{reviewedWithoutRelevance}); err == nil {
		t.Fatal("human-reviewed plan task must not silently use feasible GoldSKUs as relevance labels")
	}
	planGoldNotRelevant := task
	planGoldNotRelevant.Provenance = &ChallengeTaskProvenance{AcceptedSubstituteLabelsExplicit: true}
	planGoldNotRelevant.RelevantSKUs = []string{"a-01", "z-target"}
	if err := ValidateChallengeTaskSet(catalog, []ChallengeTask{planGoldNotRelevant}); err == nil {
		t.Fatal("feasible plan SKUs must be included in the independently judged relevant set")
	}
	wrongAttributeRelevant := task
	wrongAttributeRelevant.RelevantSKUs = []string{"a-00", "a-01", "a-02", "z-target"}
	if err := ValidateChallengeTaskSet(catalog, []ChallengeTask{wrongAttributeRelevant}); err == nil {
		t.Fatal("relevance labels must honor the task's structured hard attributes")
	}
	unavailableCatalog := catalog
	unavailableCatalog.Products = append(append([]Product(nil), catalog.Products...), Product{SKUID: "unavailable-beans",
		MerchantID: "merchant-1", Category: "coffee", Item: "beans", Format: "bag", Currency: "USDC", Status: "active",
		Attributes: map[string]any{"roast_level": "light"}})
	unavailableRelevant := task
	unavailableRelevant.RelevantSKUs = append(append([]string(nil), task.RelevantSKUs...), "unavailable-beans")
	if err := ValidateChallengeTaskSet(unavailableCatalog, []ChallengeTask{unavailableRelevant}); err != nil {
		t.Fatalf("target relevance must not depend on current inventory availability: %v", err)
	}
	unrelatedCatalog := unavailableCatalog
	unrelatedCatalog.Products = append(append([]Product(nil), unavailableCatalog.Products...), Product{SKUID: "unrelated-tea",
		MerchantID: "merchant-1", Category: "coffee", Item: "tea", Format: "bag", Currency: "USDC", Status: "active",
		Attributes: map[string]any{"roast_level": "light"}})
	unrelatedRelevant := unavailableRelevant
	unrelatedRelevant.RelevantSKUs = append(append([]string(nil), unavailableRelevant.RelevantSKUs...), "unrelated-tea")
	if err := ValidateChallengeTaskSet(unrelatedCatalog, []ChallengeTask{unrelatedRelevant}); err == nil {
		t.Fatal("relevance labels must honor the requested item or an explicitly allowed substitution")
	}
	wrongAttributeGold := task
	wrongAttributeGold.RelevantSKUs = nil // legacy diagnostics still validate feasible-plan labels
	wrongAttributeGold.Intent.RequiredAttributes = map[string]string{"roast_level": "dark"}
	if err := ValidateChallengeTaskSet(catalog, []ChallengeTask{wrongAttributeGold}); err == nil {
		t.Fatal("feasible plan labels must honor the task's structured hard attributes")
	}
}

func TestTreatmentFeasibleOfferHitCanDifferFromRawDiscoveryHit(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 1000, ConfirmationNeeded: true}
	products := make([]Product, 0, 6)
	for index := 0; index < 5; index++ {
		products = append(products, Product{SKUID: fmt.Sprintf("a-unavailable-%02d", index), MerchantID: "merchant-1",
			Category: "coffee", Item: "beans", Format: "bag", Currency: "USDC", Status: "active"})
	}
	products = append(products, Product{SKUID: "z-available-gold", MerchantID: "merchant-1", Category: "coffee",
		Item: "beans", Format: "bag", Currency: "USDC", Status: "active"})
	catalog := ImportedCatalog{Products: products, Inventory: []Inventory{{TenantID: SimulationTenantID,
		MerchantID: "merchant-1", SKUID: "z-available-gold", Region: "CN-EAST", OnHand: 1,
		Eligible: true, PriceMinor: 500, Revision: 1}}}
	task := ChallengeTask{TaskID: "post-filter-hit", Scenario: "DISCOVERY_VS_OFFER", Prompt: "buy beans",
		ExpectedDisposition: ChallengePlan, Intent: intent, GoldSKUs: []string{"z-available-gold"}}
	report, trials, err := EvaluateChallengeTasks(catalog, []ChallengeTask{task}, []BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, 5)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks: %v", err)
	}
	if len(trials) != 1 || trials[0].GoldHitAtK || trials[0].RetrievedGoldSKUCount != 0 ||
		!trials[0].TreatmentFeasibleOfferGoldHitAtK || trials[0].RetrievedTreatmentFeasibleGoldSKUCount != 1 ||
		!reflect.DeepEqual(trials[0].TreatmentFeasibleOfferSKUs, []string{"z-available-gold"}) || !trials[0].ValidPlan {
		t.Fatalf("pre-filter discovery and post-filter feasible offers must be traced separately: %+v", trials)
	}
	if report.PlanTaskHitAtK.Rate != 0 || report.TreatmentFeasibleOfferTaskHitAtK.Rate != 1 ||
		report.TreatmentFeasibleOfferSKURecallAtK.MicroRecallAtK != 1 ||
		report.TreatmentFeasibleOfferSKURecallAtK.MacroRecallAtK != 1 {
		t.Fatalf("summary should credit the feasible offer without rewriting raw discovery recall: discovery=%+v treatment=%+v",
			report.PlanTaskHitAtK, report.TreatmentFeasibleOfferSKURecallAtK)
	}
}

func TestChallengeTargetRecallReportsSetCardinalityCeiling(t *testing.T) {
	relevant := make([]string, 25)
	for index := range relevant {
		relevant[index] = fmt.Sprintf("sku-%02d", index)
	}
	tasks := []ChallengeTask{{TaskID: "broad-relevance-set", Scenario: "BROAD", ExpectedDisposition: ChallengePlan,
		RelevantSKUs: relevant}}
	trials := []ChallengeTrial{{TaskID: "broad-relevance-set", RetrievedRelevantSKUCount: 20}}
	got := summarizeChallengeTargetProductRecall(tasks, trials, "", nil, 20)
	if got.MaxPossibleRetrievedRelevantSKUCount != 20 || got.MaxPossibleMicroRecallAtK != 0.8 ||
		got.MaxPossibleMacroRecallAtK != 0.8 || got.PlanTasksWithRelevantSKUCountAboveK != 1 {
		t.Fatalf("K=20 report must expose the mathematically reachable ceiling for 25 relevant SKUs: %+v", got)
	}
	if got.RetrievedRelevantSKUCount != 20 || got.MicroRecallAtK != 0.8 || got.MacroRecallAtK != 0.8 {
		t.Fatalf("observed recall should be directly comparable to the ceiling: %+v", got)
	}
}

func TestChallengeEvaluationComparesBaselineAndTreatmentOnSamePlanTask(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
		Region: "CN-EAST", MaxPriceMinor: 1000, ConfirmationNeeded: true}
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Style: "floral", Title: "beans · bag · floral · roast profile", Currency: "USDC",
			PriceMinor: 500, Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 2, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	task := ChallengeTask{TaskID: "baseline-and-treatment", Scenario: "STANDARD", Prompt: "Buy floral beans",
		ExpectedDisposition: ChallengePlan, Intent: intent, GoldSKUs: []string{"beans-1"}}
	report, trials, err := EvaluateChallengeTasks(catalog, []ChallengeTask{task},
		[]BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks: %v", err)
	}
	if len(trials) != 1 || !trials[0].BaselineValidPlan || !trials[0].ValidPlan ||
		trials[0].BaselineTopSKU != "beans-1" || trials[0].TopSKU != "beans-1" {
		t.Fatalf("both policies should independently select the valid gold SKU: %+v", trials)
	}
	if report.PlanComparison.BaselineValidPlanRate.Numerator != 1 || report.ValidPlanRate.Numerator != 1 ||
		report.PlanComparison.TreatmentMinusBaselineValidPlanRate != 0 || report.PlanComparison.PairedOutcomes.BothValid != 1 {
		t.Fatalf("paired policy summary is inconsistent: comparison=%+v treatment=%+v", report.PlanComparison, report.ValidPlanRate)
	}
	catalog.Inventory[0].Revision = 0
	report, trials, err = EvaluateChallengeTasks(catalog, []ChallengeTask{task},
		[]BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks with missing quote revision: %v", err)
	}
	if trials[0].CandidateCount != 1 || trials[0].ValidPlan || trials[0].BaselineValidPlan ||
		report.ValidPlanRate.Numerator != 0 || report.PlanComparison.BaselineValidPlanRate.Numerator != 0 {
		t.Fatalf("candidate discovery must not certify a plan without a live quote revision: report=%+v trial=%+v", report, trials[0])
	}
}

func TestChallengePlanComparisonCountsEachPairedOutcomeOnce(t *testing.T) {
	tasks := []ChallengeTask{
		{TaskID: "both", Scenario: "PAIR", ExpectedDisposition: ChallengePlan},
		{TaskID: "base-only", Scenario: "PAIR", ExpectedDisposition: ChallengePlan},
		{TaskID: "treatment-only", Scenario: "PAIR", ExpectedDisposition: ChallengePlan},
		{TaskID: "neither", Scenario: "PAIR", ExpectedDisposition: ChallengePlan},
	}
	trials := []ChallengeTrial{
		{TaskID: "both", BaselineValidPlan: true, ValidPlan: true},
		{TaskID: "base-only", BaselineValidPlan: true, ValidPlan: false},
		{TaskID: "treatment-only", BaselineValidPlan: false, ValidPlan: true},
		{TaskID: "neither", BaselineValidPlan: false, ValidPlan: false},
	}
	summary := summarizeChallengePlanComparison(tasks, trials, "PAIR")
	if summary.BaselineValidPlanRate.Numerator != 2 || summary.BaselineValidPlanRate.Denominator != 4 ||
		summary.TreatmentMinusBaselineValidPlanRate != 0 || summary.PairedOutcomes != (ChallengePairedPlanOutcomes{
		BothValid: 1, BaselineOnly: 1, TreatmentOnly: 1, NeitherValid: 1}) {
		t.Fatalf("paired outcomes/delta were miscounted: %+v", summary)
	}
}

func TestChallengeValidationRejectsMislabelledNoPlanAndMissingField(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 1000, ConfirmationNeeded: true}
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", PriceMinor: 500, Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	mislabelled := ChallengeTask{TaskID: "wrong-negative", Scenario: "BUDGET_INFEASIBLE", Prompt: "unavailable",
		ExpectedDisposition: ChallengeNoPlan, Intent: intent}
	if _, _, err := EvaluateChallengeTasks(catalog, []ChallengeTask{mislabelled}, []BatchIntentResult{{TaskID: mislabelled.TaskID, Intent: intent}}, 20); err == nil {
		t.Fatal("accepted no-plan gold label even though an eligible product exists")
	}
	clarification := ChallengeTask{TaskID: "wrong-clarification", Scenario: "AMBIGUOUS", Prompt: "ask",
		ExpectedDisposition: ChallengeClarify, Intent: intent, RequiredMissingFields: []string{"item"}}
	if _, _, err := EvaluateChallengeTasks(catalog, []ChallengeTask{clarification}, []BatchIntentResult{{TaskID: clarification.TaskID, Intent: intent}}, 20); err == nil {
		t.Fatal("accepted a field as missing even though the gold intent populates it")
	}
}

func TestChallengeEvaluatorCanCreditExplicitlyAcceptedSubstitute(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆", PreferredStyle: "花果香",
		Region: "CN-EAST", MaxPriceMinor: 1000, AllowSubstitution: true, ConfirmationNeeded: true}
	catalog := ImportedCatalog{
		Products: []Product{
			{SKUID: "requested-origin", MerchantID: "merchant-1", Category: "coffee", Item: "耶加雪菲",
				Format: "单品豆", Style: "花果香", Currency: "USDC", Status: "active"},
			{SKUID: "accepted-origin-substitute", MerchantID: "merchant-1", Category: "coffee", Item: "曼特宁",
				Format: "单品豆", Style: "坚果可可", Currency: "USDC", Status: "active"},
		},
		Inventory: []Inventory{
			{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "requested-origin", Region: "CN-EAST",
				OnHand: 0, Eligible: true, PriceMinor: 800, Revision: 1},
			{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "accepted-origin-substitute", Region: "CN-EAST",
				OnHand: 1, Eligible: true, PriceMinor: 900, Revision: 1},
		},
	}
	task := ChallengeTask{TaskID: "substitute-1", Scenario: "STOCKOUT_SUBSTITUTION", Prompt: "缺货时允许同规格替代",
		ExpectedDisposition: ChallengePlan, Intent: intent, GoldSKUs: []string{"accepted-origin-substitute"},
		AcceptedSubstituteSKUs: []string{"accepted-origin-substitute"}}
	report, trials, err := EvaluateChallengeTasks(catalog, []ChallengeTask{task}, []BatchIntentResult{{TaskID: task.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks with accepted substitute: %v", err)
	}
	if len(trials) != 1 || !trials[0].ValidPlan || !trials[0].AcceptedSubstituteHitAtK ||
		trials[0].TopMatchType != MatchTypeSubstitute || report.ValidSubstitutePlans != 1 || report.AcceptedSubstituteHitsAtK != 1 {
		t.Fatalf("explicit substitute was not separately traced and credited: report=%+v trial=%+v", report, trials)
	}

	withoutConsent := intent
	withoutConsent.ConfirmationNeeded = false
	report, trials, err = EvaluateChallengeTasks(catalog, []ChallengeTask{task},
		[]BatchIntentResult{{TaskID: task.TaskID, Intent: withoutConsent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks with missing confirmation guard: %v", err)
	}
	if trials[0].ValidPlan || report.ValidPlanRate.Numerator != 0 || trials[0].ExactIntentMatch {
		t.Fatalf("plan without buyer confirmation must not be credited: report=%+v trial=%+v", report, trials[0])
	}

	withoutExplicitAcceptance := task
	withoutExplicitAcceptance.AcceptedSubstituteSKUs = []string{}
	if err := ValidateChallengeTaskSet(catalog, []ChallengeTask{withoutExplicitAcceptance}); err == nil {
		t.Fatal("feasible substitute gold SKU was accepted without an explicit accepted-substitute label")
	}

	substituteOnlyCatalog := ImportedCatalog{
		Products: []Product{{SKUID: "accepted-origin-substitute", MerchantID: "merchant-1", Category: "coffee", Item: "曼特宁",
			Format: "单品豆", Style: "坚果可可", Currency: "USDC", Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "accepted-origin-substitute",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 900, Revision: 1}},
	}
	substituteOnly := task
	substituteOnly.RelevantSKUs = nil // no exact target listing exists in this catalog.
	report, trials, err = EvaluateChallengeTasks(substituteOnlyCatalog, []ChallengeTask{substituteOnly},
		[]BatchIntentResult{{TaskID: substituteOnly.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("EvaluateChallengeTasks with substitute-only discovery: %v", err)
	}
	if !trials[0].AcceptedSubstituteHitAtK || trials[0].TargetProductHitAtK ||
		report.TargetProductRecallAtK.Status != "NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS" {
		t.Fatalf("an accepted substitute must not inflate exact-target recall: report=%+v trial=%+v", report, trials[0])
	}

	misclassifiedAsTarget := substituteOnly
	misclassifiedAsTarget.RelevantSKUs = []string{"accepted-origin-substitute"}
	if err := ValidateChallengeTaskSet(substituteOnlyCatalog, []ChallengeTask{misclassifiedAsTarget}); err == nil {
		t.Fatal("accepted substitute was allowed into exact-target relevance labels")
	}
}
