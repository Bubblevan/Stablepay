package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

func testWorkflowDefinition(t *testing.T) workflow.WorkflowDefinition {
	t.Helper()
	definition := workflow.WorkflowDefinition{
		WorkflowID: "repository-workflow", Version: "v1", Name: "Repository workflow",
		Input: workflow.WorkflowInputSpec{ContentType: "text/plain"}, Output: workflow.WorkflowOutputSpec{ContentType: "text/plain"},
		Currency: "USDC", MaxBudgetMinor: 1000, CreatedAt: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), FactsRef: "facts://repository-workflow/v1",
		Steps: []workflow.WorkflowStepDefinition{{StepID: "only", StepType: workflow.StepAcquireCapability,
			Capability:     workflow.CapabilityRequirement{TaskType: "repository-test", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain"},
			InputBinding:   workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"},
			ExpectedOutput: contract.ExpectedOutput{Schema: "schema://text", ContentType: "text/plain"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"},
			MaxBudgetMinor: 100, MaxTotalAttempts: 2, MaxPaymentAttempts: 1, MaxDeliveryAttempts: 1, TimeoutSeconds: 30}},
	}
	withHash, err := definition.WithComputedHash()
	if err != nil {
		t.Fatal(err)
	}
	return withHash
}

func TestWorkflowDefinitionVersionIsImmutable(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	definition := testWorkflowDefinition(t)
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatalf("same immutable definition was not idempotent: %v", err)
	}
	changed := definition
	changed.Description = "changed after publication"
	changedHash, err := changed.WithComputedHash()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctx, &changedHash); !errors.Is(err, workflow.ErrDefinitionConflict) {
		t.Fatalf("expected immutable version conflict, got %v", err)
	}
}

func TestWorkflowTransitionCASRequiresNextVersion(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	definition := testWorkflowDefinition(t)
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatal(err)
	}
	run := &workflow.WorkflowRun{WorkflowRunID: "wr:repository-cas", RequestID: "repository-cas", RequestSnapshotHash: "sha256:" + strings.Repeat("a", 64), WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, RequesterDID: "did:stablepay:test", State: workflow.WorkflowAccepted, Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, Budget: workflow.WorkflowBudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 1000, AvailableMinor: 1000}, DeadlineAt: time.Now().UTC().Add(time.Hour), Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	event := &workflow.WorkflowEvent{EventID: "we:repository-cas:1", WorkflowRunID: run.WorkflowRunID, Sequence: 1, Type: workflow.EventWorkflowCreated, WorkflowVersion: run.WorkflowVersion, DefinitionHash: run.DefinitionHash, OccurredAt: time.Now().UTC(), FactsRef: "workflow-event://repository-cas/1", PayloadHash: "sha256:" + strings.Repeat("b", 64), IdempotencyKey: "workflow:repository-cas:created"}
	step := &workflow.WorkflowStepRun{WorkflowRunID: run.WorkflowRunID, StepID: "only", State: workflow.StepPending, Version: 1}
	if err := store.CreateAggregate(ctx, run, []*workflow.WorkflowStepRun{step}, event); err != nil {
		t.Fatal(err)
	}
	next := run.Clone()
	next.State = workflow.WorkflowRunning
	// Keeping version 1 is a stale transition, even though the state changed.
	badEvent := *event
	badEvent.EventID = "we:repository-cas:2"
	badEvent.Sequence = 2
	badEvent.Type = workflow.EventStepReady
	badEvent.IdempotencyKey = "workflow:repository-cas:ready"
	if err := store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: next, Event: &badEvent}); !errors.Is(err, workflow.ErrWorkflowVersionConflict) {
		t.Fatalf("expected CAS version conflict, got %v", err)
	}
}
