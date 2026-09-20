package mysql

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
)

func TestMySQLS7MemoryAtomicIdempotencyConcurrencyAndRollback(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is required for S7 MySQL persistence integration")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := s7IntegrationRecord(now, "did:merchant:mysql-s7")
	obsA := s7IntegrationObservation(record.MemoryID, "s7-episode-a", now, "event-a")
	obsB := s7IntegrationObservation(record.MemoryID, "s7-episode-b", now.Add(time.Second), "event-b")
	t.Cleanup(func() {
		db.Exec("DELETE FROM memory_observations WHERE memory_id = ?", record.MemoryID)
		db.Exec("DELETE FROM memory_records WHERE memory_id = ?", record.MemoryID)
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, obs := range []*memory.MemoryObservation{obsA, obsB} {
		wait.Add(1)
		go func(value *memory.MemoryObservation) {
			defer wait.Done()
			<-start
			results <- store.SaveObservationAndUpdateAggregate(context.Background(), record, value, memory.OutcomeFacts{AttemptCount: 1, DeliveryValidCount: 1, FulfilledCount: 1, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: value.ObservedAt})
		}(obs)
	}
	close(start)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	read, err := store.GetMemory(context.Background(), record.MemoryID)
	if err != nil {
		t.Fatal(err)
	}
	if read.ObservationCount != 2 || read.StructuredFacts.AttemptCount != 2 || read.StructuredFacts.FulfilledCount != 2 {
		t.Fatalf("lost concurrent aggregate update: %#v", read)
	}
	if err := store.SaveObservationAndUpdateAggregate(context.Background(), record, obsA, memory.OutcomeFacts{AttemptCount: 1, DeliveryValidCount: 1, FulfilledCount: 1, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: obsA.ObservedAt}); err != nil {
		t.Fatal(err)
	}
	read, err = store.GetMemory(context.Background(), record.MemoryID)
	if err != nil || read.ObservationCount != 2 {
		t.Fatalf("replay changed aggregate: %#v err=%v", read, err)
	}

	rollbackRecord := s7IntegrationRecord(now, "did:merchant:mysql-s7-rollback")
	rollbackObservation := s7IntegrationObservation(rollbackRecord.MemoryID, "s7-episode-rollback", now, strings.Repeat("e", 300))
	err = store.SaveObservationAndUpdateAggregate(context.Background(), rollbackRecord, rollbackObservation, memory.OutcomeFacts{AttemptCount: 1, LastOutcome: "DELIVERY_RECEIVED", LastOutcomeAt: now})
	if err == nil {
		t.Fatal("oversized observation unexpectedly committed")
	}
	if _, getErr := store.GetMemory(context.Background(), rollbackRecord.MemoryID); !errors.Is(getErr, memory.ErrMemoryNotFound) {
		t.Fatalf("transaction did not roll back aggregate: %v", getErr)
	}
}

func s7IntegrationRecord(now time.Time, merchant string) *memory.MemoryRecord {
	id := memory.MemoryIDFor(memory.MemoryMerchantOutcome, memory.ScopeMerchant, "", "", merchant, "", "")
	expires := now.Add(time.Hour)
	record := &memory.MemoryRecord{MemoryID: id, Type: memory.MemoryMerchantOutcome, Scope: memory.ScopeMerchant, MerchantDID: merchant, Summary: "mysql s7 fixture", StructuredFacts: memory.OutcomeFacts{AttemptCount: 1, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: now}, SourceEpisodeIDs: []string{"s7-seed"}, SourceEventRefs: []string{"s7-event"}, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://" + id}
	record.Summary = memory.Summarize(*record)
	if err := record.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return record
}

func s7IntegrationObservation(memoryID, episodeID string, observedAt time.Time, eventRef string) *memory.MemoryObservation {
	observation := &memory.MemoryObservation{ObservationID: memory.ObservationIDFor(memoryID, episodeID, string(memory.MemoryMerchantOutcome)), MemoryID: memoryID, SourceEpisodeID: episodeID, SourceEventRef: eventRef, ObservationKind: string(memory.MemoryMerchantOutcome), Outcome: "DELIVERY_VALID", ObservedAt: observedAt}
	if err := observation.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return observation
}
