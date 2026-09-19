package mysql

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
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

	for _, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound, "mysql-discover"},
		{trace.ActionInvoke, trace.ObservationCandidatesFound, "mysql-invoke"},
		{trace.ActionParse402, trace.ObservationHTTP402, "mysql-parse-402"},
	} {
		if _, err := service.CommitRuntimeAction(ctx, application.RuntimeActionRequest{EpisodeID: episodeID,
			Action: trace.Action{Type: step.action, IdempotencyKey: step.key}, Observation: trace.Observation{Type: step.observation}, TraceID: "mysql-trace"}); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID,
		Quote: application.TrustedPaymentQuote{MerchantDID: "did:merchant:mysql", CapabilityID: "capability:mysql", PayeeDID: "did:payee:mysql", QuoteHash: "sha256:mysql-quote",
			AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}, IdempotencyKey: "mysql-reserve", TraceID: "mysql-reserve-trace"})
	if err != nil {
		t.Fatal(err)
	}
	current = reserved.Episode

	if current.State != episode.StatePaying || current.Version != 5 {
		t.Fatalf("unexpected pre-concurrency projection: %#v", current)
	}

	makeDirectTransition := func(eventID, key, code string) (*episode.CommerceEpisode, *episode.EpisodeEvent) {
		next := current.Clone()
		next.ActionCount++
		if err := next.ApplyCommittedState(episode.StatePaying, now, ""); err != nil {
			t.Fatal(err)
		}
		event, err := episode.NewEvent(eventID, episodeID, current.Version, now, episode.StatePaying,
			trace.Action{Type: trace.ActionPaymentSubmitted, IdempotencyKey: key}, trace.Observation{Type: trace.ObservationPaymentSubmitted, Code: code},
			trace.Decision{ProposedAction: trace.ActionPaymentSubmitted, ProposalID: eventID, Reason: "runtime integration"}, trace.RuntimeVerdict{Allowed: true}, episode.StatePaying, "runtime", "mysql-concurrent-trace", "s1-test")
		if err != nil {
			t.Fatal(err)
		}
		return next, event
	}
	nextA, eventA := makeDirectTransition("mysql-concurrent-a", "mysql-concurrent-a", "a")
	nextB, eventB := makeDirectTransition("mysql-concurrent-b", "mysql-concurrent-b", "b")
	start := make(chan struct{})
	transitionResults := make(chan error, 2)
	var transitionWait sync.WaitGroup
	for _, candidate := range []struct {
		next  *episode.CommerceEpisode
		event *episode.EpisodeEvent
	}{
		{next: nextA, event: eventA}, {next: nextB, event: eventB},
	} {
		transitionWait.Add(1)
		go func(candidate struct {
			next  *episode.CommerceEpisode
			event *episode.EpisodeEvent
		}) {
			defer transitionWait.Done()
			<-start
			transitionResults <- store.CommitTransition(ctx, episodeID, current.Version, candidate.next, candidate.event)
		}(candidate)
	}
	close(start)
	transitionWait.Wait()
	close(transitionResults)
	successes := 0
	for err := range transitionResults {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, repository.ErrVersionConflict) {
			t.Fatalf("unexpected same-version concurrent error: %v", err)
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

	// The same idempotency body raced against itself must produce one event;
	// depending on lock ordering, the loser is either a replay or a version
	// conflict at this repository layer. The application layer turns the
	// former into a stable replay and a changed body into a conflict.
	current, err = service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err = service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 6 || len(events) != 5 || events[4].Sequence != 5 {
		t.Fatalf("same-key race appended more than once: version=%d events=%d", current.Version, len(events))
	}
	nextSame, eventSame := makeDirectTransition("mysql-same-key", "mysql-same-key", "same")
	if err := store.CommitTransition(ctx, episodeID, current.Version, nextSame, eventSame); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitTransition(ctx, episodeID, current.Version, nextSame, eventSame); !errors.Is(err, repository.ErrIdempotentReplay) && !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected stable same-key replay/conflict, got %v", err)
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
		t.Fatalf("same-key replay changed projection/events incorrectly: version=%d events=%d", current.Version, len(events))
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

func TestMySQLTransitionStoreIdempotencyRaceReplaysOrConflictsDeterministically(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	service := application.NewService(store, application.WithClock(func() time.Time { return now }))
	created, err := service.CreateEpisode(ctx, integrationRequest(now, integrationRequestID(t)))
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
	request := application.RuntimeActionRequest{EpisodeID: episodeID, Action: trace.Action{Type: trace.ActionDiscover, IdempotencyKey: "mysql-idempotency-race"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, TraceID: "mysql-idempotency"}
	results := runConcurrentCommits(service, request, request)
	successes, replays := 0, 0
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("idempotency race failed: %v", result.err)
		}
		successes++
		if result.value.Replayed {
			replays++
		}
	}
	if successes != 2 || replays != 1 {
		t.Fatalf("expected one creator and one replay, successes=%d replays=%d", successes, replays)
	}
	changed := request
	changed.Observation.Code = "different-body"
	if _, err := service.CommitRuntimeAction(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("expected changed idempotency body conflict, got %v", err)
	}
	episodeValue, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if episodeValue.Version != 2 || len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("idempotency race duplicated the durable transition: episode=%#v events=%#v", episodeValue, events)
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

func runConcurrentCommits(service *application.Service, requests ...application.RuntimeActionRequest) []concurrentCommitResult {
	start := make(chan struct{})
	results := make(chan concurrentCommitResult, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		wait.Add(1)
		go func(request application.RuntimeActionRequest) {
			defer wait.Done()
			<-start
			value, err := service.CommitRuntimeAction(context.Background(), request)
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
