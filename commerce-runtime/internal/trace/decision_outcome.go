package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// DecisionStage describes where a model proposal stopped. It is an
// observational taxonomy; it does not grant authority to any stage.
type DecisionStage string

const (
	DecisionStageTransport       DecisionStage = "TRANSPORT"
	DecisionStageParseSchema     DecisionStage = "PARSE_SCHEMA"
	DecisionStageContextEvidence DecisionStage = "CONTEXT_EVIDENCE"
	DecisionStageContextMemory   DecisionStage = "CONTEXT_MEMORY"
	DecisionStageRuntimeGuard    DecisionStage = "RUNTIME_GUARD"
	DecisionStageAccepted        DecisionStage = "ACCEPTED"
	DecisionStageFallback        DecisionStage = "FALLBACK"
)

type DecisionOutcomeTrace struct {
	DecisionOutcomeTraceID string                   `json:"decision_outcome_trace_id"`
	DecisionAttemptID      string                   `json:"decision_attempt_id"`
	EpisodeID              string                   `json:"episode_id"`
	ModelDecisionTraceID   string                   `json:"model_decision_trace_id"`
	ProposalID             string                   `json:"proposal_id"`
	ProposedAction         string                   `json:"proposed_action"`
	Stage                  DecisionStage            `json:"stage"`
	ReachedRuntimeGuard    bool                     `json:"reached_runtime_guard"`
	GuardAccepted          bool                     `json:"guard_accepted"`
	ErrorCode              string                   `json:"error_code,omitempty"`
	CreatedAt              time.Time                `json:"created_at"`
	FactsRef               string                   `json:"facts_ref"`
	PayloadHash            string                   `json:"payload_hash"`
	SideEffectsBefore      *GuardSideEffectSnapshot `json:"side_effects_before,omitempty"`
	SideEffectsAfter       *GuardSideEffectSnapshot `json:"side_effects_after,omitempty"`
}

var ErrInvalidDecisionOutcomeTrace = errors.New("invalid decision outcome trace")

func (t DecisionOutcomeTrace) Validate() error {
	if strings.TrimSpace(t.DecisionOutcomeTraceID) == "" || strings.TrimSpace(t.DecisionAttemptID) == "" || strings.TrimSpace(t.EpisodeID) == "" || strings.TrimSpace(t.ModelDecisionTraceID) == "" || strings.TrimSpace(t.ProposalID) == "" || strings.TrimSpace(t.ProposedAction) == "" || !knownDecisionStage(t.Stage) || t.CreatedAt.IsZero() || t.FactsRef != "decision-outcome-trace://"+t.DecisionOutcomeTraceID || strings.TrimSpace(t.PayloadHash) == "" {
		return ErrInvalidDecisionOutcomeTrace
	}
	expected, err := t.PayloadHashFor()
	if err != nil || expected != t.PayloadHash {
		return ErrInvalidDecisionOutcomeTrace
	}
	return nil
}

func knownDecisionStage(value DecisionStage) bool {
	switch value {
	case DecisionStageTransport, DecisionStageParseSchema, DecisionStageContextEvidence, DecisionStageContextMemory, DecisionStageRuntimeGuard, DecisionStageAccepted, DecisionStageFallback:
		return true
	default:
		return false
	}
}

// DecisionAttemptIDFor is the stable identity of one model proposal attempt.
// Audit records for later context/guard stages must reuse this identity even
// when their trace IDs differ from the original provider trace.
func DecisionAttemptIDFor(episodeID, modelDecisionTraceID, proposalID string) string {
	canonical := strings.Join([]string{strings.TrimSpace(episodeID), strings.TrimSpace(modelDecisionTraceID), strings.TrimSpace(proposalID)}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return "decision-attempt:" + hex.EncodeToString(digest[:])
}

// CanonicalSnapshot excludes PayloadHash so the hash covers every persisted
// decision fact, including the attempt identity and side-effect snapshots.
func (t DecisionOutcomeTrace) CanonicalSnapshot() ([]byte, error) {
	copy := t
	copy.PayloadHash = ""
	return json.Marshal(copy)
}

func (t DecisionOutcomeTrace) PayloadHashFor() (string, error) {
	snapshot, err := t.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (t *DecisionOutcomeTrace) RefreshPayloadHash() error {
	if t == nil {
		return ErrInvalidDecisionOutcomeTrace
	}
	hash, err := t.PayloadHashFor()
	if err != nil {
		return err
	}
	t.PayloadHash = hash
	return nil
}

type GuardSideEffectSnapshot struct {
	PaymentIntentCount      int `json:"payment_intent_count"`
	SettlementCount         int `json:"settlement_count"`
	MerchantInvocationCount int `json:"merchant_invocation_count"`
	DeliveryArtifactCount   int `json:"delivery_artifact_count"`
	BudgetAmendmentCount    int `json:"budget_amendment_count"`
	ParentDecisionCount     int `json:"parent_decision_count"`
}

func (s GuardSideEffectSnapshot) Delta(after GuardSideEffectSnapshot) GuardSideEffectSnapshot {
	return GuardSideEffectSnapshot{
		PaymentIntentCount:      after.PaymentIntentCount - s.PaymentIntentCount,
		SettlementCount:         after.SettlementCount - s.SettlementCount,
		MerchantInvocationCount: after.MerchantInvocationCount - s.MerchantInvocationCount,
		DeliveryArtifactCount:   after.DeliveryArtifactCount - s.DeliveryArtifactCount,
		BudgetAmendmentCount:    after.BudgetAmendmentCount - s.BudgetAmendmentCount,
		ParentDecisionCount:     after.ParentDecisionCount - s.ParentDecisionCount,
	}
}

func (s GuardSideEffectSnapshot) AnyIncrease() bool {
	return s.PaymentIntentCount > 0 || s.SettlementCount > 0 || s.MerchantInvocationCount > 0 || s.DeliveryArtifactCount > 0 || s.BudgetAmendmentCount > 0 || s.ParentDecisionCount > 0
}
