package workflowruntime_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/api"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
	"github.com/stablepay/commerce-runtime/internal/validator"
	"github.com/stablepay/commerce-runtime/internal/workflow"
	workflowartifact "github.com/stablepay/commerce-runtime/internal/workflow/artifact"
	workflowruntime "github.com/stablepay/commerce-runtime/internal/workflowruntime"
)

type httpWorkflowDID struct{}

func (httpWorkflowDID) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: request.Intent.RequesterDID, MerchantDID: request.Intent.MerchantDID, CapabilityID: request.Intent.CapabilityID, PayeeDID: request.PayeeDID, QuoteHash: request.Intent.QuoteHash, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency, AuthorizationRef: "http-e2e-auth:" + request.Intent.IntentID}, nil
}

type httpWorkflowPayment struct{}

func (httpWorkflowPayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	tx := "http-e2e-tx:" + request.Intent.IntentID
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: tx, TxHash: tx, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

func (httpWorkflowPayment) Query(_ context.Context, request adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	tx := "http-e2e-tx:" + request.Intent.IntentID
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: tx, TxHash: tx, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type httpWorkflowEntitlement struct{}

func (httpWorkflowEntitlement) Verify(_ context.Context, request adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: request.IntentID, TxID: request.TxID, Reference: "http-e2e-entitlement:" + request.IntentID, EvidenceRef: "http-e2e-entitlement://" + request.IntentID}, nil
}

func TestExternalWorkflowUsesRealHTTPMerchantDataPlane(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var bodies [][]byte
	merchantCalls := map[string]int{}
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		methods = append(methods, r.Method)
		bodies = append(bodies, append([]byte(nil), body...))
		merchantCalls[r.URL.Path]++
		call := merchantCalls[r.URL.Path]
		mu.Unlock()
		if call == 1 {
			challenge := []byte(fmt.Sprintf(`{"x402Version":1,"accepts":[{"scheme":"exact","network":"devnet","maxAmountRequired":"1000000","asset":"USDC","payTo":"did:payee:http","resource":"http://%s%s","maxTimeoutSeconds":300,"extra":{"currency":"USDC","productId":"http-e2e"}}]}`, r.Host, r.URL.Path))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(challenge))
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write(challenge)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if string(body) == "hello" {
			_, _ = w.Write([]byte("HELLO"))
			return
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer merchant.Close()

	ctx := context.Background()
	store := repository.NewInMemoryStore()
	service := application.NewService(store,
		application.WithUnconstrainedSettlementPolicyForTests(),
		application.WithMerchantAdapter(adapters.NewHTTPMerchantAdapter(merchant.Client())),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: httpWorkflowDID{}, Payment: httpWorkflowPayment{}, Status: httpWorkflowPayment{}, Entitlement: httpWorkflowEntitlement{}}),
		application.WithValidatorRegistry(validator.NewBuiltinRegistry()),
		application.WithInputPayloadResolver(workflowartifact.NewResolver(store, store)),
	)
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, capability := range []catalog.MerchantCapability{
		{MerchantDID: "did:merchant:http", CapabilityID: "http-source", PayeeDID: "did:payee:http", Name: "HTTP source", Description: "HTTP source", TaskTypes: []string{"http-source"}, SemanticTags: []string{"http"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: merchant.URL + "/source", Method: http.MethodPost}, InputSchemaRef: "schema://input", OutputSchemaRef: "schema://middle", InputContentTypes: []string{"text/plain"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{contract.DefaultProtocolVersion}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: int64Ptr(1000000), PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "http-e2e", SourceRef: "http-e2e:source", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
		{MerchantDID: "did:merchant:http", CapabilityID: "http-transform", PayeeDID: "did:payee:http", Name: "HTTP transform", Description: "HTTP transform", TaskTypes: []string{"http-transform"}, SemanticTags: []string{"http"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: merchant.URL + "/transform", Method: http.MethodPost}, InputSchemaRef: "schema://middle", OutputSchemaRef: "schema://output", InputContentTypes: []string{"text/plain"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{contract.DefaultProtocolVersion}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: int64Ptr(1000000), PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "http-e2e", SourceRef: "http-e2e:transform", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		hash, err := capability.SnapshotHash()
		if err != nil {
			t.Fatal(err)
		}
		capability.SourceHash = hash
		if err := service.RegisterCapabilityVersion(ctx, &capability); err != nil {
			t.Fatal(err)
		}
	}
	runner := runtime.NewRunner(service, store, runtime.WithRootContext(ctx), runtime.WithScanInterval(5*time.Millisecond), runtime.WithRetryBackoff(5*time.Millisecond, 20*time.Millisecond))
	if err := runner.StartSupervisor(ctx); err != nil {
		t.Fatal(err)
	}
	defer runner.StopSupervisor()
	workflowRunner := workflowruntime.NewManager(store, service, store, runner, workflowruntime.Config{RootContext: ctx, ScanInterval: 5 * time.Millisecond})
	if err := workflowRunner.StartSupervisor(ctx); err != nil {
		t.Fatal(err)
	}
	defer workflowRunner.StopSupervisor()
	server := httptest.NewServer(api.NewServerWithWorkflow(service, store, runner, workflowRunner, api.AuthConfig{AllowInsecure: true}, func(context.Context) error { return nil }))
	defer server.Close()

	definition := httpWorkflowDefinition(now)
	registered := postJSON(t, server.URL+"/v1/workflows", mustJSON(definition), "http-workflow-definition")
	if registered.StatusCode != http.StatusCreated {
		t.Fatalf("HTTP workflow definition status=%d body=%s", registered.StatusCode, registered.Body)
	}
	request := workflow.WorkflowRunRequest{RequestID: "http-workflow-run", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:http-agent", Input: contract.Input{URI: "object://http-root", ContentType: "text/plain"}, BudgetLimitMinor: 3000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
	created := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
	if created.StatusCode != http.StatusAccepted {
		t.Fatalf("HTTP workflow run status=%d body=%s", created.StatusCode, created.Body)
	}
	var envelope struct {
		Run workflow.WorkflowRun `json:"workflow_run"`
	}
	decodeBody(t, created.Body, &envelope)
	var status workflow.WorkflowStatus
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID)
		decodeBody(t, response.Body, &status)
		if status.WorkflowRun != nil && status.WorkflowRun.State == workflow.WorkflowFulfilled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.WorkflowRun == nil || status.WorkflowRun.State != workflow.WorkflowFulfilled {
		t.Logf("workflow status: %#v", status)
		for _, step := range status.Steps {
			if step.ChildEpisodeID != "" {
				t.Logf("child %s: %s", step.StepID, getJSON(t, server.URL+"/v1/episodes/"+step.ChildEpisodeID).Body)
			}
		}
		t.Logf("workflow events: %s", getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID+"/events").Body)
		t.Fatalf("HTTP workflow did not reach FULFILLED")
	}
	artifact := getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID+"/artifact")
	if artifact.StatusCode != http.StatusOK || !strings.Contains(artifact.Body, "SEVMTE8=") {
		t.Fatalf("HTTP workflow artifact was not returned: status=%d body=%s", artifact.StatusCode, artifact.Body)
	}
	if status.FinalArtifact == nil || status.FinalArtifact.ContentType != "text/plain" {
		t.Fatalf("HTTP workflow final metadata missing: %#v", status.FinalArtifact)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) < 4 {
		t.Fatalf("expected initial and paid POST calls for both steps, got methods=%v", methods)
	}
	if methods[0] != http.MethodPost {
		t.Fatalf("merchant workflow did not use POST: %v", methods)
	}
	foundHello := false
	for _, body := range bodies {
		if bytes.Equal(body, []byte("hello")) {
			foundHello = true
		}
	}
	if !foundHello {
		t.Fatalf("step B did not receive materialized HTTP payload hello: bodies=%q", bodies)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func httpWorkflowDefinition(now time.Time) workflow.WorkflowDefinition {
	validatorRef := contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}
	return workflow.WorkflowDefinition{WorkflowID: "http-merchant-workflow", Version: "v1", Name: "HTTP merchant workflow", Input: workflow.WorkflowInputSpec{ContentType: "text/plain", Schema: "schema://input"}, Output: workflow.WorkflowOutputSpec{ContentType: "text/plain", Schema: "schema://output"}, Currency: "USDC", MaxBudgetMinor: 3000000, CreatedAt: now, FactsRef: "workflow-definition://http-merchant-workflow/v1", Steps: []workflow.WorkflowStepDefinition{
		{StepID: "step-a", StepType: workflow.StepAcquireCapability, Capability: workflow.CapabilityRequirement{TaskType: "http-source", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}}, InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://middle", ContentType: "text/plain"}, Validator: validatorRef, MaxBudgetMinor: 1000000, MaxTotalAttempts: 3, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2, TimeoutSeconds: 30},
		{StepID: "step-b", StepType: workflow.StepAcquireCapability, Capability: workflow.CapabilityRequirement{TaskType: "http-transform", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}}, DependsOn: []string{"step-a"}, InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowStepOutput, SourceStepID: "step-a", ContentType: "text/plain"}, RequiredInputSchemaRef: "schema://middle", ExpectedOutput: contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"}, Validator: validatorRef, MaxBudgetMinor: 1000000, MaxTotalAttempts: 3, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2, TimeoutSeconds: 30},
	}}
}

var _ adapters.PaymentStatusAdapter = httpWorkflowPayment{}
var _ adapters.PaymentAdapter = httpWorkflowPayment{}
var _ adapters.DIDAdapter = httpWorkflowDID{}
var _ adapters.EntitlementAdapter = httpWorkflowEntitlement{}
