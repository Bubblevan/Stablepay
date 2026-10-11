package agentshopping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

const DefaultRecallCutoff = 20

var ErrInvalidEvaluationSuite = errors.New("invalid shopping evaluation suite")

type EvaluationHashes struct {
	CatalogSHA256  string `json:"catalog_sha256"`
	OverlaySHA256  string `json:"overlay_sha256"`
	TasksSHA256    string `json:"tasks_sha256"`
	ManifestSHA256 string `json:"source_manifest_sha256"`
}

type EvaluationSummary struct {
	Policy                 string     `json:"policy"`
	TaskCount              int        `json:"task_count"`
	RecallCutoff           int        `json:"recall_cutoff"`
	RecallHits             int        `json:"recall_hits"`
	RelevantGoldSKUs       int        `json:"relevant_gold_skus"`
	MicroRecallAtK         float64    `json:"micro_recall_at_k"`
	MacroRecallAtK         float64    `json:"macro_recall_at_k"`
	MacroRecallAtKCI95     [2]float64 `json:"macro_recall_at_k_ci95"`
	TaskHitCount           int        `json:"tasks_with_any_gold_in_top_k"`
	TaskHitRate            float64    `json:"task_hit_rate_at_k"`
	TaskHitRateCI95        [2]float64 `json:"task_hit_rate_at_k_ci95"`
	ValidPlans             int        `json:"valid_plans"`
	ValidPlanRate          float64    `json:"valid_plan_rate"`
	ValidPlanRateCI95      [2]float64 `json:"valid_plan_rate_ci95"`
	NoCandidateTasks       int        `json:"no_candidate_tasks"`
	CandidateCountTotal    int        `json:"candidate_count_total"`
	CandidateCountMean     float64    `json:"candidate_count_mean"`
	SearchLatencyP50Millis float64    `json:"in_memory_search_latency_p50_ms"`
	SearchLatencyP95Millis float64    `json:"in_memory_search_latency_p95_ms"`
}

type EvaluationReport struct {
	SchemaVersion                           string             `json:"schema_version"`
	Evaluation                              string             `json:"evaluation"`
	DataOrigin                              string             `json:"data_origin"`
	Provider                                string             `json:"provider"`
	Model                                   string             `json:"model"`
	Seed                                    int64              `json:"seed"`
	ProductCount                            int                `json:"product_count"`
	InventoryRows                           int                `json:"inventory_rows"`
	TaskCount                               int                `json:"task_count"`
	Hashes                                  EvaluationHashes   `json:"hashes"`
	Baseline                                EvaluationSummary  `json:"baseline"`
	Treatment                               EvaluationSummary  `json:"treatment"`
	RecallDeltaTreatmentMinusBaseline       float64            `json:"paired_macro_recall_delta_treatment_minus_baseline"`
	RecallDeltaTreatmentMinusBaselineCI95   [2]float64         `json:"paired_macro_recall_delta_ci95"`
	PlanRateDeltaTreatmentMinusBaseline     float64            `json:"paired_valid_plan_rate_delta_treatment_minus_baseline"`
	PlanRateDeltaTreatmentMinusBaselineCI95 [2]float64         `json:"paired_valid_plan_rate_delta_ci95"`
	TaskHitDeltaTreatmentMinusBaseline      float64            `json:"paired_task_hit_rate_delta_treatment_minus_baseline"`
	TaskHitDeltaTreatmentMinusBaselineCI95  [2]float64         `json:"paired_task_hit_rate_delta_ci95"`
	PairedPlanOutcomes                      PairedPlanOutcomes `json:"paired_plan_outcomes"`
}

type PairedPlanOutcomes struct {
	BothValid     int `json:"both_valid"`
	BaselineOnly  int `json:"baseline_only_valid"`
	TreatmentOnly int `json:"treatment_only_valid"`
	NeitherValid  int `json:"neither_valid"`
}

type PolicyTrial struct {
	CandidateCount int      `json:"candidate_count"`
	TopSKU         string   `json:"top_sku,omitempty"`
	MatchType      string   `json:"match_type,omitempty"`
	RecallHits     int      `json:"recall_hits"`
	GoldCount      int      `json:"gold_count"`
	TaskHit        bool     `json:"task_hit_at_k"`
	PlanValid      bool     `json:"plan_valid"`
	FailureReasons []string `json:"failure_reasons,omitempty"`
}

type EvaluationTrial struct {
	TaskID    string      `json:"task_id"`
	Source    string      `json:"source_merchant_id"`
	Scenario  string      `json:"scenario,omitempty"`
	Baseline  PolicyTrial `json:"baseline"`
	Treatment PolicyTrial `json:"treatment"`
}

type rankedOffer struct {
	product   Product
	offer     Inventory
	score     int
	matchType string
}

func ValidateLockedSuite(catalog ImportedCatalog, tasks []Task) error {
	if len(catalog.Merchants) != 12 || len(catalog.Products) != 24000 || len(catalog.Inventory) != 96000 || len(tasks) != 500 {
		return fmt.Errorf("locked suite requires 12 merchants, 24,000 products, 96,000 inventory rows and 500 tasks: %w", ErrInvalidEvaluationSuite)
	}
	products := make(map[string]Product, len(catalog.Products))
	for _, product := range catalog.Products {
		if product.SKUID == "" || products[product.SKUID].SKUID != "" {
			return fmt.Errorf("empty or duplicate product SKU %q: %w", product.SKUID, ErrInvalidEvaluationSuite)
		}
		products[product.SKUID] = product
	}
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, offer := range catalog.Inventory {
		key := inventoryKey(SimulationTenantID, offer.MerchantID, offer.SKUID, offer.Region)
		if offer.Region == "" || offer.SKUID == "" || offers[key].SKUID != "" {
			return fmt.Errorf("empty or duplicate inventory identity for %q: %w", offer.SKUID, ErrInvalidEvaluationSuite)
		}
		offers[key] = offer
	}
	taskIDs, targets, goldSets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	merchantCounts := make(map[string]int, len(catalog.Merchants))
	for _, task := range tasks {
		if task.TaskID == "" || taskIDs[task.TaskID] || task.SourceMerchantID == "" || task.Intent.ConfirmationNeeded != true ||
			task.Intent.Category == "" || task.Intent.Item == "" || task.Intent.RequiredFormat == "" ||
			task.Intent.PreferredStyle == "" || task.Intent.Region == "" || task.Intent.MaxPriceMinor <= 0 ||
			len(task.GoldSKUs) == 0 || task.UniqueTarget == "" || targets[task.UniqueTarget] {
			return fmt.Errorf("task identity, intent, or gold target invariant failed for %q: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		taskIDs[task.TaskID], targets[task.UniqueTarget] = true, true
		merchantCounts[task.SourceMerchantID]++
		gold := make(map[string]bool, len(task.GoldSKUs))
		goldSetKey := strings.Join(task.GoldSKUs, "\x00")
		if goldSets[goldSetKey] {
			return fmt.Errorf("task %s duplicates a gold SKU set: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		goldSets[goldSetKey] = true
		for _, sku := range task.GoldSKUs {
			product, ok := products[sku]
			if !ok || gold[sku] || product.Category != task.Intent.Category || product.Item != task.Intent.Item ||
				product.Format != task.Intent.RequiredFormat || product.Style != task.Intent.PreferredStyle || product.Currency != "USDC" {
				return fmt.Errorf("task %s contains invalid gold SKU %s: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, sku, task.Intent.Region)]
			if !ok || !offer.Eligible || offer.OnHand < 1 || offer.PriceMinor > task.Intent.MaxPriceMinor {
				return fmt.Errorf("task %s gold SKU %s is not a feasible offer: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			gold[sku] = true
		}
		if !gold[task.UniqueTarget] {
			return fmt.Errorf("task %s unique target is not in its gold set: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	}
	for _, merchant := range catalog.Merchants {
		count := merchantCounts[merchant.MerchantID]
		if count != 41 && count != 42 {
			return fmt.Errorf("merchant %s has %d tasks, want 41 or 42: %w", merchant.MerchantID, count, ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

func EvaluateStructuredTasks(catalog ImportedCatalog, tasks []Task, cutoff, bootstrapSamples int, seed int64) (EvaluationReport, []EvaluationTrial, error) {
	if len(catalog.Products) == 0 || len(catalog.Inventory) == 0 || len(tasks) == 0 || cutoff <= 0 {
		return EvaluationReport{}, nil, ErrInvalidEvaluationSuite
	}
	products := make(map[string]Product, len(catalog.Products))
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, product := range catalog.Products {
		if _, exists := products[product.SKUID]; exists {
			return EvaluationReport{}, nil, fmt.Errorf("duplicate product %s: %w", product.SKUID, ErrInvalidEvaluationSuite)
		}
		products[product.SKUID] = product
	}
	for _, offer := range catalog.Inventory {
		key := inventoryKey(SimulationTenantID, offer.MerchantID, offer.SKUID, offer.Region)
		if _, exists := offers[key]; exists {
			return EvaluationReport{}, nil, fmt.Errorf("duplicate inventory offer %s/%s: %w", offer.SKUID, offer.Region, ErrInvalidEvaluationSuite)
		}
		offers[key] = offer
	}

	baselineAcc := newEvaluationAccumulator("B0 exact-title-tag + budget + price", len(tasks), cutoff)
	treatmentAcc := newEvaluationAccumulator("B1 hard-filter + live-stock-region-price + preference-rank", len(tasks), cutoff)
	trials := make([]EvaluationTrial, 0, len(tasks))
	pairedRecallDeltas := make([]float64, 0, len(tasks))
	pairedTaskHitDeltas := make([]float64, 0, len(tasks))
	pairedPlanDeltas := make([]float64, 0, len(tasks))
	pairedOutcomes := PairedPlanOutcomes{}
	seenIDs := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.TaskID == "" || seenIDs[task.TaskID] || len(task.GoldSKUs) == 0 || task.Intent.MaxPriceMinor <= 0 {
			return EvaluationReport{}, nil, fmt.Errorf("task identity or gold set invalid: %w", ErrInvalidEvaluationSuite)
		}
		seenIDs[task.TaskID] = true
		baselineStart := time.Now()
		baseline := rankBaseline(catalog.Products, task.Intent, cutoff)
		baselineLatency := time.Since(baselineStart)
		treatmentStart := time.Now()
		treatment := rankTreatment(catalog.Products, offers, task.Intent, cutoff)
		treatmentLatency := time.Since(treatmentStart)
		baselineTrial := makePolicyTrial(task, baseline, offers)
		treatmentTrial := makePolicyTrial(task, treatment, offers)
		baselineAcc.add(baselineTrial, baselineLatency)
		treatmentAcc.add(treatmentTrial, treatmentLatency)
		pairedRecallDeltas = append(pairedRecallDeltas,
			float64(treatmentTrial.RecallHits-baselineTrial.RecallHits)/float64(len(task.GoldSKUs)))
		pairedTaskHitDeltas = append(pairedTaskHitDeltas,
			float64(boolToInt(treatmentTrial.TaskHit)-boolToInt(baselineTrial.TaskHit)))
		pairedPlanDeltas = append(pairedPlanDeltas,
			float64(boolToInt(treatmentTrial.PlanValid)-boolToInt(baselineTrial.PlanValid)))
		switch {
		case baselineTrial.PlanValid && treatmentTrial.PlanValid:
			pairedOutcomes.BothValid++
		case baselineTrial.PlanValid:
			pairedOutcomes.BaselineOnly++
		case treatmentTrial.PlanValid:
			pairedOutcomes.TreatmentOnly++
		default:
			pairedOutcomes.NeitherValid++
		}
		trials = append(trials, EvaluationTrial{TaskID: task.TaskID, Source: task.SourceMerchantID,
			Scenario: task.Scenario, Baseline: baselineTrial, Treatment: treatmentTrial})
	}
	baselineSummary := baselineAcc.finish(bootstrapSamples, seed)
	treatmentSummary := treatmentAcc.finish(bootstrapSamples, seed+1)
	recallDelta, recallDeltaCI := bootstrapMeanCI(pairedRecallDeltas, bootstrapSamples, seed+2)
	taskHitDelta, taskHitDeltaCI := bootstrapMeanCI(pairedTaskHitDeltas, bootstrapSamples, seed+4)
	planDelta, planDeltaCI := bootstrapMeanCI(pairedPlanDeltas, bootstrapSamples, seed+3)
	return EvaluationReport{SchemaVersion: "stablepay.agent-shopping-eval.v2", Evaluation: "structured-gold-ranking",
		DataOrigin: "SYNTHETIC_SIMULATION", Provider: "not_used", Model: "not_used", Seed: catalog.Seed,
		ProductCount: len(catalog.Products), InventoryRows: len(catalog.Inventory), TaskCount: len(tasks),
		Baseline: baselineSummary, Treatment: treatmentSummary,
		RecallDeltaTreatmentMinusBaseline: recallDelta, RecallDeltaTreatmentMinusBaselineCI95: recallDeltaCI,
		TaskHitDeltaTreatmentMinusBaseline: taskHitDelta, TaskHitDeltaTreatmentMinusBaselineCI95: taskHitDeltaCI,
		PlanRateDeltaTreatmentMinusBaseline: planDelta, PlanRateDeltaTreatmentMinusBaselineCI95: planDeltaCI,
		PairedPlanOutcomes: pairedOutcomes}, trials, nil
}

func rankBaseline(products []Product, intent Intent, limit int) []rankedOffer {
	query := []string{intent.Item, intent.RequiredFormat, intent.PreferredStyle}
	candidates := make([]rankedOffer, 0, 32)
	for _, product := range products {
		if product.Category != intent.Category || product.Currency != "USDC" || product.PriceMinor > intent.MaxPriceMinor {
			continue
		}
		text := strings.ToLower(product.Title + " " + strings.Join(product.Tags, " "))
		matched := true
		for _, term := range query {
			if term == "" || !strings.Contains(text, strings.ToLower(term)) {
				matched = false
				break
			}
		}
		if matched {
			candidates = append(candidates, rankedOffer{product: product, offer: Inventory{MerchantID: product.MerchantID,
				SKUID: product.SKUID, PriceMinor: product.PriceMinor}})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].offer.PriceMinor != candidates[j].offer.PriceMinor {
			return candidates[i].offer.PriceMinor < candidates[j].offer.PriceMinor
		}
		return candidates[i].product.SKUID < candidates[j].product.SKUID
	})
	return truncateRanked(candidates, limit)
}

func rankTreatment(products []Product, offers map[string]Inventory, intent Intent, limit int) []rankedOffer {
	candidates := make([]rankedOffer, 0, 32)
	for _, product := range products {
		matchType := candidateMatchType(product, intent)
		if matchType == "" || product.Currency != "USDC" {
			continue
		}
		offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, intent.Region)]
		if !ok || !offer.Eligible || offer.OnHand-offer.Reserved < 1 || offer.PriceMinor > intent.MaxPriceMinor {
			continue
		}
		score := stylePreferenceScore(product, intent.PreferredStyle)
		candidates = append(candidates, rankedOffer{product: product, offer: offer, score: score, matchType: matchType})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].matchType != candidates[j].matchType {
			return matchTypePriority(candidates[i].matchType) > matchTypePriority(candidates[j].matchType)
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].offer.PriceMinor != candidates[j].offer.PriceMinor {
			return candidates[i].offer.PriceMinor < candidates[j].offer.PriceMinor
		}
		return candidates[i].product.SKUID < candidates[j].product.SKUID
	})
	return truncateRanked(candidates, limit)
}

func stylePreferenceScore(product Product, preferred string) int {
	if product.Style == preferred {
		return 2
	}
	if preferred != "" && strings.Contains(strings.ToLower(strings.Join(product.Tags, " ")), strings.ToLower(preferred)) {
		return 1
	}
	return 0
}

func truncateRanked(candidates []rankedOffer, limit int) []rankedOffer {
	if limit > 0 && len(candidates) > limit {
		return candidates[:limit]
	}
	return candidates
}

func makePolicyTrial(task Task, candidates []rankedOffer, offers map[string]Inventory) PolicyTrial {
	gold := make(map[string]bool, len(task.GoldSKUs))
	for _, sku := range task.GoldSKUs {
		gold[sku] = true
	}
	trial := PolicyTrial{CandidateCount: len(candidates), GoldCount: len(gold)}
	for _, candidate := range candidates {
		if gold[candidate.product.SKUID] {
			trial.RecallHits++
		}
	}
	trial.TaskHit = trial.RecallHits > 0
	if len(candidates) == 0 {
		trial.FailureReasons = []string{"NO_CANDIDATE"}
		return trial
	}
	selected := candidates[0]
	trial.TopSKU = selected.product.SKUID
	trial.MatchType = selected.matchType
	if !gold[selected.product.SKUID] {
		trial.FailureReasons = append(trial.FailureReasons, "SKU_NOT_IN_GOLD_SET")
	}
	if candidateMatchType(selected.product, task.Intent) == "" {
		trial.FailureReasons = append(trial.FailureReasons, "HARD_ATTRIBUTE_MISMATCH")
	}
	if selected.product.Currency != "USDC" {
		trial.FailureReasons = append(trial.FailureReasons, "CURRENCY_MISMATCH")
	}
	offer, ok := offers[inventoryKey(SimulationTenantID, selected.product.MerchantID, selected.product.SKUID, task.Intent.Region)]
	if !ok || offer.Region != task.Intent.Region {
		trial.FailureReasons = append(trial.FailureReasons, "REGION_INELIGIBLE")
	} else {
		if !offer.Eligible || offer.OnHand-offer.Reserved < 1 {
			trial.FailureReasons = append(trial.FailureReasons, "UNAVAILABLE")
		}
		if offer.PriceMinor > task.Intent.MaxPriceMinor {
			trial.FailureReasons = append(trial.FailureReasons, "OVER_BUDGET")
		}
		if offer.Revision <= 0 {
			trial.FailureReasons = append(trial.FailureReasons, "MISSING_QUOTE_REVISION")
		}
	}
	if !task.Intent.ConfirmationNeeded {
		trial.FailureReasons = append(trial.FailureReasons, "BUYER_CONFIRMATION_NOT_REQUIRED")
	}
	trial.PlanValid = len(trial.FailureReasons) == 0
	return trial
}

type evaluationAccumulator struct {
	policy         string
	tasks          int
	cutoff         int
	recallHits     int
	relevant       int
	taskHitCount   int
	validPlans     int
	noCandidates   int
	candidateTotal int
	macroRecall    []float64
	taskHits       []float64
	planSuccess    []float64
	latencyMillis  []float64
}

func newEvaluationAccumulator(policy string, tasks, cutoff int) *evaluationAccumulator {
	return &evaluationAccumulator{policy: policy, tasks: tasks, cutoff: cutoff,
		macroRecall: make([]float64, 0, tasks), taskHits: make([]float64, 0, tasks),
		planSuccess: make([]float64, 0, tasks), latencyMillis: make([]float64, 0, tasks)}
}

func (a *evaluationAccumulator) add(trial PolicyTrial, latency time.Duration) {
	a.recallHits += trial.RecallHits
	a.relevant += trial.GoldCount
	a.taskHitCount += boolToInt(trial.TaskHit)
	a.validPlans += boolToInt(trial.PlanValid)
	a.noCandidates += boolToInt(trial.CandidateCount == 0)
	a.candidateTotal += trial.CandidateCount
	if trial.GoldCount > 0 {
		a.macroRecall = append(a.macroRecall, float64(trial.RecallHits)/float64(trial.GoldCount))
	} else {
		a.macroRecall = append(a.macroRecall, 0)
	}
	a.taskHits = append(a.taskHits, float64(boolToInt(trial.TaskHit)))
	a.planSuccess = append(a.planSuccess, float64(boolToInt(trial.PlanValid)))
	a.latencyMillis = append(a.latencyMillis, float64(latency.Nanoseconds())/1e6)
}

func (a *evaluationAccumulator) finish(bootstrapSamples int, seed int64) EvaluationSummary {
	macroRecall, recallCI := bootstrapMeanCI(a.macroRecall, bootstrapSamples, seed)
	taskHitRate, taskHitCI := bootstrapMeanCI(a.taskHits, bootstrapSamples, seed+0x517cc1b)
	planRate, planCI := bootstrapMeanCI(a.planSuccess, bootstrapSamples, seed+0x9e3779b)
	var microRecall, candidateMean float64
	if a.relevant > 0 {
		microRecall = float64(a.recallHits) / float64(a.relevant)
	}
	if a.tasks > 0 {
		candidateMean = float64(a.candidateTotal) / float64(a.tasks)
	}
	return EvaluationSummary{Policy: a.policy, TaskCount: a.tasks, RecallCutoff: a.cutoff,
		RecallHits: a.recallHits, RelevantGoldSKUs: a.relevant, MicroRecallAtK: microRecall,
		MacroRecallAtK: macroRecall, MacroRecallAtKCI95: recallCI,
		TaskHitCount: a.taskHitCount, TaskHitRate: taskHitRate, TaskHitRateCI95: taskHitCI,
		ValidPlans:    a.validPlans,
		ValidPlanRate: planRate, ValidPlanRateCI95: planCI, NoCandidateTasks: a.noCandidates,
		CandidateCountTotal: a.candidateTotal, CandidateCountMean: candidateMean,
		SearchLatencyP50Millis: percentile(a.latencyMillis, 0.50), SearchLatencyP95Millis: percentile(a.latencyMillis, 0.95)}
}

func bootstrapMeanCI(values []float64, samples int, seed int64) (float64, [2]float64) {
	if len(values) == 0 {
		return 0, [2]float64{}
	}
	if samples < 1000 {
		samples = 10000
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, samples)
	for i := range means {
		total := 0.0
		for j := 0; j < len(values); j++ {
			total += values[rng.Intn(len(values))]
		}
		means[i] = total / float64(len(values))
	}
	sort.Float64s(means)
	lo := int(float64(samples) * 0.025)
	hi := int(float64(samples)*0.975) - 1
	if lo < 0 {
		lo = 0
	}
	if hi >= len(means) {
		hi = len(means) - 1
	}
	return mean, [2]float64{means[lo], means[hi]}
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := int(float64(len(ordered)-1) * p)
	return ordered[index]
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func ComputeEvaluationHashes(catalog ImportedCatalog, tasks []Task, manifestBytes []byte) (EvaluationHashes, error) {
	products := append([]Product(nil), catalog.Products...)
	sort.Slice(products, func(i, j int) bool {
		if products[i].MerchantID != products[j].MerchantID {
			return products[i].MerchantID < products[j].MerchantID
		}
		return products[i].SKUID < products[j].SKUID
	})
	offers := append([]Inventory(nil), catalog.Inventory...)
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].MerchantID != offers[j].MerchantID {
			return offers[i].MerchantID < offers[j].MerchantID
		}
		if offers[i].SKUID != offers[j].SKUID {
			return offers[i].SKUID < offers[j].SKUID
		}
		return offers[i].Region < offers[j].Region
	})
	productHash, err := hashJSON(products)
	if err != nil {
		return EvaluationHashes{}, err
	}
	offerHash, err := hashJSON(offers)
	if err != nil {
		return EvaluationHashes{}, err
	}
	taskHash, err := hashJSONLines(tasks)
	if err != nil {
		return EvaluationHashes{}, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	return EvaluationHashes{CatalogSHA256: productHash, OverlaySHA256: offerHash,
		TasksSHA256: taskHash, ManifestSHA256: "sha256:" + hex.EncodeToString(manifestHash[:])}, nil
}

// validateRecordedIntentHashesV1 allows an existing recorded-intent run to be
// replayed only when every v1 Merchant product fact, inventory row, task, and
// source manifest still hashes exactly as recorded. The v1 Merchant snapshot
// did not contain benchmark-generated Product.Attributes or
// Product.SubstitutionGroup; v2 derives those locally and records them in the
// full execution hash. This does not certify current parser quality or current
// taxonomy coverage.
func validateRecordedIntentHashesV1(catalog ImportedCatalog, tasks []Task, manifestBytes []byte,
	recorded EvaluationHashes) (EvaluationHashes, string, error) {
	current, err := ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return EvaluationHashes{}, "", err
	}
	if current.OverlaySHA256 != recorded.OverlaySHA256 || current.TasksSHA256 != recorded.TasksSHA256 ||
		current.ManifestSHA256 != recorded.ManifestSHA256 {
		return EvaluationHashes{}, "", fmt.Errorf("recorded intent inventory/task/source-manifest hashes differ from current execution inputs: current=%+v recorded=%+v", current, recorded)
	}
	legacyCatalogHash, err := legacyProductCatalogHashV1(catalog.Products)
	if err != nil {
		return EvaluationHashes{}, "", err
	}
	if legacyCatalogHash != recorded.CatalogSHA256 {
		return EvaluationHashes{}, "", fmt.Errorf("recorded intent catalog does not match the v1 product projection: current_v1=%s recorded=%s", legacyCatalogHash, recorded.CatalogSHA256)
	}
	return current, legacyCatalogHash, nil
}

func legacyProductCatalogHashV1(input []Product) (string, error) {
	products := append([]Product(nil), input...)
	sort.Slice(products, func(i, j int) bool {
		if products[i].MerchantID != products[j].MerchantID {
			return products[i].MerchantID < products[j].MerchantID
		}
		return products[i].SKUID < products[j].SKUID
	})
	for index := range products {
		products[index].Attributes = nil
		products[index].SubstitutionGroup = ""
	}
	return hashJSON(products)
}

func hashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func hashJSONLines[T any](values []T) (string, error) {
	hash := sha256.New()
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		if _, err := hash.Write(data); err != nil {
			return "", err
		}
		if _, err := hash.Write([]byte{'\n'}); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
