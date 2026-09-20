package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

type fixtureSource struct {
	episode     memory.EpisodeView
	events      []memory.EventView
	payments    []memory.PaymentView
	deliveries  map[string]memory.DeliveryView
	validations map[string]memory.ValidationView
}

func (f fixtureSource) GetMemoryEpisode(context.Context, string) (memory.EpisodeView, error) {
	return f.episode, nil
}
func (f fixtureSource) ListMemoryEpisodeEvents(context.Context, string) ([]memory.EventView, error) {
	return append([]memory.EventView(nil), f.events...), nil
}
func (f fixtureSource) ListMemoryPaymentIntents(context.Context, string) ([]memory.PaymentView, error) {
	return append([]memory.PaymentView(nil), f.payments...), nil
}
func (f fixtureSource) GetMemoryDeliveryArtifact(_ context.Context, id string) (memory.DeliveryView, error) {
	return f.deliveries[id], nil
}
func (f fixtureSource) GetMemoryValidationEvidence(_ context.Context, id string) (memory.ValidationView, error) {
	return f.validations[id], nil
}

func TestProjectEpisodeDerivesU2HistoryAndReplayIsStable(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	source := fixtureSource{
		episode: memory.EpisodeView{EpisodeID: "episode-u2", RequesterDID: "did:requester:test", State: "FULFILLED", SelectedCatalogVersion: "v1", SelectedCatalogHash: "sha256:catalog", DeliveryRefs: []string{"delivery-a", "delivery-b"}, ValidationRefs: []string{"validation-a", "validation-b"}, UpdatedAt: now},
		events: []memory.EventView{
			{EventID: "event-select-a", Sequence: 1, Action: "SELECT_MERCHANT", TargetMerchantDID: "did:merchant:a", CapabilityID: "transcription"},
			{EventID: "event-invalid-a", Sequence: 2, Action: "VALIDATE_DELIVERY", Observation: "DELIVERY_INVALID", TargetMerchantDID: "did:merchant:a", CapabilityID: "transcription"},
			{EventID: "event-switch-b", Sequence: 3, Action: "SWITCH_MERCHANT", Observation: "CANDIDATES_FOUND", TargetMerchantDID: "did:merchant:b", CapabilityID: "transcription"},
			{EventID: "event-valid-b", Sequence: 4, Action: "VALIDATE_DELIVERY", Observation: "DELIVERY_VALID", TargetMerchantDID: "did:merchant:b", CapabilityID: "transcription"},
		},
		payments:    []memory.PaymentView{{IntentID: "intent-a", MerchantDID: "did:merchant:a", CapabilityID: "transcription", Status: "CONFIRMED", UpdatedAt: now}, {IntentID: "intent-b", MerchantDID: "did:merchant:b", CapabilityID: "transcription", Status: "CONFIRMED", UpdatedAt: now}},
		deliveries:  map[string]memory.DeliveryView{"delivery-a": {DeliveryID: "delivery-a", EpisodeID: "episode-u2", MerchantDID: "did:merchant:a", CapabilityID: "transcription", Attempt: 1, ReceivedAt: now}, "delivery-b": {DeliveryID: "delivery-b", EpisodeID: "episode-u2", MerchantDID: "did:merchant:b", CapabilityID: "transcription", Attempt: 1, ReceivedAt: now.Add(time.Second)}},
		validations: map[string]memory.ValidationView{"validation-a": {ValidationID: "validation-a", DeliveryID: "delivery-a", Valid: false, ReasonCode: "CONTENT_MISMATCH", CreatedAt: now}, "validation-b": {ValidationID: "validation-b", DeliveryID: "delivery-b", Valid: true, ReasonCode: "VALID", CreatedAt: now.Add(time.Second)}},
	}
	store := repository.NewInMemoryStore()
	projector := memory.NewProjector(source, store, memory.WithProjectorClock(func() time.Time { return now }))
	mutations, err := projector.ProjectEpisode(context.Background(), source.episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 5 { // merchant A/B, capability A/B, recovery
		t.Fatalf("mutation count=%d", len(mutations))
	}
	for _, query := range []memory.MemoryQuery{{MerchantDID: "did:merchant:a", CapabilityID: "transcription", Now: now}, {MerchantDID: "did:merchant:b", CapabilityID: "transcription", Now: now}, {RequesterDID: "did:requester:test", Now: now}} {
		values, err := store.SearchMemories(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if len(values) == 0 {
			t.Fatalf("no memory for query %#v", query)
		}
	}
	aID := memory.MemoryIDFor(memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "", "", "did:merchant:a", "transcription", "v1")
	a, err := store.GetMemory(context.Background(), aID)
	if err != nil {
		t.Fatal(err)
	}
	if a.StructuredFacts.DeliveryInvalidCount != 1 || a.StructuredFacts.SwitchAwayCount != 1 || a.StructuredFacts.DeliveryValidCount != 0 {
		t.Fatalf("merchant A facts=%#v", a.StructuredFacts)
	}
	bID := memory.MemoryIDFor(memory.MemoryCapabilityOutcome, memory.ScopeMerchantCapability, "", "", "did:merchant:b", "transcription", "v1")
	b, err := store.GetMemory(context.Background(), bID)
	if err != nil {
		t.Fatal(err)
	}
	if b.StructuredFacts.DeliveryValidCount != 1 || b.StructuredFacts.FulfilledCount != 1 {
		t.Fatalf("merchant B facts=%#v", b.StructuredFacts)
	}
	if _, err := projector.ProjectEpisode(context.Background(), source.episode.EpisodeID); err != nil {
		t.Fatal(err)
	}
	aAfter, err := store.GetMemory(context.Background(), aID)
	if err != nil {
		t.Fatal(err)
	}
	if aAfter.ObservationCount != 1 || aAfter.StructuredFacts.DeliveryInvalidCount != 1 {
		t.Fatalf("replay was not idempotent: %#v", aAfter)
	}
}

func TestProjectEpisodeRequiresTerminalState(t *testing.T) {
	source := fixtureSource{episode: memory.EpisodeView{EpisodeID: "episode-open", State: "RECOVERING"}}
	projector := memory.NewProjector(source, repository.NewInMemoryStore())
	if _, err := projector.ProjectEpisode(context.Background(), source.episode.EpisodeID); err == nil {
		t.Fatal("non-terminal episode was projected")
	}
}
