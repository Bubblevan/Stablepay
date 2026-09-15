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
