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
	useTrace := &memory.MemoryUseTrace{MemoryUseTraceID: "mut_mysql_s7", EpisodeID: "s7-episode-trace", ModelDecisionTraceID: "trace_mysql_s7", ContextHash: "sha256:" + strings.Repeat("a", 64), RetrievedMemoryRefs: []string{"memory://" + record.MemoryID}, CitedMemoryRefs: []string{"memory://" + record.MemoryID}, ProposedAction: "SWITCH_MERCHANT", GuardAccepted: true, CreatedAt: now, FactsRef: "memory-use-trace://mut_mysql_s7"}
	if err := useTrace.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	db.Exec("DELETE FROM memory_use_traces WHERE memory_use_trace_id = ?", useTrace.MemoryUseTraceID)
	if err := store.SaveMemoryUseTrace(context.Background(), useTrace); err != nil {
		t.Fatal(err)
	}
	readTrace, err := store.GetMemoryUseTrace(context.Background(), useTrace.MemoryUseTraceID)
	if err != nil || readTrace.PayloadHash != useTrace.PayloadHash || !readTrace.GuardAccepted {
		t.Fatalf("memory use trace=%#v err=%v", readTrace, err)
	}
	traces, err := store.ListMemoryUseTraces(context.Background(), useTrace.EpisodeID)
	if err != nil || len(traces) != 1 {
		t.Fatalf("memory use trace list=%#v err=%v", traces, err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM memory_use_traces WHERE memory_use_trace_id = ?", useTrace.MemoryUseTraceID)
	})
}

func TestMySQLS72MemoryScopeAndApplicabilityRanking(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is required for S7.2 MySQL persistence integration")
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
	values := []*memory.MemoryRecord{
		s72IntegrationRecord(now, memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "did:merchant:b", "", "", "transcription", "v1", "sha256:b"),
		s72IntegrationRecord(now.Add(time.Second), memory.MemoryRequesterPreference, memory.ScopeRequester, "", "did:requester:s72", "", "", "", ""),
		s72IntegrationRecord(now.Add(2*time.Second), memory.MemoryRecoveryOutcome, memory.ScopeParentSession, "", "did:requester:s72", "session-s72", "", "", ""),
		s72IntegrationRecord(now, memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "did:merchant:rank", "", "", "transcription", "v5", "sha256:v5"),
		s72IntegrationRecord(now.Add(time.Minute), memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "did:merchant:rank", "", "", "transcription", "v4", "sha256:v4"),
		s72IntegrationRecord(now.Add(2*time.Minute), memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "did:merchant:rank", "", "", "transcription", "v3", "sha256:v3"),
	}
	t.Cleanup(func() {
		for _, value := range values {
			db.Exec("DELETE FROM memory_observations WHERE memory_id = ?", value.MemoryID)
			db.Exec("DELETE FROM memory_records WHERE memory_id = ?", value.MemoryID)
		}
	})
	for _, value := range values {
		observation := s7IntegrationObservation(value.MemoryID, value.MemoryID, value.LastObservedAt, "event-"+value.MemoryID)
		if err := store.SaveObservationAndUpdateAggregate(context.Background(), value, observation, value.StructuredFacts); err != nil {
			t.Fatal(err)
		}
	}
	candidateValues, err := store.SearchMemories(context.Background(), memory.MemoryQuery{
		RequesterDID: "did:requester:s72", ParentSessionID: "session-s72", MerchantDID: "did:merchant:b", CapabilityID: "transcription", CatalogVersion: "v1", CatalogSnapshotHash: "sha256:b",
		AllowedScopes: []memory.MemoryScope{memory.ScopeMerchant, memory.ScopeMerchantCapability}, Now: now.Add(5 * time.Minute), Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidateValues) != 1 || candidateValues[0].Scope != memory.ScopeMerchantCapability || candidateValues[0].MerchantDID != "did:merchant:b" {
		t.Fatalf("mysql candidate query crossed scope boundary: %#v", candidateValues)
	}
	ranked, err := store.SearchMemories(context.Background(), memory.MemoryQuery{
		MerchantDID: "did:merchant:rank", CapabilityID: "transcription", CatalogVersion: "v5", CatalogSnapshotHash: "sha256:v5",
		AllowedScopes: []memory.MemoryScope{memory.ScopeMerchantCapability}, Now: now.Add(5 * time.Minute), Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 || ranked[0].CatalogVersion != "v5" || ranked[0].Applicability != memory.ApplicabilityCurrent {
		t.Fatalf("mysql current applicability did not outrank newer historical versions: %#v", ranked)
	}
}

func s72IntegrationRecord(now time.Time, typ memory.MemoryType, scope memory.MemoryScope, merchant, requester, parent, capability, version, hash string) *memory.MemoryRecord {
	id := memory.MemoryIDForSnapshot(typ, scope, requester, parent, merchant, capability, version, hash)
	expires := now.Add(time.Hour)
	record := &memory.MemoryRecord{MemoryID: id, Type: typ, Scope: scope, RequesterDID: requester, ParentSessionID: parent, MerchantDID: merchant, CapabilityID: capability, CatalogVersion: version, CatalogSnapshotHash: hash, CatalogSnapshotRef: "catalog://s72/" + version, Summary: "mysql s72 fixture", StructuredFacts: memory.OutcomeFacts{AttemptCount: 1, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: now}, SourceEpisodeIDs: []string{"s72-seed"}, SourceEventRefs: []string{"s72-event"}, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://" + id}
	record.Summary = memory.Summarize(*record)
	if err := record.RefreshPayloadHash(); err != nil {
		panic(err)
	}
	return record
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
