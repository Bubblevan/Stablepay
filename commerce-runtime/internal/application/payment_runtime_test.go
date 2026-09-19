package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type fakeDIDAdapter struct {
	mu     sync.Mutex
	calls  int
	result adapters.AuthorizationResult
}

func (f *fakeDIDAdapter) AuthorizePayment(_ context.Context, _ adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, nil
}

func (f *fakeDIDAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakePaymentAdapter struct {
	mu       sync.Mutex
	calls    int
	outcome  payment.PaymentOutcome
	err      error
	requests []adapters.PaymentSubmitRequest
}

func (f *fakePaymentAdapter) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.requests = append(f.requests, request)
	return f.outcome, f.err
}

func (f *fakePaymentAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeStatusAdapter struct {
	mu       sync.Mutex
	calls    int
	outcomes []payment.PaymentOutcome
	err      error
}

func (f *fakeStatusAdapter) Query(_ context.Context, _ adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return payment.PaymentOutcome{}, f.err
	}
	if len(f.outcomes) == 0 {
		return payment.PaymentOutcome{Status: payment.OutcomeUnknown}, nil
	}
	outcome := f.outcomes[0]
	f.outcomes = f.outcomes[1:]
	return outcome, nil
}

func (f *fakeStatusAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeEntitlementAdapter struct {
	mu     sync.Mutex
	calls  int
	result adapters.EntitlementResult
}

func (f *fakeEntitlementAdapter) Verify(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	result := f.result
	if result.Status == adapters.EntitlementValid && result.PaymentIntentID == "" && result.TxID == "" && result.TxHash == "" {
		result.PaymentIntentID = query.IntentID
	}
	return result, nil
}

func (f *fakeEntitlementAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func advanceToNegotiating(t *testing.T, service *Service, current *episode.CommerceEpisode, now time.Time) *episode.CommerceEpisode {
	t.Helper()
	_ = now
	for _, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound, "s2-discover"},
		{trace.ActionInvoke, trace.ObservationCandidatesFound, "s2-invoke"},
		{trace.ActionParse402, trace.ObservationHTTP402, "s2-parse-402"},
	} {
		result, err := service.CommitRuntimeAction(context.Background(), RuntimeActionRequest{EpisodeID: current.EpisodeID, Action: trace.Action{Type: step.action, IdempotencyKey: step.key}, Observation: trace.Observation{Type: step.observation}, TraceID: "runtime-test"})
		if err != nil {
			t.Fatalf("advance %s: %v", step.action, err)
		}
		current = result.Episode
	}
	return current
}

func newS2Dependencies(quote TrustedPaymentQuote) (*fakeDIDAdapter, *fakePaymentAdapter, *fakeStatusAdapter, *fakeEntitlementAdapter, PaymentDependencies) {
	did := &fakeDIDAdapter{result: adapters.AuthorizationResult{Allowed: true, RequesterDID: quote.RequesterDID, MerchantDID: quote.MerchantDID,
		CapabilityID: quote.CapabilityID, PayeeDID: quote.PayeeDID, QuoteHash: quote.QuoteHash, AmountMinor: quote.AmountMinor, Currency: quote.Currency, AuthorizationRef: "auth-1"}}
	paymentAdapter := &fakePaymentAdapter{outcome: payment.PaymentOutcome{Status: payment.OutcomePending, TxID: "tx-1", TxHash: "hash-1", AmountMinor: quote.AmountMinor, Currency: quote.Currency}}
	status := &fakeStatusAdapter{}
	entitlement := &fakeEntitlementAdapter{result: adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: "", EvidenceRef: "entitlement-1"}}
	return did, paymentAdapter, status, entitlement, PaymentDependencies{DID: did, Payment: paymentAdapter, Status: status, Entitlement: entitlement}
}

func TestDeterministicS2PaymentLifecycleIsLLMIndependent(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:1", CapabilityID: "capability-1", PayeeDID: "did:payee:1", QuoteHash: "sha256:quote-1", AmountMinor: 300,
		Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	did, paymentAdapter, status, entitlement, dependencies := newS2Dependencies(quote)
	service.paymentDeps = dependencies
	entitlement.result.Status = adapters.EntitlementUnknown

	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s2-pay-1", ProposalID: "trusted-quote-1", Actor: "runtime", TraceID: "trace-s2-1"})
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Episode.State != episode.StatePaying || reserved.Intent.Status != payment.IntentCreated || reserved.Episode.Budget.ReservedAmount != 300 {
		t.Fatalf("unexpected reservation: %#v", reserved)
	}
	replayed, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s2-pay-1", TraceID: "trace-s2-retry"})
	if err != nil || !replayed.Replayed || replayed.Intent.IntentID != reserved.Intent.IntentID {
		t.Fatalf("same quote retry did not replay: %#v err=%v", replayed, err)
	}
	economicReplay, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s2-pay-different-retry"})
	if err != nil || !economicReplay.Replayed || economicReplay.Intent.IntentID != reserved.Intent.IntentID {
		t.Fatalf("same economic identity created a second intent: %#v err=%v", economicReplay, err)
	}
	changedQuote := quote
	changedQuote.AmountMinor = 301
	if _, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: changedQuote, IdempotencyKey: "s2-pay-1"}); !errors.Is(err, repository.ErrPaymentIntentConflict) {
		t.Fatalf("expected changed amount conflict, got %v", err)
	}
	entries, err := store.ListLedgerEntries(context.Background(), current.EpisodeID)
	if err != nil || len(entries) != 1 || entries[0].Type != ledger.EntryBudgetReserved {
		t.Fatalf("reservation was not append-only and unique: %#v err=%v", entries, err)
	}

	pending, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "trace-s2-submit")
	if err != nil {
		t.Fatal(err)
	}
	if pending.Outcome.Status != payment.OutcomePending || pending.Episode.State != episode.StatePaying || pending.Intent.Status != payment.IntentPending || pending.Episode.PaymentAttemptCount != 1 {
		t.Fatalf("unexpected pending result: %#v", pending)
	}
	if did.callCount() != 1 || paymentAdapter.callCount() != 1 {
		t.Fatalf("unexpected adapter calls after submit: did=%d payment=%d", did.callCount(), paymentAdapter.callCount())
	}

	status.outcomes = []payment.PaymentOutcome{{Status: payment.OutcomePending, TxID: "tx-1", TxHash: "hash-1", AmountMinor: 300, Currency: "USDC"}}
	reconciledPending, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "trace-s2-pending-retry")
	if err != nil || reconciledPending.Outcome.Status != payment.OutcomePending || paymentAdapter.callCount() != 1 {
		t.Fatalf("pending retry submitted a second payment: result=%#v err=%v calls=%d", reconciledPending, err, paymentAdapter.callCount())
	}

	status.outcomes = []payment.PaymentOutcome{{Status: payment.OutcomeConfirmed, TxID: "tx-1", TxHash: "hash-1", AmountMinor: 300, Currency: "USDC"}}
	confirmed, err := service.ReconcilePayment(context.Background(), reserved.Intent.IntentID, "trace-s2-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Outcome.Status != payment.OutcomeConfirmed || confirmed.Episode.State != episode.StateClaiming || confirmed.Intent.Status != payment.IntentConfirmed {
		t.Fatalf("unexpected confirmed result: %#v", confirmed)
	}
	if confirmed.Episode.Budget.ReservedAmount != 0 || confirmed.Episode.Budget.SettledAmount != 300 || confirmed.Episode.Budget.ConsumedAmount != 300 || confirmed.Episode.Budget.AvailableBudget != 700 {
		t.Fatalf("settlement projection is incorrect: %#v", confirmed.Episode.Budget)
	}
	entries, err = store.ListLedgerEntries(context.Background(), current.EpisodeID)
	if err != nil || len(entries) != 3 || entries[1].Type != ledger.EntryBudgetReleased || entries[2].Type != ledger.EntryPaymentSettled {
		t.Fatalf("settlement ledger facts are incorrect: %#v err=%v", entries, err)
	}

	entitlement.result.Status = adapters.EntitlementValid
	claimed, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "trace-s2-entitlement")
	if err != nil || claimed.Episode.State != episode.StateInvokingDelivery {
		t.Fatalf("entitlement did not advance to delivery invocation: %#v err=%v", claimed, err)
	}
	if claimed.Episode.State == episode.StateFulfilled || entitlement.callCount() != 2 {
		t.Fatalf("payment settlement bypassed entitlement or fulfilled episode: %#v calls=%d", claimed.Episode, entitlement.callCount())
	}
	events, err := store.ListByEpisode(context.Background(), current.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 9 || events[4].StateBefore != episode.StatePaying || events[4].StateAfter != episode.StatePaying {
		t.Fatalf("expected event trace for each payment step, got %d events", len(events))
	}
}

func TestUnknownPaymentOutcomeCannotBlindRetry(t *testing.T) {
	service, _, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:1", CapabilityID: "capability-1", PayeeDID: "did:payee:1", QuoteHash: "sha256:quote-unknown", AmountMinor: 300,
		Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	did, paymentAdapter, status, entitlement, dependencies := newS2Dependencies(quote)
	paymentAdapter.outcome = payment.PaymentOutcome{Status: payment.OutcomeUnknown, Reason: "submit timeout"}
	entitlement.result.Status = adapters.EntitlementUnknown
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s2-unknown", TraceID: "trace-unknown"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "trace-unknown-submit")
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome.Status != payment.OutcomeUnknown || first.Intent.Status != payment.IntentUnknown || paymentAdapter.callCount() != 1 {
		t.Fatalf("expected unknown outcome after timeout: %#v calls=%d", first, paymentAdapter.callCount())
	}
	status.err = errors.New("status endpoint unavailable")
	second, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "trace-unknown-retry")
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome.Status != payment.OutcomeUnknown || paymentAdapter.callCount() != 2 || did.callCount() != 1 {
		t.Fatalf("unknown recovery did not use one exact idempotent resend: %#v payment_calls=%d did_calls=%d", second, paymentAdapter.callCount(), did.callCount())
	}
	if len(paymentAdapter.requests) != 2 || paymentAdapter.requests[0].Intent.IdempotencyKey != paymentAdapter.requests[1].Intent.IdempotencyKey || paymentAdapter.requests[0].RequestFingerprint != paymentAdapter.requests[1].RequestFingerprint || paymentAdapter.requests[0].Intent.PayeeDID != paymentAdapter.requests[1].Intent.PayeeDID {
		t.Fatalf("unknown recovery changed the economic command: %#v", paymentAdapter.requests)
	}
	if paymentAdapter.requests[0].PayeeDID != quote.PayeeDID || paymentAdapter.requests[1].PayeeDID != quote.PayeeDID {
		t.Fatalf("unknown recovery changed the explicit payee binding: %#v", paymentAdapter.requests)
	}
}

func TestDefinitiveFailureClosesEpisodeAndReleasesReservation(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:failed", CapabilityID: "capability:failed", PayeeDID: "did:payee:failed", QuoteHash: "failed-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	_, adapter, _, _, dependencies := newS2Dependencies(quote)
	adapter.outcome = payment.PaymentOutcome{Status: payment.OutcomeFailed, Reason: "PAYMENT_DECLINED"}
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "failed-payment"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "failed-trace")
	if err != nil || failed.Episode.State != episode.StateFailed || failed.Episode.TerminalReason != "PAYMENT_DECLINED" || failed.Intent.Status != payment.IntentFailed || failed.Episode.Budget.ReservedAmount != 0 {
		t.Fatalf("definitive failure was not terminal and compensated: %#v err=%v", failed, err)
	}
	entries, err := store.ListLedgerEntries(context.Background(), current.EpisodeID)
	if err != nil || len(entries) != 2 || entries[1].Type != ledger.EntryBudgetReleased {
		t.Fatalf("failure compensation ledger is incorrect: %#v err=%v", entries, err)
	}
}

func TestAuthorizationDeniedAndExpiredIntentCloseReservation(t *testing.T) {
	t.Run("authorization denied", func(t *testing.T) {
		service, store, now, created := createFixture(t)
		current := advanceToNegotiating(t, service, created, now)
		quote := TrustedPaymentQuote{MerchantDID: "did:merchant:denied", CapabilityID: "capability:denied", PayeeDID: "did:payee:denied", QuoteHash: "denied-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
		did, _, _, _, dependencies := newS2Dependencies(quote)
		did.result.Allowed = false
		did.result.Reason = "policy denied"
		service.paymentDeps = dependencies
		reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "denied-payment"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "denied-trace")
		if !errors.Is(err, decision.ErrPaymentAuthorization) {
			t.Fatalf("expected authorization denial, got %v", err)
		}
		closed, err := store.GetPaymentIntent(context.Background(), reserved.Intent.IntentID)
		if err != nil {
			t.Fatal(err)
		}
		closedEpisode, err := store.Get(context.Background(), current.EpisodeID)
		if err != nil || closed.Status != payment.IntentFailed || closedEpisode.State != episode.StateFailed || closedEpisode.Budget.ReservedAmount != 0 {
			t.Fatalf("denial did not close and release: intent=%#v episode=%#v err=%v", closed, closedEpisode, err)
		}
	})

	t.Run("intent expired", func(t *testing.T) {
		service, store, now, created := createFixture(t)
		current := advanceToNegotiating(t, service, created, now)
		clockNow := now
		service.clock = func() time.Time { return clockNow }
		quote := TrustedPaymentQuote{MerchantDID: "did:merchant:expired", CapabilityID: "capability:expired", PayeeDID: "did:payee:expired", QuoteHash: "expired-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(time.Minute)}
		_, _, _, _, dependencies := newS2Dependencies(quote)
		service.paymentDeps = dependencies
		reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "expired-payment"})
		if err != nil {
			t.Fatal(err)
		}
		clockNow = now.Add(2 * time.Minute)
		_, err = service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "expired-trace")
		if !errors.Is(err, decision.ErrPaymentIntentExpired) {
			t.Fatalf("expected intent expiry, got %v", err)
		}
		closed, err := store.GetPaymentIntent(context.Background(), reserved.Intent.IntentID)
		if err != nil {
			t.Fatal(err)
		}
		closedEpisode, err := store.Get(context.Background(), current.EpisodeID)
		if err != nil || closed.Status != payment.IntentExpired || closedEpisode.State != episode.StateExpired || closedEpisode.Budget.ReservedAmount != 0 {
			t.Fatalf("expiry did not close and release: intent=%#v episode=%#v err=%v", closed, closedEpisode, err)
		}
	})
}

func TestInvalidOrUnboundEntitlementFailsEpisode(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:entitlement", CapabilityID: "capability:entitlement", PayeeDID: "did:payee:entitlement", QuoteHash: "entitlement-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	_, adapter, _, entitlement, dependencies := newS2Dependencies(quote)
	adapter.outcome = payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx-entitlement", TxHash: "hash-entitlement", AmountMinor: 300, Currency: "USDC"}
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "invalid-entitlement"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "entitlement-submit"); err != nil {
		t.Fatal(err)
	}
	entitlement.result = adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: "other-intent", TxID: "other-tx", EvidenceRef: "stale-evidence"}
	result, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "entitlement-invalid")
	if err != nil || result.Episode.State != episode.StateFailed || result.Episode.TerminalReason != "ENTITLEMENT_INVALID" {
		t.Fatalf("unbound entitlement did not fail episode: %#v err=%v", result, err)
	}
	stored, err := store.Get(context.Background(), current.EpisodeID)
	if err != nil || stored.State != episode.StateFailed {
		t.Fatalf("failed entitlement projection was not durable: %#v err=%v", stored, err)
	}
}

func TestEntitlementUnknownKeepsClaimingWithNonTerminalEvent(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:entitlement-unknown", CapabilityID: "capability:entitlement-unknown", PayeeDID: "did:payee:entitlement-unknown", QuoteHash: "entitlement-unknown-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	_, adapter, _, entitlement, dependencies := newS2Dependencies(quote)
	adapter.outcome = payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx-entitlement-unknown", TxHash: "hash-entitlement-unknown", AmountMinor: 300, Currency: "USDC"}
	entitlement.result = adapters.EntitlementResult{Status: adapters.EntitlementUnknown, Reason: "entitlement query timeout"}
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "unknown-entitlement"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "unknown-entitlement-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcilePayment(context.Background(), reserved.Intent.IntentID, "unknown-entitlement-confirm"); err != nil {
		t.Fatal(err)
	}
	result, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "unknown-entitlement-verify")
	if err != nil {
		t.Fatal(err)
	}
	if result.Episode.State != episode.StateClaiming || result.Episode.TerminalReason != "" || episode.IsTerminal(result.Episode.State) {
		t.Fatalf("unknown entitlement changed terminal/state semantics: %#v", result.Episode)
	}
	if result.Event == nil || result.Event.StateBefore != episode.StateClaiming || result.Event.StateAfter != episode.StateClaiming || result.Event.Observation.Type != trace.ObservationEntitlementUnknown {
		t.Fatalf("unknown entitlement event is incorrect: %#v", result.Event)
	}
	if result.Event.Sequence != result.Episode.Version-1 {
		t.Fatalf("unknown entitlement did not advance event/version together: event=%#v episode=%#v", result.Event, result.Episode)
	}
	events, err := store.ListByEpisode(context.Background(), current.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Observation.Type != trace.ObservationEntitlementUnknown {
		t.Fatalf("unknown entitlement event was not durable: %#v", events)
	}
}

func TestEntitlementWithConflictingTransactionCannotConfirmIntent(t *testing.T) {
	service, _, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:entitlement-conflict", CapabilityID: "capability:entitlement-conflict", PayeeDID: "did:payee:entitlement-conflict", QuoteHash: "entitlement-conflict-quote", AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	_, adapter, _, entitlement, dependencies := newS2Dependencies(quote)
	adapter.outcome = payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx-entitlement-conflict", TxHash: "hash-entitlement-conflict", AmountMinor: 300, Currency: "USDC"}
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "conflicting-entitlement"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "conflicting-entitlement-submit"); err != nil {
		t.Fatal(err)
	}
	entitlement.result = adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: reserved.Intent.IntentID, TxID: "different-tx", TxHash: "different-hash", EvidenceRef: "conflicting-evidence"}
	result, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "conflicting-entitlement-verify")
	if err != nil || result.Episode.State != episode.StateFailed || result.Episode.TerminalReason != "ENTITLEMENT_INVALID" {
		t.Fatalf("conflicting entitlement evidence was accepted: %#v err=%v", result, err)
	}
}

func TestConcurrentPaymentIntentReservationHasSingleEconomicEffect(t *testing.T) {
	_, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, NewService(store, WithClock(func() time.Time { return now })), created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:race", CapabilityID: "capability:race", PayeeDID: "did:payee:race", QuoteHash: "quote-race", AmountMinor: 300,
		Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	services := []*Service{
		NewService(store, WithClock(func() time.Time { return now })),
		NewService(store, WithClock(func() time.Time { return now })),
	}
	start := make(chan struct{})
	results := make(chan PaymentIntentResult, len(services))
	errorsOut := make(chan error, len(services))
	var wait sync.WaitGroup
	for _, service := range services {
		wait.Add(1)
		go func(service *Service) {
			defer wait.Done()
			<-start
			result, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s2-race", TraceID: "trace-race"})
			results <- result
			errorsOut <- err
		}(service)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	replays := 0
	for result := range results {
		if result.Replayed {
			replays++
		}
	}
	for err := range errorsOut {
		if err != nil {
			t.Fatalf("concurrent same-intent reservation failed: %v", err)
		}
	}
	if replays != 1 {
		t.Fatalf("expected one creator and one stable replay, got %d replays", replays)
	}
	entries, err := store.ListLedgerEntries(context.Background(), current.EpisodeID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("concurrent reservation appended duplicate ledger facts: %#v err=%v", entries, err)
	}
	intent, err := store.FindPaymentIntentByIdempotencyKey(context.Background(), current.EpisodeID, "s2-race")
	if err != nil || intent == nil {
		t.Fatalf("concurrent reservation did not persist one intent: %#v err=%v", intent, err)
	}
}
