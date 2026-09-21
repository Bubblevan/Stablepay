package trace

import (
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
	if strings.TrimSpace(t.DecisionOutcomeTraceID) == "" || strings.TrimSpace(t.EpisodeID) == "" || strings.TrimSpace(t.ModelDecisionTraceID) == "" || strings.TrimSpace(t.ProposalID) == "" || strings.TrimSpace(t.ProposedAction) == "" || t.Stage == "" || t.CreatedAt.IsZero() || t.FactsRef != "decision-outcome-trace://"+t.DecisionOutcomeTraceID || strings.TrimSpace(t.PayloadHash) == "" {
		return ErrInvalidDecisionOutcomeTrace
	}
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
