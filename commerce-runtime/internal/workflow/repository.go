package workflow

import "context"

// Repository is the durable workflow boundary. Implementations must preserve
// immutable definition versions, request idempotency, optimistic run CAS, and
// atomic run/step/event transitions.
type Repository interface {
	SaveDefinition(context.Context, *WorkflowDefinition) error
	GetDefinition(context.Context, string, string) (*WorkflowDefinition, error)
	GetLatestDefinition(context.Context, string) (*WorkflowDefinition, error)
	ListDefinitions(context.Context, string) ([]*WorkflowDefinition, error)

	CreateAggregate(context.Context, *WorkflowRun, []*WorkflowStepRun, *WorkflowEvent) error
	GetRun(context.Context, string) (*WorkflowRun, error)
	FindRunByRequestID(context.Context, string) (*WorkflowRun, error)
	UpdateRunCAS(context.Context, string, uint64, *WorkflowRun) error
	SaveStepRun(context.Context, *WorkflowStepRun) error
	GetStepRun(context.Context, string, string) (*WorkflowStepRun, error)
	ListStepRuns(context.Context, string) ([]*WorkflowStepRun, error)
	CommitWorkflowTransition(context.Context, WorkflowTransition) error
	SaveEvent(context.Context, *WorkflowEvent) error
	GetEventByIdempotencyKey(context.Context, string, string) (*WorkflowEvent, error)
	ListEvents(context.Context, string) ([]*WorkflowEvent, error)
	ListRunnableRuns(context.Context) ([]*WorkflowRun, error)
}

type WorkflowTransition struct {
	WorkflowRunID       string
	ExpectedRunVersion  uint64
	NextRun             *WorkflowRun
	NextStep            *WorkflowStepRun
	ExpectedStepVersion uint64
	Event               *WorkflowEvent
}
