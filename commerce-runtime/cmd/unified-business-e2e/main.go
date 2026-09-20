// Command unified-business-e2e runs the L2/U1 happy path against the real
// StablePay payment and verification planes. It intentionally has no fake
// PaymentAdapter, EntitlementAdapter, or cryptographic credential.
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

	kitexclient "github.com/cloudwego/kitex/client"
	kitexpaymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"

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

type unifiedReport struct {
	EpisodeID string `json:"episode_id"`
	LLM       struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Calls    int    `json:"calls"`
	} `json:"llm"`
	Merchant         string `json:"merchant"`
	MerchantMode     string `json:"merchant_mode"`
	PaymentIntentID  string `json:"payment_intent_id"`
	TxID             string `json:"tx_id"`
	TxHash           string `json:"tx_hash"`
	AmountMinor      int64  `json:"amount_minor"`
	Currency         string `json:"currency"`
	PaymentStatus    string `json:"payment_status"`
	Verification     string `json:"verification"`
	DeliveryStatus   int    `json:"delivery_http_status"`
	TerminalState    string `json:"terminal_state"`
	DeliveryAttempts int    `json:"delivery_attempts"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "unified-business-e2e failed: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("UNIFIED_BUSINESS_E2E_MODE")), "u2") {
		return runUnifiedU2(parent)
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Minute)
	defer cancel()
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	network := firstNonEmptyUnified(os.Getenv("COMMERCE_RUNTIME_SETTLEMENT_NETWORK"), os.Getenv("SOLANA_NETWORK"))
	mint := firstNonEmptyUnified(os.Getenv("COMMERCE_RUNTIME_USDC_MINT"), os.Getenv("USDC_MINT"))
	if network == "" || mint == "" {
		return errors.New("production unified E2E requires settlement network and USDC mint")
	}
	merchantEndpoint := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_ENDPOINT"), "http://127.0.0.1:8787/api/v1/products/ai-agent-job-2025/execute")
	gatewayURL := firstNonEmptyUnified(os.Getenv("STABLEPAY_GATEWAY_BASE_URL"), "http://127.0.0.1:8080")
	apiKey := firstNonEmptyUnified(os.Getenv("STABLEPAY_API_KEY"), "stablepay-dev-key")
	runID := shortUnifiedID()
	requesterDID := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_REQUESTER_DID"))
	merchantDID := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_MERCHANT_DID"), "did:merchant:unified-a:"+runID)
	payeeDID := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_PAYEE_DID"), didFromAddress(os.Getenv("MERCHANT_SELLER_ADDRESS")))
	if requesterDID == "" {
		requesterDID = "__from_real_credential_provider__"
	}
	if payeeDID == "" {
		return errors.New("MERCHANT_E2E_PAYEE_DID or MERCHANT_SELLER_ADDRESS is required")
	}

	db, err := mysqlrepo.Open(dsn)
	if err != nil {
		return fmt.Errorf("open commerce MySQL: %w", err)
	}
	if err := mysqlrepo.AutoMigrate(ctx, db); err != nil {
		return fmt.Errorf("migrate commerce MySQL: %w", err)
	}
	store := mysqlrepo.NewStore(db)

	credentials, err := adapters.NewRealCredentialProvider(adapters.RealCredentialProviderConfig{
		AgentKeypairPath: os.Getenv("STABLEPAY_E2E_AGENT_KEYPAIR_PATH"),
		DIDServiceAddr:   os.Getenv("STABLEPAY_DID_SERVICE_ADDR"),
		BlockchainAddr:   os.Getenv("STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR"),
		RPCTimeout:       30 * time.Second,
	})
	if err != nil {
		return err
	}
	requesterDID = credentials.AgentDID()
	if err := credentials.EnsureIdentities(ctx, payeeDID); err != nil {
		return err
	}
	if !strings.HasPrefix(payeeDID, "did:solana:") {
		return fmt.Errorf("real payee must be a Solana DID, got %q", payeeDID)
	}

	paymentClient, err := kitexpaymentservice.NewClient("payment-service", kitexclient.WithHostPorts(firstNonEmptyUnified(os.Getenv("STABLEPAY_PAYMENT_SERVICE_ADDR"), "127.0.0.1:8888")), kitexclient.WithRPCTimeout(90*time.Second))
	if err != nil {
		return fmt.Errorf("create payment-service Kitex client: %w", err)
	}
	kitexPayment := &adapters.KitexPaymentAdapter{Client: paymentClient, Credentials: credentials}
	verification := adapters.NewStablePayVerificationEntitlement(gatewayURL, apiKey)
	service := application.NewService(store,
		application.WithSettlementPolicy(application.SettlementPolicy{Network: network, Assets: map[string]string{"USDC": mint}, AllowUnconstrainedForTests: false}),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: adapters.RealDIDPolicyAdapter{}, Payment: kitexPayment, Status: kitexPayment, Entitlement: verification}),
		application.WithMerchantAdapter(adapters.NewHTTPMerchantAdapter(nil)),
		application.WithPersistentEvidenceStore(store),
		application.WithMemoryStore(store),
	)
	now := time.Now().UTC()
	if err := seedUnifiedEvidence(ctx, store, now); err != nil {
		return fmt.Errorf("seed evidence: %w", err)
	}
	if err := seedUnifiedCapability(ctx, service, merchantDID, payeeDID, merchantEndpoint, now, runID); err != nil {
		return err
	}
	request := contract.AcquireCapabilityRequest{
		RequestID: "unified-business-e2e-" + runID, ParentSessionID: "unified-business-e2e-session", RequesterDID: requesterDID,
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "ai-agent-job-2025", Description: "real StablePay Merchant delivery"},
		Input:           contract.Input{URI: "object://unified-business-e2e/input.json", ContentType: "application/json"},
		Constraints:     contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 6, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2},
		ExpectedOutput:  contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"},
	}
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		return fmt.Errorf("create episode: %w", err)
	}
	discovered, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "unified-candidates-" + runID})
	if err != nil {
		return fmt.Errorf("discover capability: %w", err)
	}
	candidate, ok := discovered.CandidateSet.FindCandidate(merchantDID, "ai-agent-job-2025")
	if !ok {
		return errors.New("unified Merchant capability was not discovered")
	}
	selected, err := service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{
		Proposal: unifiedSelectionProposal(created.Episode.EpisodeID, discovered.CandidateSet.CandidateSetID, discovered.Episode.Version-1, discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash, candidate, now),
		Action:   trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "unified-select-" + runID}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "unified-business-e2e",
	})
	if err != nil {
		return fmt.Errorf("select Merchant: %w", err)
	}
	initial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "unified-initial-" + runID})
	if err != nil || initial.Response.HTTPStatus != 402 {
		return fmt.Errorf("actual Merchant did not return HTTP 402: status=%d err=%v", initial.Response.HTTPStatus, err)
	}
	parsed, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: selected.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "unified-parse-" + runID})
	if err != nil {
		return fmt.Errorf("parse strict x402 challenge: %w", err)
	}
	if !strings.EqualFold(parsed.Quote.Network, network) || !strings.EqualFold(parsed.Quote.Asset, mint) || !strings.EqualFold(parsed.Quote.Currency, "USDC") {
		return fmt.Errorf("strict settlement binding mismatch: network=%q asset=%q currency=%q", parsed.Quote.Network, parsed.Quote.Asset, parsed.Quote.Currency)
	}
	settled, err := settleUnified(ctx, service, store, selected.Episode.EpisodeID, parsed.Quote, "unified-payment-"+runID)
	if err != nil {
		return err
	}
	credential, ok := credentials.CredentialsFor(settled.Intent.CredentialRef)
	if !ok || strings.TrimSpace(credential.Signature) == "" {
		return errors.New("real payment signature was not retained for Merchant delivery")
	}
	delivery, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "unified-delivery-" + runID, PaymentSignature: credential.Signature})
	if err != nil {
		return fmt.Errorf("actual Merchant paid delivery: %w", err)
	}
	if delivery.Response.HTTPStatus != 200 || delivery.Artifact == nil {
		return fmt.Errorf("actual Merchant paid delivery was not HTTP 200: status=%d artifact=%v", delivery.Response.HTTPStatus, delivery.Artifact != nil)
	}
	validated, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, DeliveryID: delivery.Artifact.DeliveryID, TraceID: "unified-validate-" + runID})
	if err != nil || !validated.Valid || validated.Episode.State != episode.StateFulfilled {
		return fmt.Errorf("validate real Merchant delivery: valid=%v state=%s err=%v", validated.Valid, validated.Episode.State, err)
	}
	report, err := buildUnifiedReport(ctx, store, validated.Episode.EpisodeID, settled.Intent, delivery.Response.HTTPStatus)
	if err != nil {
		return err
	}
	path := firstNonEmptyUnified(os.Getenv("UNIFIED_BUSINESS_E2E_REPORT_PATH"), filepath.Join(".local-run", "unified-business-e2e-summary.json"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("unified business E2E passed: report=%s\n%s\n", path, data)
	return nil
}

type countingUnifiedLLMClient struct {
	inner llm.LLMClient
	calls atomic.Int64
}

func (c *countingUnifiedLLMClient) GenerateDecision(ctx context.Context, request llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	c.calls.Add(1)
	return c.inner.GenerateDecision(ctx, request)
}

type controlledUnifiedMerchant struct {
	inner        adapters.MerchantAdapter
	invalidFirst bool
	deliveryCall int
}

func (m *controlledUnifiedMerchant) Invoke(ctx context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	result, err := m.inner.Invoke(ctx, request)
	if err != nil || request.Phase != invocation.PhaseDelivery {
		return result, err
	}
	m.deliveryCall++
	if m.invalidFirst && m.deliveryCall == 1 {
		// The HTTP request and the production Merchant response are real. Only
		// the validator input is controlled to exercise S5 recovery.
		result.Body = nil
		result.ContentType = "text/plain"
		result.PayloadHash = invocation.PayloadHash(result.Body)
		result.PayloadRef = "merchant-response://" + strings.TrimPrefix(result.PayloadHash, "sha256:")
	}
	return result, nil
}

type unifiedU2Report struct {
	EpisodeID        string             `json:"episode_id"`
	LLM              unifiedU2LLMReport `json:"llm"`
	Merchants        []string           `json:"merchants"`
	MerchantMode     string             `json:"merchant_mode"`
	PaymentIntents   int                `json:"payment_intents"`
	Settlements      int                `json:"settlements"`
	TxIDs            []string           `json:"tx_ids"`
	TxHashes         []string           `json:"tx_hashes"`
	AmountsMinor     []int64            `json:"amounts_minor"`
	PaymentStatuses  []string           `json:"payment_statuses"`
	Verification     string             `json:"verification"`
	DeliveryAttempts int                `json:"delivery_attempts"`
	TerminalState    string             `json:"terminal_state"`
	GuardAccepted    bool               `json:"guard_accepted"`
}

type unifiedU2LLMReport struct {
	Provider     string           `json:"provider"`
	Model        string           `json:"model"`
	Calls        int64            `json:"calls"`
	Action       trace.ActionType `json:"action"`
	TraceID      string           `json:"trace_id"`
	GuardVerdict bool             `json:"guard_verdict"`
}

func runUnifiedU2(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 18*time.Minute)
	defer cancel()
	runID := shortUnifiedID()
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	network := firstNonEmptyUnified(os.Getenv("COMMERCE_RUNTIME_SETTLEMENT_NETWORK"), os.Getenv("SOLANA_NETWORK"))
	mint := firstNonEmptyUnified(os.Getenv("COMMERCE_RUNTIME_USDC_MINT"), os.Getenv("USDC_MINT"))
	if network == "" || mint == "" {
		return errors.New("production U2 requires settlement network and USDC mint")
	}
	endpointA := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_ENDPOINT_A"), os.Getenv("MERCHANT_E2E_ENDPOINT"), "http://127.0.0.1:8788/api/v1/products/ai-agent-job-2025/execute")
	endpointB := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_ENDPOINT_B"), "http://127.0.0.1:8789/api/v1/products/ai-agent-job-2025/execute")
	merchantA := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_MERCHANT_DID_A"), "did:merchant:unified-a:"+runID)
	merchantB := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_MERCHANT_DID_B"), "did:merchant:unified-b:"+runID)
	payeeA := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_PAYEE_DID_A"), os.Getenv("MERCHANT_E2E_PAYEE_DID"), didFromAddress(os.Getenv("MERCHANT_SELLER_ADDRESS")))
	payeeB := firstNonEmptyUnified(os.Getenv("MERCHANT_E2E_PAYEE_DID_B"), didFromAddress(os.Getenv("MERCHANT_B_SELLER_ADDRESS")), payeeA)
	if payeeA == "" || payeeB == "" {
		return errors.New("U2 requires real Merchant A and B Solana payee DIDs")
	}

	db, err := mysqlrepo.Open(dsn)
	if err != nil {
		return fmt.Errorf("open commerce MySQL: %w", err)
	}
	if err := mysqlrepo.AutoMigrate(ctx, db); err != nil {
		return fmt.Errorf("migrate commerce MySQL: %w", err)
	}
	store := mysqlrepo.NewStore(db)
	credentials, err := adapters.NewRealCredentialProvider(adapters.RealCredentialProviderConfig{AgentKeypairPath: os.Getenv("STABLEPAY_E2E_AGENT_KEYPAIR_PATH"), DIDServiceAddr: os.Getenv("STABLEPAY_DID_SERVICE_ADDR"), BlockchainAddr: os.Getenv("STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR"), RPCTimeout: 30 * time.Second})
	if err != nil {
		return err
	}
	if err := credentials.EnsureIdentities(ctx, payeeA); err != nil {
		return err
	}
	if payeeB != payeeA {
		if err := credentials.EnsureIdentities(ctx, payeeB); err != nil {
			return err
		}
	}
	requesterDID := credentials.AgentDID()
	paymentClient, err := kitexpaymentservice.NewClient("payment-service", kitexclient.WithHostPorts(firstNonEmptyUnified(os.Getenv("STABLEPAY_PAYMENT_SERVICE_ADDR"), "127.0.0.1:8888")), kitexclient.WithRPCTimeout(90*time.Second))
	if err != nil {
		return fmt.Errorf("create payment-service Kitex client: %w", err)
	}
	kitexPayment := &adapters.KitexPaymentAdapter{Client: paymentClient, Credentials: credentials}
	providerName := firstNonEmptyUnified(os.Getenv("LLM_PROVIDER"), "openai-compatible")
	baseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	model := firstNonEmptyUnified(os.Getenv("LLM_MODEL"), os.Getenv("LLM_MODEL_ID"))
	if baseURL == "" || apiKey == "" || model == "" {
		return errors.New("U2 requires LLM_BASE_URL, LLM_API_KEY and LLM_MODEL/LLM_MODEL_ID")
	}
	llmClient, err := llm.NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		return fmt.Errorf("configure real U2 LLM: %w", err)
	}
	countingClient := &countingUnifiedLLMClient{inner: llmClient}
	verification := adapters.NewStablePayVerificationEntitlement(firstNonEmptyUnified(os.Getenv("STABLEPAY_GATEWAY_BASE_URL"), "http://127.0.0.1:8080"), firstNonEmptyUnified(os.Getenv("STABLEPAY_API_KEY"), "stablepay-dev-key"))
	controlledMerchant := &controlledUnifiedMerchant{inner: adapters.NewHTTPMerchantAdapter(nil), invalidFirst: true}
	service := application.NewService(store,
		application.WithSettlementPolicy(application.SettlementPolicy{Network: network, Assets: map[string]string{"USDC": mint}, AllowUnconstrainedForTests: false}),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: adapters.RealDIDPolicyAdapter{}, Payment: kitexPayment, Status: kitexPayment, Entitlement: verification}),
		application.WithMerchantAdapter(controlledMerchant),
		application.WithLLMDecisionProvider(llm.NewLLMDecisionProvider(countingClient, llm.WithProviderName(providerName), llm.WithModelRef(model), llm.WithProviderTTL(2*time.Minute))),
		application.WithPersistentEvidenceStore(store),
		application.WithMemoryStore(store),
	)
	now := time.Now().UTC()
	if err := seedUnifiedEvidence(ctx, store, now); err != nil {
		return fmt.Errorf("seed U2 evidence: %w", err)
	}
	if err := seedUnifiedCapability(ctx, service, merchantA, payeeA, endpointA, now, "u2-a-"+runID); err != nil {
		return err
	}
	if err := seedUnifiedCapability(ctx, service, merchantB, payeeB, endpointB, now, "u2-b-"+runID); err != nil {
		return err
	}
	request := contract.AcquireCapabilityRequest{RequestID: "unified-u2-" + runID, ParentSessionID: "unified-u2-session", RequesterDID: requesterDID, AcquisitionGoal: contract.AcquisitionGoal{TaskType: "ai-agent-job-2025", Description: "switch from a real invalid Merchant delivery to an eligible real Merchant B"}, Input: contract.Input{URI: "object://unified-u2/input.json", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(16 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 24, MaxPaymentAttempts: 4, MaxDeliveryAttempts: 4, AllowCrossMerchantSwitch: true}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		return fmt.Errorf("create U2 episode: %w", err)
	}
	discovered, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "unified-u2-candidates-" + runID})
	if err != nil {
		return fmt.Errorf("discover U2 capabilities: %w", err)
	}
	candidateA, ok := discovered.CandidateSet.FindCandidate(merchantA, "ai-agent-job-2025")
	if !ok {
		return errors.New("U2 Merchant A was not discovered")
	}
	selected, err := service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{Proposal: unifiedSelectionProposal(created.Episode.EpisodeID, discovered.CandidateSet.CandidateSetID, discovered.Episode.Version-1, discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash, candidateA, now), Action: trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "unified-u2-select-a-" + runID}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "unified-u2"})
	if err != nil {
		return fmt.Errorf("select U2 Merchant A: %w", err)
	}
	initial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "unified-u2-initial-a"})
	if err != nil || initial.Response.HTTPStatus != 402 {
		return fmt.Errorf("U2 Merchant A expected HTTP 402: status=%d err=%v", initial.Response.HTTPStatus, err)
	}
	parsedA, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: selected.Episode.EpisodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "unified-u2-parse-a"})
	if err != nil {
		return fmt.Errorf("parse U2 Merchant A challenge: %w", err)
	}
	settledA, err := settleUnified(ctx, service, store, selected.Episode.EpisodeID, parsedA.Quote, "unified-u2-payment-a-"+runID)
	if err != nil {
		return err
	}
	credentialA, ok := credentials.CredentialsFor(settledA.Intent.CredentialRef)
	if !ok {
		return errors.New("U2 Merchant A credential was not retained")
	}
	firstDelivery, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "unified-u2-delivery-a", PaymentSignature: credentialA.Signature})
	if err != nil || firstDelivery.Artifact == nil {
		return fmt.Errorf("invoke U2 Merchant A delivery: artifact=%v err=%v", firstDelivery.Artifact != nil, err)
	}
	invalid, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, DeliveryID: firstDelivery.Artifact.DeliveryID, TraceID: "unified-u2-validate-a"})
	if err != nil || invalid.Valid || invalid.Episode.State != episode.StateRecovering {
		return fmt.Errorf("U2 controlled invalid delivery did not enter RECOVERING: valid=%v state=%s err=%v", invalid.Valid, invalid.Episode.State, err)
	}
	beforeIntents, err := store.ListPaymentIntents(ctx, selected.Episode.EpisodeID)
	if err != nil {
		return err
	}
	commit, decisionResult, err := service.ExecuteRecoveryDecision(ctx, application.S6DecisionRequest{EpisodeID: selected.Episode.EpisodeID, Query: "The real Merchant A paid delivery was invalid and the episode is RECOVERING. Choose the eligible unattempted Merchant B and switch merchants now; do not retry Merchant A."})
	if err != nil {
		return fmt.Errorf("real U2 LLM recovery: %w", err)
	}
	if decisionResult.Proposal.ProposedAction != trace.ActionSwitchMerchant {
		return fmt.Errorf("U2 real LLM selected %s, want SWITCH_MERCHANT", decisionResult.Proposal.ProposedAction)
	}
	if commit.Event == nil || !commit.Event.RuntimeVerdict.Allowed {
		return errors.New("U2 SWITCH_MERCHANT guard was not accepted")
	}
	afterIntents, err := store.ListPaymentIntents(ctx, selected.Episode.EpisodeID)
	if err != nil {
		return err
	}
	if len(beforeIntents) != len(afterIntents) {
		return errors.New("U2 recovery decision created a payment intent before Merchant B 402")
	}
	bInitial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: selected.Episode.EpisodeID, TraceID: "unified-u2-initial-b"})
	if err != nil || bInitial.Response.HTTPStatus != 402 {
		return fmt.Errorf("U2 Merchant B expected HTTP 402: status=%d err=%v", bInitial.Response.HTTPStatus, err)
	}
	parsedB, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: selected.Episode.EpisodeID, InvocationID: bInitial.Invocation.InvocationID, TraceID: "unified-u2-parse-b"})
	if err != nil {
		return fmt.Errorf("parse U2 Merchant B challenge: %w", err)
	}
	settledB, err := settleUnified(ctx, service, store, selected.Episode.EpisodeID, parsedB.Quote, "unified-u2-payment-b-"+runID)
	if err != nil {
		return err
	}
	credentialB, ok := credentials.CredentialsFor(settledB.Intent.CredentialRef)
	if !ok {
		return errors.New("U2 Merchant B credential was not retained")
	}
	bDelivery, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, Attempt: 2, TraceID: "unified-u2-delivery-b", PaymentSignature: credentialB.Signature})
	if err != nil || bDelivery.Response.HTTPStatus != 200 || bDelivery.Artifact == nil {
		return fmt.Errorf("U2 Merchant B paid delivery failed: status=%d artifact=%v err=%v", bDelivery.Response.HTTPStatus, bDelivery.Artifact != nil, err)
	}
	finalValidation, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: selected.Episode.EpisodeID, DeliveryID: bDelivery.Artifact.DeliveryID, TraceID: "unified-u2-validate-b"})
	if err != nil || !finalValidation.Valid || finalValidation.Episode.State != episode.StateFulfilled {
		return fmt.Errorf("U2 final Merchant B delivery invalid: valid=%v state=%s err=%v", finalValidation.Valid, finalValidation.Episode.State, err)
	}
	report, err := buildUnifiedU2Report(ctx, store, selected.Episode.EpisodeID, decisionResult, commit, countingClient.calls.Load(), merchantA, merchantB)
	if err != nil {
		return err
	}
	path := firstNonEmptyUnified(os.Getenv("UNIFIED_BUSINESS_E2E_REPORT_PATH"), filepath.Join(".local-run", "unified-business-e2e-u2-summary.json"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("unified business U2 E2E passed: report=%s\n%s\n", path, data)
	return nil
}

func buildUnifiedU2Report(ctx context.Context, store *mysqlrepo.Store, episodeID string, decisionResult application.S6DecisionResult, commit application.CommitResult, calls int64, merchantA, merchantB string) (unifiedU2Report, error) {
	current, err := store.Get(ctx, episodeID)
	if err != nil {
		return unifiedU2Report{}, err
	}
	intents, err := store.ListPaymentIntents(ctx, episodeID)
	if err != nil {
		return unifiedU2Report{}, err
	}
	entries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		return unifiedU2Report{}, err
	}
	if len(intents) != 2 {
		return unifiedU2Report{}, fmt.Errorf("U2 expected two PaymentIntents, got %d", len(intents))
	}
	txIDs, txHashes, amounts, statuses := make([]string, 0, len(intents)), make([]string, 0, len(intents)), make([]int64, 0, len(intents)), make([]string, 0, len(intents))
	for _, intent := range intents {
		txIDs = append(txIDs, intent.TxID)
		txHashes = append(txHashes, intent.TxHash)
		amounts = append(amounts, intent.AmountMinor)
		statuses = append(statuses, string(intent.Status))
	}
	settlements := 0
	for _, entry := range entries {
		if entry.Type == ledger.EntryPaymentSettled {
			settlements++
		}
	}
	if settlements != 2 {
		return unifiedU2Report{}, fmt.Errorf("U2 expected two durable settlements, got %d", settlements)
	}
	if current.DeliveryAttemptCount != 2 {
		return unifiedU2Report{}, fmt.Errorf("U2 expected two delivery attempts, got %d", current.DeliveryAttemptCount)
	}
	return unifiedU2Report{EpisodeID: episodeID, LLM: unifiedU2LLMReport{Provider: decisionResult.Trace.Provider, Model: decisionResult.Proposal.ModelRef, Calls: calls, Action: decisionResult.Proposal.ProposedAction, TraceID: decisionResult.Trace.TraceID, GuardVerdict: commit.Event != nil && commit.Event.RuntimeVerdict.Allowed}, Merchants: []string{merchantA, merchantB}, MerchantMode: "production", PaymentIntents: len(intents), Settlements: settlements, TxIDs: txIDs, TxHashes: txHashes, AmountsMinor: amounts, PaymentStatuses: statuses, Verification: "real StablePay verification proof matched each PaymentIntent tx_id", DeliveryAttempts: current.DeliveryAttemptCount, TerminalState: string(current.State), GuardAccepted: commit.Event != nil && commit.Event.RuntimeVerdict.Allowed}, nil
}

func settleUnified(ctx context.Context, service *application.Service, store *mysqlrepo.Store, episodeID string, quote application.TrustedPaymentQuote, key string) (application.PaymentExecutionResult, error) {
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID, Quote: quote, IdempotencyKey: key, TraceID: key + ":reserve", Actor: "runtime"})
	if err != nil {
		return application.PaymentExecutionResult{}, fmt.Errorf("reserve real PaymentIntent: %w", err)
	}
	execution, err := service.AuthorizeAndSubmitPayment(ctx, reserved.Intent.IntentID, key+":submit")
	if err != nil && execution.Intent == nil {
		return application.PaymentExecutionResult{}, fmt.Errorf("submit real PaymentIntent: %w", err)
	}
	for attempt := 0; attempt < 60; attempt++ {
		intent := execution.Intent
		if intent == nil {
			intent, err = store.GetPaymentIntent(ctx, reserved.Intent.IntentID)
			if err != nil {
				return application.PaymentExecutionResult{}, err
			}
		}
		if intent.Status == payment.IntentConfirmed {
			break
		}
		if intent.Status == payment.IntentFailed || intent.Status == payment.IntentExpired {
			return execution, fmt.Errorf("real PaymentIntent reached %s: %s", intent.Status, intent.FailureCode)
		}
		time.Sleep(2 * time.Second)
		execution, err = service.ReconcilePayment(ctx, reserved.Intent.IntentID, fmt.Sprintf("%s:status:%d", key, attempt))
		if err != nil {
			return application.PaymentExecutionResult{}, fmt.Errorf("query real payment status: %w", err)
		}
	}
	if execution.Intent == nil || execution.Intent.Status != payment.IntentConfirmed {
		return execution, errors.New("real payment did not reach CONFIRMED before timeout")
	}
	for attempt := 0; attempt < 45; attempt++ {
		entitled, err := service.VerifyPaymentEntitlement(ctx, reserved.Intent.IntentID, fmt.Sprintf("%s:entitlement:%d", key, attempt))
		if err != nil {
			return application.PaymentExecutionResult{}, fmt.Errorf("verify real entitlement: %w", err)
		}
		if entitled.Episode != nil && entitled.Episode.State == episode.StateInvokingDelivery {
			return entitled, nil
		}
		time.Sleep(2 * time.Second)
	}
	return execution, errors.New("real verification entitlement did not become valid before timeout")
}

func buildUnifiedReport(ctx context.Context, store *mysqlrepo.Store, episodeID string, intent *payment.PaymentIntent, deliveryStatus int) (unifiedReport, error) {
	current, err := store.Get(ctx, episodeID)
	if err != nil {
		return unifiedReport{}, err
	}
	if intent == nil {
		return unifiedReport{}, errors.New("missing settled payment intent")
	}
	report := unifiedReport{EpisodeID: episodeID, Merchant: current.SelectedMerchantDID, MerchantMode: "production", PaymentIntentID: intent.IntentID, TxID: intent.TxID, TxHash: intent.TxHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, PaymentStatus: string(intent.Status), Verification: "real StablePay verification proof matched tx_id", DeliveryStatus: deliveryStatus, TerminalState: string(current.State), DeliveryAttempts: current.DeliveryAttemptCount}
	report.LLM.Provider = "not-required-u1"
	report.LLM.Model = ""
	report.LLM.Calls = 0
	return report, nil
}

func unifiedSelectionProposal(episodeID, candidateSetID string, sequence uint64, factsRef, payloadHash string, candidate catalog.Candidate, now time.Time) decision.DecisionProposal {
	return decision.DecisionProposal{ProposalID: "unified-select", EpisodeID: episodeID, BasedOnEventSequence: sequence, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: candidateSetID, Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}, EvidenceRefs: []string{factsRef, payloadHash}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
}

func seedUnifiedCapability(ctx context.Context, service *application.Service, merchantDID, payeeDID, endpoint string, now time.Time, runID string) error {
	price := int64(200)
	return service.RegisterCapabilityVersion(ctx, &catalog.MerchantCapability{MerchantDID: merchantDID, CapabilityID: "ai-agent-job-2025", PayeeDID: payeeDID, Name: "AI agent job 2025", Description: "Production Merchant Server unified L2 capability.", TaskTypes: []string{"ai-agent-job-2025"}, SemanticTags: []string{"unified-business-e2e"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: endpoint, Method: "GET"}, InputSchemaRef: "schema:unified-input", OutputSchemaRef: "schema:merchant-delivery", InputContentTypes: []string{"application/json"}, OutputContentTypes: []string{"application/json"}, SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1-" + runID, Source: "unified-business-e2e", SourceRef: "merchant://production/" + runID, SourceHash: evidence.HashString("unified:" + runID), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(9 * time.Minute), CreatedAt: now, UpdatedAt: now})
}

func seedUnifiedEvidence(ctx context.Context, store *mysqlrepo.Store, now time.Time) error {
	fixtures := []struct {
		id, sourceType, sourceRef, content string
		trust                              evidence.TrustClass
	}{
		{"unified-x402", string(evidence.SourceProtocolDoc), "fixture://unified/x402", "x402 exact binds network, asset, amount, payTo and timeout.", evidence.TrustProtocolDoc},
		{"unified-payment", string(evidence.SourceProtocolDoc), "fixture://unified/payment", "runtime owns payment confirmation and verification entitlement.", evidence.TrustProtocolDoc},
		{"unified-merchant", string(evidence.SourceMerchantCapabilityDoc), "fixture://unified/merchant", "actual production Merchant Server is the delivery capability.", evidence.TrustMerchantDoc},
	}
	for _, fixture := range fixtures {
		if err := store.SaveEvidenceRecord(ctx, evidence.NewRecord(fixture.id, fixture.sourceType, fixture.sourceRef, "v1", evidence.HashString(fixture.content), "text/plain", fixture.content, fixture.trust, now)); err != nil {
			return err
		}
	}
	return nil
}

func didFromAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	return "did:solana:" + address
}

func firstNonEmptyUnified(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shortUnifiedID() string {
	var value [6]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}
