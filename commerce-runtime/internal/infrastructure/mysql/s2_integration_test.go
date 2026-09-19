package mysql

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type mysqlS2DIDAdapter struct{}

func (mysqlS2DIDAdapter) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: request.Intent.RequesterDID, MerchantDID: request.Intent.MerchantDID,
		CapabilityID: request.Intent.CapabilityID, PayeeDID: request.Intent.PayeeDID, QuoteHash: request.Intent.QuoteHash, AmountMinor: request.Intent.AmountMinor,
		Currency: request.Intent.Currency, AuthorizationRef: "mysql-auth"}, nil
}

type mysqlS2PaymentAdapter struct{}

func (mysqlS2PaymentAdapter) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	return payment.PaymentOutcome{Status: payment.OutcomePending, TxID: "mysql-tx-1", TxHash: "mysql-hash-1", AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type mysqlS2StatusAdapter struct {
	outcome payment.PaymentOutcome
}

func (a *mysqlS2StatusAdapter) Query(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return a.outcome, nil
}

type mysqlS2EntitlementAdapter struct{}

func (mysqlS2EntitlementAdapter) Verify(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: query.IntentID, EvidenceRef: "mysql-entitlement-1"}, nil
}

func TestMySQLS2LedgerPaymentIntegration(t *testing.T) {
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

	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	status := &mysqlS2StatusAdapter{}
	service := application.NewService(NewStore(db), application.WithClock(func() time.Time { return now }), application.WithUnconstrainedSettlementPolicyForTests(), application.WithPaymentAdapters(application.PaymentDependencies{
		DID: mysqlS2DIDAdapter{}, Payment: mysqlS2PaymentAdapter{}, Status: status, Entitlement: mysqlS2EntitlementAdapter{},
	}))
	created, err := service.CreateEpisode(ctx, integrationRequest(now, integrationRequestID(t)))
	if err != nil {
		t.Fatal(err)
	}
	episodeID := created.Episode.EpisodeID
	store := NewStore(db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM payment_intents WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM ledger_entries WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM episode_events WHERE episode_id = ?", episodeID)
		db.Exec("DELETE FROM commerce_episodes WHERE episode_id = ?", episodeID)
	})

	current := created.Episode
	for index, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound, "mysql-s2-discover"},
		{trace.ActionInvoke, trace.ObservationCandidatesFound, "mysql-s2-invoke"},
		{trace.ActionParse402, trace.ObservationHTTP402, "mysql-s2-parse-402"},
	} {
		_ = index
		result, err := service.CommitRuntimeAction(ctx, application.RuntimeActionRequest{EpisodeID: episodeID, Action: trace.Action{Type: step.action, IdempotencyKey: step.key},
			Observation: trace.Observation{Type: step.observation}, TraceID: "mysql-s2-trace"})
		if err != nil {
			t.Fatal(err)
		}
		current = result.Episode
	}
	quote := application.TrustedPaymentQuote{MerchantDID: "did:merchant:mysql", CapabilityID: "capability:mysql", PayeeDID: "did:payee:mysql", QuoteHash: "sha256:mysql-s2-quote",
		AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID, Quote: quote, IdempotencyKey: "mysql-s2-pay", TraceID: "mysql-s2-reserve"})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID, Quote: quote, IdempotencyKey: "mysql-s2-pay", TraceID: "mysql-s2-reserve-retry"})
	if err != nil || !replayed.Replayed || replayed.Intent.IntentID != reserved.Intent.IntentID {
		t.Fatalf("MySQL intent idempotency did not replay: %#v err=%v", replayed, err)
	}
	entries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one reserved ledger entry, entries=%#v err=%v", entries, err)
	}
	if err := store.AppendLedgerEntry(ctx, entries[0]); !errors.Is(err, repository.ErrLedgerIdempotentReplay) {
		t.Fatalf("expected ledger idempotent replay, got %v", err)
	}
	conflict := *entries[0]
	conflict.EntryID = "mysql-s2-conflict-entry"
	conflict.AmountMinor++
	if err := store.AppendLedgerEntry(ctx, &conflict); !errors.Is(err, repository.ErrLedgerIdempotencyConflict) {
		t.Fatalf("expected ledger idempotency conflict, got %v", err)
	}

	pending, err := service.AuthorizeAndSubmitPayment(ctx, reserved.Intent.IntentID, "mysql-s2-submit")
	if err != nil || pending.Outcome.Status != payment.OutcomePending || pending.Episode.State != episode.StatePaying {
		t.Fatalf("expected MySQL pending payment, result=%#v err=%v", pending, err)
	}
	intent, err := store.GetPaymentIntent(ctx, reserved.Intent.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if intent.PayeeDID != quote.PayeeDID || intent.RequestFingerprint == "" || intent.EconomicKey != payment.EconomicIdentityKey(intent.EpisodeID, intent.MerchantDID, intent.CapabilityID, intent.PayeeDID, intent.QuoteHash, intent.AmountMinor, intent.Currency) {
		t.Fatalf("MySQL did not preserve distinct payee and command identity: %#v", intent)
	}
	beforeRollback, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEntries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := ledger.BuildProjection(beforeRollback.Budget.Currency, beforeRollback.Budget.BudgetLimitMinor, beforeRollback.Budget.RefundReusable, beforeEntries)
	if err != nil {
		t.Fatal(err)
	}
	release := &ledger.LedgerEntry{EntryID: "mysql-s2-rollback-release", EpisodeID: episodeID, Sequence: 2, Type: ledger.EntryBudgetReleased,
		Currency: intent.Currency, AmountMinor: intent.BudgetReservation, PaymentIntentID: intent.IntentID, TxID: "mysql-tx-1", IdempotencyKey: "mysql-s2-rollback-release",
		OccurredAt: now, TraceID: "mysql-s2-rollback", ReferenceHash: intent.EconomicKey, MetadataHash: ledger.HashReference("rollback release")}
	settled := &ledger.LedgerEntry{EntryID: "mysql-s2-rollback-settled", EpisodeID: episodeID, Sequence: 3, Type: ledger.EntryPaymentSettled,
		Currency: intent.Currency, AmountMinor: intent.AmountMinor, PaymentIntentID: intent.IntentID, TxID: "mysql-tx-1", IdempotencyKey: "mysql-s2-rollback-settled",
		OccurredAt: now, TraceID: "mysql-s2-rollback", ReferenceHash: intent.EconomicKey, MetadataHash: ledger.HashReference("rollback settlement")}
	if err := projection.Apply(*release); err != nil {
		t.Fatal(err)
	}
	if err := projection.Apply(*settled); err != nil {
		t.Fatal(err)
	}
	next := beforeRollback.Clone()
	next.Budget = projection.ToEpisodeBudget()
	if err := next.ApplyCommittedState(episode.StateClaiming, now, ""); err != nil {
		t.Fatal(err)
	}
	rollbackEvent, err := episode.NewEvent(beforeEvents[0].EventID, episodeID, beforeRollback.Version, now, episode.StatePaying,
		trace.Action{Type: trace.ActionPaymentConfirmed, IdempotencyKey: "mysql-s2-rollback-event"}, trace.Observation{Type: trace.ObservationPaymentConfirmed, Code: "mysql-tx-1"},
		trace.Decision{ProposedAction: trace.ActionPaymentConfirmed, ProposalID: "mysql-s2-rollback-proposal"}, trace.RuntimeVerdict{Allowed: true}, episode.StateClaiming,
		"runtime", "mysql-s2-rollback", "s2-test")
	if err != nil {
		t.Fatal(err)
	}
	intentNext := *intent
	intentNext.Status = payment.IntentConfirmed
	intentNext.TxID = "mysql-tx-1"
	intentNext.TxHash = "mysql-hash-1"
	intentNext.UpdatedAt = now
	if err := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: episodeID, ExpectedEpisodeVersion: beforeRollback.Version,
		NextEpisode: next, Event: rollbackEvent, LedgerEntries: []*ledger.LedgerEntry{release, settled}, IntentUpdate: &intentNext, ExpectedIntentStatus: payment.IntentPending}); err == nil {
		t.Fatal("expected duplicate event id to abort finance transaction")
	}
	afterRollback, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	afterEntries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	afterEvents, err := service.ListEvents(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	afterIntent, err := store.GetPaymentIntent(ctx, reserved.Intent.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Version != beforeRollback.Version || len(afterEntries) != len(beforeEntries) || len(afterEvents) != len(beforeEvents) || afterIntent.Status != payment.IntentPending {
		t.Fatalf("finance transaction was not atomic: before=%d/%d/%d/%s after=%d/%d/%d/%s", beforeRollback.Version, len(beforeEntries), len(beforeEvents), intent.Status, afterRollback.Version, len(afterEntries), len(afterEvents), afterIntent.Status)
	}

	status.outcome = payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "mysql-tx-1", TxHash: "mysql-hash-1", AmountMinor: 300, Currency: "USDC"}
	confirmed, err := service.ReconcilePayment(ctx, reserved.Intent.IntentID, "mysql-s2-confirm")
	if err != nil || confirmed.Outcome.Status != payment.OutcomeConfirmed || confirmed.Episode.State != episode.StateClaiming {
		t.Fatalf("expected confirmed MySQL reconciliation, result=%#v err=%v", confirmed, err)
	}
	claimed, err := service.VerifyPaymentEntitlement(ctx, reserved.Intent.IntentID, "mysql-s2-entitlement")
	if err != nil || claimed.Episode.State != episode.StateInvokingDelivery || claimed.Episode.State == episode.StateFulfilled {
		t.Fatalf("expected entitlement-gated delivery state, result=%#v err=%v", claimed, err)
	}
	finalEntries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalEntries) != 3 || finalEntries[1].Type != ledger.EntryBudgetReleased || finalEntries[2].Type != ledger.EntryPaymentSettled || claimed.Episode.Budget.SettledAmount != 300 || claimed.Episode.Budget.AvailableBudget != 700 {
		t.Fatalf("unexpected final MySQL ledger/projection: entries=%#v budget=%#v", finalEntries, claimed.Episode.Budget)
	}
}
