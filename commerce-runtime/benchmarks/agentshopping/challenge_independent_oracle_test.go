package agentshopping

import "testing"

func TestIndependentTargetRelevanceUsesRawIntentAndExcludesFeasibility(t *testing.T) {
	intent := Intent{Category: "coffee", Item: "ethiopia beans", RequiredFormat: "whole-bean",
		PreferredStyle: "floral", RequiredAttributes: map[string]string{"origin": "Ethiopia"}}
	products := []Product{
		{SKUID: "exact-in-stock", Category: "coffee", Item: "Ethiopia Beans", Format: "whole-bean", Style: "floral",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"origin": "ethiopia"}},
		{SKUID: "exact-different-soft-style", Category: "coffee", Item: "ethiopia beans", Format: "whole-bean", Style: "chocolate",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"origin": "Ethiopia"}},
		{SKUID: "wrong-hard-attribute", Category: "coffee", Item: "ethiopia beans", Format: "whole-bean",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"origin": "Colombia"}},
		{SKUID: "substitute-not-target", Category: "coffee", Item: "sumatra", Format: "whole-bean",
			Currency: "USDC", Status: "active", Attributes: map[string]any{"origin": "Ethiopia"}},
		{SKUID: "inactive-exact", Category: "coffee", Item: "ethiopia beans", Format: "whole-bean",
			Currency: "USDC", Status: "inactive", Attributes: map[string]any{"origin": "Ethiopia"}},
	}
	got := independentTargetProductSet(products, intent)
	if len(got) != 2 || !got["exact-in-stock"] || !got["exact-different-soft-style"] {
		t.Fatalf("independent exact target set should ignore soft style and live feasibility but enforce hard raw fields: %v", sortedIndependentSKUs(got))
	}
}

func TestIndependentPlanOracleAuditsCandidateAgainstLiveOffer(t *testing.T) {
	expected := Intent{Category: "coffee", Item: "ethiopia beans", RequiredFormat: "whole-bean", Region: "CN-EAST",
		MaxPriceMinor: 700, AllowSubstitution: true, ConfirmationNeeded: true,
		RequiredAttributes: map[string]string{"origin": "Ethiopia"}}
	task := ChallengeTask{Intent: expected}
	products := []Product{
		{SKUID: "exact", MerchantID: "m1", Category: "coffee", Item: "ethiopia beans", Format: "whole-bean",
			Currency: "USDC", Status: "active", SubstitutionGroup: "single-origin", Attributes: map[string]any{"origin": "Ethiopia"}},
		{SKUID: "sub", MerchantID: "m2", Category: "coffee", Item: "sumatra", Format: "whole-bean",
			Currency: "USDC", Status: "active", SubstitutionGroup: "single-origin", Attributes: map[string]any{"origin": "Ethiopia"}},
	}
	parsed := expected
	offers := independentOfferIndex([]Inventory{
		{TenantID: SimulationTenantID, MerchantID: "m1", SKUID: "exact", Region: "CN-EAST", OnHand: 1,
			Eligible: true, PriceMinor: 650, Revision: 1},
		{TenantID: SimulationTenantID, MerchantID: "m2", SKUID: "sub", Region: "CN-EAST", OnHand: 1,
			Eligible: true, PriceMinor: 600, Revision: 2},
	})
	if !independentCandidateIsValid(task, parsed, products[0], products, offers) {
		t.Fatal("feasible exact offer should pass the independent oracle")
	}
	if !independentCandidateIsValid(task, parsed, products[1], products, offers) {
		t.Fatal("same explicit substitution group should pass when the buyer authorized substitutes")
	}
	noConsent := expected
	noConsent.AllowSubstitution = false
	if independentCandidateIsValid(ChallengeTask{Intent: noConsent}, noConsent, products[1], products, offers) {
		t.Fatal("substitute must fail when buyer did not allow substitution")
	}

	badOffer := independentOfferIndex([]Inventory{{MerchantID: "m1", SKUID: "exact", Region: "CN-EAST",
		OnHand: 0, Eligible: true, PriceMinor: 650, Revision: 1}})
	if independentCandidateIsValid(task, parsed, products[0], products, badOffer) {
		t.Fatal("out-of-stock candidate must fail even though treatment could have surfaced it")
	}
	badBudget := expected
	badBudget.MaxPriceMinor = 600
	if independentCandidateIsValid(ChallengeTask{Intent: badBudget}, badBudget, products[0], products, offers) {
		t.Fatal("over-budget candidate must fail the independent oracle")
	}
}

func TestIndependentPlanComparisonReportsPairedRates(t *testing.T) {
	tasks := []ChallengeTask{
		{TaskID: "t1", ExpectedDisposition: ChallengePlan},
		{TaskID: "substitute-only", ExpectedDisposition: ChallengePlan},
	}
	trials := []ChallengeTrial{
		{TaskID: "t1", IndependentRelevantProductCount: 2,
			IndependentBaselineRelevantHitsAtK: 0, IndependentTreatmentRelevantHitsAtK: 1,
			IndependentBaselinePlanValid: false, IndependentTreatmentPlanValid: true},
		{TaskID: "substitute-only", IndependentRelevantProductCount: 0,
			IndependentBaselinePlanValid: false, IndependentTreatmentPlanValid: true},
	}
	got := independentPlanComparison(tasks, trials)
	if got.PlanTaskCount != 2 || got.PlanTasksWithoutExactTarget != 1 ||
		got.BaselineValidPlanRate.Denominator != 2 || got.TreatmentValidPlanRate.Numerator != 2 ||
		got.ValidPlanRateDelta != 1 || got.ValidPlanPairedOutcomes.TreatmentOnly != 2 {
		t.Fatalf("unexpected independent plan comparison: %+v", got)
	}
	if got.TargetRecallScope == "" || got.BenchmarkBoundary == "" {
		t.Fatalf("report must disclose gated retrieval scope and synthetic-task boundary: %+v", got)
	}
	if got.BaselineTargetRecallAtK.RetrievedRelevantCount != 0 ||
		got.TreatmentTargetRecallAtK.RetrievedRelevantCount != 1 ||
		got.BaselineTargetRecallAtK.TaskCount != 1 || got.TargetRecallPairedOutcomes.TreatmentOnly != 1 ||
		got.TargetRecallPairedOutcomes.NeitherHit != 0 {
		t.Fatalf("unexpected independent target recall comparison: %+v", got)
	}
}
