package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type s5Merchant struct {
	mu    sync.Mutex
	calls []adapters.MerchantInvokeRequest
}

func (m *s5Merchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, request)
	m.mu.Unlock()
	if request.Phase == invocation.PhaseInitial {
		minor := "3000000"
		if request.MerchantDID == "did:merchant:b" {
			minor = "4000000"
		}
		body, _ := json.Marshal(map[string]any{"x402Version": 2, "resource": map[string]any{"url": request.Endpoint.Endpoint}, "accepts": []any{map[string]any{"scheme": "exact", "network": "devnet", "amount": minor, "asset": "USDC", "payTo": "did:payee:s5", "maxTimeoutSeconds": 300, "extra": map[string]any{"currency": "USDC", "productId": "transcription-1"}}}})
		return adapters.MerchantInvokeResult{HTTPStatus: 402, Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(body)}, Body: body, ContentType: "application/json", OccurredAt: time.Now().UTC()}, nil
	}
	if request.MerchantDID == "did:merchant:b" {
		return adapters.MerchantInvokeResult{HTTPStatus: 200, Body: []byte("transcript from merchant b"), ContentType: "text/plain", OccurredAt: time.Now().UTC()}, nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: 200, ContentType: "text/plain", OccurredAt: time.Now().UTC()}, nil
}

type s5DID struct{}

func (s5DID) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	i := request.Intent
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: i.RequesterDID, MerchantDID: i.MerchantDID, CapabilityID: i.CapabilityID, PayeeDID: i.PayeeDID, QuoteHash: i.QuoteHash, AmountMinor: i.AmountMinor, Currency: i.Currency, AuthorizationRef: "auth:" + i.IntentID}, nil
}

type s5Payment struct{}

func (s5Payment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	i := request.Intent
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx:" + i.IntentID, TxHash: "hash:" + i.IntentID, AmountMinor: i.AmountMinor, Currency: i.Currency}, nil
}

type s5Status struct{}

func (s5Status) Query(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return payment.PaymentOutcome{Status: payment.OutcomeUnknown}, nil
}

type s5Entitlement struct{}

func (s5Entitlement) Verify(_ context.Context, request adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: request.IntentID, TxID: request.TxID, Reference: "entitlement:" + request.IntentID, EvidenceRef: "evidence:" + request.IntentID}, nil
}

func s5Dependencies() PaymentDependencies {
	return PaymentDependencies{DID: s5DID{}, Payment: s5Payment{}, Status: s5Status{}, Entitlement: s5Entitlement{}}
}

func s5BudgetRecoveryFixture(t *testing.T, requestID string) (*Service, *repository.InMemoryStore, time.Time, *episode.CommerceEpisode) {
	t.Helper()
	service, store, now := serviceFixture()
	request := requestFixture(now)
	request.RequestID = requestID
	request.Constraints.SupportedProtocolVersions = []string{"x402-v2"}
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	current := created.Episode.Clone()
	for _, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{{trace.ActionDiscover, trace.ObservationCandidatesFound, requestID + ":discover"}, {trace.ActionInvoke, trace.ObservationCandidatesFound, requestID + ":invoke"}, {trace.ActionParse402, trace.ObservationHTTP402, requestID + ":parse"}} {
		result, stepErr := service.CommitRuntimeAction(context.Background(), RuntimeActionRequest{EpisodeID: current.EpisodeID, Action: trace.Action{Type: step.action, IdempotencyKey: step.key}, Observation: trace.Observation{Type: step.observation}})
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		current = result.Episode
	}
	if err := store.AppendLedgerEntry(context.Background(), &ledger.LedgerEntry{EntryID: requestID + ":settled", EpisodeID: current.EpisodeID, Sequence: 1, Type: ledger.EntryPaymentSettled, Currency: "USDC", AmountMinor: 700, PaymentIntentID: requestID + ":intent", TxID: requestID + ":tx", IdempotencyKey: requestID + ":settled", OccurredAt: now, ReferenceHash: ledger.HashReference(requestID + ":intent")}); err != nil {
		t.Fatal(err)
	}
	current.AttemptedMerchants = []string{"did:merchant:a"}
	current.Budget.SettledAmount = 700
	current.Budget.ConsumedAmount = 700
	current.Budget.AvailableBudget = 300
	current.Budget.SunkCost = 700
	current.SelectedMerchantDID = "did:merchant:b"
	current.SelectedCapabilityID = "transcription"
	next := current.Clone()
	next.Version++
	event, err := episode.NewEvent(requestID+":projection", current.EpisodeID, current.Version, now, current.State, trace.Action{Type: trace.ActionReserveBudget, IdempotencyKey: requestID + ":projection"}, trace.Observation{Type: trace.ObservationQuoteValid}, trace.Decision{ProposedAction: trace.ActionReserveBudget, ProposalID: requestID + ":projection"}, trace.RuntimeVerdict{Allowed: true}, current.State, "runtime", requestID+":projection", DefaultRuntimeVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitTransition(context.Background(), current.EpisodeID, current.Version, next, event); err != nil {
		t.Fatal(err)
	}
	return service, store, now, next
}

func s5Capability(merchant string, now time.Time) *catalog.MerchantCapability {
	price := int64(300)
	if merchant == "did:merchant:b" {
		price = 400
	}
	endpoint := "http://merchant-a.test/execute"
	if merchant == "did:merchant:b" {
		endpoint = "http://merchant-b.test/execute"
	}
	return &catalog.MerchantCapability{MerchantDID: merchant, CapabilityID: "transcription", PayeeDID: "did:payee:s5", Name: "transcription", Description: "s5", TaskTypes: []string{"transcription"}, SemanticTags: []string{"language=en"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: endpoint, Method: "GET"}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output", InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "s5-test", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
}

func TestS5CanonicalCrossMerchantRecoveryHasTwoEconomicAttempts(t *testing.T) {
	service, store, now := serviceFixture()
	service.merchantAdapter = &s5Merchant{}
	service.paymentDeps = s5Dependencies()
	request := requestFixture(now)
	request.RequestID = "acr-s5-a"
	request.Constraints.SupportedProtocolVersions = []string{"x402-v2"}
	request.Constraints.MaxDeliveryAttempts = 4
	request.Constraints.MaxPaymentAttempts = 3
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, merchant := range []string{"did:merchant:a", "did:merchant:b"} {
		if err := service.RegisterCapabilityVersion(context.Background(), s5Capability(merchant, now)); err != nil {
			t.Fatal(err)
		}
	}
	discovered, err := service.DiscoverCapabilities(context.Background(), DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs-s5-a"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := discovered.CandidateSet.FindCandidate("did:merchant:a", "transcription")
	b, _ := discovered.CandidateSet.FindCandidate("did:merchant:b", "transcription")
	selected, err := service.CommitMerchantSelection(context.Background(), SelectMerchantRequest{Proposal: decision.DecisionProposal{ProposalID: "s5-select-a", EpisodeID: created.Episode.EpisodeID, BasedOnEventSequence: discovered.Episode.Version - 1, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: discovered.CandidateSet.CandidateSetID, Target: &decision.ProposalTarget{MerchantDID: a.MerchantDID, CapabilityID: a.CapabilityID, CatalogVersion: a.CatalogVersion, CatalogSnapshotHash: a.CatalogSnapshotHash, CatalogSnapshotRef: a.CatalogSnapshotRef}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "s5-select-a"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.InvokeSelectedMerchant(context.Background(), InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := service.ParsePaymentRequirement(context.Background(), ParsePaymentRequirementRequest{EpisodeID: created.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID})
	if err != nil {
		t.Fatal(err)
	}
	reservedA, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: created.Episode.EpisodeID, Quote: parsed.Quote, IdempotencyKey: "s5-pay-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AuthorizeAndSubmitPayment(context.Background(), reservedA.Intent.IntentID, "s5-a-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.VerifyPaymentEntitlement(context.Background(), reservedA.Intent.IntentID, "s5-a-entitlement"); err != nil {
		t.Fatal(err)
	}
	first, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := service.ValidateDelivery(context.Background(), ValidateDeliveryRequest{EpisodeID: created.Episode.EpisodeID, DeliveryID: first.Artifact.DeliveryID})
	if err != nil || invalid.Valid {
		t.Fatalf("first invalid delivery: %#v %v", invalid, err)
	}
	retry, err := service.RetrySameMerchant(context.Background(), RetrySameMerchantRequest{EpisodeID: created.Episode.EpisodeID, Proposal: decision.DecisionProposal{ProposalID: "s5-retry-a", EpisodeID: created.Episode.EpisodeID, BasedOnEventSequence: invalid.Episode.Version - 1, ProposedAction: trace.ActionRetrySameMerchant, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, Action: trace.Action{Type: trace.ActionRetrySameMerchant, IdempotencyKey: "s5-retry-a"}, Observation: trace.Observation{Type: trace.ObservationDeliveryInvalid}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: retry.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err = service.ValidateDelivery(context.Background(), ValidateDeliveryRequest{EpisodeID: created.Episode.EpisodeID, DeliveryID: second.Artifact.DeliveryID})
	if err != nil || invalid.Valid {
		t.Fatalf("second invalid delivery: %#v %v", invalid, err)
	}
	switched, err := service.SwitchMerchant(context.Background(), SwitchMerchantRequest{EpisodeID: created.Episode.EpisodeID, Proposal: decision.DecisionProposal{ProposalID: "s5-switch-b", EpisodeID: created.Episode.EpisodeID, BasedOnEventSequence: invalid.Episode.Version - 1, ProposedAction: trace.ActionSwitchMerchant, CandidateSetID: discovered.CandidateSet.CandidateSetID, Target: &decision.ProposalTarget{MerchantDID: b.MerchantDID, CapabilityID: b.CapabilityID, CatalogVersion: b.CatalogVersion, CatalogSnapshotHash: b.CatalogSnapshotHash, CatalogSnapshotRef: b.CatalogSnapshotRef}, EvidenceRefs: []string{discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, Action: trace.Action{Type: trace.ActionSwitchMerchant, IdempotencyKey: "s5-switch-b"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}})
	if err != nil {
		t.Fatal(err)
	}
	if switched.Episode.State != episode.StateInvokingDelivery {
		t.Fatalf("switch state=%s", switched.Episode.State)
	}
	initialB, err := service.InvokeSelectedMerchant(context.Background(), InvokeSelectedMerchantRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	parsedB, err := service.ParsePaymentRequirement(context.Background(), ParsePaymentRequirementRequest{EpisodeID: created.Episode.EpisodeID, InvocationID: initialB.Invocation.InvocationID})
	if err != nil {
		t.Fatal(err)
	}
	if parsedB.Quote.AmountMinor != 400 {
		t.Fatalf("B quote=%d", parsedB.Quote.AmountMinor)
	}
	reservedB, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: created.Episode.EpisodeID, Quote: parsedB.Quote, IdempotencyKey: "s5-pay-b"})
	if err != nil {
		t.Fatal(err)
	}
	if reservedB.Intent.IntentID == reservedA.Intent.IntentID {
		t.Fatal("B reused A payment intent")
	}
	if _, err = service.AuthorizeAndSubmitPayment(context.Background(), reservedB.Intent.IntentID, "s5-b-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.VerifyPaymentEntitlement(context.Background(), reservedB.Intent.IntentID, "s5-b-entitlement"); err != nil {
		t.Fatal(err)
	}
	deliveryB, err := service.InvokeDelivery(context.Background(), InvokeDeliveryRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.ValidateDelivery(context.Background(), ValidateDeliveryRequest{EpisodeID: created.Episode.EpisodeID, DeliveryID: deliveryB.Artifact.DeliveryID})
	if err != nil || !valid.Valid || valid.Episode.State != episode.StateFulfilled {
		t.Fatalf("B did not fulfill: %#v %v", valid, err)
	}
	if valid.Episode.PaymentAttemptCount != 2 || valid.Episode.DeliveryAttemptCount != 3 || len(valid.Episode.AttemptedMerchants) != 2 || valid.Episode.AttemptedMerchants[0] != "did:merchant:a" || valid.Episode.AttemptedMerchants[1] != "did:merchant:b" {
		t.Fatalf("S5 A invariants: %#v", valid.Episode)
	}
	intents, err := store.ListPaymentIntents(context.Background(), created.Episode.EpisodeID)
	if err != nil || len(intents) != 2 {
		t.Fatalf("payment intents=%#v err=%v", intents, err)
	}
	entries, err := store.ListLedgerEntries(context.Background(), created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	settled := 0
	for _, e := range entries {
		if e.Type == ledger.EntryPaymentSettled {
			settled++
		}
	}
	if settled != 2 {
		t.Fatalf("settlements=%d entries=%#v", settled, entries)
	}
}

func TestS5BudgetInsufficientPersistsRecoveryAndNoSecondReservation(t *testing.T) {
	service, store, now := serviceFixture()
	request := requestFixture(now)
	request.RequestID = "acr-s5-budget"
	request.Constraints.BudgetLimitMinor = 1000
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	current := created.Episode.Clone()
	for _, step := range []struct {
		action      trace.ActionType
		observation trace.ObservationType
		key         string
	}{{trace.ActionDiscover, trace.ObservationCandidatesFound, "s5-budget-discover"}, {trace.ActionInvoke, trace.ObservationCandidatesFound, "s5-budget-invoke"}, {trace.ActionParse402, trace.ObservationHTTP402, "s5-budget-parse"}} {
		result, stepErr := service.CommitRuntimeAction(context.Background(), RuntimeActionRequest{EpisodeID: current.EpisodeID, Action: trace.Action{Type: step.action, IdempotencyKey: step.key}, Observation: trace.Observation{Type: step.observation}})
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		current = result.Episode
	}
	current.AttemptedMerchants = []string{"did:merchant:a"}
	current.Budget.ReservedAmount = 0
	current.Budget.SettledAmount = 700
	current.Budget.ConsumedAmount = 700
	current.Budget.AvailableBudget = 300
	current.Budget.SunkCost = 700
	current.CurrentQuoteHash = "sha256:quote-b"
	current.SelectedMerchantDID = "did:merchant:b"
	current.SelectedCapabilityID = "transcription"
	if err := store.AppendLedgerEntry(context.Background(), &ledger.LedgerEntry{EntryID: "s5-settled-a", EpisodeID: current.EpisodeID, Sequence: 1, Type: ledger.EntryPaymentSettled, Currency: "USDC", AmountMinor: 700, PaymentIntentID: "s5-existing-intent", TxID: "s5-existing-tx", IdempotencyKey: "s5-existing-settled", OccurredAt: now, ReferenceHash: ledger.HashReference("s5-existing-intent")}); err != nil {
		t.Fatal(err)
	}
	next := current.Clone()
	next.Version = current.Version + 1
	event, eventErr := episode.NewEvent("s5-budget-projection", current.EpisodeID, current.Version, now, current.State, trace.Action{Type: trace.ActionReserveBudget, IdempotencyKey: "s5-budget-projection"}, trace.Observation{Type: trace.ObservationQuoteValid}, trace.Decision{ProposedAction: trace.ActionReserveBudget, ProposalID: "s5-budget-projection"}, trace.RuntimeVerdict{Allowed: true}, current.State, "runtime", "s5-budget-projection", DefaultRuntimeVersion)
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	if err := store.CommitTransition(context.Background(), current.EpisodeID, current.Version, next, event); err != nil {
		t.Fatal(err)
	}
	current = next
	quote := TrustedPaymentQuote{MerchantDID: "did:merchant:b", CapabilityID: "transcription", PayeeDID: "did:payee:s5", QuoteHash: "sha256:quote-b", AmountMinor: 500, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(time.Minute)}
	_, err = service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "s5-budget-b"})
	if !errors.Is(err, ledger.ErrInsufficientBudget) {
		t.Fatalf("expected budget insufficient, got %v", err)
	}
	latest, _ := service.GetEpisode(context.Background(), current.EpisodeID)
	if latest.State != episode.StateRecovering {
		t.Fatalf("state=%s", latest.State)
	}
	rc, err := store.GetRecoveryContextByEpisode(context.Background(), current.EpisodeID)
	if err != nil || rc.ReasonCode != recovery.ReasonBudgetInsufficient || rc.AvailableMinor != 300 {
		t.Fatalf("recovery=%#v err=%v", rc, err)
	}
	intents, _ := store.ListPaymentIntents(context.Background(), current.EpisodeID)
	if len(intents) != 0 {
		t.Fatalf("budget rejection created intent: %#v", intents)
	}
}

func TestS5ParentApprovalAmendmentIdempotencyAndDenial(t *testing.T) {
	service, store, now, current := s5BudgetRecoveryFixture(t, "acr-s5-parent-approve")
	_, err := service.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: current.EpisodeID, Quote: TrustedPaymentQuote{MerchantDID: "did:merchant:b", CapabilityID: "transcription", PayeeDID: "did:payee:s5", QuoteHash: "sha256:parent-quote", AmountMinor: 500, Currency: "USDC", RequesterDID: current.RequesterDID, ExpiresAt: now.Add(time.Minute)}, IdempotencyKey: "parent-budget-reserve"})
	if !errors.Is(err, ledger.ErrInsufficientBudget) {
		t.Fatalf("expected budget gate: %v", err)
	}
	asked, err := service.AskParent(context.Background(), AskParentRequest{EpisodeID: current.EpisodeID, ApprovalID: "approval:s5:increase", ApprovalScope: recovery.BudgetIncrease, RequestedAction: trace.ActionSwitchMerchant, RequestedBudgetIncreaseMinor: 500, Proposal: decision.DecisionProposal{ProposedAction: trace.ActionAskParent, EvidenceRefs: []string{"recovery://" + current.EpisodeID}}})
	if err != nil {
		t.Fatal(err)
	}
	if asked.Episode.State != episode.StateAwaitingParent {
		t.Fatalf("state=%s", asked.Episode.State)
	}
	approved, err := service.RecordParentDecision(context.Background(), ParentDecisionRequest{ApprovalID: "approval:s5:increase", Decision: recovery.Approve, ActorRef: "parent:1"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Episode.State != episode.StateRecovering || approved.Episode.Budget.BudgetLimitMinor != 1500 || approved.Episode.Budget.AvailableBudget != 800 {
		t.Fatalf("approved=%#v", approved.Episode)
	}
	amendment, err := store.GetBudgetAmendment(context.Background(), "amendment:approval:s5:increase")
	if err != nil || amendment.NewLimitMinor != 1500 {
		t.Fatalf("amendment=%#v err=%v", amendment, err)
	}
	replayed, err := service.RecordParentDecision(context.Background(), ParentDecisionRequest{ApprovalID: "approval:s5:increase", Decision: recovery.Approve, ActorRef: "parent:1"})
	if err != nil || !replayed.Replayed {
		t.Fatalf("parent replay=%#v err=%v", replayed, err)
	}
	if _, err = service.RecordParentDecision(context.Background(), ParentDecisionRequest{ApprovalID: "approval:s5:increase", Decision: recovery.Deny, ActorRef: "parent:1"}); !errors.Is(err, repository.ErrParentDecisionConflict) {
		t.Fatalf("expected parent conflict, got %v", err)
	}
	serviceD, _, _, currentD := s5BudgetRecoveryFixture(t, "acr-s5-parent-deny")
	if _, err = serviceD.ReservePaymentIntent(context.Background(), ReservePaymentIntentRequest{EpisodeID: currentD.EpisodeID, Quote: TrustedPaymentQuote{MerchantDID: "did:merchant:b", CapabilityID: "transcription", PayeeDID: "did:payee:s5", QuoteHash: "sha256:deny-quote", AmountMinor: 500, Currency: "USDC", RequesterDID: currentD.RequesterDID, ExpiresAt: now.Add(time.Minute)}, IdempotencyKey: "deny-budget-reserve"}); !errors.Is(err, ledger.ErrInsufficientBudget) {
		t.Fatal(err)
	}
	if _, err = serviceD.AskParent(context.Background(), AskParentRequest{EpisodeID: currentD.EpisodeID, ApprovalID: "approval:s5:deny", ApprovalScope: recovery.AllowSwitch, RequestedAction: trace.ActionSwitchMerchant, Proposal: decision.DecisionProposal{ProposedAction: trace.ActionAskParent, EvidenceRefs: []string{"recovery://" + currentD.EpisodeID}}}); err != nil {
		t.Fatal(err)
	}
	denied, err := serviceD.RecordParentDecision(context.Background(), ParentDecisionRequest{ApprovalID: "approval:s5:deny", Decision: recovery.Deny, ActorRef: "parent:2"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := serviceD.StopRecovery(context.Background(), StopRecoveryRequest{EpisodeID: currentD.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Episode.State != episode.StateFailed || failed.Episode.TerminalReason != string(recovery.ReasonParentDenied) {
		t.Fatalf("denied stop=%#v", failed.Episode)
	}
	_ = denied
}
