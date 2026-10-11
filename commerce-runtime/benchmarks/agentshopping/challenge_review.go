package agentshopping

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
)

// ChallengePromptRecord is the blind-authored portion of a human holdout.
// It contains no gold labels, so authors can prepare prompts separately from
// the annotation files.
type ChallengePromptRecord struct {
	TaskID                                string `json:"task_id"`
	Scenario                              string `json:"scenario"`
	Prompt                                string `json:"prompt"`
	PromptAuthorID                        string `json:"prompt_author_id"`
	PromptWrittenWithoutGeneratedTemplate bool   `json:"prompt_written_without_generated_template"`
	ModelBlind                            bool   `json:"model_blind"`
	BuyerDefaultRegion                    string `json:"buyer_default_region"`
	BuyerBudgetMinor                      int64  `json:"buyer_budget_minor"`
}

// ChallengeReviewerPromptRecord is the only task content reviewers receive.
// It intentionally excludes stratum, prompt-author identity, and process flags.
type ChallengeReviewerPromptRecord struct {
	TaskID             string `json:"task_id"`
	Prompt             string `json:"prompt"`
	BuyerDefaultRegion string `json:"buyer_default_region"`
	BuyerBudgetMinor   int64  `json:"buyer_budget_minor"`
}

// ChallengeReviewLabel is the structured gold annotation independently
// supplied by each reviewer. RelevantSKUs label exact target products,
// AcceptedSubstituteSKUs label allowed alternatives, and GoldSKUs are the
// feasible subset for a purchase plan in the pinned transaction snapshot.
type ChallengeReviewLabel struct {
	ExpectedDisposition    ChallengeDisposition `json:"expected_disposition"`
	Intent                 Intent               `json:"gold_intent"`
	GoldSKUs               []string             `json:"gold_skus,omitempty"`
	RelevantSKUs           []string             `json:"relevant_skus,omitempty"`
	AcceptedSubstituteSKUs []string             `json:"accepted_substitute_skus"`
	RequiredMissingFields  []string             `json:"required_missing_fields,omitempty"`
}

// ChallengeReviewAnnotation is one reviewer's independent label for one
// prompt. Keep reviewer files separate until the adjudication step.
type ChallengeReviewAnnotation struct {
	TaskID                     string               `json:"task_id"`
	ReviewerID                 string               `json:"reviewer_id"`
	ModelBlind                 bool                 `json:"model_blind"`
	LabelsFrozenBeforeModelRun bool                 `json:"labels_frozen_before_model_run"`
	Label                      ChallengeReviewLabel `json:"label"`
}

// ChallengeAdjudicationRecord records the resolved label and rationale for a
// task where the two independent annotations differ.
type ChallengeAdjudicationRecord struct {
	TaskID        string               `json:"task_id"`
	AdjudicatorID string               `json:"adjudicator_id"`
	FinalLabel    ChallengeReviewLabel `json:"final_label"`
	Rationale     string               `json:"rationale"`
}

// ChallengeReviewAudit binds the finalized tasks to the exact author and
// annotation records plus the reviewed catalog/inventory snapshot.
type ChallengeReviewAudit struct {
	SchemaVersion               string                       `json:"schema_version"`
	TaskCount                   int                          `json:"task_count"`
	ScenarioTaskCounts          map[string]int               `json:"scenario_task_counts"`
	DispositionTaskCounts       map[ChallengeDisposition]int `json:"expected_disposition_task_counts"`
	AgreementTaskCount          int                          `json:"agreement_task_count"`
	AdjudicatedTaskCount        int                          `json:"adjudicated_task_count"`
	Agreement                   ChallengeReviewAgreement     `json:"reviewer_agreement"`
	PromptAuthorIDs             []string                     `json:"prompt_author_ids"`
	ReviewerIDs                 [2]string                    `json:"reviewer_ids"`
	Hashes                      ChallengeEvaluationHashes    `json:"hashes"`
	PromptRecordsSHA256         string                       `json:"prompt_records_sha256"`
	ReviewerAPromptPacketSHA256 string                       `json:"reviewer_a_prompt_packet_sha256"`
	ReviewerBPromptPacketSHA256 string                       `json:"reviewer_b_prompt_packet_sha256"`
	ReviewerAAnnotationsSHA256  string                       `json:"reviewer_a_annotations_sha256"`
	ReviewerBAnnotationsSHA256  string                       `json:"reviewer_b_annotations_sha256"`
	AdjudicationsSHA256         string                       `json:"adjudications_sha256"`
	ModelBlindDeclared          bool                         `json:"model_blind_declared"`
	LabelsFrozenBeforeModelRun  bool                         `json:"labels_frozen_before_model_run"`
}

// CreateChallengeReviewerPromptPackets produces independently shuffled,
// metadata-stripped packets. Each reviewer receives random task aliases that
// cannot expose scenario codes embedded in the author's canonical task IDs.
func CreateChallengeReviewerPromptPackets(prompts []ChallengePromptRecord) ([]ChallengeReviewerPromptRecord,
	[]ChallengeReviewerPromptRecord, error) {
	if len(prompts) == 0 || len(prompts) > 500 {
		return nil, nil, fmt.Errorf("reviewer prompt packets require 1–500 prompts: %w", ErrInvalidEvaluationSuite)
	}
	seenTaskIDs := make(map[string]bool, len(prompts))
	seenVisibleContexts := make(map[string]bool, len(prompts))
	for _, prompt := range prompts {
		taskID := strings.TrimSpace(prompt.TaskID)
		visibleContext := normalizedReviewerPromptContext(prompt.Prompt, prompt.BuyerDefaultRegion, prompt.BuyerBudgetMinor)
		if taskID == "" || seenTaskIDs[taskID] || strings.TrimSpace(prompt.Scenario) == "" ||
			strings.TrimSpace(prompt.Prompt) == "" || strings.TrimSpace(prompt.PromptAuthorID) == "" ||
			!prompt.PromptWrittenWithoutGeneratedTemplate || !prompt.ModelBlind ||
			!allowedValue(Regions, prompt.BuyerDefaultRegion) || prompt.BuyerBudgetMinor <= 0 || seenVisibleContexts[visibleContext] {
			return nil, nil, fmt.Errorf("master prompt records are incomplete or have duplicate task IDs/prompt contexts: %w", ErrInvalidEvaluationSuite)
		}
		seenTaskIDs[taskID] = true
		seenVisibleContexts[visibleContext] = true
	}
	orderA, err := randomChallengePromptOrder(len(prompts))
	if err != nil {
		return nil, nil, fmt.Errorf("shuffle reviewer A prompt packet: %w", err)
	}
	orderB, err := randomChallengePromptOrder(len(prompts))
	if err != nil {
		return nil, nil, fmt.Errorf("shuffle reviewer B prompt packet: %w", err)
	}
	if len(prompts) > 1 && equalChallengePromptOrder(orderA, orderB) {
		for attempts := 0; attempts < 32 && equalChallengePromptOrder(orderA, orderB); attempts++ {
			orderB, err = randomChallengePromptOrder(len(prompts))
			if err != nil {
				return nil, nil, fmt.Errorf("reshuffle reviewer B prompt packet: %w", err)
			}
		}
		if equalChallengePromptOrder(orderA, orderB) {
			return nil, nil, fmt.Errorf("could not produce distinct reviewer prompt orderings: %w", ErrInvalidEvaluationSuite)
		}
	}
	usedAliases := make(map[string]bool, 2*len(prompts))
	project := func(order []int) ([]ChallengeReviewerPromptRecord, error) {
		packet := make([]ChallengeReviewerPromptRecord, 0, len(order))
		for _, index := range order {
			prompt := prompts[index]
			var reviewTaskID string
			for attempts := 0; attempts < 16; attempts++ {
				reviewTaskID, err = newChallengeReviewerTaskID()
				if err != nil {
					return nil, err
				}
				if !usedAliases[reviewTaskID] {
					break
				}
				reviewTaskID = ""
			}
			if reviewTaskID == "" {
				return nil, fmt.Errorf("could not allocate a unique reviewer task alias: %w", ErrInvalidEvaluationSuite)
			}
			usedAliases[reviewTaskID] = true
			packet = append(packet, ChallengeReviewerPromptRecord{TaskID: reviewTaskID, Prompt: prompt.Prompt,
				BuyerDefaultRegion: prompt.BuyerDefaultRegion, BuyerBudgetMinor: prompt.BuyerBudgetMinor})
		}
		return packet, nil
	}
	packetA, err := project(orderA)
	if err != nil {
		return nil, nil, err
	}
	packetB, err := project(orderB)
	if err != nil {
		return nil, nil, err
	}
	return packetA, packetB, nil
}

func newChallengeReviewerTaskID() (string, error) {
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return "", err
	}
	return "review-" + hex.EncodeToString(randomID), nil
}

func randomChallengePromptOrder(count int) ([]int, error) {
	order := make([]int, count)
	for index := range order {
		order[index] = index
	}
	for index := count - 1; index > 0; index-- {
		limit := big.NewInt(int64(index + 1))
		swap, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, err
		}
		other := int(swap.Int64())
		order[index], order[other] = order[other], order[index]
	}
	return order, nil
}

func equalChallengePromptOrder(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type ChallengeReviewAgreementRate struct {
	ComparedTaskCount int     `json:"compared_task_count"`
	ExactMatchCount   int     `json:"exact_match_count"`
	ExactMatchRate    float64 `json:"exact_match_rate"`
}

type ChallengeReviewSetAgreement struct {
	ComparedTaskCount int     `json:"compared_task_count"`
	ExactMatchCount   int     `json:"exact_match_count"`
	ExactMatchRate    float64 `json:"exact_match_rate"`
	MacroJaccard      float64 `json:"macro_jaccard"`
	MicroJaccard      float64 `json:"micro_jaccard"`
}

type ChallengeReviewDispositionAgreement struct {
	ChallengeReviewAgreementRate
	CohenKappa       *float64 `json:"cohen_kappa"`
	CohenKappaStatus string   `json:"cohen_kappa_status"`
}

type ChallengeReviewAgreement struct {
	CompleteLabels         ChallengeReviewAgreementRate        `json:"complete_labels"`
	Disposition            ChallengeReviewDispositionAgreement `json:"disposition"`
	Intent                 ChallengeReviewAgreementRate        `json:"intent"`
	FeasibleGoldSKUs       ChallengeReviewSetAgreement         `json:"feasible_gold_skus"`
	RelevantSKUs           ChallengeReviewSetAgreement         `json:"relevant_skus"`
	AcceptedSubstituteSKUs ChallengeReviewSetAgreement         `json:"accepted_substitute_skus"`
	RequiredMissingFields  ChallengeReviewSetAgreement         `json:"required_missing_fields"`
}

type challengeReviewSetAgreementAccumulator struct {
	compared       int
	exact          int
	macroJaccard   float64
	totalIntersect int
	totalUnion     int
}

func (a *challengeReviewSetAgreementAccumulator) add(left, right []string) {
	left = sortedUniqueStrings(left)
	right = sortedUniqueStrings(right)
	a.compared++
	if stringSlicesEqual(left, right) {
		a.exact++
	}
	leftSet := make(map[string]bool, len(left))
	for _, value := range left {
		leftSet[value] = true
	}
	intersect := 0
	for _, value := range right {
		if leftSet[value] {
			intersect++
		}
	}
	union := len(left) + len(right) - intersect
	a.totalIntersect += intersect
	a.totalUnion += union
	if union == 0 {
		a.macroJaccard++
		return
	}
	a.macroJaccard += float64(intersect) / float64(union)
}

func (a challengeReviewSetAgreementAccumulator) metrics() ChallengeReviewSetAgreement {
	result := ChallengeReviewSetAgreement{ComparedTaskCount: a.compared, ExactMatchCount: a.exact}
	if a.compared > 0 {
		result.ExactMatchRate = float64(a.exact) / float64(a.compared)
		result.MacroJaccard = a.macroJaccard / float64(a.compared)
	}
	if a.totalUnion == 0 {
		if a.compared > 0 {
			result.MicroJaccard = 1
		}
		return result
	}
	result.MicroJaccard = float64(a.totalIntersect) / float64(a.totalUnion)
	return result
}

// FinalizeHumanReviewedChallengeTasks merges two separate reviewer streams
// and adjudications into an evaluator-ready suite. It verifies task coverage,
// independent pseudonymous identities, review packet hashes, and every final
// label against the pinned catalog/inventory snapshot. It cannot prove that
// the people behind those IDs performed the stated process.
func FinalizeHumanReviewedChallengeTasks(catalog ImportedCatalog, manifestBytes []byte,
	prompts []ChallengePromptRecord, reviewerA, reviewerB []ChallengeReviewAnnotation,
	adjudications []ChallengeAdjudicationRecord) ([]ChallengeTask, ChallengeReviewAudit, error) {
	// Compatibility helper for package-level tests and legacy callers. It uses
	// real randomized aliases, then translates legacy canonical task IDs before
	// entering the same strict packet validation as the CLI path.
	packetA, packetB, err := CreateChallengeReviewerPromptPackets(prompts)
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	_, reviewerAMap, err := validateChallengeReviewerPromptPacket(prompts, packetA, "reviewer A")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	_, reviewerBMap, err := validateChallengeReviewerPromptPacket(prompts, packetB, "reviewer B")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerA, err = aliasChallengeReviewAnnotations(reviewerA, reviewerAMap, "reviewer A")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerB, err = aliasChallengeReviewAnnotations(reviewerB, reviewerBMap, "reviewer B")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	return FinalizeHumanReviewedChallengeTasksWithPackets(catalog, manifestBytes,
		prompts, packetA, packetB, reviewerA, reviewerB, adjudications)
}

func FinalizeHumanReviewedChallengeTasksWithPackets(catalog ImportedCatalog, manifestBytes []byte,
	prompts []ChallengePromptRecord, reviewerAPrompts, reviewerBPrompts []ChallengeReviewerPromptRecord,
	reviewerA, reviewerB []ChallengeReviewAnnotation,
	adjudications []ChallengeAdjudicationRecord) ([]ChallengeTask, ChallengeReviewAudit, error) {
	if len(prompts) == 0 || len(prompts) > 500 || len(reviewerA) != len(prompts) || len(reviewerB) != len(prompts) {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("review packet must contain 1–500 prompts and two complete annotation sets: %w", ErrInvalidEvaluationSuite)
	}
	promptHash, err := hashJSONLines(prompts)
	if err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("hash prompt records: %w", err)
	}
	reviewerAHash, err := hashJSONLines(reviewerA)
	if err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("hash reviewer A annotations: %w", err)
	}
	reviewerBHash, err := hashJSONLines(reviewerB)
	if err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("hash reviewer B annotations: %w", err)
	}
	adjudicationsHash, err := hashJSONLines(adjudications)
	if err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("hash adjudications: %w", err)
	}
	seedHashes, err := ComputeChallengeHashes(catalog, nil, manifestBytes)
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	promptByID := make(map[string]ChallengePromptRecord, len(prompts))
	orderedPrompts := make([]ChallengePromptRecord, len(prompts))
	authorIDs := make(map[string]bool)
	seenPromptContexts := make(map[string]string, len(prompts))
	scenarioTaskCounts := make(map[string]int)
	for index, prompt := range prompts {
		id := strings.TrimSpace(prompt.TaskID)
		if id == "" || promptByID[id].TaskID != "" || strings.TrimSpace(prompt.Scenario) == "" ||
			strings.TrimSpace(prompt.Prompt) == "" || strings.TrimSpace(prompt.PromptAuthorID) == "" ||
			!prompt.PromptWrittenWithoutGeneratedTemplate || !prompt.ModelBlind ||
			!allowedValue(Regions, prompt.BuyerDefaultRegion) || prompt.BuyerBudgetMinor <= 0 {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("prompt record %q lacks valid human-authorship or independent buyer context: %w", id, ErrInvalidEvaluationSuite)
		}
		prompt.TaskID = id
		prompt.Scenario = strings.ToUpper(strings.TrimSpace(prompt.Scenario))
		prompt.PromptAuthorID = strings.TrimSpace(prompt.PromptAuthorID)
		promptKey := normalizedChallengePromptContext(prompt)
		if previousTaskID, exists := seenPromptContexts[promptKey]; exists {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("prompt %s duplicates the normalized prompt and buyer context from task %s: %w", id, previousTaskID, ErrInvalidEvaluationSuite)
		}
		seenPromptContexts[promptKey] = id
		scenarioTaskCounts[prompt.Scenario]++
		promptByID[id] = prompt
		orderedPrompts[index] = prompt
		authorIDs[prompt.PromptAuthorID] = true
	}
	reviewerAPacketHash, reviewerAMap, err := validateChallengeReviewerPromptPacket(orderedPrompts, reviewerAPrompts, "reviewer A")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerBPacketHash, reviewerBMap, err := validateChallengeReviewerPromptPacket(orderedPrompts, reviewerBPrompts, "reviewer B")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	if err := requireDistinctChallengeReviewerPromptAliases(reviewerAMap, reviewerBMap); err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerA, err = canonicalizeChallengeReviewAnnotations(reviewerA, reviewerAMap, "reviewer A")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerB, err = canonicalizeChallengeReviewAnnotations(reviewerB, reviewerBMap, "reviewer B")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerAByID, reviewerAID, err := indexChallengeReviewAnnotations(reviewerA, len(prompts), "reviewer A")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewerBByID, reviewerBID, err := indexChallengeReviewAnnotations(reviewerB, len(prompts), "reviewer B")
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	if reviewerAID == reviewerBID {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("reviewers A and B must have distinct pseudonymous IDs: %w", ErrInvalidEvaluationSuite)
	}
	for authorID := range authorIDs {
		if authorID == reviewerAID || authorID == reviewerBID {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("prompt author %s must be distinct from both reviewers: %w", authorID, ErrInvalidEvaluationSuite)
		}
	}
	if err := requireSameChallengeTaskIDs(promptByID, reviewerAByID, "reviewer A"); err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	if err := requireSameChallengeTaskIDs(promptByID, reviewerBByID, "reviewer B"); err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	reviewProducts, reviewOffers, err := challengeEvaluationSnapshot(catalog)
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	// Validate each independent annotation before calculating agreement. If an
	// invalid SKU, infeasible plan, or inconsistent disposition were only
	// noticed after adjudication, the audit could publish agreement statistics
	// over labels that were never valid for the pinned snapshot.
	for _, prompt := range orderedPrompts {
		for _, review := range []struct {
			name string
			row  ChallengeReviewAnnotation
		}{
			{name: "reviewer A", row: reviewerAByID[prompt.TaskID]},
			{name: "reviewer B", row: reviewerBByID[prompt.TaskID]},
		} {
			if err := validateChallengeReviewLabel(reviewProducts, reviewOffers, prompt, review.row.Label); err != nil {
				return nil, ChallengeReviewAudit{}, fmt.Errorf("%s label for task %s is invalid against the pinned snapshot: %w", review.name, prompt.TaskID, err)
			}
		}
	}
	adjudicationByID := make(map[string]ChallengeAdjudicationRecord, len(adjudications))
	for _, adjudication := range adjudications {
		id := strings.TrimSpace(adjudication.TaskID)
		if id == "" || adjudicationByID[id].TaskID != "" || promptByID[id].TaskID == "" ||
			strings.TrimSpace(adjudication.AdjudicatorID) == "" || strings.TrimSpace(adjudication.Rationale) == "" {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("adjudication record %q is empty, duplicated, or unbound: %w", id, ErrInvalidEvaluationSuite)
		}
		adjudication.TaskID = id
		adjudication.AdjudicatorID = strings.TrimSpace(adjudication.AdjudicatorID)
		if authorIDs[adjudication.AdjudicatorID] || adjudication.AdjudicatorID == reviewerAID || adjudication.AdjudicatorID == reviewerBID {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("adjudicator %s must be distinct from authors and reviewers: %w", adjudication.AdjudicatorID, ErrInvalidEvaluationSuite)
		}
		adjudicationByID[id] = adjudication
	}
	if len(adjudicationByID) > len(prompts) {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("too many adjudication records: %w", ErrInvalidEvaluationSuite)
	}

	tasks := make([]ChallengeTask, 0, len(prompts))
	agreementCount, adjudicatedCount := 0, 0
	var dispositionConfusion [3][3]int
	var feasibleGoldAgreement, relevantSKUAgreement, acceptedSubstituteAgreement, missingFieldsAgreement challengeReviewSetAgreementAccumulator
	intentAgreementCount := 0
	dispositionTaskCounts := make(map[ChallengeDisposition]int)
	for _, prompt := range orderedPrompts { // preserve the prompt author's frozen task order.
		id := prompt.TaskID
		a := reviewerAByID[id]
		b := reviewerBByID[id]
		leftDisposition, leftOK := challengeDispositionIndex(a.Label.ExpectedDisposition)
		rightDisposition, rightOK := challengeDispositionIndex(b.Label.ExpectedDisposition)
		if !leftOK || !rightOK {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("reviewers supplied an unsupported disposition for %s: %w", id, ErrInvalidEvaluationSuite)
		}
		dispositionConfusion[leftDisposition][rightDisposition]++
		if challengeReviewIntentsEqual(a.Label.Intent, b.Label.Intent) {
			intentAgreementCount++
		}
		if a.Label.ExpectedDisposition == ChallengePlan && b.Label.ExpectedDisposition == ChallengePlan {
			feasibleGoldAgreement.add(a.Label.GoldSKUs, b.Label.GoldSKUs)
			relevantSKUAgreement.add(a.Label.RelevantSKUs, b.Label.RelevantSKUs)
		}
		acceptedSubstituteAgreement.add(a.Label.AcceptedSubstituteSKUs, b.Label.AcceptedSubstituteSKUs)
		if a.Label.ExpectedDisposition == ChallengeClarify && b.Label.ExpectedDisposition == ChallengeClarify {
			missingFieldsAgreement.add(a.Label.RequiredMissingFields, b.Label.RequiredMissingFields)
		}
		labelsAgree, err := challengeReviewLabelsEqual(a.Label, b.Label)
		if err != nil {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("compare independent labels for %s: %w", id, err)
		}
		finalLabel := a.Label
		adjudicatorID := ""
		reviewStatus := ChallengeGoldReviewAgreement
		if labelsAgree {
			if _, exists := adjudicationByID[id]; exists {
				return nil, ChallengeReviewAudit{}, fmt.Errorf("task %s has an unnecessary adjudication despite reviewer agreement: %w", id, ErrInvalidEvaluationSuite)
			}
			agreementCount++
		} else {
			adjudication, exists := adjudicationByID[id]
			if !exists {
				return nil, ChallengeReviewAudit{}, fmt.Errorf("task %s has reviewer disagreement without adjudication: %w", id, ErrInvalidEvaluationSuite)
			}
			finalLabel = adjudication.FinalLabel
			adjudicatorID = adjudication.AdjudicatorID
			reviewStatus = ChallengeGoldReviewAdjudicated
			adjudicatedCount++
		}
		if finalLabel.AcceptedSubstituteSKUs == nil {
			return nil, ChallengeReviewAudit{}, fmt.Errorf("final label for %s must explicitly include accepted_substitute_skus (use [] when none are accepted): %w", id, ErrInvalidEvaluationSuite)
		}
		dispositionTaskCounts[finalLabel.ExpectedDisposition]++
		tasks = append(tasks, ChallengeTask{TaskID: prompt.TaskID, Scenario: prompt.Scenario, Prompt: prompt.Prompt,
			ExpectedDisposition: finalLabel.ExpectedDisposition, Intent: cloneChallengeReviewIntent(finalLabel.Intent),
			BuyerDefaultRegion: prompt.BuyerDefaultRegion, BuyerBudgetMinor: prompt.BuyerBudgetMinor,
			GoldSKUs: cloneStrings(finalLabel.GoldSKUs), RelevantSKUs: cloneStrings(finalLabel.RelevantSKUs),
			AcceptedSubstituteSKUs: cloneStrings(finalLabel.AcceptedSubstituteSKUs),
			RequiredMissingFields:  cloneStrings(finalLabel.RequiredMissingFields),
			Provenance: &ChallengeTaskProvenance{PromptSource: ChallengePromptSourceHumanAuthored,
				PromptAuthorID: prompt.PromptAuthorID, PromptWrittenWithoutGeneratedTemplate: prompt.PromptWrittenWithoutGeneratedTemplate,
				AcceptedSubstituteLabelsExplicit: true,
				GoldLabelReviewStatus:            reviewStatus, GoldReviewerIDs: []string{reviewerAID, reviewerBID},
				GoldAdjudicatorID: adjudicatorID, ReviewCatalogSHA256: seedHashes.CatalogSHA256,
				ReviewInventorySHA256: seedHashes.InventorySHA256, PromptRecordsSHA256: promptHash,
				ReviewerAAnnotationsSHA256: reviewerAHash, ReviewerBAnnotationsSHA256: reviewerBHash,
				AdjudicationsSHA256: adjudicationsHash, ModelBlind: prompt.ModelBlind && a.ModelBlind && b.ModelBlind,
				LabelsFrozenBeforeModelRun: a.LabelsFrozenBeforeModelRun && b.LabelsFrozenBeforeModelRun}})
	}
	if len(adjudicationByID) != adjudicatedCount {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("adjudication file contains records for tasks without disagreements: %w", ErrInvalidEvaluationSuite)
	}
	if err := ValidateChallengeTaskSet(catalog, tasks); err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("final human-reviewed labels do not match the pinned snapshot: %w", err)
	}
	hashes, err := ComputeChallengeHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return nil, ChallengeReviewAudit{}, err
	}
	if err := ValidateChallengeTaskProvenance(tasks, hashes); err != nil {
		return nil, ChallengeReviewAudit{}, fmt.Errorf("final human-review provenance is incomplete: %w", err)
	}
	dispositionAgreement := challengeReviewAgreementRate(len(prompts), diagonalSum(dispositionConfusion))
	kappa, kappaStatus := challengeDispositionCohenKappa(dispositionConfusion, len(prompts))
	ids := make([]string, 0, len(authorIDs))
	for id := range authorIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	audit := ChallengeReviewAudit{SchemaVersion: "stablepay.agent-shopping-human-review.v5",
		TaskCount: len(tasks), ScenarioTaskCounts: scenarioTaskCounts, DispositionTaskCounts: dispositionTaskCounts,
		AgreementTaskCount: agreementCount, AdjudicatedTaskCount: adjudicatedCount,
		Agreement: ChallengeReviewAgreement{
			CompleteLabels:         challengeReviewAgreementRate(len(prompts), agreementCount),
			Disposition:            ChallengeReviewDispositionAgreement{ChallengeReviewAgreementRate: dispositionAgreement, CohenKappa: kappa, CohenKappaStatus: kappaStatus},
			Intent:                 challengeReviewAgreementRate(len(prompts), intentAgreementCount),
			FeasibleGoldSKUs:       feasibleGoldAgreement.metrics(),
			RelevantSKUs:           relevantSKUAgreement.metrics(),
			AcceptedSubstituteSKUs: acceptedSubstituteAgreement.metrics(),
			RequiredMissingFields:  missingFieldsAgreement.metrics(),
		},
		PromptAuthorIDs: ids, ReviewerIDs: [2]string{reviewerAID, reviewerBID}, Hashes: hashes,
		PromptRecordsSHA256: promptHash, ReviewerAPromptPacketSHA256: reviewerAPacketHash,
		ReviewerBPromptPacketSHA256: reviewerBPacketHash, ReviewerAAnnotationsSHA256: reviewerAHash,
		ReviewerBAnnotationsSHA256: reviewerBHash, AdjudicationsSHA256: adjudicationsHash,
		ModelBlindDeclared: true, LabelsFrozenBeforeModelRun: true}
	return tasks, audit, nil
}

func indexChallengeReviewAnnotations(records []ChallengeReviewAnnotation, expectedCount int, reviewerName string) (map[string]ChallengeReviewAnnotation, string, error) {
	byID := make(map[string]ChallengeReviewAnnotation, len(records))
	reviewerID := ""
	for _, record := range records {
		id := strings.TrimSpace(record.TaskID)
		currentReviewerID := strings.TrimSpace(record.ReviewerID)
		if id == "" || currentReviewerID == "" || byID[id].TaskID != "" ||
			!record.ModelBlind || !record.LabelsFrozenBeforeModelRun {
			return nil, "", fmt.Errorf("%s annotation %q lacks unique task identity or blind/freeze declaration: %w", reviewerName, id, ErrInvalidEvaluationSuite)
		}
		if record.Label.AcceptedSubstituteSKUs == nil {
			return nil, "", fmt.Errorf("%s annotation %s must explicitly label accepted_substitute_skus (use [] when none are accepted): %w", reviewerName, id, ErrInvalidEvaluationSuite)
		}
		if reviewerID == "" {
			reviewerID = currentReviewerID
		} else if reviewerID != currentReviewerID {
			return nil, "", fmt.Errorf("%s file must contain one stable reviewer pseudonym: %w", reviewerName, ErrInvalidEvaluationSuite)
		}
		record.TaskID = id
		record.ReviewerID = currentReviewerID
		byID[id] = record
	}
	if len(byID) != expectedCount {
		return nil, "", fmt.Errorf("%s annotation count %d does not match prompt count %d: %w", reviewerName, len(byID), expectedCount, ErrInvalidEvaluationSuite)
	}
	return byID, reviewerID, nil
}

func requireSameChallengeTaskIDs(prompts map[string]ChallengePromptRecord,
	annotations map[string]ChallengeReviewAnnotation, reviewerName string) error {
	for id := range prompts {
		if annotations[id].TaskID == "" {
			return fmt.Errorf("%s annotations are missing task %s: %w", reviewerName, id, ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

func validateChallengeReviewerPromptPacket(master []ChallengePromptRecord,
	packet []ChallengeReviewerPromptRecord, reviewerName string) (string, map[string]string, error) {
	if len(packet) != len(master) {
		return "", nil, fmt.Errorf("%s prompt packet has %d tasks; expected %d: %w", reviewerName, len(packet), len(master), ErrInvalidEvaluationSuite)
	}
	masterByVisibleContext := make(map[string]ChallengePromptRecord, len(master))
	for _, prompt := range master {
		key := normalizedReviewerPromptContext(prompt.Prompt, prompt.BuyerDefaultRegion, prompt.BuyerBudgetMinor)
		if _, exists := masterByVisibleContext[key]; exists {
			return "", nil, fmt.Errorf("%s prompt packet cannot bind duplicate visible prompt/context records: %w", reviewerName, ErrInvalidEvaluationSuite)
		}
		masterByVisibleContext[key] = prompt
	}
	seenReviewerIDs := make(map[string]bool, len(packet))
	seenMasterIDs := make(map[string]bool, len(packet))
	reviewerToMaster := make(map[string]string, len(packet))
	for _, row := range packet {
		reviewTaskID := strings.TrimSpace(row.TaskID)
		key := normalizedReviewerPromptContext(row.Prompt, row.BuyerDefaultRegion, row.BuyerBudgetMinor)
		prompt, exists := masterByVisibleContext[key]
		if !validChallengeReviewerTaskID(reviewTaskID) || !exists || seenReviewerIDs[reviewTaskID] || seenMasterIDs[prompt.TaskID] ||
			row.TaskID != reviewTaskID || row.Prompt != prompt.Prompt ||
			row.BuyerDefaultRegion != prompt.BuyerDefaultRegion || row.BuyerBudgetMinor != prompt.BuyerBudgetMinor {
			return "", nil, fmt.Errorf("%s prompt packet row %q is missing, duplicated, or differs from the frozen prompt/context: %w", reviewerName, reviewTaskID, ErrInvalidEvaluationSuite)
		}
		seenReviewerIDs[reviewTaskID] = true
		seenMasterIDs[prompt.TaskID] = true
		reviewerToMaster[reviewTaskID] = prompt.TaskID
	}
	hash, err := hashJSONLines(packet)
	if err != nil {
		return "", nil, fmt.Errorf("hash %s prompt packet: %w", reviewerName, err)
	}
	return hash, reviewerToMaster, nil
}

func requireDistinctChallengeReviewerPromptAliases(reviewerAMap, reviewerBMap map[string]string) error {
	for reviewerTaskID := range reviewerAMap {
		if _, reused := reviewerBMap[reviewerTaskID]; reused {
			return fmt.Errorf("reviewer task aliases must be independent across prompt packets: %w", ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

func validChallengeReviewerTaskID(taskID string) bool {
	const prefix = "review-"
	if !strings.HasPrefix(taskID, prefix) || len(taskID) != len(prefix)+32 {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(taskID, prefix))
	return err == nil && len(decoded) == 16
}

func aliasChallengeReviewAnnotations(records []ChallengeReviewAnnotation,
	reviewerToMaster map[string]string, reviewerName string) ([]ChallengeReviewAnnotation, error) {
	masterToReviewer := make(map[string]string, len(reviewerToMaster))
	for reviewerID, masterID := range reviewerToMaster {
		masterToReviewer[masterID] = reviewerID
	}
	aliased := make([]ChallengeReviewAnnotation, len(records))
	for index, record := range records {
		masterID := strings.TrimSpace(record.TaskID)
		reviewerID, exists := masterToReviewer[masterID]
		if !exists {
			return nil, fmt.Errorf("%s legacy annotation task %q is not in the master prompts: %w", reviewerName, masterID, ErrInvalidEvaluationSuite)
		}
		record.TaskID = reviewerID
		aliased[index] = record
	}
	return aliased, nil
}

func canonicalizeChallengeReviewAnnotations(records []ChallengeReviewAnnotation,
	reviewerToMaster map[string]string, reviewerName string) ([]ChallengeReviewAnnotation, error) {
	canonical := make([]ChallengeReviewAnnotation, len(records))
	seen := make(map[string]bool, len(records))
	for index, record := range records {
		reviewTaskID := strings.TrimSpace(record.TaskID)
		masterTaskID, exists := reviewerToMaster[reviewTaskID]
		if !exists || seen[masterTaskID] {
			return nil, fmt.Errorf("%s annotation task %q is missing from or duplicated in the blinded prompt packet: %w", reviewerName, reviewTaskID, ErrInvalidEvaluationSuite)
		}
		record.TaskID = masterTaskID
		canonical[index] = record
		seen[masterTaskID] = true
	}
	if len(seen) != len(reviewerToMaster) {
		return nil, fmt.Errorf("%s annotation coverage does not match the blinded prompt packet: %w", reviewerName, ErrInvalidEvaluationSuite)
	}
	return canonical, nil
}

func validateChallengeReviewLabel(products []Product, offers map[string]Inventory,
	prompt ChallengePromptRecord, label ChallengeReviewLabel) error {
	task := ChallengeTask{
		TaskID: prompt.TaskID, Scenario: prompt.Scenario, Prompt: prompt.Prompt,
		ExpectedDisposition: label.ExpectedDisposition, Intent: cloneChallengeReviewIntent(label.Intent),
		BuyerDefaultRegion: prompt.BuyerDefaultRegion, BuyerBudgetMinor: prompt.BuyerBudgetMinor,
		GoldSKUs: cloneStrings(label.GoldSKUs), RelevantSKUs: cloneStrings(label.RelevantSKUs),
		AcceptedSubstituteSKUs: cloneStrings(label.AcceptedSubstituteSKUs),
		RequiredMissingFields:  cloneStrings(label.RequiredMissingFields),
		Provenance:             &ChallengeTaskProvenance{AcceptedSubstituteLabelsExplicit: true},
	}
	return validateChallengeTasks(products, offers, []ChallengeTask{task})
}

func challengeReviewLabelsEqual(left, right ChallengeReviewLabel) (bool, error) {
	leftBytes, err := json.Marshal(normalizeChallengeReviewLabel(left))
	if err != nil {
		return false, err
	}
	rightBytes, err := json.Marshal(normalizeChallengeReviewLabel(right))
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftBytes, rightBytes), nil
}

func challengeReviewIntentsEqual(left, right Intent) bool {
	leftBytes, leftErr := json.Marshal(cloneChallengeReviewIntent(left))
	rightBytes, rightErr := json.Marshal(cloneChallengeReviewIntent(right))
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func normalizedChallengePromptContext(prompt ChallengePromptRecord) string {
	return normalizedReviewerPromptContext(prompt.Prompt, prompt.BuyerDefaultRegion, prompt.BuyerBudgetMinor)
}

func normalizedReviewerPromptContext(prompt, region string, budget int64) string {
	normalizedText := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(prompt)), " "))
	return fmt.Sprintf("%s\x00%s\x00%d", normalizedText, strings.TrimSpace(region), budget)
}

func challengeReviewAgreementRate(compared, exact int) ChallengeReviewAgreementRate {
	result := ChallengeReviewAgreementRate{ComparedTaskCount: compared, ExactMatchCount: exact}
	if compared > 0 {
		result.ExactMatchRate = float64(exact) / float64(compared)
	}
	return result
}

func challengeDispositionIndex(disposition ChallengeDisposition) (int, bool) {
	switch disposition {
	case ChallengePlan:
		return 0, true
	case ChallengeNoPlan:
		return 1, true
	case ChallengeClarify:
		return 2, true
	default:
		return 0, false
	}
}

func diagonalSum(confusion [3][3]int) int {
	return confusion[0][0] + confusion[1][1] + confusion[2][2]
}

func challengeDispositionCohenKappa(confusion [3][3]int, total int) (*float64, string) {
	if total <= 0 {
		return nil, "NOT_APPLICABLE_NO_TASKS"
	}
	rowTotals, columnTotals := [3]int{}, [3]int{}
	for row := range confusion {
		for column := range confusion[row] {
			rowTotals[row] += confusion[row][column]
			columnTotals[column] += confusion[row][column]
		}
	}
	observed := float64(diagonalSum(confusion)) / float64(total)
	expected := 0.0
	for category := range rowTotals {
		expected += float64(rowTotals[category]) * float64(columnTotals[category]) / float64(total*total)
	}
	if math.Abs(1-expected) < 1e-12 {
		return nil, "UNDEFINED_DEGENERATE_MARGINALS"
	}
	kappa := (observed - expected) / (1 - expected)
	return &kappa, "DEFINED"
}

func normalizeChallengeReviewLabel(label ChallengeReviewLabel) ChallengeReviewLabel {
	label.GoldSKUs = sortedUniqueStrings(label.GoldSKUs)
	label.RelevantSKUs = sortedUniqueStrings(label.RelevantSKUs)
	label.AcceptedSubstituteSKUs = sortedUniqueStrings(label.AcceptedSubstituteSKUs)
	label.RequiredMissingFields = sortedUniqueStrings(label.RequiredMissingFields)
	label.Intent = cloneChallengeReviewIntent(label.Intent)
	return label
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := cloneStrings(values)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneChallengeReviewIntent(intent Intent) Intent {
	if intent.RequiredAttributes != nil {
		attributes := make(map[string]string, len(intent.RequiredAttributes))
		for key, value := range intent.RequiredAttributes {
			attributes[key] = value
		}
		intent.RequiredAttributes = attributes
	}
	return intent
}

func validSHA256Digest(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}
