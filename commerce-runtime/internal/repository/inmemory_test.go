package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestEventSequenceIsUniqueAndHistoryIsAppendOnly(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	request := contract.AcquireCapabilityRequest{RequestID: "acr_repo", RequesterDID: "did:agent",
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "task", Description: "description"},
		Input:           contract.Input{Ref: "input", ContentType: "text/plain"},
		Constraints:     contract.Constraints{BudgetLimitMinor: 1, Currency: "USDC", DeadlineAt: now.Add(time.Hour), MaxTotalAttempts: 2, MaxPaymentAttempts: 1, MaxDeliveryAttempts: 1},
		Validator:       contract.ValidatorRef{Kind: "builtin", Name: "validator", Version: "v1"}}
	value, err := episode.New("ce_repo", request, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	event, err := episode.NewEvent("evt_1", value.EpisodeID, 1, now, episode.StateAccepted,
		trace.Action{Type: trace.ActionDiscover, IdempotencyKey: "key-1"}, trace.Observation{Type: trace.ObservationCandidatesFound},
		trace.Decision{ProposedAction: trace.ActionDiscover, ProposalID: "proposal-1"}, trace.RuntimeVerdict{Allowed: true}, episode.StateDiscovering,
		"runtime", "trace", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	duplicate := event.Clone()
	duplicate.EventID = "evt_2"
	if err := store.Append(context.Background(), duplicate); !errors.Is(err, ErrEventSequenceConflict) {
		t.Fatalf("expected duplicate sequence rejection, got %v", err)
	}
	loaded, err := store.ListByEpisode(context.Background(), value.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	loaded[0].StateAfter = episode.StateFailed
	loadedAgain, err := store.ListByEpisode(context.Background(), value.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedAgain[0].StateAfter != episode.StateDiscovering {
		t.Fatal("repository exposed mutable event history")
	}
}
