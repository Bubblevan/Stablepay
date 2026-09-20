package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type apiDID struct{}

func (apiDID) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	intent := request.Intent
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: "test-auth:" + intent.IntentID}, nil
}

type apiPayment struct{ submits atomic.Int32 }

func (p *apiPayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	p.submits.Add(1)
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: "tx:" + request.Intent.IntentID, TxHash: "hash:" + request.Intent.IntentID, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type apiEntitlement struct{}

func (apiEntitlement) Verify(_ context.Context, request adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: request.IntentID, TxID: request.TxID, Reference: "entitlement:" + request.IntentID, EvidenceRef: "entitlement://" + request.IntentID}, nil
}

type apiMerchant struct {
	calls         atomic.Int32
	deliveryCalls atomic.Int32
	invalidFirst  bool
}

func (m *apiMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.calls.Add(1)
	now := time.Now().UTC()
	if request.Phase == invocation.PhaseInitial {
		challenge := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.test/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"200000","asset":"USDC","payTo":"did:payee:test","maxTimeoutSeconds":300,"extra":{"currency":"USDC","productId":"api-test"}}]}`)
		return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, ContentType: "application/json", Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(challenge)}, Body: challenge, PayloadHash: invocation.PayloadHash(challenge), PayloadRef: "merchant-response://challenge", OccurredAt: now}, nil
	}
	body := []byte(`{"artifact":"fulfilled by merchant"}`)
	if m.invalidFirst && m.deliveryCalls.Add(1) == 1 {
		body = nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "application/json", Body: body, PayloadHash: invocation.PayloadHash(body), PayloadRef: "merchant-response://delivery", OccurredAt: now}, nil
}

func TestExternalHTTPAndMCPDriveDurableEpisode(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{}
	paymentAdapter := &apiPayment{}
	service := application.NewService(store,
		application.WithUnconstrainedSettlementPolicyForTests(),
		application.WithMerchantAdapter(merchant),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: paymentAdapter, Entitlement: apiEntitlement{}}),
		application.WithMemoryStore(store),
	)
	now := time.Now().UTC()
	capability := testCapability(now)
	if err := service.RegisterCapabilityVersion(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	runner := runtime.NewRunner(service, store)
	server := httptest.NewServer(NewServer(service, store, runner, AuthConfig{Token: "s10-test-token"}, func(context.Context) error { return nil }))
	defer server.Close()

	request := contract.AcquireCapabilityRequest{RequestID: "external-api-1", ParentSessionID: "external-session", RequesterDID: "did:stablepay:external-agent", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "api-test", Description: "retrieve a fulfilled artifact"}, Input: contract.Input{URI: "object://input", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 20, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
	body, _ := json.Marshal(request)
	response := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, "s10-test-token", "external-api-1", body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("acquire status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Episode struct {
			EpisodeID string `json:"episode_id"`
		} `json:"episode"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Episode.EpisodeID == "" {
		t.Fatal("acquire did not return episode id")
	}

	var final struct {
		Episode struct {
			State string `json:"state"`
		} `json:"episode"`
		Artifact struct {
			Body string `json:"body"`
		} `json:"artifact"`
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		status := doJSON(t, server.URL+"/v1/episodes/"+created.Episode.EpisodeID, http.MethodGet, "s10-test-token", "", nil)
		_ = json.Unmarshal(status.Body.Bytes(), &final)
		if final.Episode.State == "FULFILLED" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Episode.State != "FULFILLED" {
		t.Fatalf("episode did not fulfill: %#v", final)
	}
	if decoded, err := base64.StdEncoding.DecodeString(final.Artifact.Body); err != nil || !strings.Contains(string(decoded), "fulfilled") {
		t.Fatalf("final artifact missing: %q err=%v", final.Artifact.Body, err)
	}

	replay := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, "s10-test-token", "external-api-1", body)
	if replay.Code != http.StatusOK {
		t.Fatalf("idempotent replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	unauthorized := doJSON(t, server.URL+"/v1/episodes/"+created.Episode.EpisodeID, http.MethodGet, "", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	health := doJSON(t, server.URL+"/healthz", http.MethodGet, "", "", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d", health.Code)
	}

	mcpBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	mcp := doJSON(t, server.URL+"/mcp", http.MethodPost, "s10-test-token", "", mcpBody)
	if mcp.Code != http.StatusOK || !strings.Contains(mcp.Body.String(), "stablepay.acquire") || !strings.Contains(mcp.Body.String(), "stablepay.status") {
		t.Fatalf("MCP tools/list failed: %s", mcp.Body.String())
	}
	statusBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"stablepay.status","arguments":{"episode_id":"` + created.Episode.EpisodeID + `"}}}`)
	mcpStatus := doJSON(t, server.URL+"/mcp", http.MethodPost, "s10-test-token", "", statusBody)
	if mcpStatus.Code != http.StatusOK || !strings.Contains(mcpStatus.Body.String(), "FULFILLED") {
		t.Fatalf("MCP status failed: %s", mcpStatus.Body.String())
	}
}

func TestRunnerResumeUsesPersistedState(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: &apiPayment{}, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	request := contract.AcquireCapabilityRequest{RequestID: "resume-1", ParentSessionID: "resume-session", RequesterDID: "did:stablepay:resume", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "api-test", Description: "resume"}, Input: contract.Input{URI: "object://input", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 20, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first := runtime.NewRunner(service, store, runtime.WithMaxSteps(1))
	if _, err := first.RunEpisode(context.Background(), created.Episode.EpisodeID); !errors.Is(err, runtime.ErrRunnerStepLimit) {
		t.Fatalf("expected step limit, got %v", err)
	}
	resumed := runtime.NewRunner(service, store)
	result, err := resumed.ResumeEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Episode.State != "FULFILLED" {
		t.Fatalf("resume state=%s", result.Episode.State)
	}
}

func TestParentDecisionHTTPBoundary(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{invalidFirst: true}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: &apiPayment{}, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	request := contract.AcquireCapabilityRequest{RequestID: "parent-api-1", ParentSessionID: "parent-session", RequesterDID: "did:stablepay:parent", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "api-test", Description: "parent approval"}, Input: contract.Input{URI: "object://input", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 20, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.NewRunner(service, store)
	if _, err := runner.RunEpisode(context.Background(), created.Episode.EpisodeID); err == nil {
		t.Fatal("expected runner to pause on missing LLM after entering recovery")
	}
	current, err := service.GetEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil || current.State != episode.StateRecovering {
		t.Fatalf("recovery state=%s err=%v", current.State, err)
	}
	events, err := service.ListEvents(context.Background(), current.EpisodeID)
	if err != nil || len(events) == 0 {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	proposal := decision.DecisionProposal{ProposalID: "parent-api-proposal", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionAskParent, CandidateSetID: current.SelectedCandidateSetID, EvidenceRefs: []string{last.Observation.FactsRef}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	if _, err := service.AskParent(context.Background(), application.AskParentRequest{EpisodeID: current.EpisodeID, Proposal: proposal, Action: trace.Action{Type: trace.ActionAskParent, IdempotencyKey: "parent-api-ask"}, Observation: trace.Observation{Type: trace.ObservationPolicyDenied}, Actor: "runtime", TraceID: "parent-api-ask", ApprovalID: "approval:parent-api-1", ApprovalScope: recovery.AllowSwitch, RequestedAction: trace.ActionSwitchMerchant}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(service, store, runner, AuthConfig{Token: "s10-test-token"}, func(context.Context) error { return nil }))
	defer server.Close()
	status := doJSON(t, server.URL+"/v1/episodes/"+current.EpisodeID, http.MethodGet, "s10-test-token", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), "approval:parent-api-1") {
		t.Fatalf("parent approval missing: %s", status.Body.String())
	}
	decisionBody := []byte(`{"approval_id":"approval:parent-api-1","decision":"DENY","actor_ref":"parent:test"}`)
	response := doJSON(t, server.URL+"/v1/episodes/"+current.EpisodeID+"/parent-decisions", http.MethodPost, "s10-test-token", "approval:parent-api-1", decisionBody)
	if response.Code != http.StatusAccepted {
		t.Fatalf("parent decision status=%d body=%s", response.Code, response.Body.String())
	}
	latest, err := service.GetEpisode(context.Background(), current.EpisodeID)
	if err != nil || latest.State != episode.StateRecovering {
		t.Fatalf("denied parent decision state=%s err=%v", latest.State, err)
	}
}

func doJSON(t *testing.T, url, method, token, idempotency string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	response := httptest.NewRecorder()
	// The recorder path is used for direct handler calls when url is a test
	// server URL only in this helper's HTTP client branch below.
	if strings.HasPrefix(url, "http://127.0.0.1") || strings.HasPrefix(url, "http://localhost") {
		client := &http.Client{Timeout: 5 * time.Second}
		actual, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer actual.Body.Close()
		response.Code = actual.StatusCode
		_, _ = response.Body.ReadFrom(actual.Body)
		return response
	}
	t.Fatal("unexpected test URL")
	return response
}

func testCapability(now time.Time) *catalog.MerchantCapability {
	price := int64(200)
	return &catalog.MerchantCapability{MerchantDID: "did:merchant:test", CapabilityID: "api-test", PayeeDID: "did:payee:test", Name: "API test", Description: "test", TaskTypes: []string{"api-test"}, SemanticTags: []string{"test"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: "https://merchant.test/execute", Method: "GET"}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output", InputContentTypes: []string{"application/json"}, OutputContentTypes: []string{"application/json"}, SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "api-test", SourceRef: "test://api", SourceHash: evidence.HashString("api-test"), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
}
