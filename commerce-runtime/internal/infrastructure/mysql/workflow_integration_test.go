package mysql

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

func TestMySQLWorkflowRepositoryRoundTripAndCAS(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	definition := workflow.WorkflowDefinition{
		WorkflowID: "mysql-s8-" + suffix, Version: "v1", Name: "MySQL workflow",
		Input: workflow.WorkflowInputSpec{ContentType: "text/plain"}, Output: workflow.WorkflowOutputSpec{ContentType: "text/plain"},
		Currency: "USDC", MaxBudgetMinor: 500, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), FactsRef: "mysql://workflow/" + suffix,
		Steps: []workflow.WorkflowStepDefinition{{StepID: "only", StepType: workflow.StepAcquireCapability,
			Capability:   workflow.CapabilityRequirement{TaskType: "mysql-s8", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain"},
			InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://text", ContentType: "text/plain"},
			Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}, MaxBudgetMinor: 100, MaxTotalAttempts: 2, MaxPaymentAttempts: 1, MaxDeliveryAttempts: 1, TimeoutSeconds: 60}},
	}
	definition, err = definition.WithComputedHash()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Exec("DELETE FROM workflow_events WHERE workflow_run_id = ?", "wr:mysql-s8-"+suffix)
		db.Exec("DELETE FROM workflow_step_runs WHERE workflow_run_id = ?", "wr:mysql-s8-"+suffix)
		db.Exec("DELETE FROM workflow_runs WHERE workflow_run_id = ?", "wr:mysql-s8-"+suffix)
		db.Exec("DELETE FROM workflow_definitions WHERE workflow_id = ? AND version = ?", definition.WorkflowID, definition.Version)
	}()
	readDefinition, err := store.GetDefinition(ctx, definition.WorkflowID, definition.Version)
	if err != nil || readDefinition.DefinitionHash != definition.DefinitionHash {
		t.Fatalf("definition round trip failed: %#v err=%v", readDefinition, err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	runID := "wr:mysql-s8-" + suffix
	run := &workflow.WorkflowRun{WorkflowRunID: runID, RequestID: "mysql-request-" + suffix, RequestSnapshotHash: "sha256:" + strings.Repeat("a", 64), WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, RequesterDID: "did:stablepay:mysql", State: workflow.WorkflowAccepted, Input: contract.Input{URI: "object://mysql-root", ContentType: "text/plain"}, Budget: workflow.WorkflowBudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 500, AvailableMinor: 500}, DeadlineAt: now.Add(time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now}
	event := &workflow.WorkflowEvent{EventID: runID + ":event:1", WorkflowRunID: runID, Sequence: 1, Type: workflow.EventWorkflowCreated, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, OccurredAt: now, FactsRef: "workflow-event://" + runID + "/1", PayloadHash: "sha256:" + strings.Repeat("b", 64), IdempotencyKey: "workflow:" + runID + ":created"}
	step := &workflow.WorkflowStepRun{WorkflowRunID: runID, StepID: "only", State: workflow.StepPending, Version: 1}
	if err := store.CreateAggregate(ctx, run, []*workflow.WorkflowStepRun{step}, event); err != nil {
		t.Fatal(err)
	}
	nextRun := run.Clone()
	nextRun.State = workflow.WorkflowRunning
	nextRun.Version = 2
	nextRun.UpdatedAt = now.Add(time.Second)
	nextStep := step.Clone()
	nextStep.State = workflow.StepReady
	nextStep.Version = 2
	nextEvent := &workflow.WorkflowEvent{EventID: runID + ":event:2", WorkflowRunID: runID, Sequence: 2, Type: workflow.EventStepReady, StepID: step.StepID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, OccurredAt: now.Add(time.Second), FactsRef: "workflow-event://" + runID + "/2", PayloadHash: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: "workflow:" + runID + ":ready"}
	if err := store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: runID, ExpectedRunVersion: 1, NextRun: nextRun, NextStep: nextStep, ExpectedStepVersion: 1, Event: nextEvent}); err != nil {
		t.Fatal(err)
	}
	readRun, err := store.GetRun(ctx, runID)
	if err != nil || readRun.State != workflow.WorkflowRunning || readRun.Version != 2 {
		t.Fatalf("run CAS round trip failed: %#v err=%v", readRun, err)
	}
	readStep, err := store.GetStepRun(ctx, runID, "only")
	if err != nil || readStep.State != workflow.StepReady || readStep.Version != 2 {
		t.Fatalf("step CAS round trip failed: %#v err=%v", readStep, err)
	}
	events, err := store.ListEvents(ctx, runID)
	if err != nil || len(events) != 2 || events[1].Sequence != 2 {
		t.Fatalf("event append-only round trip failed: %#v err=%v", events, err)
	}
}
