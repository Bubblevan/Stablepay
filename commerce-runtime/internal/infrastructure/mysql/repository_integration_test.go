package mysql

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

// TestMySQLTransitionStoreConcurrencyAndAtomicity exercises the real InnoDB
// adapter. Set COMMERCE_RUNTIME_MYSQL_DSN to a disposable MySQL database DSN
// to run it, for example user:password@tcp(127.0.0.1:3306)/database?parseTime=true.
func TestMySQLTransitionStoreConcurrencyAndAtomicity(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}

	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	service := application.NewService(store, application.WithClock(func() time.Time { return now }))
	request := integrationRequest(now, integrationRequestID(t))
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	episodeID := created.Episode.EpisodeID
	t.Cleanup(func() {
		db.Exec("DELETE FROM payment_intents WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM ledger_entries WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM episode_events WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM commerce_episodes WHERE episode_id = ?", episodeID)
	})

	for index, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound, "mysql-discover"},
		{trace.ActionInvoke, trace.ObservationCandidatesFound, "mysql-invoke"},
		{trace.ActionParse402, trace.ObservationHTTP402, "mysql-parse-402"},
	} {
		current, err := service.GetEpisode(ctx, episodeID)
		if err != nil {
			t.Fatal(err)
		}
		proposal := integrationProposal(episodeID, current.Version-1, step.action, now, fmt.Sprintf("mysql-proposal-%d", index))
		if _, err := service.CommitProposal(ctx, application.CommitRequest{
			Proposal:    proposal,
			Action:      trace.Action{Type: step.action, IdempotencyKey: step.key},
			Observation: trace.Observation{Type: step.observation}, Actor: "runtime", TraceID: "mysql-trace",
		}); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID,
		Quote: application.TrustedPaymentQuote{MerchantDID: "did:merchant:mysql", CapabilityID: "capability:mysql", QuoteHash: "sha256:mysql-quote",
			AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}, IdempotencyKey: "mysql-reserve", TraceID: "mysql-reserve-trace"})
	if err != nil {
		t.Fatal(err)
	}
	current = reserved.Episode

	if current.State != episode.StatePaying || current.Version != 5 {
		t.Fatalf("unexpected pre-concurrency projection: %#v", current)
	}

	concurrentProposal := integrationProposal(episodeID, current.Version-1, trace.ActionPaymentSubmitted, now, "mysql-concurrent-proposal")
	concurrent := func(key string) application.CommitRequest {
		return application.CommitRequest{
			Proposal:    concurrentProposal,
			Action:      trace.Action{Type: trace.ActionPaymentSubmitted, IdempotencyKey: key},
			Observation: trace.Observation{Type: trace.ObservationPaymentSubmitted}, Actor: "runtime", TraceID: "mysql-concurrent-trace",
		}
	}
	results := runConcurrentCommits(service, concurrent("mysql-concurrent-a"), concurrent("mysql-concurrent-b"))
	successes := 0
	for _, result := range results {
		if result.err == nil {
			successes++
			continue
		}
		if !errors.Is(result.err, repository.ErrVersionConflict) && !errors.Is(result.err, decision.ErrStaleEventSequence) {
			t.Fatalf("unexpected same-version concurrent error: %v", result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one concurrent commit, got %d", successes)
	}

	current, err = service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("concurrent commit appended %d events, want 5", len(events))
	}
	if current.Version != 6 || current.ActionCount != 5 || events[4].Sequence != 5 {
		t.Fatalf("concurrent commit advanced projection/events incorrectly: version=%d actions=%d last=%#v", current.Version, current.ActionCount, events[4])
	}

	// The same idempotency body raced against itself must produce one event and
	// a deterministic replay for the loser, regardless of lock acquisition.
	pendingProposal := integrationProposal(episodeID, current.Version-1, trace.ActionPaymentPending, now, "mysql-pending-proposal")
	pending := func() application.CommitRequest {
		return application.CommitRequest{
			Proposal:    pendingProposal,
			Action:      trace.Action{Type: trace.ActionPaymentPending, IdempotencyKey: "mysql-pending-race"},
			Observation: trace.Observation{Type: trace.ObservationPaymentPending, Code: "awaiting-confirmation"}, Actor: "runtime", TraceID: "mysql-pending-trace",
		}
	}
	pendingResults := runConcurrentCommits(service, pending(), pending())
	replays := 0
	for _, result := range pendingResults {
		if result.err != nil {
			t.Fatalf("same idempotency race failed: %v", result.err)
		}
		if result.value.Replayed {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("expected exactly one idempotent replay, got %d", replays)
	}
	current, err = service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err = service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 7 || len(events) != 6 || events[5].Sequence != 6 {
		t.Fatalf("same-key race appended more than once: version=%d events=%d", current.Version, len(events))
	}

	// A same-key, different-body race must resolve as an idempotency conflict,
	// not as a misleading optimistic-version error.
	conflictProposal := integrationProposal(episodeID, current.Version-1, trace.ActionPaymentStatusQueried, now, "mysql-status-proposal")
	conflict := func(code string) application.CommitRequest {
		return application.CommitRequest{
			Proposal:    conflictProposal,
			Action:      trace.Action{Type: trace.ActionPaymentStatusQueried, IdempotencyKey: "mysql-status-race"},
			Observation: trace.Observation{Type: trace.ObservationPaymentStatusQueried, Code: code}, Actor: "runtime", TraceID: "mysql-status-trace",
		}
	}
	conflictResults := runConcurrentCommits(service, conflict("body-a"), conflict("body-b"))
	conflictCount := 0
	for _, result := range conflictResults {
		if result.err == nil {
			continue
		}
		if errors.Is(result.err, repository.ErrIdempotencyConflict) {
			conflictCount++
			continue
		}
		t.Fatalf("unexpected different-body race error: %v", result.err)
	}
	if conflictCount != 1 {
		t.Fatalf("expected exactly one idempotency conflict, got %d", conflictCount)
	}
	current, err = service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err = service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 8 || len(events) != 7 || events[6].Sequence != 7 {
		t.Fatalf("different-body race changed projection/events incorrectly: version=%d events=%d", current.Version, len(events))
	}

	// Force the event insert to fail after the projection UPDATE. The
	// duplicate event_id proves the SQL transaction rolls the UPDATE back.
	beforeRollback := current.Clone()
	next := current.Clone()
	if err := next.ApplyCommittedState(current.State, now, ""); err != nil {
		t.Fatal(err)
	}
	rollbackEvent, err := episode.NewEvent(
		events[0].EventID, episodeID, current.Version, now, current.State,
		trace.Action{Type: trace.ActionPaymentPending, IdempotencyKey: "mysql-rollback-key"},
		trace.Observation{Type: trace.ObservationPaymentPending},
		trace.Decision{ProposedAction: trace.ActionPaymentPending, ProposalID: "mysql-rollback-proposal"},
		trace.RuntimeVerdict{Allowed: true}, current.State, "runtime", "mysql-rollback-trace", "test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitTransition(ctx, episodeID, current.Version, next, rollbackEvent); err == nil {
		t.Fatal("expected duplicate event_id to abort the transaction")
	}
	afterRollback, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	remainingEvents, err := service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Version != beforeRollback.Version || afterRollback.ActionCount != beforeRollback.ActionCount || len(remainingEvents) != len(events) {
		t.Fatalf("projection/event append was not atomic: before=%#v after=%#v events=%d/%d", beforeRollback, afterRollback, len(events), len(remainingEvents))
	}
}

func integrationRequestID(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 12)
	if _, err := cryptorand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	return "mysql-integration-" + hex.EncodeToString(bytes)
}

type concurrentCommitResult struct {
	value application.CommitResult
	err   error
}

func runConcurrentCommits(service *application.Service, requests ...application.CommitRequest) []concurrentCommitResult {
	start := make(chan struct{})
	results := make(chan concurrentCommitResult, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		wait.Add(1)
		go func(request application.CommitRequest) {
			defer wait.Done()
			<-start
			value, err := service.CommitProposal(context.Background(), request)
			results <- concurrentCommitResult{value: value, err: err}
		}(request)
	}
	close(start)
	wait.Wait()
	close(results)
	output := make([]concurrentCommitResult, 0, len(requests))
	for result := range results {
		output = append(output, result)
	}
	return output
}

func integrationRequest(now time.Time, requestID string) contract.AcquireCapabilityRequest {
	return contract.AcquireCapabilityRequest{
		RequestID: requestID, ParentSessionID: "mysql-session", RequesterDID: "did:stablepay:integration",
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "integration", Description: "exercise MySQL transition commit"},
		Input:           contract.Input{Ref: "integration-input", ContentType: "application/octet-stream"},
		Constraints:     contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(time.Hour), MaxTotalAttempts: 20, MaxPaymentAttempts: 5, MaxDeliveryAttempts: 5},
		ExpectedOutput:  contract.ExpectedOutput{Schema: "integration", ContentType: "application/json"},
		Validator:       contract.ValidatorRef{Kind: "builtin", Name: "integration-validator", Version: "v1"},
	}
}

func integrationProposal(episodeID string, sequence uint64, action trace.ActionType, now time.Time, proposalID string) decision.DecisionProposal {
	return decision.DecisionProposal{
		ProposalID: proposalID, EpisodeID: episodeID, BasedOnEventSequence: sequence,
		ProposedAction: action, Rationale: "mysql integration test", Confidence: 1,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
}
