package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func applicationCapability(merchant, capability, payee, task string, price int64, version string, now time.Time) *catalog.MerchantCapability {
	return &catalog.MerchantCapability{MerchantDID: merchant, CapabilityID: capability, PayeeDID: payee, Name: capability, Description: "catalog fact",
		TaskTypes: []string{task}, SemanticTags: []string{"language=en"}, InvokeEndpoint: catalog.EndpointRef{Ref: "merchant://" + merchant + "/" + capability},
		QuoteEndpoint: catalog.EndpointRef{Ref: "quote://" + merchant + "/" + capability}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output",
		InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v1"},
		SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive,
		Availability: catalog.AvailabilityAvailable, CatalogVersion: version, Source: "test", SourceRef: "fixture:" + merchant,
		ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
}

func TestS3DiscoverySelectionGuardAndPayeeBinding(t *testing.T) {
	service, store, now := serviceFixture()
	request := requestFixture(now)
	request.Constraints.BudgetLimitMinor = 10
	createdResult, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	created := createdResult.Episode
	a := applicationCapability("did:merchant:a", "transcription-premium", "did:merchant:a", "transcription", 8, "v1", now)
	b := applicationCapability("did:merchant:b", "transcription", "did:solana:payee-b", "transcription", 12, "v1", now)
	c := applicationCapability("did:merchant:c", "image-generation", "did:solana:payee-c", "image-generation", 5, "v1", now)
	for _, capability := range []*catalog.MerchantCapability{a, b, c} {
		if err := service.RegisterCapabilityVersion(context.Background(), capability); err != nil {
			t.Fatal(err)
		}
	}
	discovered, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.EpisodeID, CandidateSetID: "cs-s3-e1"})
	if err != nil {
		t.Fatal(err)
	}
	if discovered.NoEligible || discovered.Episode.State != episode.StateDiscovering || len(discovered.CandidateSet.Candidates) != 1 || discovered.CandidateSet.Candidates[0].MerchantDID != a.MerchantDID {
		t.Fatalf("unexpected S3 discovery result: %#v", discovered)
	}
	candidate := discovered.CandidateSet.Candidates[0]
	current := discovered.Episode
	v2 := a.Clone()
	v2.CatalogVersion = "v2"
	v2.PayeeDID = "did:solana:payee-new"
	v2.UpdatedAt = now.Add(time.Minute)
	if err := service.RegisterCapabilityVersion(context.Background(), v2); err != nil {
		t.Fatal(err)
	}
	proposal := decision.DecisionProposal{ProposalID: "select-a", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1,
		ProposedAction: trace.ActionSelectMerchant, CandidateSetID: discovered.CandidateSet.CandidateSetID,
		Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID}, Confidence: 1,
		EvidenceRefs: []string{discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash}, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	selected, err := service.CommitMerchantSelection(context.Background(), SelectMerchantRequest{Proposal: proposal,
		Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "select-a"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound,
			FactsRef: discovered.CandidateSet.FactsRef, PayloadHash: discovered.CandidateSet.PayloadHash}, Actor: "runtime", TraceID: "trace-s3"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Episode.SelectedMerchantDID != a.MerchantDID || selected.Episode.SelectedCapabilityID != a.CapabilityID || selected.Episode.SelectedCatalogVersion != "v1" || selected.Episode.SelectedCatalogSnapshotHash != candidate.CatalogSnapshotHash || selected.Episode.SelectedCandidateSetID != discovered.CandidateSet.CandidateSetID {
		t.Fatalf("selection did not bind catalog snapshot: %#v", selected.Episode)
	}
	events, err := service.ListEvents(context.Background(), created.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := episode.Reconstruct(created, events)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SelectedCatalogSnapshotHash != selected.Episode.SelectedCatalogSnapshotHash || replayed.SelectedCandidateSetID != selected.Episode.SelectedCandidateSetID {
		t.Fatalf("selection binding was not replayable: %#v vs %#v", replayed, selected.Episode)
	}
	payee, err := service.ResolveSelectedPayeeDID(context.Background(), created.EpisodeID)
	if err != nil || payee != a.PayeeDID {
		t.Fatalf("selected payee was not resolved from catalog fact: %s, %v", payee, err)
	}
	old, err := store.GetCapabilityVersion(context.Background(), a.MerchantDID, a.CapabilityID, "v1")
	if err != nil || old.PayeeDID != a.PayeeDID {
		t.Fatalf("catalog v1 was mutated by v2 registration: %#v, %v", old, err)
	}
	payee, err = service.ResolveSelectedPayeeDID(context.Background(), created.EpisodeID)
	if err != nil || payee != a.PayeeDID {
		t.Fatalf("old Episode followed the new catalog payee: %s, %v", payee, err)
	}

	// A second Episode demonstrates that a filtered candidate cannot be
	// selected even when the DecisionProposal names a real catalog entry.
	request = requestFixture(now)
	request.RequestID = "acr_s3_filtered"
	request.Constraints.BudgetLimitMinor = 10
	other, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: other.Episode.EpisodeID, CandidateSetID: "cs-s3-e2"})
	if err != nil {
		t.Fatal(err)
	}
	bad := proposal
	bad.ProposalID = "select-filtered"
	bad.EpisodeID = other.Episode.EpisodeID
	bad.BasedOnEventSequence = filtered.Episode.Version - 1
	bad.CandidateSetID = filtered.CandidateSet.CandidateSetID
	bad.Target = &decision.ProposalTarget{MerchantDID: b.MerchantDID, CapabilityID: b.CapabilityID}
	bad.EvidenceRefs = []string{filtered.CandidateSet.FactsRef, filtered.CandidateSet.PayloadHash}
	if _, err := service.CommitMerchantSelection(context.Background(), SelectMerchantRequest{Proposal: bad,
		Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "select-filtered"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "runtime"}); !errors.Is(err, decision.ErrMerchantNotInCandidateSet) {
		t.Fatalf("filtered merchant was accepted: %v", err)
	}
	bad.ProposalID = "select-hallucinated"
	bad.Target = &decision.ProposalTarget{MerchantDID: "did:merchant:hallucinated", CapabilityID: "missing"}
	if _, err := service.CommitMerchantSelection(context.Background(), SelectMerchantRequest{Proposal: bad,
		Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "select-hallucinated"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "runtime"}); !errors.Is(err, decision.ErrMerchantNotInCandidateSet) {
		t.Fatalf("hallucinated merchant was accepted: %v", err)
	}
}

func TestS3NoEligibleCandidateHasDeterministicTerminalPath(t *testing.T) {
	service, _, now, _ := createFixture(t)
	request := requestFixture(now)
	request.RequestID = "acr_s3_none"
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterCapabilityVersion(context.Background(), applicationCapability("did:merchant:image", "image-generation", "did:solana:image", "image-generation", 5, "v1", now)); err != nil {
		t.Fatal(err)
	}
	result, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs-s3-none"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoEligible || result.Episode.State != episode.StateFailed || result.Episode.TerminalReason != "NO_ELIGIBLE_MERCHANT" {
		t.Fatalf("unexpected no-candidate terminal result: %#v", result.Episode)
	}
	events, err := service.ListEvents(context.Background(), created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Observation.Type != trace.ObservationNoEligibleCandidate || events[1].Observation.Code != "NO_ELIGIBLE_MERCHANT" {
		t.Fatalf("no-candidate evidence path is not deterministic: %#v", events)
	}
	replayed, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs-s3-none"})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Event.EventID != events[0].EventID || replayed.TerminalEvent.EventID != events[1].EventID || replayed.Episode.ActionCount != result.Episode.ActionCount {
		t.Fatalf("no-candidate discovery replay was not stable: %#v", replayed)
	}
}

func TestS3DiscoveryReplayRecoversFrozenCandidateSet(t *testing.T) {
	service, store, now := serviceFixture()
	request := requestFixture(now)
	request.RequestID = "acr_s3_discovery_replay"
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	v1 := applicationCapability("did:merchant:replay", "transcription", "did:solana:replay", "transcription", 8, "v1", now)
	if err := service.RegisterCapabilityVersion(context.Background(), v1); err != nil {
		t.Fatal(err)
	}
	first, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || len(first.CandidateSet.Candidates) != 1 || first.CandidateSet.CandidateSetID != "cs:"+created.Episode.EpisodeID {
		t.Fatalf("unexpected first discovery result: %#v", first)
	}
	v2 := v1.Clone()
	v2.CatalogVersion = "v2"
	v2.Status = catalog.StatusInactive
	v2.UpdatedAt = now.Add(time.Minute)
	if err := service.RegisterCapabilityVersion(context.Background(), v2); err != nil {
		t.Fatal(err)
	}
	second, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.CandidateSet.CandidateSetID != first.CandidateSet.CandidateSetID || second.CandidateSet.PayloadHash != first.CandidateSet.PayloadHash || second.Event.EventID != first.Event.EventID || second.CandidateSet.Candidates[0].CatalogVersion != "v1" {
		t.Fatalf("discovery replay reread or changed the catalog fact: first=%#v second=%#v", first, second)
	}
	if second.Episode.ActionCount != first.Episode.ActionCount {
		t.Fatalf("discovery replay changed ActionCount: %d -> %d", first.Episode.ActionCount, second.Episode.ActionCount)
	}
	events, err := store.ListByEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("discovery replay appended another event: %#v", events)
	}
	if _, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs:different-discovery"}); !errors.Is(err, decision.ErrActionNotAllowed) {
		t.Fatalf("different discovery identity was accepted: %v", err)
	}
}

func TestS3ExpiredCandidateSetRejectedByRuntimeGuard(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	_, _, _, created := createFixture(t)
	current := created.Clone()
	if err := current.ApplyTransition(episode.StateDiscovering, now, ""); err != nil {
		t.Fatal(err)
	}
	capability := applicationCapability("did:merchant:expired", "transcription", "did:solana:expired", "transcription", 8, "v1", now)
	query := catalog.DiscoveryQuery{TaskType: "transcription", InputContentType: "audio/mpeg", ExpectedOutputContentType: "text/plain", SupportedProtocolVersions: []string{"x402-v1"}, Currency: "USDC", BudgetLimitMinor: 10, Deadline: now.Add(time.Hour)}
	set, err := catalog.BuildCandidateSet("cs-expired", current.EpisodeID, current.RequestID, query, []*catalog.MerchantCapability{capability}, now.Add(-2*time.Minute), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	proposal := decision.DecisionProposal{ProposalID: "expired-selection", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: set.CandidateSetID,
		Target: &decision.ProposalTarget{MerchantDID: capability.MerchantDID, CapabilityID: capability.CapabilityID}, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	_, err = decision.NewRuntimeGuard().EvaluateMerchantSelection(current, proposal, nil, trace.Observation{Type: trace.ObservationCandidatesFound}, now, set)
	if !errors.Is(err, catalog.ErrCandidateSetExpired) {
		t.Fatalf("expired candidate set was accepted: %v", err)
	}
}

func TestS3ExpiredFrozenCatalogCandidateRejectedAtSelection(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	_, _, _, created := createFixture(t)
	current := created.Clone()
	if err := current.ApplyTransition(episode.StateDiscovering, now, ""); err != nil {
		t.Fatal(err)
	}
	capability := applicationCapability("did:merchant:frozen-expired", "transcription", "did:solana:frozen-expired", "transcription", 8, "v1", now)
	capability.ValidUntil = now.Add(time.Minute)
	query := catalog.DiscoveryQuery{TaskType: "transcription", InputContentType: "audio/mpeg", ExpectedOutputContentType: "text/plain", SupportedProtocolVersions: []string{"x402-v1"}, Currency: "USDC", BudgetLimitMinor: 10, Deadline: now.Add(time.Hour)}
	set, err := catalog.BuildCandidateSet("cs-frozen-expired", current.EpisodeID, current.RequestID, query, []*catalog.MerchantCapability{capability}, now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	proposal := decision.DecisionProposal{ProposalID: "frozen-expired-selection", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: set.CandidateSetID,
		Target: &decision.ProposalTarget{MerchantDID: capability.MerchantDID, CapabilityID: capability.CapabilityID}, CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(3 * time.Minute)}
	_, err = decision.NewRuntimeGuard().EvaluateMerchantSelection(current, proposal, nil, trace.Observation{Type: trace.ObservationCandidatesFound}, now.Add(2*time.Minute), set)
	if !errors.Is(err, catalog.ErrCatalogSnapshotExpired) {
		t.Fatalf("expired frozen catalog candidate was accepted: %v", err)
	}
}
