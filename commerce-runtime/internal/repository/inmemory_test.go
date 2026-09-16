package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
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

func repositoryCapability(merchant, capability, version string, status catalog.CapabilityStatus, now time.Time) *catalog.MerchantCapability {
	price := int64(8)
	return &catalog.MerchantCapability{MerchantDID: merchant, CapabilityID: capability, PayeeDID: "did:solana:payee:" + capability,
		Name: capability, Description: "repository catalog fact", TaskTypes: []string{"transcription"}, SemanticTags: []string{"language=en"},
		InvokeEndpoint: catalog.EndpointRef{Ref: "merchant://" + merchant + "/" + capability}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output",
		InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v1"},
		SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: status,
		Availability: catalog.AvailabilityAvailable, CatalogVersion: version, Source: "repository-test", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour),
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
}

func TestInMemoryCurrentPointerFollowsLatestVersionAcrossDeactivation(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := NewInMemoryStore()
	merchant := "did:merchant:current-pointer"
	for _, value := range []*catalog.MerchantCapability{
		repositoryCapability(merchant, "transcription", "v1", catalog.StatusActive, now),
		repositoryCapability(merchant, "transcription", "v2", catalog.StatusInactive, now),
	} {
		if err := store.SaveCapabilityVersion(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.GetCurrentActiveCapability(context.Background(), merchant, "transcription")
	if err != nil || current.CatalogVersion != "v2" || current.Status != catalog.StatusInactive {
		t.Fatalf("current pointer did not retain inactive authoritative version: %#v, %v", current, err)
	}
	active, err := store.ListActiveCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range active {
		if value.MerchantDID == merchant && value.CapabilityID == "transcription" {
			t.Fatalf("discovery fell back to historical v1: %#v", value)
		}
	}
}

func TestInMemoryDeprecatedCurrentVersionHasNoCandidate(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := NewInMemoryStore()
	merchant := "did:merchant:deprecated-pointer"
	for _, value := range []*catalog.MerchantCapability{
		repositoryCapability(merchant, "transcription", "v1", catalog.StatusActive, now),
		repositoryCapability(merchant, "transcription", "v2", catalog.StatusDeprecated, now),
	} {
		if err := store.SaveCapabilityVersion(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.GetCurrentActiveCapability(context.Background(), merchant, "transcription")
	if err != nil || current.CatalogVersion != "v2" || current.Status != catalog.StatusDeprecated {
		t.Fatalf("current pointer did not retain deprecated authoritative version: %#v, %v", current, err)
	}
	active, err := store.ListActiveCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range active {
		if value.MerchantDID == merchant && value.CapabilityID == "transcription" {
			t.Fatalf("discovery returned a historical active version after deprecation: %#v", value)
		}
	}
}

func TestInMemoryCurrentPointerMovesPastDeactivationToNewActiveVersion(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	store := NewInMemoryStore()
	merchant := "did:merchant:reactivated-pointer"
	for _, value := range []*catalog.MerchantCapability{
		repositoryCapability(merchant, "transcription", "v1", catalog.StatusActive, now),
		repositoryCapability(merchant, "transcription", "v2", catalog.StatusInactive, now),
		repositoryCapability(merchant, "transcription", "v3", catalog.StatusActive, now),
	} {
		if err := store.SaveCapabilityVersion(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.GetCurrentActiveCapability(context.Background(), merchant, "transcription")
	if err != nil || current.CatalogVersion != "v3" {
		t.Fatalf("current pointer did not move to v3: %#v, %v", current, err)
	}
	active, err := store.ListActiveCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found *catalog.MerchantCapability
	for _, value := range active {
		if value.MerchantDID == merchant && value.CapabilityID == "transcription" {
			found = value
		}
	}
	if found == nil || found.CatalogVersion != "v3" {
		t.Fatalf("discovery did not return current v3: %#v", found)
	}
}
