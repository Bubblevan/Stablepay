// Command business-e2e runs the first reproducible StablePay business E2E.
// It uses the production MySQL repository, HTTP merchant adapter, settlement
// policy and LLM/runtime boundaries. Payment and entitlement are deterministic
// test-plane adapters; no payment credential or private key is accepted here.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	mysqlrepo "github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type countingLLMClient struct {
	inner llm.LLMClient
	calls atomic.Int64
}

func (c *countingLLMClient) GenerateDecision(ctx context.Context, request llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	c.calls.Add(1)
	return c.inner.GenerateDecision(ctx, request)
}

type fakeDID struct{}

func (fakeDID) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	intent := request.Intent
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: "business-e2e-auth:" + intent.IntentID}, nil
}

type fakePayment struct{ submits atomic.Int64 }

func (p *fakePayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	p.submits.Add(1)
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "business-e2e-tx:" + request.Intent.IntentID, TxHash: "business-e2e-hash:" + request.Intent.IntentID, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type fakePaymentStatus struct{}

func (fakePaymentStatus) Query(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return payment.PaymentOutcome{Status: payment.OutcomeUnknown}, nil
}

type fakeEntitlement struct{}

func (fakeEntitlement) Verify(_ context.Context, request adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: request.IntentID, TxID: request.TxID, Reference: "business-e2e-entitlement:" + request.IntentID, EvidenceRef: "business-e2e-entitlement:" + request.IntentID}, nil
}

// controlledMerchant keeps the network call real but injects one explicit,
// local test failure into the first delivery response. The production merchant
// server itself remains unmodified and receives the real HTTP requests.
type controlledMerchant struct {
	inner             adapters.MerchantAdapter
	invalidFirst      bool
	deliveryCallCount int
}

func (m *controlledMerchant) Invoke(ctx context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	result, err := m.inner.Invoke(ctx, request)
	if err != nil || request.Phase != invocation.PhaseDelivery {
		return result, err
	}
	m.deliveryCallCount++
	if m.invalidFirst && m.deliveryCallCount == 1 {
		result.Body = []byte{}
		result.ContentType = "text/plain"
		result.PayloadHash = invocation.PayloadHash(result.Body)
		result.PayloadRef = "merchant-response://" + strings.TrimPrefix(result.PayloadHash, "sha256:")
	}
	return result, nil
}

type businessReport struct {
	EpisodeID        string            `json:"episode_id"`
	LLM              businessLLMReport `json:"llm"`
	Merchants        []string          `json:"merchants"`
	PaymentIntents   int               `json:"payment_intents"`
	Settlements      int               `json:"settlements"`
	TxIDs            []string          `json:"tx_ids"`
	DeliveryAttempts int               `json:"delivery_attempts"`
	TerminalState    episode.State     `json:"terminal_state"`
	StateTransitions []stateTransition `json:"state_transitions"`
	GuardAccepted    bool              `json:"guard_accepted"`
	FinancialFacts   string            `json:"financial_facts"`
}

type businessLLMReport struct {
	Provider      string           `json:"provider"`
	Model         string           `json:"model"`
	Calls         int64            `json:"calls"`
	Action        trace.ActionType `json:"action"`
	TraceID       string           `json:"trace_id"`
	ContextHash   string           `json:"context_hash"`
	GuardVerdict  bool             `json:"guard_verdict"`
	EvidenceCount int              `json:"evidence_count"`
}

type stateTransition struct {
	Sequence uint64           `json:"sequence"`
	Before   episode.State    `json:"before"`
	After    episode.State    `json:"after"`
	Action   trace.ActionType `json:"action"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "business-e2e failed: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	providerName, baseURL, apiKey, model := os.Getenv("LLM_PROVIDER"), os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL")
	if providerName == "" || baseURL == "" || model == "" {
		return errors.New("LLM_PROVIDER, LLM_BASE_URL, and LLM_MODEL are required")
	}
	network := firstNonEmpty(os.Getenv("COMMERCE_RUNTIME_SETTLEMENT_NETWORK"), os.Getenv("SOLANA_NETWORK"))
	usdcMint := firstNonEmpty(os.Getenv("COMMERCE_RUNTIME_USDC_MINT"), os.Getenv("USDC_MINT"))
	if network == "" || usdcMint == "" {
		return errors.New("SOLANA_NETWORK and USDC_MINT are required; production settlement policy is fail-closed")
	}
	merchantEndpoint := firstNonEmpty(os.Getenv("MERCHANT_E2E_ENDPOINT"), "http://127.0.0.1:8787/api/v1/products/ai-agent-job-2025/execute")
	runID := shortID()
	merchantDID := firstNonEmpty(os.Getenv("MERCHANT_E2E_MERCHANT_DID"), "did:merchant:actual-a:"+runID)
	merchantBDID := firstNonEmpty(os.Getenv("MERCHANT_E2E_MERCHANT_B_DID"), "did:merchant:actual-b:"+runID)
	payee := firstNonEmpty(os.Getenv("MERCHANT_SELLER_ADDRESS"), "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR")

	db, err := mysqlrepo.Open(dsn)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	if err := mysqlrepo.AutoMigrate(ctx, db); err != nil {
		return fmt.Errorf("mysql migrate: %w", err)
	}
	store := mysqlrepo.NewStore(db)

	llmClient, err := llm.NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		return fmt.Errorf("configure llm: %w", err)
	}
	countingClient := &countingLLMClient{inner: llmClient}
	now := time.Now().UTC()
	service := application.NewService(store,
		application.WithSettlementPolicy(application.SettlementPolicy{Network: network, Assets: map[string]string{"USDC": usdcMint}}),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: fakeDID{}, Payment: &fakePayment{}, Status: fakePaymentStatus{}, Entitlement: fakeEntitlement{}}),
		application.WithMerchantAdapter(&controlledMerchant{inner: adapters.NewHTTPMerchantAdapter(nil), invalidFirst: true}),
		application.WithLLMDecisionProvider(llm.NewLLMDecisionProvider(countingClient, llm.WithProviderName(providerName), llm.WithModelRef(model), llm.WithProviderTTL(2*time.Minute))),
		application.WithPersistentEvidenceStore(store),
	)
	if err := seedEvidence(ctx, store, now); err != nil {
		return fmt.Errorf("seed evidence: %w", err)
	}
	if err := seedCapability(ctx, service, merchantDID, payee, merchantEndpoint, now, "merchant-a-"+runID); err != nil {
		return err
	}
	if err := seedCapability(ctx, service, merchantBDID, payee, merchantEndpoint, now, "merchant-b-"+runID); err != nil {
		return err
	}

	requestID := "business-e2e-" + runID
	candidateSetID := "business-e2e-candidates-" + runID
	request := contract.AcquireCapabilityRequest{RequestID: requestID, ParentSessionID: "business-e2e-session", RequesterDID: "did:stablepay:business-e2e", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "ai-agent-job-2025", Description: "acquire the actual merchant AI agent job report"}, Input: contract.Input{URI: "object://business-e2e/input.json", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(7 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 12, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3, AllowCrossMerchantSwitch: true}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	discovered, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: candidateSetID})
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	a, ok := discovered.CandidateSet.FindCandidate(merchantDID, "ai-agent-job-2025")
	if !ok {
		return errors.New("merchant A was not discovered")
	}
	persistedCandidates, err := store.GetCandidateSet(ctx, discovered.CandidateSet.CandidateSetID)
	if err != nil {
		return fmt.Errorf("reload candidate set: %w", err)
	}
	if err := persistedCandidates.ValidateAt(time.Now().UTC()); err != nil {
		return fmt.Errorf("persisted candidate set invalid before selection: %w", err)
	}
	selectKey := "business-e2e-select-a:" + runID
	selected, err := service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{Proposal: applicationProposal(created.Episode.EpisodeID, discovered.CandidateSet.CandidateSetID, discovered.Episode.Version-1, trace.ActionSelectMerchant, discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash, a, now), Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: selectKey}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "business-e2e"})
	if err != nil {
		return fmt.Errorf("select merchant A: %w", err)
	}
	initial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "business-e2e-initial"})
	if err != nil || initial.Response.HTTPStatus != 402 {
		return fmt.Errorf("merchant A expected HTTP 402, response=%#v err=%v", initial.Response, err)
	}
	parsed, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: created.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "business-e2e-parse-a"})
	if err != nil {
		return fmt.Errorf("parse x402: %w", err)
	}
	firstIntent, err := settle(ctx, service, created.Episode.EpisodeID, parsed.Quote, "business-e2e-payment-a:"+runID)
	if err != nil {
		return err
	}
	firstDelivery, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: created.Episode.EpisodeID, TraceID: "business-e2e-delivery-a", PaymentSignature: "business-e2e-payment-signature-a"})
	if err != nil {
		return fmt.Errorf("first paid delivery: %w", err)
	}
	if firstDelivery.Artifact == nil {
		return errors.New("first delivery artifact was not persisted")
	}
	invalid, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: created.Episode.EpisodeID, DeliveryID: firstDelivery.Artifact.DeliveryID, TraceID: "business-e2e-validate-a"})
	if err != nil || invalid.Valid {
		return fmt.Errorf("expected injected invalid delivery, result=%#v err=%v", invalid, err)
	}
	beforeLLMIntents, err := store.ListPaymentIntents(ctx, created.Episode.EpisodeID)
	if err != nil {
		return fmt.Errorf("list pre-llm intents: %w", err)
	}
	commit, decisionResult, err := service.ExecuteRecoveryDecision(ctx, application.S6DecisionRequest{EpisodeID: created.Episode.EpisodeID, Query: "merchant A delivery failed; choose the eligible unattempted merchant B or retry the controlled delivery"})
	if err != nil {
		return fmt.Errorf("real LLM recovery: %w", err)
	}
	action := decisionResult.Proposal.ProposedAction
	if action != trace.ActionRetrySameMerchant && action != trace.ActionSwitchMerchant {
		return fmt.Errorf("real LLM selected non-progressing action %s", action)
	}
	afterLLMIntents, err := store.ListPaymentIntents(ctx, created.Episode.EpisodeID)
	if err != nil {
		return fmt.Errorf("list post-llm intents: %w", err)
	}
	if len(afterLLMIntents) != len(beforeLLMIntents) {
		return errors.New("LLM decision created a payment intent directly")
	}
	if action == trace.ActionSwitchMerchant {
		if err := completeMerchantB(ctx, service, created.Episode.EpisodeID, merchantBDID, runID); err != nil {
			return err
		}
	} else if _, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: created.Episode.EpisodeID, Attempt: 2, TraceID: "business-e2e-delivery-retry", PaymentSignature: "business-e2e-payment-signature-a"}); err != nil {
		return fmt.Errorf("retry delivery: %w", err)
	}
	current, err := service.GetEpisode(ctx, created.Episode.EpisodeID)
	if err != nil {
		return err
	}
	if len(current.DeliveryRefs) == 0 {
		return errors.New("valid delivery artifact was not persisted")
	}
	valid, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: current.EpisodeID, DeliveryID: current.DeliveryRefs[len(current.DeliveryRefs)-1], TraceID: "business-e2e-validate-final"})
	if err != nil || !valid.Valid || valid.Episode.State != episode.StateFulfilled {
		return fmt.Errorf("final delivery validation failed: result=%#v err=%v", valid, err)
	}
	report, err := buildReport(ctx, store, current.EpisodeID, decisionResult, commit, countingClient.calls.Load(), []string{merchantDID, merchantBDID}, firstIntent.IntentID)
	if err != nil {
		return err
	}
	path := firstNonEmpty(os.Getenv("BUSINESS_E2E_REPORT_PATH"), filepath.Join(".local-run", "business-e2e-summary.json"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("business-e2e passed: report=%s\n%s\n", path, payload)
	return nil
}

func settle(ctx context.Context, service *application.Service, episodeID string, quote application.TrustedPaymentQuote, key string) (*payment.PaymentIntent, error) {
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID, Quote: quote, IdempotencyKey: key, TraceID: key + ":reserve"})
	if err != nil {
		return nil, fmt.Errorf("reserve %s: %w", key, err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(ctx, reserved.Intent.IntentID, key+":submit"); err != nil {
		return nil, fmt.Errorf("settle %s: %w", key, err)
	}
	if _, err := service.VerifyPaymentEntitlement(ctx, reserved.Intent.IntentID, key+":entitlement"); err != nil {
		return nil, fmt.Errorf("entitlement %s: %w", key, err)
	}
	return reserved.Intent, nil
}

func completeMerchantB(ctx context.Context, service *application.Service, episodeID, merchantDID, runID string) error {
	initial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: episodeID, TraceID: "business-e2e-initial-b"})
	if err != nil || initial.Response.HTTPStatus != 402 {
		return fmt.Errorf("merchant B expected HTTP 402, response=%#v err=%v", initial.Response, err)
	}
	parsed, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: episodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "business-e2e-parse-b"})
	if err != nil {
		return fmt.Errorf("parse merchant B x402: %w", err)
	}
	if parsed.Quote.MerchantDID != merchantDID {
		return fmt.Errorf("merchant B quote bound to %s, want %s", parsed.Quote.MerchantDID, merchantDID)
	}
	if _, err := settle(ctx, service, episodeID, parsed.Quote, "business-e2e-payment-b:"+runID); err != nil {
		return err
	}
	current, err := service.GetEpisode(ctx, episodeID)
	if err != nil {
		return err
	}
	delivery, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: episodeID, Attempt: current.DeliveryAttemptCount + 1, TraceID: "business-e2e-delivery-b", PaymentSignature: "business-e2e-payment-signature-b"})
	if err != nil {
		return fmt.Errorf("merchant B delivery: %w", err)
	}
	if delivery.Response.HTTPStatus != 200 {
		return fmt.Errorf("merchant B delivery status=%d", delivery.Response.HTTPStatus)
	}
	return nil
}

func applicationProposal(episodeID, candidateSetID string, sequence uint64, action trace.ActionType, factsRef, payloadHash string, candidate catalog.Candidate, now time.Time) decision.DecisionProposal {
	return decision.DecisionProposal{ProposalID: "business-e2e-select-a", EpisodeID: episodeID, BasedOnEventSequence: sequence, ProposedAction: action, CandidateSetID: candidateSetID, Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}, EvidenceRefs: []string{factsRef, payloadHash}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
}

func seedCapability(ctx context.Context, service *application.Service, merchantDID, payee, endpoint string, now time.Time, source string) error {
	price := int64(200)
	payeeDID := payee
	if !strings.HasPrefix(strings.ToLower(payeeDID), "did:") {
		payeeDID = "did:solana:" + payeeDID
	}
	value := &catalog.MerchantCapability{MerchantDID: merchantDID, CapabilityID: "ai-agent-job-2025", PayeeDID: payeeDID, Name: "AI agent job 2025", Description: "Actual Merchant Server paid delivery capability.", TaskTypes: []string{"ai-agent-job-2025"}, SemanticTags: []string{"business-e2e"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: endpoint, Method: "GET"}, InputSchemaRef: "schema:business-e2e-input", OutputSchemaRef: "schema:merchant-delivery", InputContentTypes: []string{"application/json"}, OutputContentTypes: []string{"application/json"}, SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1-" + source, Source: "business-e2e", SourceRef: "merchant://" + source, SourceHash: evidence.HashString("business-e2e:" + source), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(6 * time.Minute), CreatedAt: now, UpdatedAt: now}
	return service.RegisterCapabilityVersion(ctx, value)
}

func seedEvidence(ctx context.Context, store *mysqlrepo.Store, now time.Time) error {
	fixtures := []struct {
		id, sourceType, sourceRef, content string
		trust                              evidence.TrustClass
	}{
		{"x402-protocol-v2", string(evidence.SourceProtocolDoc), "fixture://x402/protocol-v2", "x402 exact scheme binds network, asset, amount, payTo and timeout; runtime owns payment confirmation.", evidence.TrustProtocolDoc},
		{"stablepay-runtime-payment", string(evidence.SourceProtocolDoc), "fixture://stablepay/payment-runtime", "StablePay reserves budget, submits payment and verifies entitlement before merchant delivery.", evidence.TrustProtocolDoc},
		{"merchant-a-business-e2e", string(evidence.SourceMerchantCapabilityDoc), "fixture://merchant/a/business-e2e", "Merchant A delivery is controlled by the E2E harness and may require recovery after invalid delivery.", evidence.TrustMerchantDoc},
		{"merchant-b-business-e2e", string(evidence.SourceMerchantCapabilityDoc), "fixture://merchant/b/business-e2e", "Merchant B is the eligible unattempted recovery candidate for the same capability.", evidence.TrustMerchantDoc},
	}
	for _, fixture := range fixtures {
		record := evidence.NewRecord(fixture.id, fixture.sourceType, fixture.sourceRef, "v1", evidence.HashString(fixture.content), "text/plain", fixture.content, fixture.trust, now)
		if err := store.SaveEvidenceRecord(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func buildReport(ctx context.Context, store *mysqlrepo.Store, episodeID string, decisionResult application.S6DecisionResult, commit application.CommitResult, calls int64, merchants []string, firstIntentID string) (businessReport, error) {
	current, err := store.Get(ctx, episodeID)
	if err != nil {
		return businessReport{}, err
	}
	intents, err := store.ListPaymentIntents(ctx, episodeID)
	if err != nil {
		return businessReport{}, err
	}
	entries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		return businessReport{}, err
	}
	events, err := store.ListByEpisode(ctx, episodeID)
	if err != nil {
		return businessReport{}, err
	}
	txIDs := make([]string, 0)
	settlements := 0
	for _, entry := range entries {
		if entry.Type == ledger.EntryPaymentSettled {
			settlements++
			if entry.TxID != "" {
				txIDs = append(txIDs, entry.TxID)
			}
		}
	}
	if len(txIDs) == 0 && firstIntentID != "" {
		txIDs = append(txIDs, "business-e2e-tx:"+firstIntentID)
	}
	transitions := make([]stateTransition, 0, len(events))
	for _, event := range events {
		transitions = append(transitions, stateTransition{Sequence: event.Sequence, Before: event.StateBefore, After: event.StateAfter, Action: event.Action.Type})
	}
	return businessReport{EpisodeID: episodeID, LLM: businessLLMReport{Provider: decisionResult.Trace.Provider, Model: decisionResult.Proposal.ModelRef, Calls: calls, Action: decisionResult.Proposal.ProposedAction, TraceID: decisionResult.Trace.TraceID, ContextHash: decisionResult.Trace.ContextHash, GuardVerdict: commit.Event != nil && commit.Event.RuntimeVerdict.Allowed, EvidenceCount: len(decisionResult.Proposal.EvidenceRefs)}, Merchants: merchants, PaymentIntents: len(intents), Settlements: settlements, TxIDs: txIDs, DeliveryAttempts: current.DeliveryAttemptCount, TerminalState: current.State, StateTransitions: transitions, GuardAccepted: commit.Event != nil && commit.Event.RuntimeVerdict.Allowed, FinancialFacts: "runtime-owned"}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shortID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
