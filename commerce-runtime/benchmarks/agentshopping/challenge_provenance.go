package agentshopping

import (
	"fmt"
	"strings"
)

const (
	ChallengePromptSourceHumanAuthored = "HUMAN_AUTHORED"
	ChallengeGoldReviewAgreement       = "DOUBLE_ANNOTATED_AGREEMENT"
	ChallengeGoldReviewAdjudicated     = "DOUBLE_ANNOTATED_ADJUDICATED"
	ChallengeProvenanceUnverified      = "UNVERIFIED_INPUT_REQUIRES_INDEPENDENT_REVIEW"
	ChallengeProvenanceDeclared        = "HUMAN_REVIEW_DECLARED_NOT_EXTERNALLY_AUDITED"
)

// ValidateChallengeTaskProvenance enforces the declared review gate for every
// task. Reviewer IDs should be stable pseudonyms, not personal information.
// This validates the submitted declarations and snapshot binding; it cannot
// independently prove that the human review was actually performed.
func ValidateChallengeTaskProvenance(tasks []ChallengeTask, hashes ChallengeEvaluationHashes) error {
	if len(tasks) == 0 {
		return fmt.Errorf("empty challenge suite: %w", ErrInvalidEvaluationSuite)
	}
	if !strings.HasPrefix(hashes.CatalogSHA256, "sha256:") || !strings.HasPrefix(hashes.InventorySHA256, "sha256:") {
		return fmt.Errorf("challenge review gate requires computed catalog and inventory hashes: %w", ErrInvalidEvaluationSuite)
	}
	for _, task := range tasks {
		if err := validateChallengeTaskReviewDeclaration(task); err != nil {
			return err
		}
		provenance := task.Provenance
		if strings.TrimSpace(provenance.ReviewCatalogSHA256) != hashes.CatalogSHA256 ||
			strings.TrimSpace(provenance.ReviewInventorySHA256) != hashes.InventorySHA256 {
			return fmt.Errorf("task %s review is not bound to the evaluated catalog and inventory hashes: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

func validateChallengeTaskReviewDeclaration(task ChallengeTask) error {
	provenance := task.Provenance
	if provenance == nil {
		return fmt.Errorf("task %s has no human-review provenance: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	if provenance.PromptSource != ChallengePromptSourceHumanAuthored ||
		strings.TrimSpace(provenance.PromptAuthorID) == "" ||
		!provenance.PromptWrittenWithoutGeneratedTemplate {
		return fmt.Errorf("task %s prompt is not declared independently human-authored: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	if !provenance.AcceptedSubstituteLabelsExplicit {
		return fmt.Errorf("task %s lacks an explicit accepted-substitute labeling decision: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	if len(provenance.GoldReviewerIDs) < 2 {
		return fmt.Errorf("task %s lacks two independent gold reviewers: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	adjudicatorID := strings.TrimSpace(provenance.GoldAdjudicatorID)
	switch provenance.GoldLabelReviewStatus {
	case ChallengeGoldReviewAgreement:
		if adjudicatorID != "" {
			return fmt.Errorf("task %s declares reviewer agreement but also names an adjudicator: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	case ChallengeGoldReviewAdjudicated:
		if adjudicatorID == "" {
			return fmt.Errorf("task %s has reviewer disagreement without adjudication: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	default:
		return fmt.Errorf("task %s has an unsupported gold-review status: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	for _, digest := range []string{provenance.ReviewCatalogSHA256, provenance.ReviewInventorySHA256,
		provenance.PromptRecordsSHA256, provenance.ReviewerAAnnotationsSHA256,
		provenance.ReviewerBAnnotationsSHA256, provenance.AdjudicationsSHA256} {
		if !validSHA256Digest(digest) {
			return fmt.Errorf("task %s is missing a valid source-record SHA-256 for human review: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	}
	if !provenance.ModelBlind || !provenance.LabelsFrozenBeforeModelRun {
		return fmt.Errorf("task %s review is not declared model-blind and frozen before evaluation: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	identities := map[string]bool{strings.TrimSpace(provenance.PromptAuthorID): true}
	for _, reviewerID := range provenance.GoldReviewerIDs {
		reviewerID = strings.TrimSpace(reviewerID)
		if reviewerID == "" || identities[reviewerID] {
			return fmt.Errorf("task %s has empty or non-independent author/reviewer identities: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		identities[reviewerID] = true
	}
	if adjudicatorID != "" && identities[adjudicatorID] {
		return fmt.Errorf("task %s adjudicator must be distinct from author and reviewers: %w", task.TaskID, ErrInvalidEvaluationSuite)
	}
	return nil
}

// ChallengeTaskProvenanceStatus stays conservative: all checks passing means
// the review declarations are complete, not independently verified.
func ChallengeTaskProvenanceStatus(tasks []ChallengeTask, hashes ChallengeEvaluationHashes) string {
	if err := ValidateChallengeTaskProvenance(tasks, hashes); err != nil {
		return ChallengeProvenanceUnverified
	}
	return ChallengeProvenanceDeclared
}
