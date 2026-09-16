package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
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
	mu      sync.Mutex
	calls   int
	outcome payment.PaymentOutcome
	err     error
}

func (f *fakePaymentAdapter) Submit(_ context.Context, _ adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
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

func (f *fakeEntitlementAdapter) Verify(_ context.Context, _ adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, nil
}

func (f *fakeEntitlementAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func advanceToNegotiating(t *testing.T, service *Service, current *episode.CommerceEpisode, now time.Time) *episode.CommerceEpisode {
	t.Helper()
	for _, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound, "s2-discover"},
		{trace.ActionInvoke, trace.ObservationCandidatesFound, "s2-invoke"},
		{trace.ActionParse402, trace.ObservationHTTP402, "s2-parse-402"},
	} {
		proposal := makeProposal(current.EpisodeID, current.Version-1, step.action, now)
		result, err := service.CommitProposal(context.Background(), commitRequest(proposal, step.action, step.key, trace.Observation{Type: step.observation}))
		if err != nil {
			t.Fatalf("advance %s: %v", step.action, err)
		}
		current = result.Episode
	}
	return current
}

func newS2Dependencies(quote TrustedPaymentQuote) (*fakeDIDAdapter, *fakePaymentAdapter, *fakeStatusAdapter, *fakeEntitlementAdapter, PaymentDependencies) {
	did := &fakeDIDAdapter{result: adapters.AuthorizationResult{Allowed: true, RequesterDID: quote.RequesterDID, MerchantDID: quote.MerchantDID,
		CapabilityID: quote.CapabilityID, QuoteHash: quote.QuoteHash, AmountMinor: quote.AmountMinor, Currency: quote.Currency, AuthorizationRef: "auth-1"}}
	paymentAdapter := &fakePaymentAdapter{outcome: payment.PaymentOutcome{Status: payment.OutcomePending, TxID: "tx-1", TxHash: "hash-1", AmountMinor: quote.AmountMinor, Currency: quote.Currency}}
	status := &fakeStatusAdapter{}
	entitlement := &fakeEntitlementAdapter{result: adapters.EntitlementResult{Status: adapters.EntitlementValid, Reference: "entitlement-1"}}
	return did, paymentAdapter, status, entitlement, PaymentDependencies{DID: did, Payment: paymentAdapter, Status: status, Entitlement: entitlement}
}

func TestDeterministicS2PaymentLifecycleIsLLMIndependent(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:1", CapabilityID: "capability-1", QuoteHash: "sha256:quote-1", AmountMinor: 300,
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
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:1", CapabilityID: "capability-1", QuoteHash: "sha256:quote-unknown", AmountMinor: 300,
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
	if second.Outcome.Status != payment.OutcomeUnknown || paymentAdapter.callCount() != 1 || did.callCount() != 1 {
		t.Fatalf("unknown retry submitted a duplicate payment: %#v payment_calls=%d did_calls=%d", second, paymentAdapter.callCount(), did.callCount())
	}
}

func TestConcurrentPaymentIntentReservationHasSingleEconomicEffect(t *testing.T) {
	_, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, NewService(store, WithClock(func() time.Time { return now })), created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:race", CapabilityID: "capability:race", QuoteHash: "quote-race", AmountMinor: 300,
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
