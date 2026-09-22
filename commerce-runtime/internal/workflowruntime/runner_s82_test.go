package workflowruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

type workflowFactsStore struct {
	*repository.InMemoryStore
	ledgerFn func(context.Context, string) ([]*ledger.LedgerEntry, error)
}

func (s *workflowFactsStore) ListLedgerEntries(ctx context.Context, episodeID string) ([]*ledger.LedgerEntry, error) {
	return s.ledgerFn(ctx, episodeID)
}

func workflowS82Definition(t *testing.T, id string) workflow.WorkflowDefinition {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	definition := workflow.WorkflowDefinition{
		WorkflowID: id, Version: "v1", Name: "S8.2 lifecycle", Currency: "USDC", MaxBudgetMinor: 100,
		Input:     workflow.WorkflowInputSpec{ContentType: "text/plain", Schema: "schema://input"},
		Output:    workflow.WorkflowOutputSpec{ContentType: "text/plain", Schema: "schema://output"},
		CreatedAt: now, FactsRef: "workflow-definition://" + id + "/v1",
		Steps: []workflow.WorkflowStepDefinition{{
			StepID: "step-a", StepType: workflow.StepAcquireCapability,
			Capability:     workflow.CapabilityRequirement{TaskType: "s82-task", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}},
			InputBinding:   workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"},
			ExpectedOutput: contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"},
			Validator:      contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}, MaxBudgetMinor: 100,
			MaxTotalAttempts: 2, MaxPaymentAttempts: 1, MaxDeliveryAttempts: 1, TimeoutSeconds: 30,
		}},
	}
	value, err := definition.WithComputedHash()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func workflowS82Request(definition workflow.WorkflowDefinition) workflow.WorkflowRunRequest {
	return workflow.WorkflowRunRequest{
		RequestID: "s82-request-" + definition.WorkflowID, WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version,
		RequesterDID: "did:stablepay:s82", Input: contract.Input{URI: "object://s82", ContentType: "text/plain"},
		BudgetLimitMinor: 100, Currency: "USDC", DeadlineAt: time.Now().UTC().Add(time.Hour),
	}
}

func TestGetStatusFailsClosedOnCorruptBudgetFacts(t *testing.T) {
	ctx := context.Background()
	base := repository.NewInMemoryStore()
	service := application.NewService(base)
	facts := &workflowFactsStore{InMemoryStore: base, ledgerFn: func(context.Context, string) ([]*ledger.LedgerEntry, error) {
		return []*ledger.LedgerEntry{{
			EntryID: "ledger-corrupt", EpisodeID: "child", Sequence: 1, Type: ledger.EntryBudgetReserved,
			Currency: "EUR", AmountMinor: 1, PaymentIntentID: "intent-corrupt", IdempotencyKey: "ledger-corrupt",
			OccurredAt: time.Now().UTC(), ReferenceHash: ledger.HashReference("corrupt"),
		}}, nil
	}}
	manager := NewManager(base, service, facts, nil, Config{RootContext: ctx})
	definition := workflowS82Definition(t, "s82-budget-fail-closed")
	if _, err := manager.RegisterDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	created, err := manager.CreateRun(ctx, workflowS82Request(definition))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RunWorkflow(ctx, created.Run.WorkflowRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetStatus(ctx, created.Run.WorkflowRunID); !errors.Is(err, ledger.ErrProjectionInvariant) {
		t.Fatalf("corrupt budget projection was not rejected: %v", err)
	}
}

type blockingWorkflowFacts struct {
	*repository.InMemoryStore
	started chan struct{}
}

func (s *blockingWorkflowFacts) ListLedgerEntries(ctx context.Context, _ string) ([]*ledger.LedgerEntry, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestWorkflowStopCancelsAndWaitsForWorkers(t *testing.T) {
	ctx := context.Background()
	store := repository.NewInMemoryStore()
	service := application.NewService(store)
	facts := &blockingWorkflowFacts{InMemoryStore: store, started: make(chan struct{}, 1)}
	manager := NewManager(store, service, facts, nil, Config{RootContext: ctx, ScanInterval: time.Hour})
	definition := workflowS82Definition(t, "s82-worker-lifecycle")
	if _, err := manager.RegisterDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	created, err := manager.CreateRun(ctx, workflowS82Request(definition))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartSupervisor(ctx); err != nil {
		t.Fatal(err)
	}
	manager.Enqueue(ctx, created.Run.WorkflowRunID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		steps, listErr := store.ListStepRuns(ctx, created.Run.WorkflowRunID)
		if listErr == nil && len(steps) == 1 && steps[0].ChildEpisodeID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	manager.Enqueue(ctx, created.Run.WorkflowRunID)
	select {
	case <-facts.started:
	case <-time.After(time.Second):
		t.Fatal("workflow worker did not reach a cancellable fact read")
	}
	done := make(chan struct{})
	go func() {
		manager.StopSupervisor()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StopSupervisor did not wait for the canceled worker")
	}
	if manager.SupervisorStarted() || manager.workerCtx != nil || manager.workerAccepting {
		t.Fatal("workflow workers remained accepting after StopSupervisor")
	}
}
