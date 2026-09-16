package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func serviceFixture() (*Service, *repository.InMemoryStore, time.Time) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := repository.NewInMemoryStore()
	nextID := 0
	service := NewService(store,
		WithClock(func() time.Time { return now }),
		WithIDGenerator(func(prefix string) string { nextID++; return prefix + "_" + string(rune('a'+nextID)) }),
	)
	return service, store, now
}

func requestFixture(now time.Time) contract.AcquireCapabilityRequest {
	return contract.AcquireCapabilityRequest{
		RequestID: "acr_service", ParentSessionID: "session_service", RequesterDID: "did:stablepay:agent",
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "transcription", Description: "transcribe audio"},
		Input:           contract.Input{URI: "object://audio/1.mp3", ContentType: "audio/mpeg"},
		Constraints:     contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(time.Hour), MaxTotalAttempts: 20, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2},
		ExpectedOutput:  contract.ExpectedOutput{Schema: "transcript", ContentType: "text/plain"},
		Validator:       contract.ValidatorRef{Kind: "builtin", Name: "transcript_validator", Version: "v1"},
	}
}

func makeProposal(episodeID string, sequence uint64, action trace.ActionType, now time.Time) decision.DecisionProposal {
	return decision.DecisionProposal{
		ProposalID: "proposal_" + string(action), EpisodeID: episodeID, BasedOnEventSequence: sequence,
		ProposedAction: action, Rationale: "deterministic test rule", Confidence: 1,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
}

func commitRequest(proposal decision.DecisionProposal, action trace.ActionType, key string, observation trace.Observation) CommitRequest {
	return CommitRequest{Proposal: proposal, Action: trace.Action{Type: action, IdempotencyKey: key}, Observation: observation, Actor: "runtime", TraceID: "trace-test"}
}

func createFixture(t *testing.T) (*Service, *repository.InMemoryStore, time.Time, *episode.CommerceEpisode) {
	service, store, now := serviceFixture()
	result, err := service.CreateEpisode(context.Background(), requestFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	return service, store, now, result.Episode
}

func TestCreateAndCommitProposalProducesStructuredEvent(t *testing.T) {
	service, store, now, created := createFixture(t)
	initial := created.Clone()
	proposal := makeProposal(created.EpisodeID, 0, trace.ActionDiscover, now)
	result, err := service.CommitProposal(context.Background(), commitRequest(proposal, trace.ActionDiscover, "transition-1", trace.Observation{Type: trace.ObservationCandidatesFound, FactsRef: "object://facts/1"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Episode.State != episode.StateDiscovering || result.Episode.Version != 2 {
		t.Fatalf("unexpected commit result: %#v", result)
	}
	events, err := store.ListByEpisode(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 1 || events[0].StateBefore != episode.StateAccepted || events[0].StateAfter != episode.StateDiscovering {
		t.Fatalf("unexpected event: %#v", events)
	}
	if !events[0].RuntimeVerdict.Allowed || events[0].Action.Type != trace.ActionDiscover || events[0].Decision.ProposalID != proposal.ProposalID {
		t.Fatalf("event did not retain structured decision trace: %#v", events[0])
	}
	reconstructed, err := episode.Reconstruct(initial, events)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Episode, reconstructed) {
		t.Fatalf("replay differs from projection:\nprojection=%#v\nreplay=%#v", result.Episode, reconstructed)
	}
}

func TestDeterministicS1FlowReachesFulfilledWithoutLLM(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:s1", CapabilityID: "capability:s1", PayeeDID: "did:payee:s1", QuoteHash: "s1-quote", AmountMinor: 300,
		Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}
	_, paymentAdapter, status, entitlement, dependencies := newS2Dependencies(quote)
	service.paymentDeps = dependencies
	entitlement.result.Status = adapters.EntitlementUnknown
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s1-runtime-payment", TraceID: "s1-runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "s1-submit"); err != nil {
		t.Fatal(err)
	}
	status.outcomes = []payment.PaymentOutcome{{Status: payment.OutcomeConfirmed, TxID: "tx-1", TxHash: "hash-1", AmountMinor: 300, Currency: "USDC"}}
	if _, err := service.ReconcilePayment(context.Background(), reserved.Intent.IntentID, "s1-confirm"); err != nil {
		t.Fatal(err)
	}
	entitlement.result.Status = adapters.EntitlementValid
	claimed, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "s1-entitlement")
	if err != nil {
		t.Fatal(err)
	}
	current = claimed.Episode
	for index, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{
		{trace.ActionInvoke, trace.ObservationDeliveryValid, "s1-delivery-invoke"},
		{trace.ActionValidateDelivery, trace.ObservationDeliveryValid, "s1-delivery-validate"},
	} {
		proposal := makeProposal(current.EpisodeID, current.Version-1, step.action, now)
		result, err := service.CommitProposal(context.Background(), commitRequest(proposal, step.action, step.key, trace.Observation{Type: step.observation}))
		if err != nil {
			t.Fatalf("delivery step %d (%s): %v", index+1, step.action, err)
		}
		current = result.Episode
	}
	if current.State != episode.StateFulfilled || current.PaymentAttemptCount != 1 || current.DeliveryAttemptCount != 1 {
		t.Fatalf("unexpected fulfilled projection: %#v", current)
	}
	events, err := store.ListByEpisode(context.Background(), current.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 11 {
		t.Fatalf("expected 11 events, got %d", len(events))
	}
	for index, event := range events {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event sequence %d is %d", index+1, event.Sequence)
		}
	}
	if paymentAdapter.callCount() != 1 {
		t.Fatalf("runtime submitted payment more than once: %d", paymentAdapter.callCount())
	}
}

func TestRequestAndTransitionIdempotency(t *testing.T) {
	service, store, now, created := createFixture(t)
	second, err := service.CreateEpisode(context.Background(), requestFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Episode.EpisodeID != created.EpisodeID {
		t.Fatal("same request did not replay the existing episode")
	}
	different := requestFixture(now)
	different.AcquisitionGoal.Description = "different contract"
	if _, err := service.CreateEpisode(context.Background(), different); !errors.Is(err, repository.ErrRequestIDConflict) {
		t.Fatalf("expected request conflict, got %v", err)
	}
	proposal := makeProposal(created.EpisodeID, 0, trace.ActionDiscover, now)
	request := commitRequest(proposal, trace.ActionDiscover, "transition-1", trace.Observation{Type: trace.ObservationCandidatesFound})
	first, err := service.CommitProposal(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.CommitProposal(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Event.EventID != first.Event.EventID || replayed.Episode.Version != first.Episode.Version {
		t.Fatal("duplicate transition did not replay deterministically")
	}
	events, err := store.ListByEpisode(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("duplicate transition appended %d events", len(events))
	}
	changed := request
	changed.Observation.Code = "changed-body"
	if _, err := service.CommitProposal(context.Background(), changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("expected transition idempotency conflict, got %v", err)
	}
}

func TestCreateEpisodeUsesInjectedClockForDeadlineSemantics(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := repository.NewInMemoryStore()
	service := NewService(store, WithClock(func() time.Time { return now }))
	request := requestFixture(now)
	request.Constraints.DeadlineAt = now.Add(-time.Second)
	if _, err := service.CreateEpisode(context.Background(), request); !errors.Is(err, contract.ErrInvalidDeadline) {
		t.Fatalf("expected injected-clock deadline rejection, got %v", err)
	}
}

func TestSameStatePaymentStepIsCommittedWithoutNewEpisodeState(t *testing.T) {
	service, store, now, created := createFixture(t)
	steps := []struct {
		action      trace.ActionType
		observation trace.ObservationType
	}{
		{trace.ActionDiscover, trace.ObservationCandidatesFound},
		{trace.ActionInvoke, trace.ObservationCandidatesFound},
		{trace.ActionParse402, trace.ObservationHTTP402},
	}
	for index, step := range steps {
		proposal := makeProposal(created.EpisodeID, uint64(index), step.action, now)
		result, err := service.CommitProposal(context.Background(), commitRequest(proposal, step.action, "payment-flow-"+string(rune('a'+index)), trace.Observation{Type: step.observation}))
		if err != nil {
			t.Fatal(err)
		}
		created = result.Episode
	}
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: created.EpisodeID,
		Quote: TrustedPaymentQuote{MerchantDID: "did:merchant:same-state", CapabilityID: "capability:same-state", PayeeDID: "did:payee:same-state", QuoteHash: "same-state-quote", AmountMinor: 100,
			Currency: "USDC", RequesterDID: created.RequesterDID, ExpiresAt: now.Add(30 * time.Minute)}, IdempotencyKey: "same-state-reserve", TraceID: "same-state-trace"})
	if err != nil {
		t.Fatal(err)
	}
	created = reserved.Episode
	next := created.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StatePaying, now, ""); err != nil {
		t.Fatal(err)
	}
	event, err := episode.NewEvent("evt-same-state-runtime", created.EpisodeID, created.Version, now, episode.StatePaying,
		trace.Action{Type: trace.ActionPaymentSubmitted, IdempotencyKey: "payment-submitted"}, trace.Observation{Type: trace.ObservationPaymentSubmitted},
		trace.Decision{ProposedAction: trace.ActionPaymentSubmitted, ProposalID: "runtime-payment-submitted", Reason: "deterministic runtime test"},
		trace.RuntimeVerdict{Allowed: true}, episode.StatePaying, "runtime", "same-state-trace", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitFinanceTransition(context.Background(), repository.FinanceTransition{EpisodeID: created.EpisodeID,
		ExpectedEpisodeVersion: created.Version, NextEpisode: next, Event: event}); err != nil {
		t.Fatal(err)
	}
	if next.State != episode.StatePaying || next.Version != 6 || next.ActionCount != 5 {
		t.Fatalf("same-state payment event changed projection unexpectedly: %#v", next)
	}
	events, err := store.ListByEpisode(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[4].StateBefore != episode.StatePaying || events[4].StateAfter != episode.StatePaying || events[4].Sequence != 5 {
		t.Fatalf("unexpected same-state event: %#v", events)
	}
}

func TestRuntimeGuardRejectsInvalidProposalCases(t *testing.T) {
	service, _, now, created := createFixture(t)
	base := func(action trace.ActionType, sequence uint64, key string) CommitRequest {
		return commitRequest(makeProposal(created.EpisodeID, sequence, action, now), action, key, trace.Observation{Type: trace.ObservationCandidatesFound})
	}
	future := base(trace.ActionDiscover, 99, "future")
	if _, err := service.CommitProposal(context.Background(), future); !errors.Is(err, decision.ErrFutureEventSequence) {
		t.Fatalf("future proposal: %v", err)
	}
	if _, err := service.CommitProposal(context.Background(), base(trace.ActionDiscover, 0, "advance")); err != nil {
		t.Fatal(err)
	}
	stale := base(trace.ActionSelectMerchant, 0, "stale")
	if _, err := service.CommitProposal(context.Background(), stale); !errors.Is(err, decision.ErrStaleEventSequence) {
		t.Fatalf("stale proposal: %v", err)
	}
	illegal := base(trace.ActionValidateDelivery, 1, "illegal")
	if _, err := service.CommitProposal(context.Background(), illegal); !errors.Is(err, decision.ErrActionNotAllowed) {
		t.Fatalf("illegal action: %v", err)
	}
	unknownEvidence := base(trace.ActionSelectMerchant, 1, "unknown-evidence")
	unknownEvidence.Proposal.EvidenceRefs = []string{"object://evidence/unknown"}
	if _, err := service.CommitProposal(context.Background(), unknownEvidence); !errors.Is(err, decision.ErrUnknownEvidenceReference) {
		t.Fatalf("unknown evidence: %v", err)
	}
	expired := base(trace.ActionSelectMerchant, 1, "expired")
	expired.Proposal.ExpiresAt = now.Add(-time.Second)
	if _, err := service.CommitProposal(context.Background(), expired); !errors.Is(err, decision.ErrProposalExpired) {
		t.Fatalf("expired proposal: %v", err)
	}
	if _, err := service.CommitProposal(context.Background(), base(trace.ActionStop, 1, "terminal")); err != nil {
		t.Fatal(err)
	}
	terminal := base(trace.ActionDiscover, 2, "after-terminal")
	if _, err := service.CommitProposal(context.Background(), terminal); !errors.Is(err, decision.ErrProposalAfterTerminal) {
		t.Fatalf("terminal proposal: %v", err)
	}
}

func TestConcurrentTransitionsOnlyOneWins(t *testing.T) {
	service, store, now, created := createFixture(t)
	proposalA := makeProposal(created.EpisodeID, 0, trace.ActionDiscover, now)
	proposalB := makeProposal(created.EpisodeID, 0, trace.ActionDiscover, now)
	requestA := commitRequest(proposalA, trace.ActionDiscover, "concurrent-a", trace.Observation{Type: trace.ObservationCandidatesFound})
	requestB := commitRequest(proposalB, trace.ActionDiscover, "concurrent-b", trace.Observation{Type: trace.ObservationCandidatesFound})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, request := range []CommitRequest{requestA, requestB} {
		wait.Add(1)
		go func(request CommitRequest) {
			defer wait.Done()
			<-start
			_, err := service.CommitProposal(context.Background(), request)
			results <- err
		}(request)
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, repository.ErrVersionConflict) && !errors.Is(err, decision.ErrStaleEventSequence) {
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one successful transition, got %d", successes)
	}
	events, err := store.ListByEpisode(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("expected exactly one event, got %#v", events)
	}
	current, err := service.GetEpisode(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || current.ActionCount != 1 {
		t.Fatalf("projection was committed more than once: %#v", current)
	}
}

func TestStaticProviderHasNoCommitAuthority(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	provider := decision.StaticDecisionProvider{Proposal: makeProposal("ce_1", 0, trace.ActionDiscover, now)}
	proposal, err := provider.Propose(context.Background(), decision.ProposalInput{EpisodeID: "ce_1"})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ProposalID == "" {
		t.Fatal("static provider did not return proposal")
	}
}

func TestDecisionProposalCannotCommitPaymentFacts(t *testing.T) {
	service, store, now, created := createFixture(t)
	current := advanceToNegotiating(t, service, created, now)
	for index, action := range []trace.ActionType{
		trace.ActionReserveBudget, trace.ActionNegotiateAndPay, trace.ActionCreatePayment,
		trace.ActionPaymentAuthorizationChecked, trace.ActionPaymentSubmitted, trace.ActionPaymentPending,
		trace.ActionPaymentStatusQueried, trace.ActionPaymentConfirmed, trace.ActionPaymentFailed,
		trace.ActionPaymentUnknown, trace.ActionVerifyEntitlement,
	} {
		proposal := makeProposal(current.EpisodeID, current.Version-1, action, now)
		if _, err := service.CommitProposal(context.Background(), commitRequest(proposal, action, "blocked-payment-action-"+string(rune('a'+index)), trace.Observation{Type: trace.ObservationPaymentConfirmed})); !errors.Is(err, decision.ErrActionNotAllowed) {
			t.Fatalf("expected runtime-owned action %s to be blocked, got %v", action, err)
		}
	}
	if current.State != episode.StateNegotiating {
		t.Fatalf("blocked proposal changed state: %s", current.State)
	}
	entries, err := store.ListLedgerEntries(context.Background(), current.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("decision proposal created ledger facts: %#v", entries)
	}
}
