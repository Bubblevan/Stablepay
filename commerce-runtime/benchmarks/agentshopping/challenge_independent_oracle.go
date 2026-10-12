package agentshopping

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The synthetic shopping oracle intentionally does not call candidateMatchType,
// requiredAttributesMatch, feasibleChallengeProducts, or rankTreatment. It
// evaluates frozen user constraints directly against raw catalog and inventory
// facts so the treatment cannot grade itself through a shared filter helper.
const independentShoppingOracleVersion = "raw-catalog-contract-v1"

type IndependentRecallSummary struct {
	TaskCount              int           `json:"task_count"`
	RelevantProductCount   int           `json:"relevant_product_count"`
	RetrievedRelevantCount int           `json:"retrieved_relevant_product_count"`
	MicroRecallAtK         float64       `json:"micro_recall_at_k"`
	MacroRecallAtK         float64       `json:"macro_recall_at_k"`
	MacroRecallAtKCI95     [2]float64    `json:"macro_recall_at_k_ci95_task_bootstrap"`
	TaskHitCount           int           `json:"task_hit_count"`
	TaskHitRateAtK         ChallengeRate `json:"task_hit_rate_at_k"`
}

type IndependentRecallPairedOutcomes struct {
	TreatmentOnly int `json:"treatment_only_hit"`
	BaselineOnly  int `json:"baseline_only_hit"`
	BothHit       int `json:"both_hit"`
	NeitherHit    int `json:"neither_hit"`
}

type IndependentShoppingOracleSummary struct {
	Version                     string                          `json:"oracle_version"`
	RelevanceDefinition         string                          `json:"relevance_definition"`
	TargetRecallScope           string                          `json:"target_recall_scope"`
	ValidPlanDefinition         string                          `json:"valid_plan_definition"`
	BenchmarkBoundary           string                          `json:"benchmark_boundary"`
	PlanTaskCount               int                             `json:"plan_task_count"`
	PlanTasksWithoutExactTarget int                             `json:"plan_tasks_without_exact_target"`
	BaselineTargetRecallAtK     IndependentRecallSummary        `json:"baseline_target_product_recall_at_k"`
	TreatmentTargetRecallAtK    IndependentRecallSummary        `json:"treatment_target_product_recall_at_k"`
	TargetRecallPairedOutcomes  IndependentRecallPairedOutcomes `json:"target_recall_paired_outcomes"`
	BaselineValidPlanRate       ChallengeRate                   `json:"baseline_valid_plan_rate"`
	TreatmentValidPlanRate      ChallengeRate                   `json:"treatment_valid_plan_rate"`
	ValidPlanRateDelta          float64                         `json:"valid_plan_rate_delta_treatment_minus_baseline"`
	ValidPlanRateDeltaCI95      [2]float64                      `json:"valid_plan_rate_delta_ci95_task_bootstrap"`
	ValidPlanPairedOutcomes     ChallengePairedPlanOutcomes     `json:"valid_plan_paired_outcomes"`
}

type independentRecallAccumulator struct {
	tasks       int
	relevant    int
	hits        int
	taskHits    int
	macroValues []float64
}

func (a *independentRecallAccumulator) add(relevant, hits int) {
	if relevant <= 0 {
		return
	}
	a.tasks++
	a.relevant += relevant
	a.hits += hits
	a.macroValues = append(a.macroValues, float64(hits)/float64(relevant))
	if hits > 0 {
		a.taskHits++
	}
}

func (a independentRecallAccumulator) finish(seed int64) IndependentRecallSummary {
	result := IndependentRecallSummary{TaskCount: a.tasks, RelevantProductCount: a.relevant,
		RetrievedRelevantCount: a.hits, TaskHitCount: a.taskHits,
		TaskHitRateAtK: challengeRate(a.taskHits, a.tasks)}
	if a.relevant > 0 {
		result.MicroRecallAtK = float64(a.hits) / float64(a.relevant)
	}
	if len(a.macroValues) > 0 {
		result.MacroRecallAtK, result.MacroRecallAtKCI95 = bootstrapMeanCI(a.macroValues, 10000, seed)
	}
	return result
}

type independentOfferKey struct {
	merchant string
	sku      string
	region   string
}

func independentOfferIndex(inventory []Inventory) map[independentOfferKey]Inventory {
	result := make(map[independentOfferKey]Inventory, len(inventory))
	for _, offer := range inventory {
		result[independentOfferKey{merchant: offer.MerchantID, sku: offer.SKUID, region: offer.Region}] = offer
	}
	return result
}

func independentTargetProductSet(products []Product, intent Intent) map[string]bool {
	result := make(map[string]bool)
	for _, product := range products {
		if product.Status != "active" || product.Currency != "USDC" ||
			!sameRawText(product.Category, intent.Category) || !sameRawText(product.Item, intent.Item) ||
			!sameRawText(product.Format, intent.RequiredFormat) || !independentAttributesMatch(product.Attributes, intent.RequiredAttributes) {
			continue
		}
		result[product.SKUID] = true
	}
	return result
}

func independentCandidateIsValid(task ChallengeTask, parsed Intent, candidate Product,
	products []Product, offers map[independentOfferKey]Inventory) bool {
	if !independentHardIntentMatches(parsed, task.Intent) || !task.Intent.ConfirmationNeeded ||
		candidate.Status != "active" || candidate.Currency != "USDC" ||
		!independentAttributesMatch(candidate.Attributes, task.Intent.RequiredAttributes) {
		return false
	}

	exact := sameRawText(candidate.Category, task.Intent.Category) &&
		sameRawText(candidate.Item, task.Intent.Item) &&
		sameRawText(candidate.Format, task.Intent.RequiredFormat)
	if !exact {
		if task.AcceptedSubstituteSKUs != nil {
			if !stringSet(task.AcceptedSubstituteSKUs)[candidate.SKUID] {
				return false
			}
		} else if !independentSubstituteAllowed(candidate, products, task.Intent) {
			return false
		}
	}
	offer, ok := offers[independentOfferKey{merchant: candidate.MerchantID, sku: candidate.SKUID, region: task.Intent.Region}]
	if !ok || !sameRawText(offer.Region, task.Intent.Region) || !offer.Eligible ||
		offer.OnHand-offer.Reserved < 1 || offer.PriceMinor <= 0 || offer.PriceMinor > task.Intent.MaxPriceMinor || offer.Revision <= 0 {
		return false
	}
	return true
}

func independentSubstituteAllowed(candidate Product, products []Product, intent Intent) bool {
	if !intent.AllowSubstitution || candidate.SubstitutionGroup == "" ||
		!sameRawText(candidate.Category, intent.Category) || !sameRawText(candidate.Format, intent.RequiredFormat) {
		return false
	}
	for _, requested := range products {
		if requested.Status == "active" && requested.Currency == "USDC" && requested.SubstitutionGroup != "" &&
			sameRawText(requested.Category, intent.Category) && sameRawText(requested.Item, intent.Item) &&
			sameRawText(requested.Format, intent.RequiredFormat) &&
			independentAttributesMatch(requested.Attributes, intent.RequiredAttributes) &&
			sameRawText(candidate.SubstitutionGroup, requested.SubstitutionGroup) {
			return true
		}
	}
	return false
}

func independentHardIntentMatches(actual, expected Intent) bool {
	if !sameRawText(actual.Category, expected.Category) || !sameRawText(actual.Item, expected.Item) ||
		!sameRawText(actual.RequiredFormat, expected.RequiredFormat) || !sameRawText(actual.Region, expected.Region) ||
		actual.MaxPriceMinor != expected.MaxPriceMinor || actual.AllowSubstitution != expected.AllowSubstitution ||
		actual.ConfirmationNeeded != expected.ConfirmationNeeded || actual.ClarificationRequired {
		return false
	}
	if len(actual.RequiredAttributes) != len(expected.RequiredAttributes) {
		return false
	}
	for key, expectedValue := range expected.RequiredAttributes {
		actualValue, exists := actual.RequiredAttributes[key]
		if !exists || !sameRawText(actualValue, expectedValue) {
			return false
		}
	}
	return true
}

func independentAttributesMatch(attributes map[string]any, required map[string]string) bool {
	for key, expected := range required {
		if !rawAttributeContains(attributes[key], expected) {
			return false
		}
	}
	return true
}

func rawAttributeContains(raw any, expected string) bool {
	switch value := raw.(type) {
	case string:
		return sameRawText(value, expected)
	case []string:
		for _, item := range value {
			if sameRawText(item, expected) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if rawAttributeContains(item, expected) {
				return true
			}
		}
	case float64, float32, int, int32, int64, bool:
		return sameRawText(fmt.Sprint(value), expected)
	}
	return false
}

func sameRawText(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func independentTopSKUs(products []Product, cutoff int) []string {
	if len(products) > cutoff {
		products = products[:cutoff]
	}
	result := make([]string, len(products))
	for index, product := range products {
		result[index] = product.SKUID
	}
	return result
}

func independentHitsAtK(top []string, relevant map[string]bool) int {
	hits := 0
	for _, sku := range top {
		if relevant[sku] {
			hits++
		}
	}
	return hits
}

func independentPlanComparison(tasks []ChallengeTask, trials []ChallengeTrial) IndependentShoppingOracleSummary {
	trialByID := make(map[string]ChallengeTrial, len(trials))
	for _, trial := range trials {
		trialByID[trial.TaskID] = trial
	}
	var baselineRecall, treatmentRecall independentRecallAccumulator
	var planCount, noExactTargetCount, baselineValid, treatmentValid int
	var paired ChallengePairedPlanOutcomes
	planDeltas := make([]float64, 0)
	var recallPaired IndependentRecallPairedOutcomes
	for _, task := range tasks {
		if task.ExpectedDisposition != ChallengePlan {
			continue
		}
		planCount++
		trial := trialByID[task.TaskID]
		baselineHits := trial.IndependentBaselineRelevantHitsAtK
		treatmentHits := trial.IndependentTreatmentRelevantHitsAtK
		relevant := trial.IndependentRelevantProductCount
		if relevant == 0 {
			noExactTargetCount++
		} else {
			baselineRecall.add(relevant, baselineHits)
			treatmentRecall.add(relevant, treatmentHits)
			baselineHit, treatmentHit := baselineHits > 0, treatmentHits > 0
			switch {
			case treatmentHit && !baselineHit:
				recallPaired.TreatmentOnly++
			case baselineHit && !treatmentHit:
				recallPaired.BaselineOnly++
			case baselineHit && treatmentHit:
				recallPaired.BothHit++
			default:
				recallPaired.NeitherHit++
			}
		}
		baseValid, treatmentPlanValid := trial.IndependentBaselinePlanValid, trial.IndependentTreatmentPlanValid
		if baseValid {
			baselineValid++
		}
		if treatmentPlanValid {
			treatmentValid++
		}
		planDeltas = append(planDeltas, float64(boolToInt(treatmentPlanValid)-boolToInt(baseValid)))
		switch {
		case baseValid && treatmentPlanValid:
			paired.BothValid++
		case baseValid:
			paired.BaselineOnly++
		case treatmentPlanValid:
			paired.TreatmentOnly++
		default:
			paired.NeitherValid++
		}
	}
	result := IndependentShoppingOracleSummary{
		Version:             independentShoppingOracleVersion,
		RelevanceDefinition: "Active USDC listings with exact category/item/format and all explicitly hard required attributes; inventory, region, price, and soft style preferences do not define relevance.",
		TargetRecallScope:   "Top-K is drawn from the existing discovery candidate list after category/item/format and allowed-substitution gating; this is not full-catalog retrieval recall. Task Hit@K may saturate when generated prompts name an exact catalog item.",
		ValidPlanDefinition: "Top-1 candidate is exact or belongs to the requested product's explicit substitution_group with buyer substitution permission, satisfies hard parsed intent and confirmation requirement, and has an active USDC offer in the requested region with stock, valid revision, and price within budget.",
		BenchmarkBoundary:   "Tasks and PLAN dispositions are automatically generated from the same pinned synthetic catalog and deterministic inventory/price overlay; prompts and labels have no independent human review. These paired scores are synthetic development diagnostics, not a resume-quality independent holdout.",
		PlanTaskCount:       planCount, PlanTasksWithoutExactTarget: noExactTargetCount,
		BaselineTargetRecallAtK:  baselineRecall.finish(0x243f6a88),
		TreatmentTargetRecallAtK: treatmentRecall.finish(0x85a308d3), TargetRecallPairedOutcomes: recallPaired,
		BaselineValidPlanRate:   challengeRate(baselineValid, planCount),
		TreatmentValidPlanRate:  challengeRate(treatmentValid, planCount),
		ValidPlanPairedOutcomes: paired,
	}
	if planCount > 0 {
		result.ValidPlanRateDelta = float64(treatmentValid-baselineValid) / float64(planCount)
		_, result.ValidPlanRateDeltaCI95 = bootstrapMeanCI(planDeltas, 10000, 0x13198a2e)
	}
	if math.IsNaN(result.ValidPlanRateDelta) || math.IsInf(result.ValidPlanRateDelta, 0) {
		result.ValidPlanRateDelta = 0
	}
	return result
}

func sortedIndependentSKUs(set map[string]bool) []string {
	result := make([]string, 0, len(set))
	for sku := range set {
		result = append(result, sku)
	}
	sort.Strings(result)
	return result
}
