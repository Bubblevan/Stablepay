package application

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

func TestHistoricalMemoryCannotOverrideCurrentTrustedQuote(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := repository.NewInMemoryStore()
	service := NewService(store, WithClock(func() time.Time { return now }), WithUnconstrainedSettlementPolicyForTests(), WithMemoryStore(store))
	created, err := service.CreateEpisode(context.Background(), requestFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	record := boundaryMemory(now, "did:merchant:history", "capability:history", memory.MemoryMerchantOutcome)
	observation := &memory.MemoryObservation{ObservationID: memory.ObservationIDFor(record.MemoryID, created.Episode.EpisodeID, string(record.Type)), MemoryID: record.MemoryID, SourceEpisodeID: created.Episode.EpisodeID, SourceEventRef: "history-event", ObservationKind: string(record.Type), ObservedAt: now}
	if err := observation.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservationAndUpdateAggregate(context.Background(), record, observation, record.StructuredFacts); err != nil {
		t.Fatal(err)
	}
	current := advanceToNegotiating(t, service, created.Episode, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:history", CapabilityID: "capability:history", PayeeDID: "did:payee:history", QuoteHash: "sha256:current-quote", AmountMinor: 500, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s7-current-quote", TraceID: "s7-quote"})
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Intent.AmountMinor != 500 || reserved.Episode.Budget.ReservedAmount != 500 {
		t.Fatalf("historical memory changed trusted quote: intent=%#v episode=%#v", reserved.Intent, reserved.Episode)
	}
	decisionContext, err := service.BuildDecisionContext(context.Background(), S6DecisionRequest{EpisodeID: current.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisionContext.RetrievedMemories) == 0 {
		t.Fatal("historical memory was not available as advisory context")
	}
}

func boundaryMemory(now time.Time, merchant, capability string, typ memory.MemoryType) *memory.MemoryRecord {
	id := memory.MemoryIDFor(typ, memory.ScopeMerchant, "", "", merchant, "", "")
	expires := now.Add(time.Hour)
	record := &memory.MemoryRecord{MemoryID: id, Type: typ, Scope: memory.ScopeMerchant, MerchantDID: merchant, Summary: "historical average was 200 minor units; advisory only", StructuredFacts: memory.OutcomeFacts{AttemptCount: 1, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: now}, SourceEpisodeIDs: []string{"prior"}, SourceEventRefs: []string{"prior-event"}, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://" + id}
	record.Summary = memory.Summarize(*record)
	if err := record.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	_ = capability
	return record
}
