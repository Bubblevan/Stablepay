package llm_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

// This opt-in test proves the persistent-memory -> DecisionContext -> real
// DeepSeek path without spending Devnet payment budget. It never runs in the
// ordinary unit gate unless LLM credentials are explicitly present.
func TestRealDeepSeekMemoryIntegration(t *testing.T) {
	providerName := firstNonEmptyEnv("LLM_PROVIDER", "openai-compatible")
	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := firstNonEmptyEnv("LLM_MODEL", "LLM_MODEL_ID")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("LLM_BASE_URL, LLM_API_KEY and LLM_MODEL/LLM_MODEL_ID are required")
	}
	store := repository.NewInMemoryStore()
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := realMemoryFixture(now)
	observation := &memory.MemoryObservation{ObservationID: memory.ObservationIDFor(record.MemoryID, "memory-history-1", string(record.Type)), MemoryID: record.MemoryID, SourceEpisodeID: "memory-history-1", SourceEventRef: "memory-event-1", ObservationKind: string(record.Type), Outcome: "DELIVERY_INVALID", ObservedAt: now}
	if err := observation.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservationAndUpdateAggregate(context.Background(), record, observation, record.StructuredFacts); err != nil {
		t.Fatal(err)
	}
	retrieved, err := store.SearchMemories(context.Background(), memory.MemoryQuery{MerchantDID: record.MerchantDID, RequesterDID: "did:requester:s7-real", Now: now, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(retrieved) != 1 {
		t.Fatalf("persistent memory was not retrieved: %#v", retrieved)
	}
	evidenceRecord := evidence.NewRecord("memory-evidence", string(evidence.SourceProtocolDoc), "protocol://memory", "v1", evidence.HashString("memory"), "text/plain", "Historical memory is advisory.", evidence.TrustProtocolDoc, now)
	ctxValue, err := llm.BuildDecisionContext(llm.ContextInput{Episode: &episode.CommerceEpisode{EpisodeID: "memory-e2", RequestID: "memory-e2", State: episode.StateRecovering, DeadlineAt: now.Add(10 * time.Minute), Version: 2, Budget: episode.BudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 1000, AvailableBudget: 1000, RefundReusable: true}}, AllowedActions: []trace.ActionType{trace.ActionStop, trace.ActionAskParent}, RetrievedMemories: []memory.MemoryRecord{*retrieved[0]}, RetrievedEvidence: []evidence.EvidenceRecord{*evidenceRecord}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := llm.NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := llm.NewLLMDecisionProvider(client, llm.WithProviderName(providerName), llm.WithModelRef(model), llm.WithProviderTTL(2*time.Minute))
	callCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := provider.ProposeWithTrace(callCtx, ctxValue)
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.Status != llm.TraceSuccess || len(result.Proposal.MemoryRefs) == 0 {
		t.Fatalf("real model did not cite current memory: trace=%#v proposal=%#v", result.Trace, result.Proposal)
	}
	if err := llm.ValidateProposalMemoryRefsAgainstContext(ctxValue, result.Proposal); err != nil {
		t.Fatal(err)
	}
	t.Logf("real memory integration provider=%s model=%s calls=1 memory_ref=%s action=%s", providerName, model, result.Proposal.MemoryRefs[0], result.Proposal.ProposedAction)
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func realMemoryFixture(now time.Time) *memory.MemoryRecord {
	id := memory.MemoryIDFor(memory.MemoryMerchantOutcome, memory.ScopeMerchant, "", "", "did:merchant:memory", "", "")
	expires := now.Add(24 * time.Hour)
	record := &memory.MemoryRecord{MemoryID: id, Type: memory.MemoryMerchantOutcome, Scope: memory.ScopeMerchant, MerchantDID: "did:merchant:memory", Summary: "did:merchant:memory outcome history: attempts=1, valid=0, invalid=1, fulfilled=0, payment_failed=0, merchant_error=0, switch_away=1, last=DELIVERY_INVALID", StructuredFacts: memory.OutcomeFacts{AttemptCount: 1, DeliveryInvalidCount: 1, SwitchAwayCount: 1, RecentFailureStreak: 1, LastOutcome: "DELIVERY_INVALID", LastOutcomeAt: now}, SourceEpisodeIDs: []string{"memory-history-1"}, SourceEventRefs: []string{"memory-event-1"}, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://" + id}
	if err := record.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return record
}
