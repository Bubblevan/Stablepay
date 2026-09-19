package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type s4FaultMerchant struct {
	mu    sync.Mutex
	calls []adapters.MerchantInvokeRequest
}

func (m *s4FaultMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, request)
	call := len(m.calls)
	m.mu.Unlock()
	if request.Phase == invocation.PhaseInitial {
		payload := map[string]any{"x402Version": 2, "resource": map[string]any{"url": request.Endpoint.Endpoint}, "accepts": []any{map[string]any{
			"scheme": "exact", "network": "devnet", "amount": "300", "asset": "USDC", "payTo": "11111111111111111111111111111111", "maxTimeoutSeconds": 300,
			"extra": map[string]any{"currency": "USDC", "productId": "transcription-1", "skillDid": "did:solana:11111111111111111111111111111111"},
		}}}
		body, _ := json.Marshal(payload)
		encoded := base64.StdEncoding.EncodeToString(body)
		return adapters.MerchantInvokeResult{HTTPStatus: 402, Headers: map[string]string{"PAYMENT-REQUIRED": encoded}, Body: body, ContentType: "application/json", OccurredAt: time.Now().UTC()}, nil
	}
	body := []byte("transcript from retry")
	if call == 2 {
		body = nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: 200, Body: body, ContentType: "text/plain", OccurredAt: time.Now().UTC()}, nil
}

func TestS4CanonicalInnerLoopHasOnePaymentAndTwoDeliveryAttempts(t *testing.T) {
	service, store, now := serviceFixture()
	merchant := &s4FaultMerchant{}
	service.merchantAdapter = merchant
	request := requestFixture(now)
	request.RequestID = "acr-s4-canonical"
	request.Constraints.SupportedProtocolVersions = []string{"x402-v2"}
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://merchant.test/api/v1/products/transcription-1/execute"
	price := int64(250)
	capability := &catalog.MerchantCapability{MerchantDID: "did:merchant:a", CapabilityID: "transcription", PayeeDID: "did:solana:11111111111111111111111111111111", Name: "transcription", Description: "canonical S4 merchant", TaskTypes: []string{"transcription"}, SemanticTags: []string{"language=en"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: endpoint, Method: "GET"}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output", InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "s4-test", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
	if err := service.RegisterCapabilityVersion(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	discovered, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs-s4-canonical"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := discovered.CandidateSet.Candidates[0]
	selected, err := service.CommitMerchantSelection(context.Background(), SelectMerchantRequest{Proposal: decisionProposalForS4(created.Episode.EpisodeID, discovered.Episode.Version-1, discovered.CandidateSet.CandidateSetID, candidate, now), Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "select-s4"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "runtime", TraceID: "s4-select"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.InvokeSelectedMerchant(context.Background(), InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "s4-initial"})
	if err != nil {
		t.Fatal(err)
	}
	replayedInitial, err := service.InvokeSelectedMerchant(context.Background(), InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "s4-initial-replay"})
	if err != nil || !replayedInitial.Replayed || len(merchant.calls) != 1 {
		t.Fatalf("initial invocation was not safely replayable: result=%#v err=%v calls=%d", replayedInitial, err, len(merchant.calls))
	}
	parsed, err := service.ParsePaymentRequirement(context.Background(), ParsePaymentRequirementRequest{EpisodeID: selected.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "s4-parse"})
	if err != nil {
		t.Fatal(err)
	}
	replayedParsed, err := service.ParsePaymentRequirement(context.Background(), ParsePaymentRequirementRequest{EpisodeID: selected.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "s4-parse-replay"})
	if err != nil || !replayedParsed.Replayed || replayedParsed.Quote.QuoteHash != parsed.Quote.QuoteHash {
		t.Fatalf("payment requirement was not safely replayable: result=%#v err=%v", replayedParsed, err)
	}
	quote := parsed.Quote
	did, paymentAdapter, status, entitlement, dependencies := newS2Dependencies(quote)
	service.paymentDeps = dependencies
	reserved, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: parsed.Episode.EpisodeID, Quote: quote, IdempotencyKey: "s4-payment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(context.Background(), reserved.Intent.IntentID, "s4-submit"); err != nil {
		t.Fatal(err)
	}
	status.outcomes = []payment.PaymentOutcome{{Status: payment.OutcomeConfirmed, TxID: "s4-tx", TxHash: "s4-hash", AmountMinor: quote.AmountMinor, Currency: quote.Currency}}
	if _, err := service.ReconcilePayment(context.Background(), reserved.Intent.IntentID, "s4-reconcile"); err != nil {
		t.Fatal(err)
	}
	entitlement.result.Status = adapters.EntitlementValid
	claimed, err := service.VerifyPaymentEntitlement(context.Background(), reserved.Intent.IntentID, "s4-entitlement")
	if err != nil {
		t.Fatal(err)
	}
	firstDelivery, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: claimed.Episode.EpisodeID, TraceID: "s4-delivery-1"})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := service.ValidateDelivery(context.Background(), ValidateDeliveryRequest{EpisodeID: claimed.Episode.EpisodeID, DeliveryID: firstDelivery.Artifact.DeliveryID, TraceID: "s4-validate-1"})
	if err != nil || invalid.Valid || invalid.Episode.State != episode.StateRecovering {
		t.Fatalf("invalid delivery did not enter recovery: %#v err=%v", invalid, err)
	}
	replayedDelivery, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: invalid.Episode.EpisodeID, Attempt: 1, TraceID: "s4-delivery-1-replay"})
	if err != nil || !replayedDelivery.Replayed || len(merchant.calls) != 2 {
		t.Fatalf("delivery invocation was not safely replayable: result=%#v err=%v calls=%d", replayedDelivery, err, len(merchant.calls))
	}
	retryProposal := decisionProposalForRetry(invalid.Episode, now)
	retried, err := service.RetrySameMerchant(context.Background(), RetrySameMerchantRequest{EpisodeID: invalid.Episode.EpisodeID, Proposal: retryProposal, Action: trace.Action{Type: trace.ActionRetrySameMerchant, IdempotencyKey: "s4-retry"}, Observation: trace.Observation{Type: trace.ObservationDeliveryInvalid}, TraceID: "s4-retry"})
	if err != nil {
		t.Fatal(err)
	}
	secondDelivery, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: retried.Episode.EpisodeID, TraceID: "s4-delivery-2"})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.ValidateDelivery(context.Background(), ValidateDeliveryRequest{EpisodeID: retried.Episode.EpisodeID, DeliveryID: secondDelivery.Artifact.DeliveryID, TraceID: "s4-validate-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !valid.Valid || valid.Episode.State != episode.StateFulfilled || valid.Episode.PaymentAttemptCount != 1 || valid.Episode.DeliveryAttemptCount != 2 || valid.Episode.RetryCount != 1 {
		t.Fatalf("canonical S4 acceptance failed: %#v", valid.Episode)
	}
	intent, err := store.FindPaymentIntentByQuoteHash(context.Background(), valid.Episode.EpisodeID, quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListLedgerEntries(context.Background(), valid.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	settlements := 0
	for _, entry := range entries {
		if entry.Type == ledger.EntryPaymentSettled {
			settlements++
		}
	}
	if intent.IntentID != reserved.Intent.IntentID || paymentAdapter.callCount() != 1 || settlements != 1 || len(merchant.calls) != 3 || did.callCount() != 1 {
		t.Fatalf("no-second-payment invariant failed: intent=%s payment_calls=%d settlements=%d merchant_calls=%d did_calls=%d", intent.IntentID, paymentAdapter.callCount(), settlements, len(merchant.calls), did.callCount())
	}
}

func decisionProposalForS4(episodeID string, sequence uint64, setID string, candidate catalog.Candidate, now time.Time) decision.DecisionProposal {
	return decision.DecisionProposal{ProposalID: "select-s4-proposal", EpisodeID: episodeID, BasedOnEventSequence: sequence, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: setID, Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
}
func decisionProposalForRetry(current *episode.CommerceEpisode, now time.Time) decision.DecisionProposal {
	return decision.DecisionProposal{ProposalID: "retry-s4-proposal", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionRetrySameMerchant, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
}
