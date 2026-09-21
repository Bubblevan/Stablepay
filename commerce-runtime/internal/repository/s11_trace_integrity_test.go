package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestInMemoryTraceReadsRejectTampering(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	memoryTrace := &memory.MemoryUseTrace{MemoryUseTraceID: "memory-use-1", EpisodeID: "episode-1", ModelDecisionTraceID: "model-1", ContextHash: "sha256:context", ProposedAction: "RETRY_SAME_MERCHANT", CreatedAt: now, FactsRef: "memory-use-trace://memory-use-1"}
	if err := memoryTrace.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMemoryUseTrace(context.Background(), memoryTrace); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.memoryUseTraces[memoryTrace.MemoryUseTraceID].ProposedAction = "SWITCH_MERCHANT"
	store.mu.Unlock()
	if _, err := store.GetMemoryUseTrace(context.Background(), memoryTrace.MemoryUseTraceID); err == nil {
		t.Fatal("tampered memory-use trace was returned")
	}

	outcome := &trace.DecisionOutcomeTrace{DecisionOutcomeTraceID: "outcome-1", DecisionAttemptID: trace.DecisionAttemptIDFor("episode-1", "model-1", "proposal-1"), EpisodeID: "episode-1", ModelDecisionTraceID: "model-1", ProposalID: "proposal-1", ProposedAction: "RETRY_SAME_MERCHANT", Stage: trace.DecisionStageAccepted, GuardAccepted: true, CreatedAt: now, FactsRef: "decision-outcome-trace://outcome-1"}
	if err := outcome.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDecisionOutcomeTrace(context.Background(), outcome); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.decisionOutcomeTraces[outcome.DecisionOutcomeTraceID].GuardAccepted = false
	store.mu.Unlock()
	if _, err := store.ListDecisionOutcomeTraces(context.Background(), outcome.EpisodeID); err == nil {
		t.Fatal("tampered decision outcome was returned")
	}
}
