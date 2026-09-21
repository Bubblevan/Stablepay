package trace

import (
	"testing"
	"time"
)

func TestDecisionOutcomeTraceRejectsTamperedPayload(t *testing.T) {
	value := DecisionOutcomeTrace{
		DecisionOutcomeTraceID: "dot-1",
		DecisionAttemptID:      DecisionAttemptIDFor("episode-1", "model-1", "proposal-1"),
		EpisodeID:              "episode-1",
		ModelDecisionTraceID:   "model-1",
		ProposalID:             "proposal-1",
		ProposedAction:         "RETRY_SAME_MERCHANT",
		Stage:                  DecisionStageRuntimeGuard,
		ReachedRuntimeGuard:    true,
		GuardAccepted:          false,
		CreatedAt:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		FactsRef:               "decision-outcome-trace://dot-1",
	}
	if err := value.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("fresh decision outcome rejected: %v", err)
	}
	value.ProposedAction = "SWITCH_MERCHANT"
	if err := value.Validate(); err == nil {
		t.Fatal("tampered decision outcome passed hash validation")
	}
}
