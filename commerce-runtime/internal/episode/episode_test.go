package episode

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func episodeRequest(deadline time.Time) contract.AcquireCapabilityRequest {
	return contract.AcquireCapabilityRequest{
		RequestID: "acr_episode", RequesterDID: "did:stablepay:agent",
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "transcription", Description: "transcribe"},
		Input:           contract.Input{Ref: "input-1", ContentType: "audio/mpeg"},
		Constraints:     contract.Constraints{BudgetLimitMinor: 10, Currency: "USDC", DeadlineAt: deadline, MaxTotalAttempts: 4, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2},
		Validator:       contract.ValidatorRef{Kind: "builtin", Name: "transcript", Version: "v1"},
	}
}

func TestEpisodeStateMachineAndTerminalIrreversibility(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	value, err := New("ce_episode", episodeRequest(now.Add(time.Hour)), now)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != StateAccepted || value.Version != 1 {
		t.Fatalf("unexpected initial projection: %#v", value)
	}
	if err := value.ApplyTransition(StateDiscovering, now, ""); err != nil {
		t.Fatal(err)
	}
	if value.Version != 2 || value.State != StateDiscovering {
		t.Fatalf("unexpected transition result: %#v", value)
	}
	if err := value.ApplyTransition(StateFulfilled, now, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	if err := value.ApplyTransition(StateFailed, now, "delivery retry exhausted"); err != nil {
		t.Fatal(err)
	}
	if !IsTerminal(value.State) || value.TerminalReason != "delivery retry exhausted" {
		t.Fatalf("expected terminal episode: %#v", value)
	}
	if err := value.ApplyTransition(StateDiscovering, now, ""); !errors.Is(err, ErrTerminalEpisode) {
		t.Fatalf("expected terminal rejection, got %v", err)
	}
}

func TestExpiredEpisodeCannotTransition(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	value, err := New("ce_expired", episodeRequest(now.Add(time.Minute)), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.ApplyTransition(StateDiscovering, now.Add(2*time.Minute), ""); !errors.Is(err, ErrEpisodeExpired) {
		t.Fatalf("expected expired episode, got %v", err)
	}
}

func TestContractSnapshotIsCopiedAndStateMappingDoesNotAcceptArbitraryTarget(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	request := episodeRequest(now.Add(time.Hour))
	value, err := New("ce_snapshot", request, now)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), value.ContractSnapshot...)
	request.AcquisitionGoal.Description = "changed after creation"
	if !reflect.DeepEqual(value.ContractSnapshot, original) {
		t.Fatal("episode snapshot changed after source request mutation")
	}
	if next, ok := StateForAction(StateDiscovering, trace.ActionInvoke, trace.ObservationCandidatesFound); !ok || next != StateInvoking {
		t.Fatalf("unexpected action mapping: %s %t", next, ok)
	}
	if _, ok := StateForAction(StateDiscovering, trace.ActionValidateDelivery, trace.ObservationDeliveryValid); ok {
		t.Fatal("runtime accepted an action that is not allowed in the current state")
	}
}

func TestReplayReconstructsState(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	initial, err := New("ce_replay", episodeRequest(now.Add(time.Hour)), now)
	if err != nil {
		t.Fatal(err)
	}
	current := initial.Clone()
	if err := current.ApplyTransition(StateDiscovering, now.Add(time.Second), ""); err != nil {
		t.Fatal(err)
	}
	current.ActionCount++
	event1, err := NewEvent("evt_1", current.EpisodeID, 1, now.Add(time.Second), StateAccepted,
		trace.Action{Type: trace.ActionDiscover, IdempotencyKey: "a1"},
		trace.Observation{Type: trace.ObservationCandidatesFound},
		trace.Decision{ProposedAction: trace.ActionDiscover, ProposalID: "p1"},
		trace.RuntimeVerdict{Allowed: true}, StateDiscovering, "runtime", "trace-1", "test")
	if err != nil {
		t.Fatal(err)
	}
	reconstructed, err := Reconstruct(initial, []*EpisodeEvent{event1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, reconstructed) {
		t.Fatalf("reconstructed projection differs:\ncurrent=%#v\nreconstructed=%#v", current, reconstructed)
	}
}

func TestBudgetRefundSemanticsAreExplicit(t *testing.T) {
	budget := BudgetSnapshot{
		Currency:         "USDC",
		BudgetLimitMinor: 100,
		ReservedAmount:   10,
		SettledAmount:    70,
		RefundedAmount:   20,
		RefundReusable:   true,
	}
	if err := budget.Recalculate(); err != nil {
		t.Fatal(err)
	}
	if budget.ConsumedAmount != 50 || budget.AvailableBudget != 40 || budget.SunkCost != 50 {
		t.Fatalf("unexpected reusable-refund budget: %#v", budget)
	}

	budget.RefundReusable = false
	if err := budget.Recalculate(); err != nil {
		t.Fatal(err)
	}
	if budget.ConsumedAmount != 70 || budget.AvailableBudget != 20 || budget.SunkCost != 50 {
		t.Fatalf("unexpected non-reusable-refund budget: %#v", budget)
	}

	budget.RefundReusable = true
	budget.RefundedAmount = 70
	if err := budget.Recalculate(); err != nil {
		t.Fatal(err)
	}
	if budget.ConsumedAmount != 0 || budget.AvailableBudget != 90 || budget.SunkCost != 0 {
		t.Fatalf("max(0, settled-refunded) semantics not applied: %#v", budget)
	}
}

func TestSameStatePaymentEventsAdvanceVersionAndReplay(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	initial, err := New("ce_same_state", episodeRequest(now.Add(time.Hour)), now)
	if err != nil {
		t.Fatal(err)
	}
	current := initial.Clone()
	for _, state := range []State{StateDiscovering, StateInvoking, StateNegotiating, StatePaying} {
		if err := current.ApplyTransition(state, now, ""); err != nil {
			t.Fatal(err)
		}
	}
	event, err := NewEvent("evt_same_state", current.EpisodeID, current.Version,
		now, StatePaying,
		trace.Action{Type: trace.ActionPaymentSubmitted, IdempotencyKey: "payment-submitted-1"},
		trace.Observation{Type: trace.ObservationPaymentSubmitted},
		trace.Decision{ProposedAction: trace.ActionPaymentSubmitted, ProposalID: "proposal-payment-submitted"},
		trace.RuntimeVerdict{Allowed: true}, StatePaying, "runtime", "trace-same-state", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.ApplyCommittedState(StatePaying, now, ""); err != nil {
		t.Fatal(err)
	}
	if current.State != StatePaying || current.Version != 6 {
		t.Fatalf("same-state event changed state incorrectly: %#v", current)
	}
	reconstructed, err := Reconstruct(initial, append([]*EpisodeEvent{
		mustEvent(t, "evt_1", initial.EpisodeID, 1, now, StateAccepted, trace.ActionDiscover, trace.ObservationCandidatesFound, StateDiscovering),
		mustEvent(t, "evt_2", initial.EpisodeID, 2, now, StateDiscovering, trace.ActionInvoke, trace.ObservationCandidatesFound, StateInvoking),
		mustEvent(t, "evt_3", initial.EpisodeID, 3, now, StateInvoking, trace.ActionParse402, trace.ObservationHTTP402, StateNegotiating),
		mustEvent(t, "evt_4", initial.EpisodeID, 4, now, StateNegotiating, trace.ActionReserveBudget, trace.ObservationQuoteValid, StatePaying),
	}, event))
	if err != nil {
		t.Fatal(err)
	}
	if reconstructed.State != StatePaying || reconstructed.Version != current.Version {
		t.Fatalf("same-state event was not replayed: %#v", reconstructed)
	}
}

func mustEvent(t *testing.T, eventID, episodeID string, sequence uint64, occurredAt time.Time, before State, action trace.ActionType, observation trace.ObservationType, after State) *EpisodeEvent {
	t.Helper()
	event, err := NewEvent(eventID, episodeID, sequence, occurredAt, before,
		trace.Action{Type: action, IdempotencyKey: eventID},
		trace.Observation{Type: observation},
		trace.Decision{ProposedAction: action, ProposalID: "proposal-" + eventID},
		trace.RuntimeVerdict{Allowed: true}, after, "runtime", "trace-"+eventID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return event
}
