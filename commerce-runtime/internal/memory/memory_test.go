package memory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

func TestMemoryPayloadHashTamperAndCanonicalIdentity(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	record := testRecord(now, memory.MemoryMerchantOutcome, memory.ScopeMerchant, "did:merchant:a", "", "", "")
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	tampered := *record
	tampered.Summary = "changed without refreshing hash"
	if err := tampered.Validate(); err != memory.ErrInvalidMemory {
		t.Fatalf("tampered record error=%v", err)
	}
	if memory.MemoryIDFor(memory.MemoryMerchantOutcome, memory.ScopeMerchant, "", "", "did:merchant:a", "", "") != record.MemoryID {
		t.Fatal("memory identity is not deterministic")
	}
	secret := *record
	secret.Summary = "historical private_key must not be persisted"
	if err := secret.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := secret.Validate(); err != memory.ErrInvalidMemory {
		t.Fatalf("sensitive memory summary accepted: %v", err)
	}
}

func TestMemoryObservationAndAggregateAreIdempotentAndConcurrent(t *testing.T) {
	store := repository.NewInMemoryStore()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	record := testRecord(now, memory.MemoryMerchantOutcome, memory.ScopeMerchant, "did:merchant:a", "", "", "")
	observation := testObservation(record.MemoryID, "episode-1", now)
	delta := memory.OutcomeFacts{AttemptCount: 1, DeliveryInvalidCount: 1, RecentFailureStreak: 1, LastOutcome: "DELIVERY_INVALID", LastOutcomeAt: now}
	start := make(chan struct{})
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errors <- store.SaveObservationAndUpdateAggregate(context.Background(), record, observation, delta)
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	read, err := store.GetMemory(context.Background(), record.MemoryID)
	if err != nil {
		t.Fatal(err)
	}
	if read.ObservationCount != 1 || read.StructuredFacts.AttemptCount != 1 || read.StructuredFacts.DeliveryInvalidCount != 1 {
		t.Fatalf("replayed aggregate was incremented: %#v", read)
	}
}

func TestMemoryScopeIsolationTTLVersionAndRanking(t *testing.T) {
	store := repository.NewInMemoryStore()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	merchant := testRecord(now, memory.MemoryMerchantOutcome, memory.ScopeMerchant, "did:merchant:a", "", "", "")
	capability := testRecord(now.Add(time.Second), memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "did:merchant:a", "", "", "transcription")
	capability.CatalogVersion = "v1"
	capability.MemoryID = memory.MemoryIDFor(capability.Type, capability.Scope, "", "", capability.MerchantDID, capability.CapabilityID, capability.CatalogVersion)
	capability.FactsRef = "memory://" + capability.MemoryID
	if err := capability.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	requester := testRecord(now.Add(2*time.Second), memory.MemoryRequesterPreference, memory.ScopeRequester, "", "did:requester:a", "", "")
	expired := testRecord(now.Add(-48*time.Hour), memory.MemoryMerchantOutcome, memory.ScopeMerchant, "did:merchant:expired", "", "", "")
	for _, value := range []*memory.MemoryRecord{merchant, capability, requester, expired} {
		if err := store.SaveObservationAndUpdateAggregate(context.Background(), value, testObservation(value.MemoryID, value.MemoryID, value.LastObservedAt), value.StructuredFacts); err != nil {
			t.Fatal(err)
		}
	}
	query := memory.MemoryQuery{RequesterDID: "did:requester:a", MerchantDID: "did:merchant:a", CapabilityID: "transcription", CatalogVersion: "v3", Now: now, Limit: 10}
	values, err := store.SearchMemories(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 {
		t.Fatalf("unexpected scoped memories: %d", len(values))
	}
	if values[0].Scope != memory.ScopeMerchantCapability || values[0].Applicability != memory.ApplicabilityHistoricalVersion {
		t.Fatalf("specific/version ordering not deterministic: %#v", values)
	}
	for _, value := range values {
		if value.MerchantDID == "did:merchant:expired" || value.RequesterDID == "did:requester:other" {
			t.Fatalf("expired or isolated memory returned: %#v", value)
		}
	}
}

func testRecord(now time.Time, typ memory.MemoryType, scope memory.MemoryScope, merchant, requester, parent, capability string) *memory.MemoryRecord {
	id := memory.MemoryIDFor(typ, scope, requester, parent, merchant, capability, "")
	expires := now.Add(24 * time.Hour)
	record := &memory.MemoryRecord{MemoryID: id, Type: typ, Scope: scope, RequesterDID: requester, ParentSessionID: parent, MerchantDID: merchant, CapabilityID: capability, Summary: "initial deterministic outcome", StructuredFacts: memory.OutcomeFacts{AttemptCount: 1, LastOutcome: "DELIVERY_INVALID", LastOutcomeAt: now, RecentFailureStreak: 1}, SourceEpisodeIDs: []string{"episode-1"}, SourceEventRefs: []string{"event-1"}, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://" + id}
	record.Summary = memory.Summarize(*record)
	if err := record.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return record
}

func testObservation(memoryID, episodeID string, now time.Time) *memory.MemoryObservation {
	observation := &memory.MemoryObservation{ObservationID: memory.ObservationIDFor(memoryID, episodeID, string(memory.MemoryMerchantOutcome)), MemoryID: memoryID, SourceEpisodeID: episodeID, SourceEventRef: "event-1", ObservationKind: string(memory.MemoryMerchantOutcome), Outcome: "DELIVERY_INVALID", ObservedAt: now}
	if err := observation.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return observation
}
