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
	calls                    atomic.Int32
	initialCalls             atomic.Int32
	deliveryCalls            atomic.Int32
	invalidFirst             bool
	transientInitialFailures int32
}

func (m *apiMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.calls.Add(1)
	now := time.Now().UTC()
	if request.Phase == invocation.PhaseInitial {
		initialCall := m.initialCalls.Add(1)
		if initialCall <= m.transientInitialFailures {
			return adapters.MerchantInvokeResult{}, errors.New("merchant transport timeout")
		}
		challenge := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.test/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"200000","asset":"USDC","payTo":"did:payee:test","maxTimeoutSeconds":300,"extra":{"currency":"USDC","productId":"api-test"}}]}`)
		return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, ContentType: "application/json", Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(challenge)}, Body: challenge, PayloadHash: invocation.PayloadHash(challenge), PayloadRef: "merchant-response://challenge", OccurredAt: now}, nil
	}
	body := []byte(`{"artifact":"fulfilled by merchant"}`)
	if m.invalidFirst && m.deliveryCalls.Add(1) == 1 {
		body = nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "application/json", Body: body, PayloadHash: invocation.PayloadHash(body), PayloadRef: "merchant-response://delivery", OccurredAt: now}, nil
}

type pendingPayment struct {
	submits atomic.Int32
	queries atomic.Int32
}

func (p *pendingPayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	p.submits.Add(1)
	return payment.PaymentOutcome{Status: payment.OutcomePending, IntentID: request.Intent.IntentID, TxID: "pending:" + request.Intent.IntentID, TxHash: "pending-hash:" + request.Intent.IntentID, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

func (p *pendingPayment) Query(_ context.Context, request adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	p.queries.Add(1)
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: request.Intent.TxID, TxHash: request.Intent.TxHash, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

func TestExternalHTTPAndMCPDriveDurableEpisode(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{transientInitialFailures: 1}
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
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := runtime.NewRunner(service, store, runtime.WithRootContext(root), runtime.WithScanInterval(5*time.Millisecond), runtime.WithRetryBackoff(5*time.Millisecond, 20*time.Millisecond))
	if err := runner.StartSupervisor(root); err != nil {
		t.Fatal(err)
	}
	defer runner.StopSupervisor()
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
	changed := request
	changed.AcquisitionGoal.Description = "changed contract must conflict"
	changedBody, _ := json.Marshal(changed)
	conflict := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, "s10-test-token", "external-api-1", changedBody)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("changed idempotency payload status=%d body=%s", conflict.Code, conflict.Body.String())
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
	if mcp.Code != http.StatusOK || !strings.Contains(mcp.Body.String(), "stablepay.acquire") || !strings.Contains(mcp.Body.String(), "stablepay.status") || !strings.Contains(mcp.Body.String(), "parent_session_id") || !strings.Contains(mcp.Body.String(), "budget_limit_minor") {
		t.Fatalf("MCP tools/list failed: %s", mcp.Body.String())
	}
	mcpAcquireArgs, _ := json.Marshal(request)
	mcpAcquireBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "stablepay.acquire", "arguments": json.RawMessage(mcpAcquireArgs)}})
	mcpAcquire := doJSON(t, server.URL+"/mcp", http.MethodPost, "s10-test-token", "", mcpAcquireBody)
	if mcpAcquire.Code != http.StatusOK || !strings.Contains(mcpAcquire.Body.String(), `"replayed":true`) {
		t.Fatalf("MCP acquire replay failed: %s", mcpAcquire.Body.String())
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
	replay := doJSON(t, server.URL+"/v1/episodes/"+current.EpisodeID+"/parent-decisions", http.MethodPost, "s10-test-token", "approval:parent-api-1", decisionBody)
	if replay.Code != http.StatusAccepted || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("parent decision replay failed: status=%d body=%s", replay.Code, replay.Body.String())
	}
	conflict := doJSON(t, server.URL+"/v1/episodes/"+current.EpisodeID+"/parent-decisions", http.MethodPost, "s10-test-token", "approval:parent-api-1", []byte(`{"approval_id":"approval:parent-api-1","decision":"APPROVE","actor_ref":"parent:test"}`))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("parent decision conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	unknown := doJSON(t, server.URL+"/v1/episodes/"+current.EpisodeID+"/parent-decisions", http.MethodPost, "s10-test-token", "approval:unknown", []byte(`{"approval_id":"approval:unknown","decision":"DENY","actor_ref":"parent:test"}`))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown approval status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	latest, err := service.GetEpisode(context.Background(), current.EpisodeID)
	if err != nil || latest.State != episode.StateRecovering {
		t.Fatalf("denied parent decision state=%s err=%v", latest.State, err)
	}
}

func TestSupervisorTransientFailureRecovery(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{transientInitialFailures: 2}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: &apiPayment{}, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := runtime.NewRunner(service, store, runtime.WithRootContext(root), runtime.WithScanInterval(5*time.Millisecond), runtime.WithRetryBackoff(5*time.Millisecond, 20*time.Millisecond))
	if err := runner.StartSupervisor(root); err != nil {
		t.Fatal(err)
	}
	defer runner.StopSupervisor()
	created, err := service.CreateEpisode(root, testAcquireRequest("supervisor-transient-1", now))
	if err != nil {
		t.Fatal(err)
	}
	runner.Enqueue(nil, created.Episode.EpisodeID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := service.GetEpisode(root, created.Episode.EpisodeID)
		if getErr == nil && current.State == episode.StateFulfilled {
			status, statusErr := store.GetEpisodeExecutionStatus(root, current.EpisodeID)
			if statusErr != nil {
				t.Fatal(statusErr)
			}
			if status.Status != repository.ExecutionCompleted || status.AttemptCount < 3 {
				t.Fatalf("unexpected execution status: %#v", status)
			}
			if merchant.initialCalls.Load() != 3 {
				t.Fatalf("expected two transient calls and one success, got %d", merchant.initialCalls.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	current, _ := service.GetEpisode(root, created.Episode.EpisodeID)
	t.Fatalf("supervisor did not recover transient failure, state=%s", current.State)
}

func TestSupervisorPermanentOutagePersistsBackoffWithoutBusyLoop(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{transientInitialFailures: 100}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: &apiPayment{}, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := runtime.NewRunner(service, store, runtime.WithRootContext(root), runtime.WithScanInterval(5*time.Millisecond), runtime.WithRetryBackoff(100*time.Millisecond, 100*time.Millisecond))
	if err := runner.StartSupervisor(root); err != nil {
		t.Fatal(err)
	}
	defer runner.StopSupervisor()
	created, err := service.CreateEpisode(root, testAcquireRequest("supervisor-permanent-1", now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunEpisode(root, created.Episode.EpisodeID); err == nil {
		t.Fatal("expected transient merchant failure")
	}
	status, err := store.GetEpisodeExecutionStatus(root, created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != repository.ExecutionRetryWait || status.AttemptCount != 1 || status.NextRetryAt == nil || !status.NextRetryAt.After(time.Now()) {
		t.Fatalf("expected persisted retry schedule: %#v", status)
	}
	initialAttempts := status.AttemptCount
	time.Sleep(20 * time.Millisecond)
	status, err = store.GetEpisodeExecutionStatus(root, created.Episode.EpisodeID)
	if err != nil || status.AttemptCount != initialAttempts {
		t.Fatalf("supervisor retried before next_retry_at: %#v err=%v", status, err)
	}
}

func TestRunnerPendingPaymentReconcilesWithoutSecondSubmit(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{}
	paymentAdapter := &pendingPayment{}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: paymentAdapter, Status: paymentAdapter, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateEpisode(context.Background(), testAcquireRequest("pending-payment-1", now))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.NewRunner(service, store)
	result, err := runner.RunEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil || result.Episode.State != episode.StateFulfilled {
		t.Fatalf("pending payment did not reconcile: state=%s err=%v", result.Episode.State, err)
	}
	if paymentAdapter.submits.Load() != 1 || paymentAdapter.queries.Load() < 1 {
		t.Fatalf("expected one submit and reconciliation query, submits=%d queries=%d", paymentAdapter.submits.Load(), paymentAdapter.queries.Load())
	}
	if result.Episode.PaymentAttemptCount != 1 {
		t.Fatalf("unexpected payment attempt count: %d", result.Episode.PaymentAttemptCount)
	}
}

func TestRunnerRestartReplaysPersistedMerchantInvocation(t *testing.T) {
	store := repository.NewInMemoryStore()
	merchant := &apiMerchant{}
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests(), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: &apiPayment{}, Entitlement: apiEntitlement{}}))
	now := time.Now().UTC()
	if err := service.RegisterCapabilityVersion(context.Background(), testCapability(now)); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateEpisode(context.Background(), testAcquireRequest("merchant-replay-1", now))
	if err != nil {
		t.Fatal(err)
	}
	first := runtime.NewRunner(service, store, runtime.WithMaxSteps(2))
	if _, err := first.RunEpisode(context.Background(), created.Episode.EpisodeID); !errors.Is(err, runtime.ErrRunnerStepLimit) {
		t.Fatalf("expected step limit before invocation, got %v", err)
	}
	current, err := service.GetEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil || current.State != episode.StateInvoking {
		t.Fatalf("unexpected persisted state before invocation: %s err=%v", current.State, err)
	}
	if _, err := service.InvokeSelectedMerchant(context.Background(), application.InvokeSelectedMerchantRequest{EpisodeID: current.EpisodeID, TraceID: "merchant-replay-inflight"}); err != nil {
		t.Fatal(err)
	}
	second := runtime.NewRunner(service, store)
	result, err := second.ResumeEpisode(context.Background(), current.EpisodeID)
	if err != nil || result.Episode.State != episode.StateFulfilled {
		t.Fatalf("restart resume failed: state=%s err=%v", result.Episode.State, err)
	}
	if merchant.initialCalls.Load() != 1 {
		t.Fatalf("persisted merchant invocation was duplicated: initial calls=%d", merchant.initialCalls.Load())
	}
}

func TestRunnerDeadlineUsesCanonicalExpiredEpisodeState(t *testing.T) {
	store := repository.NewInMemoryStore()
	service := application.NewService(store, application.WithUnconstrainedSettlementPolicyForTests())
	now := time.Now().UTC()
	request := testAcquireRequest("deadline-expiry-1", now)
	request.Constraints.DeadlineAt = now.Add(20 * time.Millisecond)
	created, err := service.CreateEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	runner := runtime.NewRunner(service, store)
	result, err := runner.RunEpisode(context.Background(), created.Episode.EpisodeID)
	if err != nil || result.Episode.State != episode.StateExpired {
		t.Fatalf("deadline did not transition episode to EXPIRED: state=%s err=%v", result.Episode.State, err)
	}
	if result.Episode.TerminalReason != "EPISODE_DEADLINE_EXCEEDED" {
		t.Fatalf("unexpected terminal reason: %s", result.Episode.TerminalReason)
	}
	status, err := store.GetEpisodeExecutionStatus(context.Background(), result.Episode.EpisodeID)
	if err != nil || status.Status != repository.ExecutionCompleted {
		t.Fatalf("unexpected execution status after expiry: %#v err=%v", status, err)
	}
}

func testAcquireRequest(id string, now time.Time) contract.AcquireCapabilityRequest {
	return contract.AcquireCapabilityRequest{RequestID: id, ParentSessionID: "session:" + id, RequesterDID: "did:stablepay:test-agent", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "api-test", Description: "runtime hardening"}, Input: contract.Input{URI: "object://input", ContentType: "application/json"}, Constraints: contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 20, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3}, ExpectedOutput: contract.ExpectedOutput{Schema: "merchant-delivery", ContentType: "application/json"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
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
