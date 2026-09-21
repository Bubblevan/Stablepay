package workflowruntime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/api"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/livelocal"
	"github.com/stablepay/commerce-runtime/internal/workflow"
	workflowruntime "github.com/stablepay/commerce-runtime/internal/workflowruntime"
)

func TestExternalTwoStepWorkflowLocalE2E(t *testing.T) {
	root, err := livelocal.New(context.Background(), livelocal.Config{MemoryMode: "on", LLMMode: "rule"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	server := httptest.NewServer(root.Handler)
	defer server.Close()
	definition := localDefinition()
	definitionPayload := mustJSON(definition)
	response := postJSON(t, server.URL+"/v1/workflows", definitionPayload, "workflow-definition-register")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("definition status=%d body=%s", response.StatusCode, response.Body)
	}
	var registered struct {
		Definition workflow.WorkflowDefinition `json:"definition"`
	}
	decodeBody(t, response.Body, &registered)
	if registered.Definition.DefinitionHash == "" {
		t.Fatal("definition hash missing")
	}

	request := workflow.WorkflowRunRequest{RequestID: "s8-e2e-run-1", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:s8-agent", Input: contract.Input{URI: "https://example.invalid/root", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(2 * time.Minute)}
	response = postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("run status=%d body=%s", response.StatusCode, response.Body)
	}
	var created struct {
		Run workflow.WorkflowRun `json:"workflow_run"`
	}
	decodeBody(t, response.Body, &created)
	if created.Run.WorkflowRunID == "" {
		t.Fatal("workflow run id missing")
	}

	var status workflow.WorkflowStatus
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response = getJSON(t, server.URL+"/v1/workflow-runs/"+created.Run.WorkflowRunID)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status endpoint=%d body=%s", response.StatusCode, response.Body)
		}
		decodeBody(t, response.Body, &status)
		if status.WorkflowRun.State == workflow.WorkflowFulfilled {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if status.WorkflowRun == nil || status.WorkflowRun.State != workflow.WorkflowFulfilled {
		t.Logf("workflow status: %#v", status)
		for _, step := range status.Steps {
			if step.ChildEpisodeID != "" {
				t.Logf("child %s: %s", step.StepID, getJSON(t, server.URL+"/v1/episodes/"+step.ChildEpisodeID).Body)
			}
		}
		events := getJSON(t, server.URL+"/v1/workflow-runs/"+created.Run.WorkflowRunID+"/events")
		t.Logf("workflow events: %s", events.Body)
		t.Fatalf("workflow did not fulfill: %#v", status.WorkflowRun)
	}
	if len(status.Steps) != 2 || status.Steps[0].State != workflow.StepFulfilled || status.Steps[1].State != workflow.StepFulfilled {
		t.Fatalf("unexpected steps: %#v", status.Steps)
	}
	if status.Budget.SettledMinor != 200 || status.Budget.ConsumedMinor != 200 || status.Budget.AvailableMinor != 4999800 {
		t.Fatalf("unexpected workflow budget: %#v", status.Budget)
	}
	if status.FinalArtifact == nil || string(status.FinalArtifact.Body) != "stablepay live-local artifact" || status.FinalValidation == nil || !status.FinalValidation.Valid {
		t.Fatalf("final validated artifact was not returned by workflow status: artifact=%#v validation=%#v", status.FinalArtifact, status.FinalValidation)
	}

	events := getJSON(t, server.URL+"/v1/workflow-runs/"+created.Run.WorkflowRunID+"/events")
	if events.StatusCode != http.StatusOK {
		t.Fatalf("events status=%d body=%s", events.StatusCode, events.Body)
	}
	if !bytes.Contains([]byte(events.Body), []byte(workflow.EventWorkflowFulfilled)) {
		t.Fatalf("workflow fulfilled event missing: %s", events.Body)
	}
	for _, step := range status.Steps {
		if step.ChildEpisodeID == "" || step.OutputRef == "" || step.OutputHash == "" {
			t.Fatalf("artifact projection missing: %#v", step)
		}
		ep := getJSON(t, server.URL+"/v1/episodes/"+step.ChildEpisodeID)
		if ep.StatusCode != http.StatusOK {
			t.Fatalf("child status=%d body=%s", ep.StatusCode, ep.Body)
		}
		if !bytes.Contains([]byte(ep.Body), []byte(created.Run.WorkflowRunID)) {
			t.Fatalf("child parent session missing: %s", ep.Body)
		}
	}
	var secondInput contract.AcquireCapabilityRequest
	var firstEpisodeID, secondEpisodeID string
	for _, step := range status.Steps {
		child, err := root.Service.GetEpisode(context.Background(), step.ChildEpisodeID)
		if err != nil {
			t.Fatal(err)
		}
		var request contract.AcquireCapabilityRequest
		if err := json.Unmarshal(child.ContractSnapshot, &request); err != nil {
			t.Fatal(err)
		}
		if step.StepID == "step-a" {
			firstEpisodeID = child.EpisodeID
		} else if step.StepID == "step-b" {
			secondInput = request
			secondEpisodeID = child.EpisodeID
		}
	}
	if firstEpisodeID == "" || secondEpisodeID == "" || secondInput.ParentEpisodeID != firstEpisodeID {
		t.Fatalf("step B did not bind the upstream child episode: parent=%q upstream=%q request=%#v", secondInput.ParentEpisodeID, firstEpisodeID, secondInput)
	}
	if secondInput.Input.Ref == "" || secondInput.Input.SHA256 == "" || strings.HasPrefix(secondInput.Input.SHA256, "sha256:") {
		t.Fatalf("step B did not receive a validated artifact reference/hash: %#v", secondInput.Input)
	}
}

func TestExternalMCPWorkflowSurfaceAndAuth(t *testing.T) {
	root, err := livelocal.New(context.Background(), livelocal.Config{MemoryMode: "off", LLMMode: "rule"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	public := httptest.NewServer(root.Handler)
	defer public.Close()
	secure := httptest.NewServer(api.NewServerWithWorkflow(root.Service, root.Store, root.Runner, root.Workflow, api.AuthConfig{Token: "s8-workflow-token"}, func(context.Context) error { return nil }, root.Variant))
	defer secure.Close()

	definition := localDefinition()
	definition.WorkflowID = "mcp-two-step-commerce"
	registered := postJSON(t, public.URL+"/v1/workflows", mustJSON(definition), "mcp-definition")
	if registered.StatusCode != http.StatusCreated {
		t.Fatalf("definition registration status=%d body=%s", registered.StatusCode, registered.Body)
	}
	unauthorized := getJSON(t, secure.URL+"/v1/workflows/"+definition.WorkflowID+"/"+definition.Version)
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("workflow auth status=%d body=%s", unauthorized.StatusCode, unauthorized.Body)
	}
	tools := postAuthenticated(t, secure.URL+"/mcp", "s8-workflow-token", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if tools.StatusCode != http.StatusOK || !strings.Contains(tools.Body, "stablepay.run_workflow") || !strings.Contains(tools.Body, "stablepay.workflow_status") || !strings.Contains(tools.Body, "stablepay.workflow_approve") || strings.Contains(tools.Body, "stablepay.step") {
		t.Fatalf("MCP workflow tools/list failed: %s", tools.Body)
	}
	request := workflow.WorkflowRunRequest{RequestID: "mcp-workflow-run-1", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:mcp-agent", Input: contract.Input{URI: "https://example.invalid/mcp-root", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(2 * time.Minute)}
	arguments := mustJSON(request)
	runCall := mustJSON(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "stablepay.run_workflow", "arguments": json.RawMessage(arguments)}})
	runResponse := postAuthenticated(t, secure.URL+"/mcp", "s8-workflow-token", runCall)
	if runResponse.StatusCode != http.StatusOK || !strings.Contains(runResponse.Body, "workflow_run_id") {
		t.Fatalf("MCP workflow run failed: %s", runResponse.Body)
	}
	var runEnvelope struct {
		Result struct {
			StructuredContent struct {
				Run workflow.WorkflowRun `json:"workflow_run"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	decodeBody(t, runResponse.Body, &runEnvelope)
	if runEnvelope.Result.StructuredContent.Run.WorkflowRunID == "" {
		t.Fatalf("MCP did not return workflow run: %s", runResponse.Body)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		statusCall := mustJSON(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "stablepay.workflow_status", "arguments": map[string]string{"workflow_run_id": runEnvelope.Result.StructuredContent.Run.WorkflowRunID}}})
		statusResponse := postAuthenticated(t, secure.URL+"/mcp", "s8-workflow-token", statusCall)
		if statusResponse.StatusCode != http.StatusOK {
			t.Fatalf("MCP workflow status HTTP %d: %s", statusResponse.StatusCode, statusResponse.Body)
		}
		if strings.Contains(statusResponse.Body, `"state":"FULFILLED"`) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("MCP workflow did not reach FULFILLED")
}

func TestWorkflowRunIdempotencyBudgetAndUpstreamFailure(t *testing.T) {
	t.Run("idempotency", func(t *testing.T) {
		root, server := newWorkflowLocalServer(t)
		defer root.Close()
		defer server.Close()
		definition := localDefinition()
		registerWorkflow(t, server.URL, definition)
		request := workflow.WorkflowRunRequest{RequestID: "s8-idempotency", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:s8-agent", Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
		first := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
		second := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
		if first.StatusCode != http.StatusAccepted || second.StatusCode != http.StatusOK || !strings.Contains(second.Body, `"replayed":true`) {
			t.Fatalf("workflow idempotency failed: first=%d second=%d body=%s", first.StatusCode, second.StatusCode, second.Body)
		}
		request.RequesterDID = "did:stablepay:changed-agent"
		changed := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
		if changed.StatusCode != http.StatusConflict {
			t.Fatalf("changed workflow request status=%d body=%s", changed.StatusCode, changed.Body)
		}
	})

	t.Run("budget exhaustion", func(t *testing.T) {
		root, server := newWorkflowLocalServer(t)
		defer root.Close()
		defer server.Close()
		definition := localDefinition()
		definition.WorkflowID = "budget-two-step-commerce"
		registerWorkflow(t, server.URL, definition)
		request := workflow.WorkflowRunRequest{RequestID: "s8-budget-exhaustion", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:s8-agent", Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, BudgetLimitMinor: 1000050, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
		created := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
		if created.StatusCode != http.StatusAccepted {
			t.Fatalf("budget run status=%d body=%s", created.StatusCode, created.Body)
		}
		status := waitWorkflowState(t, server.URL, created.RunID(), workflow.WorkflowFailed)
		if status.Budget.SettledMinor <= 0 || status.Budget.AvailableMinor >= 1000000 || status.Steps[0].State != workflow.StepFulfilled || status.Steps[1].State != workflow.StepFailed {
			t.Fatalf("budget was not projected from the completed child: %#v", status)
		}
	})

	t.Run("upstream failure", func(t *testing.T) {
		root, server := newWorkflowLocalServer(t)
		defer root.Close()
		defer server.Close()
		definition := localDefinition()
		definition.WorkflowID = "upstream-failure-commerce"
		definition.Steps[0].Capability.TaskType = "s8-no-eligible-merchant"
		registerWorkflow(t, server.URL, definition)
		request := workflow.WorkflowRunRequest{RequestID: "s8-upstream-failure", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:s8-agent", Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
		created := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
		if created.StatusCode != http.StatusAccepted {
			t.Fatalf("upstream-failure run status=%d body=%s", created.StatusCode, created.Body)
		}
		status := waitWorkflowState(t, server.URL, created.RunID(), workflow.WorkflowFailed)
		if status.Steps[0].State != workflow.StepFailed || status.Steps[1].ChildEpisodeID != "" {
			t.Fatalf("workflow ran downstream step after upstream failure: %#v", status.Steps)
		}
	})
}

func TestWorkflowSupervisorRestartResumesPersistedRun(t *testing.T) {
	root, server := newWorkflowLocalServer(t)
	defer root.Close()
	defer server.Close()
	definition := localDefinition()
	definition.WorkflowID = "restart-two-step-commerce"
	registerWorkflow(t, server.URL, definition)
	request := workflow.WorkflowRunRequest{RequestID: "s8-restart", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:s8-agent", Input: contract.Input{URI: "object://restart-root", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
	created := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
	if created.StatusCode != http.StatusAccepted {
		t.Fatalf("restart run status=%d body=%s", created.StatusCode, created.Body)
	}
	runID := created.RunID()
	root.Workflow.StopSupervisor()
	restarted := workflowruntime.NewManager(root.Store, root.Service, root.Store, root.Runner, workflowruntime.Config{RootContext: context.Background(), ScanInterval: 5 * time.Millisecond})
	if err := restarted.ResumePersisted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartSupervisor(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.StopSupervisor()
	status := waitWorkflowState(t, server.URL, runID, workflow.WorkflowFulfilled)
	if status.WorkflowRun.WorkflowRunID != runID || len(status.Steps) != 2 {
		t.Fatalf("restarted workflow projection is incomplete: %#v", status)
	}
}

func localDefinition() workflow.WorkflowDefinition {
	validator := contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}
	return workflow.WorkflowDefinition{WorkflowID: "two-step-commerce", Version: "v1", Name: "Two step commerce", Description: "S8 local acceptance workflow", Input: workflow.WorkflowInputSpec{ContentType: "text/plain", Schema: "schema://input"}, Output: workflow.WorkflowOutputSpec{ContentType: "text/plain", Schema: "schema://output"}, Currency: "USDC", MaxBudgetMinor: 5000000, FactsRef: "workflow-definition://two-step-commerce/v1", Steps: []workflow.WorkflowStepDefinition{
		{StepID: "step-a", StepType: workflow.StepAcquireCapability, Capability: workflow.CapabilityRequirement{TaskType: "s8-source", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}}, InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://middle", ContentType: "text/plain"}, Validator: validator, MaxBudgetMinor: 2000000, MaxTotalAttempts: 10, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3, TimeoutSeconds: 60},
		{StepID: "step-b", StepType: workflow.StepAcquireCapability, Capability: workflow.CapabilityRequirement{TaskType: "s8-transform", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}}, DependsOn: []string{"step-a"}, InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowStepOutput, SourceStepID: "step-a", ContentType: "text/plain"}, RequiredInputSchemaRef: "schema://middle", ExpectedOutput: contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"}, Validator: validator, MaxBudgetMinor: 2000000, MaxTotalAttempts: 10, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3, TimeoutSeconds: 60},
	}}
}

type httpResult struct {
	StatusCode int
	Body       string
}

func postJSON(t *testing.T, endpoint string, body []byte, idempotency string) httpResult {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotency)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return httpResult{StatusCode: response.StatusCode, Body: string(data)}
}

func postAuthenticated(t *testing.T, endpoint, token string, body []byte) httpResult {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return httpResult{StatusCode: response.StatusCode, Body: string(data)}
}

func newWorkflowLocalServer(t *testing.T) (*livelocal.Runtime, *httptest.Server) {
	t.Helper()
	root, err := livelocal.New(context.Background(), livelocal.Config{MemoryMode: "on", LLMMode: "rule"})
	if err != nil {
		t.Fatal(err)
	}
	return root, httptest.NewServer(root.Handler)
}

func registerWorkflow(t *testing.T, endpoint string, definition workflow.WorkflowDefinition) {
	t.Helper()
	response := postJSON(t, endpoint+"/v1/workflows", mustJSON(definition), "definition:"+definition.WorkflowID)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("workflow definition status=%d body=%s", response.StatusCode, response.Body)
	}
}

func waitWorkflowState(t *testing.T, endpoint, runID, expected string) workflow.WorkflowStatus {
	t.Helper()
	var status workflow.WorkflowStatus
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		response := getJSON(t, endpoint+"/v1/workflow-runs/"+runID)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("workflow status=%d body=%s", response.StatusCode, response.Body)
		}
		decodeBody(t, response.Body, &status)
		if status.WorkflowRun != nil && status.WorkflowRun.State == expected {
			return status
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("workflow %s did not reach %s: %#v", runID, expected, status.WorkflowRun)
	return status
}

func (r httpResult) RunID() string {
	var payload struct {
		Run workflow.WorkflowRun `json:"workflow_run"`
	}
	if err := json.Unmarshal([]byte(r.Body), &payload); err != nil {
		return ""
	}
	return payload.Run.WorkflowRunID
}
func getJSON(t *testing.T, endpoint string) httpResult {
	t.Helper()
	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return httpResult{StatusCode: response.StatusCode, Body: string(data)}
}
func decodeBody(t *testing.T, body string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}
func mustJSON(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal fixture: %v", err))
	}
	return payload
}
