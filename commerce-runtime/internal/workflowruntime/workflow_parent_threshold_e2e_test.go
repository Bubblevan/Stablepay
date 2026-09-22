package workflowruntime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

func TestWorkflowParentThresholdUsesExistingApprovalBoundary(t *testing.T) {
	root, server := newWorkflowLocalServer(t)
	defer root.Close()
	defer server.Close()

	definition := localDefinition()
	definition.WorkflowID = "parent-threshold-commerce"
	definition.Steps[0].AllowCrossMerchantSwitch = true
	definition.Steps[0].RequireParentConfirmationAboveMinor = 50
	registered := postJSON(t, server.URL+"/v1/workflows", mustJSON(definition), "parent-threshold-definition")
	if registered.StatusCode != http.StatusCreated {
		t.Fatalf("parent threshold definition status=%d body=%s", registered.StatusCode, registered.Body)
	}
	request := workflow.WorkflowRunRequest{RequestID: "parent-threshold-run", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, RequesterDID: "did:stablepay:parent-threshold", Input: contract.Input{URI: "object://parent-threshold", ContentType: "text/plain"}, BudgetLimitMinor: 5000000, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Minute)}
	created := postJSON(t, server.URL+"/v1/workflow-runs", mustJSON(request), request.RequestID)
	if created.StatusCode != http.StatusAccepted {
		t.Fatalf("parent threshold run status=%d body=%s", created.StatusCode, created.Body)
	}
	var envelope struct {
		Run workflow.WorkflowRun `json:"workflow_run"`
	}
	decodeBody(t, created.Body, &envelope)

	var awaiting workflow.WorkflowStatus
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID)
		decodeBody(t, response.Body, &awaiting)
		if awaiting.WorkflowRun != nil && awaiting.WorkflowRun.State == workflow.WorkflowAwaitingParent {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if awaiting.WorkflowRun == nil || awaiting.WorkflowRun.State != workflow.WorkflowAwaitingParent || len(awaiting.Steps) != 2 {
		t.Fatalf("workflow did not pause at parent threshold: %#v", awaiting)
	}
	var childID string
	for _, step := range awaiting.Steps {
		if step.StepID == "step-a" {
			childID = step.ChildEpisodeID
		}
	}
	if childID == "" {
		t.Fatal("parent-threshold child episode was not attached")
	}
	child, err := root.Service.GetEpisode(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.State != "AWAITING_PARENT" {
		t.Fatalf("child did not pause at threshold: %s", child.State)
	}
	intents, err := root.Store.ListPaymentIntents(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 {
		t.Fatalf("parent threshold created a payment intent before approval: %#v", intents)
	}
	var childRequest contract.AcquireCapabilityRequest
	if err := json.Unmarshal(child.ContractSnapshot, &childRequest); err != nil {
		t.Fatal(err)
	}
	if !childRequest.Constraints.AllowCrossMerchantSwitch || childRequest.Constraints.RequireParentConfirmationAboveMinor != 50 {
		t.Fatalf("workflow safety constraints were not propagated: %#v", childRequest.Constraints)
	}
	approvals, err := root.Store.ListParentApprovalRequests(context.Background(), childID)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("expected one durable parent approval, approvals=%#v err=%v", approvals, err)
	}
	if approvals[0].ReasonCode != recovery.ReasonParentConfirmation {
		t.Fatalf("unexpected threshold reason: %s", approvals[0].ReasonCode)
	}
	decision := mustJSON(map[string]any{"approval_id": approvals[0].ApprovalID, "decision": "APPROVE", "actor_ref": "parent:s82"})
	approved := postJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID+"/parent-decisions", decision, approvals[0].ApprovalID)
	if approved.StatusCode != http.StatusAccepted {
		t.Fatalf("parent approval status=%d body=%s", approved.StatusCode, approved.Body)
	}
	var fulfilled workflow.WorkflowStatus
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		response := getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID)
		decodeBody(t, response.Body, &fulfilled)
		if fulfilled.WorkflowRun != nil && fulfilled.WorkflowRun.State == workflow.WorkflowFulfilled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if fulfilled.WorkflowRun == nil || fulfilled.WorkflowRun.State != workflow.WorkflowFulfilled {
		t.Logf("post-approval workflow status: %#v", fulfilled)
		for _, step := range fulfilled.Steps {
			if step.ChildEpisodeID != "" {
				t.Logf("post-approval child %s: %s", step.StepID, getJSON(t, server.URL+"/v1/episodes/"+step.ChildEpisodeID).Body)
			}
		}
		t.Logf("post-approval workflow events: %s", getJSON(t, server.URL+"/v1/workflow-runs/"+envelope.Run.WorkflowRunID+"/events").Body)
		t.Fatalf("approved workflow did not fulfill")
	}
	if fulfilled.WorkflowRun.State != workflow.WorkflowFulfilled || fulfilled.FinalArtifact == nil {
		t.Fatalf("approved workflow did not fulfill with final artifact: %#v", fulfilled)
	}
}
