package agentshopping

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ChallengeDisposition describes the expected decision at the discovery
// boundary. It is intentionally separate from Task so the original locked
// all-feasible suite and its historical metrics remain unchanged.
type ChallengeDisposition string

const (
	ChallengePlan      ChallengeDisposition = "PLAN"
	ChallengeNoPlan    ChallengeDisposition = "NO_PLAN"
	ChallengeClarify   ChallengeDisposition = "CLARIFY"
	ChallengeParseFail ChallengeDisposition = "PARSE_FAILED"
)

// ChallengeTask keeps feasible plan labels (GoldSKUs), exact target-product
// relevance (RelevantSKUs), and explicitly accepted alternatives separate.
// Human-reviewed tasks bind substitute-label decisions in provenance;
// generated legacy drafts may omit these new labels and remain diagnostics.
type ChallengeTask struct {
	TaskID                 string                   `json:"task_id"`
	Scenario               string                   `json:"scenario"`
	Prompt                 string                   `json:"prompt"`
	ExpectedDisposition    ChallengeDisposition     `json:"expected_disposition"`
	Intent                 Intent                   `json:"gold_intent"`
	BuyerDefaultRegion     string                   `json:"buyer_default_region,omitempty"`
	BuyerBudgetMinor       int64                    `json:"buyer_budget_minor,omitempty"`
	GoldSKUs               []string                 `json:"gold_skus,omitempty"`
	RelevantSKUs           []string                 `json:"relevant_skus,omitempty"`
	AcceptedSubstituteSKUs []string                 `json:"accepted_substitute_skus,omitempty"`
	RequiredMissingFields  []string                 `json:"required_missing_fields,omitempty"`
	Provenance             *ChallengeTaskProvenance `json:"provenance,omitempty"`
}

// ChallengeTaskProvenance records self-attested review controls for a human
// holdout. It is deliberately separate from the prompt sent to the model.
// These declarations are auditable metadata, not cryptographic proof that the
// named review process occurred.
type ChallengeTaskProvenance struct {
	PromptSource                          string   `json:"prompt_source,omitempty"`
	PromptAuthorID                        string   `json:"prompt_author_id,omitempty"`
	PromptWrittenWithoutGeneratedTemplate bool     `json:"prompt_written_without_generated_template,omitempty"`
	AcceptedSubstituteLabelsExplicit      bool     `json:"accepted_substitute_labels_explicit,omitempty"`
	GoldLabelReviewStatus                 string   `json:"gold_label_review_status,omitempty"`
	GoldReviewerIDs                       []string `json:"gold_reviewer_ids,omitempty"`
	GoldAdjudicatorID                     string   `json:"gold_adjudicator_id,omitempty"`
	ReviewCatalogSHA256                   string   `json:"review_catalog_sha256,omitempty"`
	ReviewInventorySHA256                 string   `json:"review_inventory_sha256,omitempty"`
	PromptRecordsSHA256                   string   `json:"prompt_records_sha256,omitempty"`
	ReviewerAAnnotationsSHA256            string   `json:"reviewer_a_annotations_sha256,omitempty"`
	ReviewerBAnnotationsSHA256            string   `json:"reviewer_b_annotations_sha256,omitempty"`
	AdjudicationsSHA256                   string   `json:"adjudications_sha256,omitempty"`
	ModelBlind                            bool     `json:"model_blind,omitempty"`
	LabelsFrozenBeforeModelRun            bool     `json:"labels_frozen_before_model_run,omitempty"`
}

type ChallengeRate struct {
	// Status is populated when the rate depends on labels with a separate
	// provenance gate; ordinary rates leave it empty.
	Status      string     `json:"status,omitempty"`
	Numerator   int        `json:"numerator"`
	Denominator int        `json:"denominator"`
	Rate        float64    `json:"rate"`
	CI95        [2]float64 `json:"ci95_wilson"`
}

// ChallengeSKURecallSummary reports retrieval against the task's predeclared
// snapshot-feasible SKU labels. Those labels are independent of the retrieval
// cutoff/order but are not human relevance labels; the draft generator derives
// them from the synthetic snapshot. Macro recall is primary, with task-level
// bootstrap resampling to preserve clustering among SKUs in each task.
type ChallengeSKURecallSummary struct {
	TaskCount             int        `json:"task_count"`
	RetrievedGoldSKUCount int        `json:"retrieved_gold_sku_count"`
	GoldSKUCount          int        `json:"gold_sku_count"`
	MicroRecallAtK        float64    `json:"micro_recall_at_k"`
	MacroRecallAtK        float64    `json:"macro_recall_at_k"`
	MacroRecallAtKCI95    [2]float64 `json:"macro_recall_at_k_ci95_task_bootstrap"`
}

// ChallengeTargetProductRecallSummary measures discovery against exact-target
// listings. Accepted alternatives are measured separately and cannot inflate
// target-product recall. Labels do not depend on current stock, region, or
// price. Status distinguishes computed values with unverified labels from
// values whose double-review provenance is structurally declared; it cannot
// prove that the human review actually occurred.
type ChallengeTargetProductRecallSummary struct {
	Status                               string     `json:"status"`
	PlanTaskCount                        int        `json:"plan_task_count"`
	LabeledPlanTaskCount                 int        `json:"labeled_plan_task_count"`
	DeclaredDoubleReviewedTaskCount      int        `json:"declared_double_reviewed_task_count"`
	UnverifiedLabeledPlanTaskCount       int        `json:"unverified_labeled_plan_task_count"`
	RetrievedRelevantSKUCount            int        `json:"retrieved_relevant_sku_count"`
	RelevantSKUCount                     int        `json:"relevant_sku_count"`
	MicroRecallAtK                       float64    `json:"micro_recall_at_k"`
	MacroRecallAtK                       float64    `json:"macro_recall_at_k"`
	MacroRecallAtKCI95                   [2]float64 `json:"macro_recall_at_k_ci95_task_bootstrap"`
	MaxPossibleRetrievedRelevantSKUCount int        `json:"max_possible_retrieved_relevant_sku_count"`
	MaxPossibleMicroRecallAtK            float64    `json:"max_possible_micro_recall_at_k"`
	MaxPossibleMacroRecallAtK            float64    `json:"max_possible_macro_recall_at_k"`
	PlanTasksWithRelevantSKUCountAboveK  int        `json:"plan_tasks_with_relevant_sku_count_above_k"`
}

type ChallengePairedPlanOutcomes struct {
	BothValid     int `json:"both_valid"`
	BaselineOnly  int `json:"baseline_only_valid"`
	TreatmentOnly int `json:"treatment_only_valid"`
	NeitherValid  int `json:"neither_valid"`
}

type ChallengePlanComparison struct {
	BaselineValidPlanRate               ChallengeRate               `json:"baseline_valid_plan_rate"`
	TreatmentMinusBaselineValidPlanRate float64                     `json:"valid_plan_rate_delta_treatment_minus_baseline"`
	DeltaCI95                           [2]float64                  `json:"valid_plan_rate_delta_ci95_task_bootstrap"`
	PairedOutcomes                      ChallengePairedPlanOutcomes `json:"paired_outcomes"`
}

type ChallengeEvaluationHashes struct {
	CatalogSHA256   string `json:"catalog_sha256"`
	InventorySHA256 string `json:"inventory_sha256"`
	TasksSHA256     string `json:"tasks_sha256"`
	ManifestSHA256  string `json:"source_manifest_sha256"`
}

type ChallengeStratum struct {
	TaskCount                          int                                 `json:"task_count"`
	PlanTasks                          int                                 `json:"plan_tasks"`
	CorrectDecisionRate                ChallengeRate                       `json:"correct_decision_rate_all_tasks"`
	FeasibleSKURecallAtK               ChallengeSKURecallSummary           `json:"feasible_sku_recall_at_k"`
	TreatmentFeasibleOfferSKURecallAtK ChallengeSKURecallSummary           `json:"treatment_feasible_offer_sku_recall_at_k"`
	TargetProductRecallAtK             ChallengeTargetProductRecallSummary `json:"target_product_recall_at_k"`
	PlanTaskHitAtK                     ChallengeRate                       `json:"plan_task_hit_at_k"`
	TreatmentFeasibleOfferTaskHitAtK   ChallengeRate                       `json:"treatment_feasible_offer_task_hit_at_k"`
	TargetProductTaskHitAtK            ChallengeRate                       `json:"target_product_task_hit_at_k"`
	PlanComparison                     ChallengePlanComparison             `json:"plan_comparison"`
	ValidPlanRate                      ChallengeRate                       `json:"valid_plan_rate"`
	AcceptedSubstituteHitsAtK          int                                 `json:"accepted_substitute_hits_at_k"`
	ValidSubstitutePlans               int                                 `json:"valid_substitute_plans"`
	NoPlanTasks                        int                                 `json:"no_plan_tasks"`
	CorrectNoPlanRate                  ChallengeRate                       `json:"correct_no_plan_rate"`
	ClarificationTasks                 int                                 `json:"clarification_tasks"`
	CorrectClarifyRate                 ChallengeRate                       `json:"correct_clarification_rate"`
	ClarificationQuestionRate          ChallengeRate                       `json:"clarification_question_provided_rate"`
	ClarificationQuestionCueRate       ChallengeRate                       `json:"clarification_question_missing_field_cue_rate"`
	UnexpectedPlanCount                int                                 `json:"unexpected_plan_for_non_plan_count"`
	ParseFailures                      int                                 `json:"parse_failures"`
}

type ChallengeEvaluationReport struct {
	SchemaVersion                      string                              `json:"schema_version"`
	Evaluation                         string                              `json:"evaluation"`
	DataOrigin                         string                              `json:"data_origin"`
	Hashes                             ChallengeEvaluationHashes           `json:"hashes"`
	TaskCount                          int                                 `json:"task_count"`
	RecallCutoff                       int                                 `json:"recall_cutoff"`
	DiscoveryCandidateWindow           int                                 `json:"discovery_candidate_window"`
	ExactIntentRate                    ChallengeRate                       `json:"exact_intent_rate_on_complete_intents"`
	CorrectDecisionRate                ChallengeRate                       `json:"correct_decision_rate_all_tasks"`
	PlanTasks                          int                                 `json:"plan_tasks"`
	FeasibleSKURecallAtK               ChallengeSKURecallSummary           `json:"feasible_sku_recall_at_k"`
	TreatmentFeasibleOfferSKURecallAtK ChallengeSKURecallSummary           `json:"treatment_feasible_offer_sku_recall_at_k"`
	TargetProductRecallAtK             ChallengeTargetProductRecallSummary `json:"target_product_recall_at_k"`
	PlanTaskHitAtK                     ChallengeRate                       `json:"plan_task_hit_at_k"`
	TreatmentFeasibleOfferTaskHitAtK   ChallengeRate                       `json:"treatment_feasible_offer_task_hit_at_k"`
	TargetProductTaskHitAtK            ChallengeRate                       `json:"target_product_task_hit_at_k"`
	PlanComparison                     ChallengePlanComparison             `json:"plan_comparison"`
	ValidPlanRate                      ChallengeRate                       `json:"valid_plan_rate"`
	AcceptedSubstituteHitsAtK          int                                 `json:"accepted_substitute_hits_at_k"`
	ValidSubstitutePlans               int                                 `json:"valid_substitute_plans"`
	NoPlanTasks                        int                                 `json:"no_plan_tasks"`
	CorrectNoPlanRate                  ChallengeRate                       `json:"correct_no_plan_rate"`
	ClarificationTasks                 int                                 `json:"clarification_tasks"`
	CorrectClarifyRate                 ChallengeRate                       `json:"correct_clarification_rate"`
	ClarificationQuestionRate          ChallengeRate                       `json:"clarification_question_provided_rate"`
	ClarificationQuestionCueRate       ChallengeRate                       `json:"clarification_question_missing_field_cue_rate"`
	UnexpectedPlanCount                int                                 `json:"unexpected_plan_for_non_plan_count"`
	ParseFailures                      int                                 `json:"parse_failures"`
	ByScenario                         map[string]ChallengeStratum         `json:"by_scenario"`
}

type ChallengeTrial struct {
	TaskID                                    string               `json:"task_id"`
	Scenario                                  string               `json:"scenario"`
	ExpectedDisposition                       ChallengeDisposition `json:"expected_disposition"`
	ActualDisposition                         ChallengeDisposition `json:"actual_disposition"`
	ParseValid                                bool                 `json:"parse_valid"`
	ExactIntentMatch                          bool                 `json:"exact_intent_match"`
	BaselineCandidateCount                    int                  `json:"baseline_candidate_count"`
	BaselineTopSKU                            string               `json:"baseline_top_sku,omitempty"`
	BaselineValidPlan                         bool                 `json:"baseline_valid_plan"`
	BaselineFailureReasons                    []string             `json:"baseline_failure_reasons,omitempty"`
	DiscoveryCandidateCount                   int                  `json:"discovery_candidate_count"`
	DiscoveryTopKCount                        int                  `json:"discovery_recall_top_k_count"`
	CandidateCount                            int                  `json:"feasible_candidate_count"`
	GoldSKUCount                              int                  `json:"gold_sku_count,omitempty"`
	RetrievedGoldSKUCount                     int                  `json:"retrieved_gold_sku_count,omitempty"`
	TreatmentFeasibleOfferSKUs                []string             `json:"treatment_feasible_offer_skus,omitempty"`
	RetrievedTreatmentFeasibleGoldSKUCount    int                  `json:"retrieved_treatment_feasible_gold_sku_count,omitempty"`
	RelevantSKUCount                          int                  `json:"relevant_sku_count,omitempty"`
	RetrievedRelevantSKUCount                 int                  `json:"retrieved_relevant_sku_count,omitempty"`
	TopSKU                                    string               `json:"top_sku,omitempty"`
	TopMatchType                              string               `json:"top_match_type,omitempty"`
	GoldHitAtK                                bool                 `json:"discovery_hit_any_gold_at_k"`
	TreatmentFeasibleOfferGoldHitAtK          bool                 `json:"treatment_feasible_offer_hit_any_gold_at_k"`
	TargetProductHitAtK                       bool                 `json:"discovery_hit_any_relevant_product_at_k"`
	AcceptedSubstituteHitAtK                  bool                 `json:"accepted_substitute_hit_at_k"`
	ValidPlan                                 bool                 `json:"valid_plan"`
	CorrectDecision                           bool                 `json:"correct_decision"`
	ClarificationQuestion                     bool                 `json:"clarification_question_provided"`
	ClarificationQuestionTargetsMissingFields bool                 `json:"clarification_question_targets_missing_fields"`
	FailureReasons                            []string             `json:"failure_reasons,omitempty"`
}

// ComputeChallengeHashes binds a challenge run to an ordered task file and
// canonical catalog/inventory snapshots. The task hash intentionally changes
// when task order or any prompt/label changes.
func ComputeChallengeHashes(catalog ImportedCatalog, tasks []ChallengeTask, manifestBytes []byte) (ChallengeEvaluationHashes, error) {
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
		return ChallengeEvaluationHashes{}, err
	}
	offerHash, err := hashJSON(offers)
	if err != nil {
		return ChallengeEvaluationHashes{}, err
	}
	taskHash, err := hashJSONLines(tasks)
	if err != nil {
		return ChallengeEvaluationHashes{}, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	return ChallengeEvaluationHashes{CatalogSHA256: productHash, InventorySHA256: offerHash,
		TasksSHA256: taskHash, ManifestSHA256: "sha256:" + hex.EncodeToString(manifestHash[:])}, nil
}

// ValidateChallengeTaskSet checks labels against the pinned snapshot before a
// model call is made, preventing malformed or infeasible holdouts from
// consuming usage or quietly producing misleading denominators.
func ValidateChallengeTaskSet(catalog ImportedCatalog, tasks []ChallengeTask) error {
	activeProducts, offers, err := challengeEvaluationSnapshot(catalog)
	if err != nil {
		return err
	}
	return validateChallengeTasks(activeProducts, offers, tasks)
}

// EvaluateChallengeTasks grades complete, infeasible, and ambiguous requests
// against the same pinned catalog/inventory snapshot. CorrectDecisionRate
// includes every task: PLAN requires a valid plan, NO_PLAN requires a correct
// abstention, and CLARIFY requires a useful question without filling missing
// fields. Planning uses the same fixed candidate window as the runtime, then
// applies live constraints and the response limit. Recall@K is measured on the
// first K products before those filters. Parse failures remain misses in all
// applicable denominators.
func EvaluateChallengeTasks(catalog ImportedCatalog, tasks []ChallengeTask, results []BatchIntentResult, cutoff int) (ChallengeEvaluationReport, []ChallengeTrial, error) {
	if len(tasks) == 0 || len(results) != len(tasks) || cutoff <= 0 || cutoff > DefaultDiscoveryCandidateWindow {
		return ChallengeEvaluationReport{}, nil, fmt.Errorf("invalid challenge dimensions: %w", ErrInvalidEvaluationSuite)
	}
	activeProducts, offers, err := challengeEvaluationSnapshot(catalog)
	if err != nil {
		return ChallengeEvaluationReport{}, nil, err
	}
	if err := validateChallengeTasks(activeProducts, offers, tasks); err != nil {
		return ChallengeEvaluationReport{}, nil, err
	}
	reviewSnapshotHashes, err := ComputeChallengeHashes(catalog, nil, nil)
	if err != nil {
		return ChallengeEvaluationReport{}, nil, fmt.Errorf("hash target-relevance review snapshot: %w", err)
	}
	doubleReviewedLabels := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if ValidateChallengeTaskProvenance([]ChallengeTask{task}, reviewSnapshotHashes) == nil {
			doubleReviewedLabels[task.TaskID] = true
		}
	}
	productBySKU := make(map[string]Product, len(activeProducts))
	for _, product := range activeProducts {
		productBySKU[product.SKUID] = product
	}
	resultByID := make(map[string]BatchIntentResult, len(results))
	for _, result := range results {
		if result.TaskID == "" || resultByID[result.TaskID].TaskID != "" {
			return ChallengeEvaluationReport{}, nil, fmt.Errorf("empty or duplicate challenge result id: %w", ErrInvalidEvaluationSuite)
		}
		resultByID[result.TaskID] = result
	}

	report := ChallengeEvaluationReport{SchemaVersion: "stablepay.agent-shopping-challenge.v8",
		Evaluation: "paired-b0-b1-two-stage-discovery-and-plan-quality", DataOrigin: "CHALLENGE_INPUT_PROVENANCE_UNVERIFIED",
		TaskCount: len(tasks), RecallCutoff: cutoff, DiscoveryCandidateWindow: DefaultDiscoveryCandidateWindow,
		ByScenario: make(map[string]ChallengeStratum)}
	trials := make([]ChallengeTrial, 0, len(tasks))
	var exactIntentCorrect, completeIntentCount int
	for _, task := range tasks {
		result, ok := resultByID[task.TaskID]
		if !ok {
			return ChallengeEvaluationReport{}, nil, fmt.Errorf("missing challenge result for %s: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		trial := ChallengeTrial{TaskID: task.TaskID, Scenario: task.Scenario,
			ExpectedDisposition: task.ExpectedDisposition, ActualDisposition: ChallengeParseFail}
		if task.ExpectedDisposition == ChallengePlan {
			trial.RelevantSKUCount = len(task.RelevantSKUs)
		}
		stratum := report.ByScenario[task.Scenario]
		stratum.TaskCount++
		switch task.ExpectedDisposition {
		case ChallengePlan:
			report.PlanTasks++
			stratum.PlanTasks++
		case ChallengeNoPlan:
			report.NoPlanTasks++
			stratum.NoPlanTasks++
		case ChallengeClarify:
			report.ClarificationTasks++
			stratum.ClarificationTasks++
		}
		if task.ExpectedDisposition != ChallengeClarify {
			completeIntentCount++
		}
		if result.Error != "" {
			if task.ExpectedDisposition == ChallengePlan {
				trial.GoldSKUCount = len(task.GoldSKUs)
			}
			report.ParseFailures++
			stratum.ParseFailures++
			trial.FailureReasons = append(trial.FailureReasons, "INTENT_PARSE_FAILED")
			trials = append(trials, trial)
			report.ByScenario[task.Scenario] = stratum
			continue
		}
		trial.ParseValid = true
		parsed := result.Intent
		trial.ExactIntentMatch = sameChallengeIntent(parsed, task.Intent)
		if task.ExpectedDisposition != ChallengeClarify {
			if trial.ExactIntentMatch {
				exactIntentCorrect++
			}
		}
		if !parsed.ClarificationRequired {
			baseline := rankBaseline(activeProducts, parsed, cutoff)
			trial.BaselineCandidateCount = len(baseline)
			if len(baseline) == 0 {
				trial.BaselineFailureReasons = append(trial.BaselineFailureReasons, "NO_BASELINE_CANDIDATE")
			}
			if len(baseline) > 0 {
				trial.BaselineTopSKU = baseline[0].product.SKUID
				baselineTask := Task{TaskID: task.TaskID, Intent: task.Intent, GoldSKUs: task.GoldSKUs}
				baselineTrial := makePolicyTrial(baselineTask, baseline, offers)
				trial.BaselineFailureReasons = append(trial.BaselineFailureReasons, baselineTrial.FailureReasons...)
				trial.BaselineValidPlan = baselineTrial.PlanValid && hardChallengeIntentMatches(parsed, task.Intent)
				if !hardChallengeIntentMatches(parsed, task.Intent) {
					trial.BaselineFailureReasons = append(trial.BaselineFailureReasons, "PARSED_INTENT_MISMATCH")
				}
			}
		}
		if parsed.ClarificationRequired {
			trial.ActualDisposition = ChallengeClarify
			trial.ClarificationQuestion = strings.TrimSpace(parsed.ClarificationQuestion) != ""
			if task.ExpectedDisposition == ChallengeClarify {
				if trial.ClarificationQuestion {
					report.ClarificationQuestionRate.Numerator++
					stratum.ClarificationQuestionRate.Numerator++
				}
				trial.ClarificationQuestionTargetsMissingFields = clarificationQuestionTargetsMissingFields(
					parsed.ClarificationQuestion, task.RequiredMissingFields)
				if trial.ClarificationQuestionTargetsMissingFields {
					report.ClarificationQuestionCueRate.Numerator++
					stratum.ClarificationQuestionCueRate.Numerator++
				} else {
					trial.FailureReasons = append(trial.FailureReasons, "CLARIFICATION_QUESTION_MISSING_FIELD_CUE")
				}
			}
		} else {
			discovery := rankDiscoveryProducts(activeProducts, parsed, DefaultDiscoveryCandidateWindow)
			trial.DiscoveryCandidateCount = len(discovery)
			recallCandidates := discovery
			if len(recallCandidates) > cutoff {
				recallCandidates = recallCandidates[:cutoff]
			}
			trial.DiscoveryTopKCount = len(recallCandidates)
			candidates := rankTreatment(discovery, offers, parsed, cutoff)
			trial.CandidateCount = len(candidates)
			if len(candidates) == 0 {
				trial.ActualDisposition = ChallengeNoPlan
			} else {
				trial.ActualDisposition = ChallengePlan
				trial.TopSKU = candidates[0].product.SKUID
				trial.TopMatchType = candidates[0].matchType
			}
			if task.ExpectedDisposition == ChallengePlan {
				trial.GoldSKUCount = len(task.GoldSKUs)
				gold := stringSet(task.GoldSKUs)
				relevant := stringSet(task.RelevantSKUs)
				acceptedSubstitutes := challengeAcceptedSubstituteSet(task, productBySKU)
				for _, product := range recallCandidates {
					if gold[product.SKUID] {
						trial.RetrievedGoldSKUCount++
						trial.GoldHitAtK = true
					}
					if relevant[product.SKUID] {
						trial.RetrievedRelevantSKUCount++
						trial.TargetProductHitAtK = true
					}
					if acceptedSubstitutes[product.SKUID] && discoveryMatchType(product, parsed) == MatchTypeSubstitute {
						trial.AcceptedSubstituteHitAtK = true
					}
				}
				for _, candidate := range candidates {
					trial.TreatmentFeasibleOfferSKUs = append(trial.TreatmentFeasibleOfferSKUs, candidate.product.SKUID)
					if gold[candidate.product.SKUID] {
						trial.RetrievedTreatmentFeasibleGoldSKUCount++
						trial.TreatmentFeasibleOfferGoldHitAtK = true
					}
				}
				if len(candidates) > 0 {
					treatmentTask := Task{TaskID: task.TaskID, Intent: task.Intent, GoldSKUs: task.GoldSKUs}
					treatmentPlan := makePolicyTrial(treatmentTask, candidates, offers)
					trial.ValidPlan = treatmentPlan.PlanValid && hardChallengeIntentMatches(parsed, task.Intent)
					trial.FailureReasons = append(trial.FailureReasons, treatmentPlan.FailureReasons...)
					if !hardChallengeIntentMatches(parsed, task.Intent) {
						trial.FailureReasons = append(trial.FailureReasons, "PARSED_INTENT_MISMATCH")
					}
				}
			}
		}
		trial.CorrectDecision = challengeDecisionCorrect(task, parsed, trial)
		if trial.ActualDisposition == ChallengePlan && task.ExpectedDisposition != ChallengePlan {
			trial.FailureReasons = append(trial.FailureReasons, "PLAN_GENERATED_FOR_NON_PLAN_TASK")
			report.UnexpectedPlanCount++
			stratum.UnexpectedPlanCount++
		}
		if task.ExpectedDisposition == ChallengePlan {
			if trial.GoldHitAtK {
				report.PlanTaskHitAtK.Numerator++
				stratum.PlanTaskHitAtK.Numerator++
			}
			if trial.AcceptedSubstituteHitAtK {
				report.AcceptedSubstituteHitsAtK++
				stratum.AcceptedSubstituteHitsAtK++
			}
			if trial.TreatmentFeasibleOfferGoldHitAtK {
				report.TreatmentFeasibleOfferTaskHitAtK.Numerator++
				stratum.TreatmentFeasibleOfferTaskHitAtK.Numerator++
			}
			if trial.ValidPlan {
				report.ValidPlanRate.Numerator++
				stratum.ValidPlanRate.Numerator++
				if trial.TopMatchType == MatchTypeSubstitute {
					report.ValidSubstitutePlans++
					stratum.ValidSubstitutePlans++
				}
			} else if trial.ActualDisposition != ChallengePlan {
				trial.FailureReasons = append(trial.FailureReasons, "NO_PURCHASE_PLAN")
			} else {
				trial.FailureReasons = append(trial.FailureReasons, "TOP_CANDIDATE_NOT_VALID_GOLD")
			}
		} else if task.ExpectedDisposition == ChallengeNoPlan {
			if trial.CorrectDecision {
				report.CorrectNoPlanRate.Numerator++
				stratum.CorrectNoPlanRate.Numerator++
			}
		} else {
			if trial.CorrectDecision {
				report.CorrectClarifyRate.Numerator++
				stratum.CorrectClarifyRate.Numerator++
			}
		}
		trials = append(trials, trial)
		report.ByScenario[task.Scenario] = stratum
	}
	if completeIntentCount > 0 {
		report.ExactIntentRate = challengeRate(exactIntentCorrect, completeIntentCount)
	}
	correctDecisions := 0
	for _, trial := range trials {
		if trial.CorrectDecision {
			correctDecisions++
		}
	}
	report.CorrectDecisionRate = challengeRate(correctDecisions, len(tasks))
	report.FeasibleSKURecallAtK = summarizeChallengeSKURecall(tasks, trials, "")
	report.TreatmentFeasibleOfferSKURecallAtK = summarizeTreatmentFeasibleOfferSKURecall(tasks, trials, "")
	report.TargetProductRecallAtK = summarizeChallengeTargetProductRecall(tasks, trials, "", doubleReviewedLabels, cutoff)
	report.PlanTaskHitAtK = challengeRate(report.PlanTaskHitAtK.Numerator, report.PlanTasks)
	report.TreatmentFeasibleOfferTaskHitAtK = challengeRate(report.TreatmentFeasibleOfferTaskHitAtK.Numerator, report.PlanTasks)
	report.TargetProductTaskHitAtK = challengeRate(countTargetProductTaskHits(trials, ""), countLabeledPlanTasks(tasks, ""))
	report.TargetProductTaskHitAtK.Status = report.TargetProductRecallAtK.Status
	report.PlanComparison = summarizeChallengePlanComparison(tasks, trials, "")
	report.ValidPlanRate = challengeRate(report.ValidPlanRate.Numerator, report.PlanTasks)
	report.CorrectNoPlanRate = challengeRate(report.CorrectNoPlanRate.Numerator, report.NoPlanTasks)
	report.CorrectClarifyRate = challengeRate(report.CorrectClarifyRate.Numerator, report.ClarificationTasks)
	report.ClarificationQuestionRate = challengeRate(report.ClarificationQuestionRate.Numerator, report.ClarificationTasks)
	report.ClarificationQuestionCueRate = challengeRate(report.ClarificationQuestionCueRate.Numerator, report.ClarificationTasks)
	for scenario, stratum := range report.ByScenario {
		correctScenarioDecisions := 0
		for _, trial := range trials {
			if trial.Scenario == scenario && trial.CorrectDecision {
				correctScenarioDecisions++
			}
		}
		stratum.CorrectDecisionRate = challengeRate(correctScenarioDecisions, stratum.TaskCount)
		stratum.FeasibleSKURecallAtK = summarizeChallengeSKURecall(tasks, trials, scenario)
		stratum.TreatmentFeasibleOfferSKURecallAtK = summarizeTreatmentFeasibleOfferSKURecall(tasks, trials, scenario)
		stratum.TargetProductRecallAtK = summarizeChallengeTargetProductRecall(tasks, trials, scenario, doubleReviewedLabels, cutoff)
		stratum.PlanTaskHitAtK = challengeRate(stratum.PlanTaskHitAtK.Numerator, stratum.PlanTasks)
		stratum.TreatmentFeasibleOfferTaskHitAtK = challengeRate(stratum.TreatmentFeasibleOfferTaskHitAtK.Numerator, stratum.PlanTasks)
		stratum.TargetProductTaskHitAtK = challengeRate(countTargetProductTaskHits(trials, scenario), countLabeledPlanTasks(tasks, scenario))
		stratum.TargetProductTaskHitAtK.Status = stratum.TargetProductRecallAtK.Status
		stratum.PlanComparison = summarizeChallengePlanComparison(tasks, trials, scenario)
		stratum.ValidPlanRate = challengeRate(stratum.ValidPlanRate.Numerator, stratum.PlanTasks)
		stratum.CorrectNoPlanRate = challengeRate(stratum.CorrectNoPlanRate.Numerator, stratum.NoPlanTasks)
		stratum.CorrectClarifyRate = challengeRate(stratum.CorrectClarifyRate.Numerator, stratum.ClarificationTasks)
		stratum.ClarificationQuestionRate = challengeRate(stratum.ClarificationQuestionRate.Numerator, stratum.ClarificationTasks)
		stratum.ClarificationQuestionCueRate = challengeRate(stratum.ClarificationQuestionCueRate.Numerator, stratum.ClarificationTasks)
		report.ByScenario[scenario] = stratum
	}
	return report, trials, nil
}

func summarizeTreatmentFeasibleOfferSKURecall(tasks []ChallengeTask, trials []ChallengeTrial, scenario string) ChallengeSKURecallSummary {
	trialByID := make(map[string]ChallengeTrial, len(trials))
	for _, trial := range trials {
		trialByID[trial.TaskID] = trial
	}
	var summary ChallengeSKURecallSummary
	taskRecalls := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		if task.ExpectedDisposition != ChallengePlan || (scenario != "" && task.Scenario != scenario) || len(task.GoldSKUs) == 0 {
			continue
		}
		trial := trialByID[task.TaskID]
		hits := trial.RetrievedTreatmentFeasibleGoldSKUCount
		if hits > len(task.GoldSKUs) {
			hits = len(task.GoldSKUs)
		}
		summary.TaskCount++
		summary.GoldSKUCount += len(task.GoldSKUs)
		summary.RetrievedGoldSKUCount += hits
		taskRecalls = append(taskRecalls, float64(hits)/float64(len(task.GoldSKUs)))
	}
	if summary.GoldSKUCount > 0 {
		summary.MicroRecallAtK = float64(summary.RetrievedGoldSKUCount) / float64(summary.GoldSKUCount)
	}
	if len(taskRecalls) > 0 {
		summary.MacroRecallAtK, summary.MacroRecallAtKCI95 = bootstrapMeanCI(taskRecalls, 10000, 0xa54ff53a)
	}
	return summary
}

func summarizeChallengePlanComparison(tasks []ChallengeTask, trials []ChallengeTrial, scenario string) ChallengePlanComparison {
	trialByID := make(map[string]ChallengeTrial, len(trials))
	for _, trial := range trials {
		trialByID[trial.TaskID] = trial
	}
	var summary ChallengePlanComparison
	baselineValid := 0
	deltas := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		if task.ExpectedDisposition != ChallengePlan || (scenario != "" && task.Scenario != scenario) {
			continue
		}
		trial := trialByID[task.TaskID]
		baselineOK, treatmentOK := trial.BaselineValidPlan, trial.ValidPlan
		if baselineOK {
			baselineValid++
		}
		switch {
		case baselineOK && treatmentOK:
			summary.PairedOutcomes.BothValid++
		case baselineOK:
			summary.PairedOutcomes.BaselineOnly++
		case treatmentOK:
			summary.PairedOutcomes.TreatmentOnly++
		default:
			summary.PairedOutcomes.NeitherValid++
		}
		deltas = append(deltas, float64(boolToInt(treatmentOK)-boolToInt(baselineOK)))
	}
	summary.BaselineValidPlanRate = challengeRate(baselineValid, len(deltas))
	if len(deltas) > 0 {
		summary.TreatmentMinusBaselineValidPlanRate, summary.DeltaCI95 = bootstrapMeanCI(deltas, 10000, 0xbb67ae85)
	}
	return summary
}

func summarizeChallengeSKURecall(tasks []ChallengeTask, trials []ChallengeTrial, scenario string) ChallengeSKURecallSummary {
	trialByID := make(map[string]ChallengeTrial, len(trials))
	for _, trial := range trials {
		trialByID[trial.TaskID] = trial
	}
	var summary ChallengeSKURecallSummary
	taskRecalls := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		if task.ExpectedDisposition != ChallengePlan || (scenario != "" && task.Scenario != scenario) || len(task.GoldSKUs) == 0 {
			continue
		}
		trial := trialByID[task.TaskID]
		hits := trial.RetrievedGoldSKUCount
		if hits > len(task.GoldSKUs) {
			hits = len(task.GoldSKUs)
		}
		summary.TaskCount++
		summary.GoldSKUCount += len(task.GoldSKUs)
		summary.RetrievedGoldSKUCount += hits
		taskRecalls = append(taskRecalls, float64(hits)/float64(len(task.GoldSKUs)))
	}
	if summary.GoldSKUCount > 0 {
		summary.MicroRecallAtK = float64(summary.RetrievedGoldSKUCount) / float64(summary.GoldSKUCount)
	}
	if len(taskRecalls) > 0 {
		summary.MacroRecallAtK, summary.MacroRecallAtKCI95 = bootstrapMeanCI(taskRecalls, 10000, 0x6a09e667)
	}
	return summary
}

func summarizeChallengeTargetProductRecall(tasks []ChallengeTask, trials []ChallengeTrial, scenario string,
	doubleReviewedLabels map[string]bool, cutoff int) ChallengeTargetProductRecallSummary {
	trialByID := make(map[string]ChallengeTrial, len(trials))
	for _, trial := range trials {
		trialByID[trial.TaskID] = trial
	}
	var summary ChallengeTargetProductRecallSummary
	taskRecalls := make([]float64, 0, len(tasks))
	maxPossibleTaskRecalls := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		relevant := task.RelevantSKUs
		if task.ExpectedDisposition == ChallengePlan && (scenario == "" || task.Scenario == scenario) {
			summary.PlanTaskCount++
		}
		if task.ExpectedDisposition != ChallengePlan || (scenario != "" && task.Scenario != scenario) || len(relevant) == 0 {
			continue
		}
		trial := trialByID[task.TaskID]
		hits := trial.RetrievedRelevantSKUCount
		if hits > len(relevant) {
			hits = len(relevant)
		}
		summary.LabeledPlanTaskCount++
		if doubleReviewedLabels[task.TaskID] {
			summary.DeclaredDoubleReviewedTaskCount++
		} else {
			summary.UnverifiedLabeledPlanTaskCount++
		}
		summary.RelevantSKUCount += len(relevant)
		summary.RetrievedRelevantSKUCount += hits
		taskRecalls = append(taskRecalls, float64(hits)/float64(len(relevant)))
		maxPossibleHits := len(relevant)
		if maxPossibleHits > cutoff {
			maxPossibleHits = cutoff
			summary.PlanTasksWithRelevantSKUCountAboveK++
		}
		summary.MaxPossibleRetrievedRelevantSKUCount += maxPossibleHits
		maxPossibleTaskRecalls = append(maxPossibleTaskRecalls, float64(maxPossibleHits)/float64(len(relevant)))
	}
	if summary.RelevantSKUCount > 0 {
		summary.MicroRecallAtK = float64(summary.RetrievedRelevantSKUCount) / float64(summary.RelevantSKUCount)
	}
	if len(taskRecalls) > 0 {
		summary.MacroRecallAtK, summary.MacroRecallAtKCI95 = bootstrapMeanCI(taskRecalls, 10000, 0x3c6ef372)
		for _, recall := range maxPossibleTaskRecalls {
			summary.MaxPossibleMacroRecallAtK += recall
		}
		summary.MaxPossibleMacroRecallAtK /= float64(len(maxPossibleTaskRecalls))
	}
	if summary.RelevantSKUCount > 0 {
		summary.MaxPossibleMicroRecallAtK = float64(summary.MaxPossibleRetrievedRelevantSKUCount) / float64(summary.RelevantSKUCount)
	}
	switch {
	case summary.PlanTaskCount == 0:
		summary.Status = "NO_PLAN_TASKS"
	case summary.LabeledPlanTaskCount == 0:
		summary.Status = "NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS"
	case summary.LabeledPlanTaskCount < summary.PlanTaskCount:
		summary.Status = "PARTIAL_RELEVANCE_LABEL_COVERAGE"
	case summary.DeclaredDoubleReviewedTaskCount == summary.LabeledPlanTaskCount:
		summary.Status = "AVAILABLE_DECLARED_DOUBLE_REVIEW_NOT_EXTERNALLY_VERIFIED"
	default:
		summary.Status = "AVAILABLE_UNVERIFIED_RELEVANCE_LABELS"
	}
	return summary
}

func countTargetProductTaskHits(trials []ChallengeTrial, scenario string) int {
	hits := 0
	for _, trial := range trials {
		if trial.ExpectedDisposition == ChallengePlan && trial.RelevantSKUCount > 0 &&
			(scenario == "" || trial.Scenario == scenario) && trial.TargetProductHitAtK {
			hits++
		}
	}
	return hits
}

func countLabeledPlanTasks(tasks []ChallengeTask, scenario string) int {
	count := 0
	for _, task := range tasks {
		if task.ExpectedDisposition == ChallengePlan && len(task.RelevantSKUs) > 0 &&
			(scenario == "" || task.Scenario == scenario) {
			count++
		}
	}
	return count
}

func challengeEvaluationSnapshot(catalog ImportedCatalog) ([]Product, map[string]Inventory, error) {
	activeProducts := make([]Product, 0, len(catalog.Products))
	seenProducts := make(map[string]bool, len(catalog.Products))
	for _, product := range catalog.Products {
		if product.SKUID == "" || seenProducts[product.SKUID] {
			return nil, nil, fmt.Errorf("empty or duplicate challenge product SKU %q: %w", product.SKUID, ErrInvalidEvaluationSuite)
		}
		seenProducts[product.SKUID] = true
		if product.Status == "active" {
			activeProducts = append(activeProducts, product)
		}
	}
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, offer := range catalog.Inventory {
		key := inventoryKey(SimulationTenantID, offer.MerchantID, offer.SKUID, offer.Region)
		if offer.SKUID == "" || offer.Region == "" || offers[key].SKUID != "" {
			return nil, nil, fmt.Errorf("empty or duplicate challenge inventory identity for %q: %w", offer.SKUID, ErrInvalidEvaluationSuite)
		}
		offers[key] = offer
	}
	return activeProducts, offers, nil
}

func validateChallengeTasks(products []Product, offers map[string]Inventory, tasks []ChallengeTask) error {
	productBySKU := make(map[string]Product, len(products))
	for _, product := range products {
		productBySKU[product.SKUID] = product
	}
	seenIDs := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.TaskID == "" || seenIDs[task.TaskID] || strings.TrimSpace(task.Scenario) == "" ||
			strings.TrimSpace(task.Prompt) == "" || !task.Intent.ConfirmationNeeded {
			return fmt.Errorf("challenge task identity or prompt invalid for %q: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		if task.Provenance != nil && !task.Provenance.AcceptedSubstituteLabelsExplicit {
			return fmt.Errorf("human-reviewed task %s lacks an explicit accepted-substitute decision: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		seenIDs[task.TaskID] = true
		switch task.ExpectedDisposition {
		case ChallengePlan:
			if task.Intent.ClarificationRequired || task.Intent.Category == "" || task.Intent.Item == "" ||
				task.Intent.RequiredFormat == "" || task.Intent.Region == "" || task.Intent.MaxPriceMinor <= 0 ||
				!allowedValue(Regions, task.Intent.Region) ||
				len(task.GoldSKUs) == 0 || len(task.RequiredMissingFields) != 0 {
				return fmt.Errorf("plan task %s lacks a complete intent or gold SKU set: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
			if task.Provenance != nil && len(task.RelevantSKUs) == 0 {
				return fmt.Errorf("human-reviewed plan task %s lacks separate relevant SKU labels: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
			if err := validateHumanTaskContext(task); err != nil {
				return err
			}
		case ChallengeNoPlan:
			if task.Intent.ClarificationRequired || task.Intent.Category == "" || task.Intent.Item == "" ||
				task.Intent.RequiredFormat == "" || task.Intent.Region == "" || task.Intent.MaxPriceMinor <= 0 ||
				!allowedValue(Regions, task.Intent.Region) ||
				len(task.GoldSKUs) != 0 || len(task.RequiredMissingFields) != 0 {
				return fmt.Errorf("no-plan task %s must have a complete intent and no gold SKUs: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
			if err := validateHumanTaskContext(task); err != nil {
				return err
			}
			if len(rankTreatment(products, offers, task.Intent, 0)) != 0 {
				return fmt.Errorf("no-plan task %s is actually feasible in the pinned snapshot: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
		case ChallengeClarify:
			if !task.Intent.ClarificationRequired || task.Intent.Region == "" || !allowedValue(Regions, task.Intent.Region) ||
				task.Intent.MaxPriceMinor <= 0 || len(task.GoldSKUs) != 0 || len(task.RequiredMissingFields) == 0 {
				return fmt.Errorf("clarification task %s must name missing fields and require clarification: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
			if err := validateHumanTaskContext(task); err != nil {
				return err
			}
			hasDecisionCriticalMissingField := false
			seenMissingFields := make(map[string]bool, len(task.RequiredMissingFields))
			for _, field := range task.RequiredMissingFields {
				if seenMissingFields[field] {
					return fmt.Errorf("clarification task %s repeats missing field %s: %w", task.TaskID, field, ErrInvalidEvaluationSuite)
				}
				seenMissingFields[field] = true
				if challengeIntentField(task.Intent, field) != "" {
					return fmt.Errorf("clarification task %s marks populated field %s as missing: %w", task.TaskID, field, ErrInvalidEvaluationSuite)
				}
				if field == "preferred_style" {
					return fmt.Errorf("clarification task %s treats optional preferred_style as a required missing field: %w", task.TaskID, ErrInvalidEvaluationSuite)
				}
				hasDecisionCriticalMissingField = true
			}
			if !hasDecisionCriticalMissingField {
				return fmt.Errorf("clarification task %s cannot block purchase only for an optional preferred style: %w", task.TaskID, ErrInvalidEvaluationSuite)
			}
		default:
			return fmt.Errorf("challenge task %s has unsupported expected disposition %q: %w", task.TaskID, task.ExpectedDisposition, ErrInvalidEvaluationSuite)
		}
		acceptedSubstitutes := challengeAcceptedSubstituteSet(task, productBySKU)
		seenAcceptedSubstitutes := make(map[string]bool, len(task.AcceptedSubstituteSKUs))
		for _, sku := range task.AcceptedSubstituteSKUs {
			product, ok := productBySKU[sku]
			if !ok || seenAcceptedSubstitutes[sku] || candidateMatchType(product, task.Intent) != MatchTypeSubstitute {
				return fmt.Errorf("challenge task %s has invalid accepted substitute SKU %s: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			seenAcceptedSubstitutes[sku] = true
		}
		seenRelevant := make(map[string]bool, len(task.RelevantSKUs))
		for _, sku := range task.RelevantSKUs {
			product, ok := productBySKU[sku]
			// Target-product relevance is exact-item only. Approved alternatives
			// are labeled and scored separately so they cannot inflate recall.
			if !ok || seenRelevant[sku] || product.Status != "active" || product.Currency != "USDC" ||
				candidateMatchType(product, task.Intent) != MatchTypeExact {
				return fmt.Errorf("challenge task %s has invalid relevant SKU %s: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			seenRelevant[sku] = true
		}
		seenGold := make(map[string]bool, len(task.GoldSKUs))
		for _, sku := range task.GoldSKUs {
			product, ok := productBySKU[sku]
			matchType := candidateMatchType(product, task.Intent)
			if !ok || seenGold[sku] || product.Status != "active" || product.Currency != "USDC" ||
				matchType == "" || !requiredAttributesMatch(product.Attributes, task.Intent.RequiredAttributes) ||
				(matchType == MatchTypeSubstitute && !acceptedSubstitutes[sku]) {
				return fmt.Errorf("challenge task %s has invalid gold SKU %s: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, sku, task.Intent.Region)]
			if !ok || !offer.Eligible || offer.OnHand-offer.Reserved < 1 || offer.PriceMinor > task.Intent.MaxPriceMinor {
				return fmt.Errorf("challenge task %s gold SKU %s is not available under expected constraints: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			if matchType == MatchTypeExact && len(task.RelevantSKUs) != 0 && !seenRelevant[sku] {
				return fmt.Errorf("challenge task %s feasible plan SKU %s is not human-labeled relevant: %w", task.TaskID, sku, ErrInvalidEvaluationSuite)
			}
			seenGold[sku] = true
		}
		if task.ExpectedDisposition == ChallengePlan && len(rankTreatment(products, offers, task.Intent, 0)) == 0 {
			return fmt.Errorf("plan task %s has no feasible candidate in the pinned snapshot: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

func validateHumanTaskContext(task ChallengeTask) error {
	if task.Provenance == nil {
		return nil
	}
	if !allowedValue(Regions, task.BuyerDefaultRegion) || task.BuyerBudgetMinor <= 0 ||
		task.Intent.MaxPriceMinor > task.BuyerBudgetMinor {
		return fmt.Errorf("human-reviewed task %s must declare independent buyer context and keep its gold budget within the buyer cap: %w",
			task.TaskID, ErrInvalidEvaluationSuite)
	}
	return nil
}

func challengeDecisionCorrect(task ChallengeTask, parsed Intent, trial ChallengeTrial) bool {
	switch task.ExpectedDisposition {
	case ChallengePlan:
		return trial.ActualDisposition == ChallengePlan && trial.ValidPlan
	case ChallengeNoPlan:
		return trial.ActualDisposition == ChallengeNoPlan && trial.ExactIntentMatch
	case ChallengeClarify:
		if trial.ActualDisposition != ChallengeClarify || !trial.ClarificationQuestion ||
			!trial.ClarificationQuestionTargetsMissingFields || !trial.ExactIntentMatch {
			return false
		}
		for _, field := range task.RequiredMissingFields {
			if challengeIntentField(parsed, field) != "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sameChallengeIntent(actual, expected Intent) bool {
	return actual.Category == expected.Category && actual.Item == expected.Item &&
		actual.RequiredFormat == expected.RequiredFormat && actual.PreferredStyle == expected.PreferredStyle &&
		sameRequiredAttributes(actual.RequiredAttributes, expected.RequiredAttributes) &&
		actual.Region == expected.Region && actual.MaxPriceMinor == expected.MaxPriceMinor &&
		actual.AllowSubstitution == expected.AllowSubstitution && actual.ConfirmationNeeded == expected.ConfirmationNeeded &&
		actual.ClarificationRequired == expected.ClarificationRequired
}

func hardChallengeIntentMatches(actual, expected Intent) bool {
	return actual.Category == expected.Category && actual.Item == expected.Item &&
		actual.RequiredFormat == expected.RequiredFormat && actual.Region == expected.Region &&
		actual.MaxPriceMinor == expected.MaxPriceMinor && actual.AllowSubstitution == expected.AllowSubstitution &&
		actual.ConfirmationNeeded == expected.ConfirmationNeeded &&
		sameRequiredAttributes(actual.RequiredAttributes, expected.RequiredAttributes)
}

func challengeIntentField(intent Intent, field string) string {
	switch field {
	case "category":
		return intent.Category
	case "item":
		return intent.Item
	case "required_format":
		return intent.RequiredFormat
	case "preferred_style":
		return intent.PreferredStyle
	case "region":
		return intent.Region
	default:
		return "<unsupported>"
	}
}

// clarificationQuestionTargetsMissingFields is a conservative lexical proxy,
// not a semantic judge. It requires an interrogative/request form and at least
// one field-specific cue for every reviewer-declared blocking field. A human
// review of generated questions is still needed to establish naturalness or
// semantic adequacy.
func clarificationQuestionTargetsMissingFields(question string, fields []string) bool {
	question = strings.ToLower(strings.TrimSpace(question))
	if question == "" || len(fields) == 0 {
		return false
	}
	askForms := []string{"?", "？", "请问", "请告诉", "请提供", "请告知", "麻烦告知", "麻烦告诉", "请说明", "please tell", "please provide", "could you", "can you", "what ", "which ", "where ", "do you", "would you"}
	if !containsAnyChallengeCue(question, askForms) {
		return false
	}
	fieldCues := map[string][]string{
		"category":        {"品类", "类别", "分类", "哪一类", "什么类", "什么类别", "什么类型", "category", "which type", "what kind"},
		"item":            {"商品", "产品", "哪款", "哪种", "买什么", "想买什么", "具体要什么", "sku", "item", "product", "which one", "which item", "what item"},
		"required_format": {"规格", "包装", "容量", "尺寸", "袋装", "罐装", "盒装", "格式", "形式", "format", "packaging", "package", "size", "volume"},
		"preferred_style": {"风味", "口味", "偏好", "风格", "款式", "烘焙", "style", "flavor", "flavour", "preference"},
		"region":          {"地区", "区域", "配送", "送到哪里", "送达", "收货", "地址", "城市", "region", "delivery", "address", "city", "where to"},
	}
	targetedBlockingField := false
	for _, field := range fields {
		if field == "preferred_style" {
			continue // A soft preference may be volunteered, but cannot block a plan.
		}
		cues, ok := fieldCues[field]
		if !ok || !containsAnyChallengeCue(question, cues) {
			return false
		}
		targetedBlockingField = true
	}
	return targetedBlockingField
}

func containsAnyChallengeCue(text string, cues []string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func challengeAcceptedSubstituteSet(task ChallengeTask, products map[string]Product) map[string]bool {
	accepted := stringSet(task.AcceptedSubstituteSKUs)
	if task.AcceptedSubstituteSKUs != nil || task.Provenance != nil {
		return accepted
	}
	// Compatibility for pre-review generated diagnostics: their only explicit
	// substitute signal was a feasible substitute in GoldSKUs. Human-reviewed
	// suites never use this fallback because provenance requires a separate
	// accepted-substitute label decision.
	for _, sku := range task.GoldSKUs {
		if product, ok := products[sku]; ok && candidateMatchType(product, task.Intent) == MatchTypeSubstitute {
			accepted[sku] = true
		}
	}
	return accepted
}

func challengeRate(numerator, denominator int) ChallengeRate {
	result := ChallengeRate{Numerator: numerator, Denominator: denominator}
	if denominator == 0 {
		return result
	}
	result.Rate = float64(numerator) / float64(denominator)
	result.CI95 = wilsonInterval(numerator, denominator)
	return result
}

func wilsonInterval(successes, total int) [2]float64 {
	if total <= 0 {
		return [2]float64{}
	}
	p := float64(successes) / float64(total)
	const z = 1.959963984540054
	z2 := z * z
	denominator := 1 + z2/float64(total)
	center := (p + z2/(2*float64(total))) / denominator
	half := z * math.Sqrt((p*(1-p)+z2/(4*float64(total)))/float64(total)) / denominator
	return [2]float64{math.Max(0, center-half), math.Min(1, center+half)}
}

// SortedChallengeScenarios provides a stable presentation order for reports.
func SortedChallengeScenarios(report ChallengeEvaluationReport) []string {
	scenarios := make([]string, 0, len(report.ByScenario))
	for scenario := range report.ByScenario {
		scenarios = append(scenarios, scenario)
	}
	sort.Strings(scenarios)
	return scenarios
}
