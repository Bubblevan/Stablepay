// Package workflowruntime runs durable WorkflowRuns by creating one existing
// CommerceEpisode for each executable workflow step.
package workflowruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

type ChildEnqueuer interface{ Enqueue(context.Context, string) }

type EpisodeFactReader interface {
	ListLedgerEntries(context.Context, string) ([]*ledger.LedgerEntry, error)
	GetDeliveryArtifact(context.Context, string) (*invocation.DeliveryArtifact, error)
	GetValidationEvidence(context.Context, string) (*invocation.ValidationEvidence, error)
}

type Config struct {
	ScanInterval time.Duration
	RootContext  context.Context
	Clock        func() time.Time
}

type Manager struct {
	store        workflow.Repository
	episodes     *application.Service
	facts        EpisodeFactReader
	childRunner  ChildEnqueuer
	clock        func() time.Time
	rootContext  context.Context
	scanInterval time.Duration
	locks        sync.Map
	started      atomic.Bool
	stopMu       sync.Mutex
	stop         context.CancelFunc
	wg           sync.WaitGroup
}

type CreateRunResult struct {
	Run      *workflow.WorkflowRun
	Replayed bool
}

func NewManager(store workflow.Repository, episodes *application.Service, facts EpisodeFactReader, childRunner ChildEnqueuer, config Config) *Manager {
	root := config.RootContext
	if root == nil {
		root = context.Background()
	}
	clock := config.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	interval := config.ScanInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	return &Manager{store: store, episodes: episodes, facts: facts, childRunner: childRunner, clock: clock, rootContext: root, scanInterval: interval}
}

func (m *Manager) RegisterDefinition(ctx context.Context, definition workflow.WorkflowDefinition) (*workflow.WorkflowDefinition, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("workflow repository is unavailable")
	}
	definition = definition.Normalize()
	// A registration request may omit server-owned metadata. Once an
	// immutable ID/version exists, reuse that metadata before computing the
	// hash so retrying the same registration remains idempotent.
	if definition.WorkflowID != "" && definition.Version != "" && definition.CreatedAt.IsZero() {
		if existing, err := m.store.GetDefinition(ctx, definition.WorkflowID, definition.Version); err == nil && existing != nil {
			definition.CreatedAt = existing.CreatedAt
			if definition.FactsRef == "" {
				definition.FactsRef = existing.FactsRef
			}
		}
	}
	if definition.CreatedAt.IsZero() {
		definition.CreatedAt = m.now()
	}
	if definition.FactsRef == "" {
		definition.FactsRef = "workflow-definition://" + definition.WorkflowID + "/" + definition.Version
	}
	withHash, err := definition.WithComputedHash()
	if err != nil {
		return nil, err
	}
	if err := m.store.SaveDefinition(ctx, &withHash); err != nil {
		return nil, err
	}
	return withHash.Clone(), nil
}

func (m *Manager) GetDefinition(ctx context.Context, workflowID, version string) (*workflow.WorkflowDefinition, error) {
	if strings.TrimSpace(version) == "" {
		return m.store.GetLatestDefinition(ctx, workflowID)
	}
	return m.store.GetDefinition(ctx, workflowID, version)
}

func (m *Manager) CreateRun(ctx context.Context, request workflow.WorkflowRunRequest) (CreateRunResult, error) {
	if m == nil || m.store == nil || m.episodes == nil {
		return CreateRunResult{}, errors.New("workflow runtime is unavailable")
	}
	now := m.now()
	request = normalizeRunRequest(request)
	if request.RequestID == "" || request.WorkflowID == "" || request.RequesterDID == "" || (request.Input.URI == "" && request.Input.Ref == "") || request.Input.ContentType == "" || request.BudgetLimitMinor <= 0 || request.Currency == "" || request.DeadlineAt.IsZero() || !request.DeadlineAt.After(now) {
		return CreateRunResult{}, workflow.ErrInvalidWorkflowRun
	}
	definition, err := m.GetDefinition(ctx, request.WorkflowID, request.WorkflowVersion)
	if err != nil {
		return CreateRunResult{}, err
	}
	if request.WorkflowVersion == "" {
		request.WorkflowVersion = definition.Version
	}
	if request.Currency != definition.Currency || request.Input.ContentType != definition.Input.ContentType {
		return CreateRunResult{}, fmt.Errorf("%w: workflow run currency or root input content type does not match definition", workflow.ErrInvalidWorkflowRun)
	}
	if definition.MaxBudgetMinor > 0 && request.BudgetLimitMinor > definition.MaxBudgetMinor {
		return CreateRunResult{}, fmt.Errorf("%w: run budget exceeds workflow maximum", workflow.ErrInvalidWorkflowRun)
	}
	requestHash, err := hashValue(request)
	if err != nil {
		return CreateRunResult{}, err
	}
	if existing, findErr := m.store.FindRunByRequestID(ctx, request.RequestID); findErr == nil {
		if existing.RequestSnapshotHash != requestHash {
			return CreateRunResult{}, workflow.ErrWorkflowRequestConflict
		}
		return CreateRunResult{Run: existing, Replayed: true}, nil
	}
	order, err := workflow.TopologicalOrder(*definition)
	if err != nil {
		return CreateRunResult{}, err
	}
	runID := "wr:" + shortHash(request.RequestID+"\x00"+definition.DefinitionHash)
	run := &workflow.WorkflowRun{WorkflowRunID: runID, RequestID: request.RequestID, RequestSnapshotHash: requestHash, WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, RequesterDID: request.RequesterDID, ParentSessionID: request.ParentSessionID, State: workflow.WorkflowAccepted, Input: request.Input, Budget: initialBudget(definition.Currency, request.BudgetLimitMinor), DeadlineAt: request.DeadlineAt, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := run.Validate(); err != nil {
		return CreateRunResult{}, err
	}
	steps := make([]*workflow.WorkflowStepRun, 0, len(order))
	for _, stepID := range order {
		steps = append(steps, &workflow.WorkflowStepRun{WorkflowRunID: runID, StepID: stepID, State: workflow.StepPending, Version: 1})
	}
	event, err := m.newEvent(ctx, run, workflow.EventWorkflowCreated, "", "", "workflow-created")
	if err != nil {
		return CreateRunResult{}, err
	}
	if err := m.store.CreateAggregate(ctx, run, steps, event); err != nil {
		if existing, findErr := m.store.FindRunByRequestID(ctx, request.RequestID); findErr == nil && existing.RequestSnapshotHash == requestHash {
			return CreateRunResult{Run: existing, Replayed: true}, nil
		}
		return CreateRunResult{}, err
	}
	return CreateRunResult{Run: run.Clone()}, nil
}

func (m *Manager) GetStatus(ctx context.Context, runID string) (*workflow.WorkflowStatus, error) {
	run, err := m.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	definition, err := m.store.GetDefinition(ctx, run.WorkflowID, run.WorkflowVersion)
	if err != nil {
		return nil, err
	}
	steps, err := m.store.ListStepRuns(ctx, run.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	if budget, budgetErr := m.rebuildBudget(ctx, run, steps); budgetErr == nil {
		run.Budget = budget
	}
	status := &workflow.WorkflowStatus{WorkflowRun: run, Definition: definition, Budget: run.Budget, Steps: steps}
	// The durable WorkflowRun stores only artifact references and hashes. The
	// public status projection may resolve the final validated artifact from
	// the existing Episode fact store without copying its body into workflow
	// state.
	if m.facts != nil && m.episodes != nil {
		if order, orderErr := workflow.TopologicalOrder(*definition); orderErr == nil {
			for index := len(order) - 1; index >= 0; index-- {
				step, found := findStepRun(steps, order[index])
				if !found || step.State != workflow.StepFulfilled || step.ChildEpisodeID == "" || step.DeliveryID == "" {
					continue
				}
				status.FinalArtifact, _ = m.facts.GetDeliveryArtifact(ctx, step.DeliveryID)
				if child, childErr := m.episodes.GetEpisode(ctx, step.ChildEpisodeID); childErr == nil && len(child.ValidationEvidenceRefs) > 0 {
					status.FinalValidation, _ = m.facts.GetValidationEvidence(ctx, child.ValidationEvidenceRefs[len(child.ValidationEvidenceRefs)-1])
				}
				break
			}
		}
	}
	return status, nil
}

func (m *Manager) GetObservability(ctx context.Context, runID string) (*workflow.WorkflowObservability, error) {
	status, err := m.GetStatus(ctx, runID)
	if err != nil {
		return nil, err
	}
	childIDs := make([]string, 0, len(status.Steps))
	for _, step := range status.Steps {
		if step.ChildEpisodeID != "" {
			childIDs = append(childIDs, step.ChildEpisodeID)
		}
	}
	sort.Strings(childIDs)
	return &workflow.WorkflowObservability{WorkflowRunID: status.WorkflowRun.WorkflowRunID, State: status.WorkflowRun.State, DefinitionHash: status.WorkflowRun.DefinitionHash, Budget: status.Budget, Steps: status.Steps, ChildEpisodeIDs: childIDs}, nil
}

func (m *Manager) ListEvents(ctx context.Context, runID string) ([]*workflow.WorkflowEvent, error) {
	return m.store.ListEvents(ctx, runID)
}

func (m *Manager) RunWorkflow(ctx context.Context, runID string) error {
	_, err := m.run(ctx, runID)
	return err
}
func (m *Manager) ResumeWorkflow(ctx context.Context, runID string) error {
	return m.RunWorkflow(ctx, runID)
}

func (m *Manager) StartSupervisor(ctx context.Context) error {
	if m == nil || m.store == nil {
		return errors.New("workflow repository is unavailable")
	}
	if !m.started.CompareAndSwap(false, true) {
		return nil
	}
	if ctx == nil {
		ctx = m.rootContext
	}
	workerCtx, cancel := context.WithCancel(ctx)
	m.stopMu.Lock()
	m.stop = cancel
	m.stopMu.Unlock()
	m.wg.Add(1)
	go m.supervisorLoop(workerCtx)
	return nil
}

func (m *Manager) StopSupervisor() {
	if m == nil {
		return
	}
	m.stopMu.Lock()
	stop := m.stop
	m.stop = nil
	m.stopMu.Unlock()
	if stop != nil {
		stop()
	}
	m.wg.Wait()
	m.started.Store(false)
}

func (m *Manager) SupervisorStarted() bool { return m != nil && m.started.Load() }

func (m *Manager) ResumePersisted(ctx context.Context) error {
	runs, err := m.store.ListRunnableRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run != nil {
			m.Enqueue(ctx, run.WorkflowRunID)
		}
	}
	return nil
}

func (m *Manager) Enqueue(_ context.Context, runID string) {
	if m == nil || strings.TrimSpace(runID) == "" {
		return
	}
	value, _ := m.locks.LoadOrStore("job:"+runID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return
	}
	go func() { defer lock.Unlock(); _, _ = m.run(m.rootContext, runID) }()
}

func (m *Manager) supervisorLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.ResumePersisted(ctx)
		}
	}
}

type runResult struct{ Run *workflow.WorkflowRun }

func (m *Manager) run(ctx context.Context, runID string) (runResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	value, _ := m.locks.LoadOrStore("run:"+runID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	run, err := m.store.GetRun(ctx, runID)
	if err != nil {
		return runResult{}, err
	}
	if workflow.IsTerminalRun(run.State) {
		return runResult{Run: run}, nil
	}
	if !m.now().Before(run.DeadlineAt) {
		return m.expire(ctx, run)
	}
	definition, err := m.store.GetDefinition(ctx, run.WorkflowID, run.WorkflowVersion)
	if err != nil {
		return runResult{}, err
	}
	steps, err := m.store.ListStepRuns(ctx, run.WorkflowRunID)
	if err != nil {
		return runResult{}, err
	}
	budget, err := m.rebuildBudget(ctx, run, steps)
	if err != nil {
		return m.fail(ctx, run, "budget_projection", err)
	}
	run.Budget = budget

	for _, step := range steps {
		if step == nil || step.ChildEpisodeID == "" || (step.State != workflow.StepRunning && step.State != workflow.StepReady) {
			continue
		}
		child, childErr := m.episodes.GetEpisode(ctx, step.ChildEpisodeID)
		if childErr != nil {
			return runResult{}, childErr
		}
		if child.State == episode.StateAwaitingParent {
			if run.State != workflow.WorkflowAwaitingParent {
				run, err = m.commitRunOnly(ctx, run, workflow.WorkflowAwaitingParent, workflow.EventWorkflowAwaitingParent, step.StepID, child.EpisodeID, "awaiting-parent:"+step.StepID)
				if err != nil {
					return runResult{}, err
				}
			}
			return runResult{Run: run}, nil
		}
		if episode.IsTerminal(child.State) {
			if child.State != episode.StateFulfilled {
				return m.failStep(ctx, run, step, fmt.Sprintf("child episode %s ended in %s", child.EpisodeID, child.State))
			}
			if err := m.completeStepFromArtifact(ctx, run, step, child); err != nil {
				return m.failStep(ctx, run, step, err.Error())
			}
			return runResult{Run: run}, nil
		}
		// The child may have been durably attached immediately before a
		// process crash and never reached the in-memory enqueue call. The
		// child runner owns episode execution; re-enqueue is only a prompt and
		// is safe because its own persisted idempotency/CAS rules deduplicate it.
		if m.childRunner != nil {
			m.childRunner.Enqueue(ctx, child.EpisodeID)
		}
		if run.State != workflow.WorkflowRunning {
			run, err = m.commitRunOnly(ctx, run, workflow.WorkflowRunning, "", step.StepID, child.EpisodeID, "child-running:"+step.StepID)
			if err != nil {
				return runResult{}, err
			}
		}
		return runResult{Run: run}, nil
	}

	if allStepsFulfilled(steps) {
		return m.fulfill(ctx, run)
	}
	next, ok := nextReadyStep(*definition, steps)
	if !ok {
		return m.fail(ctx, run, "workflow_not_ready", workflow.ErrWorkflowNotReady)
	}
	if next.State == workflow.StepPending {
		run, err = m.markReady(ctx, run, next)
		if err != nil {
			return runResult{}, err
		}
		steps, err = m.store.ListStepRuns(ctx, run.WorkflowRunID)
		if err != nil {
			return runResult{}, err
		}
		next, _ = findStepRun(steps, next.StepID)
	}
	if next.State == workflow.StepReady && next.ChildEpisodeID == "" {
		if run.Budget.AvailableMinor <= 0 {
			return m.fail(ctx, run, "workflow_budget_exhausted", workflow.ErrWorkflowBudget)
		}
		request, inputRef, inputHash, err := m.buildChildRequest(run, *definition, next, steps)
		if err != nil {
			return m.fail(ctx, run, "child_request", err)
		}
		created, err := m.episodes.CreateEpisode(ctx, request)
		if err != nil {
			return m.fail(ctx, run, "child_create", err)
		}
		if err := m.attachChild(ctx, run, next, created.Episode, request.RequestID, inputRef, inputHash); err != nil {
			return runResult{}, err
		}
		if m.childRunner != nil {
			m.childRunner.Enqueue(ctx, created.Episode.EpisodeID)
		}
	}
	return runResult{Run: run}, nil
}

func (m *Manager) markReady(ctx context.Context, run *workflow.WorkflowRun, step *workflow.WorkflowStepRun) (*workflow.WorkflowRun, error) {
	nextRun := run.Clone()
	nextRun.State = workflow.WorkflowRunning
	nextRun.Version++
	nextRun.UpdatedAt = m.now()
	nextStep := step.Clone()
	nextStep.State = workflow.StepReady
	nextStep.Version++
	event, err := m.newEvent(ctx, nextRun, workflow.EventStepReady, step.StepID, "", "ready:"+step.StepID+":"+strconv.FormatUint(nextRun.Version, 10))
	if err != nil {
		return nil, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: nextRun, NextStep: nextStep, ExpectedStepVersion: step.Version, Event: event}); err != nil {
		return nil, err
	}
	return nextRun, nil
}

func (m *Manager) attachChild(ctx context.Context, run *workflow.WorkflowRun, step *workflow.WorkflowStepRun, child *episode.CommerceEpisode, requestID, inputRef, inputHash string) error {
	if child == nil {
		return workflow.ErrInvalidWorkflowRun
	}
	nextRun := run.Clone()
	nextRun.State = workflow.WorkflowRunning
	nextRun.Version++
	nextRun.UpdatedAt = m.now()
	nextStep := step.Clone()
	nextStep.State = workflow.StepRunning
	nextStep.ChildEpisodeID = child.EpisodeID
	nextStep.ChildRequestID = requestID
	nextStep.InputRef = inputRef
	nextStep.InputHash = inputHash
	nextStep.Attempt++
	nextStep.StartedAt = m.now()
	nextStep.Version++
	event, err := m.newEvent(ctx, nextRun, workflow.EventStepEpisodeCreated, step.StepID, child.EpisodeID, "episode-created:"+step.StepID+":"+strconv.Itoa(nextStep.Attempt))
	if err != nil {
		return err
	}
	return m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: nextRun, NextStep: nextStep, ExpectedStepVersion: step.Version, Event: event})
}

func (m *Manager) completeStepFromArtifact(ctx context.Context, run *workflow.WorkflowRun, step *workflow.WorkflowStepRun, child *episode.CommerceEpisode) error {
	if m.facts == nil || len(child.DeliveryRefs) == 0 || len(child.ValidationEvidenceRefs) == 0 {
		return workflow.ErrWorkflowArtifact
	}
	artifact, err := m.facts.GetDeliveryArtifact(ctx, child.DeliveryRefs[len(child.DeliveryRefs)-1])
	if err != nil || artifact == nil {
		return workflow.ErrWorkflowArtifact
	}
	validation, err := m.facts.GetValidationEvidence(ctx, child.ValidationEvidenceRefs[len(child.ValidationEvidenceRefs)-1])
	if err != nil || validation == nil || !validation.Valid || validation.PayloadHash != artifact.PayloadHash {
		return workflow.ErrWorkflowArtifact
	}
	definition, err := m.store.GetDefinition(ctx, run.WorkflowID, run.WorkflowVersion)
	if err != nil {
		return err
	}
	stepDefinition, ok := findStep(*definition, step.StepID)
	if !ok || normalizeType(stepDefinition.ExpectedOutput.ContentType) != normalizeType(artifact.ContentType) {
		return workflow.ErrWorkflowArtifact
	}
	now := m.now()
	nextRun := run.Clone()
	nextRun.State = workflow.WorkflowRunning
	nextRun.Version++
	nextRun.UpdatedAt = now
	nextStep := step.Clone()
	nextStep.State = workflow.StepFulfilled
	nextStep.OutputRef = "workflow-artifact://" + run.WorkflowRunID + "/" + step.StepID
	nextStep.OutputHash = artifact.PayloadHash
	nextStep.ContentType = artifact.ContentType
	nextStep.DeliveryID = artifact.DeliveryID
	nextStep.CompletedAt = &now
	nextStep.Version++
	event, err := m.newEvent(ctx, nextRun, workflow.EventStepFulfilled, step.StepID, child.EpisodeID, "step-fulfilled:"+step.StepID+":"+strconv.FormatUint(nextRun.Version, 10))
	if err != nil {
		return err
	}
	return m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: nextRun, NextStep: nextStep, ExpectedStepVersion: step.Version, Event: event})
}

func (m *Manager) fulfill(ctx context.Context, run *workflow.WorkflowRun) (runResult, error) {
	next := run.Clone()
	next.State = workflow.WorkflowFulfilled
	next.Version++
	next.UpdatedAt = m.now()
	event, err := m.newEvent(ctx, next, workflow.EventWorkflowFulfilled, "", "", "workflow-fulfilled:"+strconv.FormatUint(next.Version, 10))
	if err != nil {
		return runResult{}, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: next, Event: event}); err != nil {
		return runResult{}, err
	}
	return runResult{Run: next}, nil
}

func (m *Manager) failStep(ctx context.Context, run *workflow.WorkflowRun, step *workflow.WorkflowStepRun, reason string) (runResult, error) {
	now := m.now()
	nextStep := step.Clone()
	nextStep.State = workflow.StepFailed
	nextStep.Version++
	nextStep.CompletedAt = &now
	nextRun := run.Clone()
	nextRun.State = workflow.WorkflowFailed
	nextRun.Version++
	nextRun.UpdatedAt = now
	event, err := m.newEvent(ctx, nextRun, workflow.EventStepFailed, step.StepID, step.ChildEpisodeID, "step-failed:"+step.StepID+":"+strconv.FormatUint(nextRun.Version, 10))
	if err != nil {
		return runResult{}, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: nextRun, NextStep: nextStep, ExpectedStepVersion: step.Version, Event: event}); err != nil {
		return runResult{}, err
	}
	return runResult{Run: nextRun}, fmt.Errorf("%s: %w", reason, workflow.ErrWorkflowArtifact)
}

func (m *Manager) fail(ctx context.Context, run *workflow.WorkflowRun, reason string, cause error) (runResult, error) {
	next := run.Clone()
	next.State = workflow.WorkflowFailed
	next.Version++
	next.UpdatedAt = m.now()
	event, err := m.newEvent(ctx, next, workflow.EventWorkflowFailed, "", "", "workflow-failed:"+reason+":"+strconv.FormatUint(next.Version, 10))
	if err != nil {
		return runResult{}, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: next, Event: event}); err != nil {
		return runResult{}, err
	}
	return runResult{Run: next}, cause
}

func (m *Manager) expire(ctx context.Context, run *workflow.WorkflowRun) (runResult, error) {
	next := run.Clone()
	next.State = workflow.WorkflowExpired
	next.Version++
	next.UpdatedAt = m.now()
	event, err := m.newEvent(ctx, next, workflow.EventWorkflowExpired, "", "", "workflow-expired:"+strconv.FormatUint(next.Version, 10))
	if err != nil {
		return runResult{}, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: next, Event: event}); err != nil {
		return runResult{}, err
	}
	return runResult{Run: next}, workflow.ErrWorkflowDeadline
}

func (m *Manager) commitRunOnly(ctx context.Context, run *workflow.WorkflowRun, state, eventType, stepID, childID, key string) (*workflow.WorkflowRun, error) {
	next := run.Clone()
	next.State = state
	next.Version++
	next.UpdatedAt = m.now()
	if eventType == "" {
		eventType = workflow.EventWorkflowCreated
	}
	event, err := m.newEvent(ctx, next, eventType, stepID, childID, key)
	if err != nil {
		return nil, err
	}
	if err := m.store.CommitWorkflowTransition(ctx, workflow.WorkflowTransition{WorkflowRunID: run.WorkflowRunID, ExpectedRunVersion: run.Version, NextRun: next, Event: event}); err != nil {
		return nil, err
	}
	return next, nil
}

func (m *Manager) buildChildRequest(run *workflow.WorkflowRun, definition workflow.WorkflowDefinition, stepRun *workflow.WorkflowStepRun, stepRuns []*workflow.WorkflowStepRun) (contract.AcquireCapabilityRequest, string, string, error) {
	step, ok := findStep(definition, stepRun.StepID)
	if !ok {
		return contract.AcquireCapabilityRequest{}, "", "", workflow.ErrWorkflowNotReady
	}
	input := run.Input
	inputRef := input.Ref
	if inputRef == "" {
		inputRef = input.URI
	}
	if step.InputBinding.Source == workflow.WorkflowStepOutput {
		upstream, ok := findStepRun(stepRuns, step.InputBinding.SourceStepID)
		if !ok || upstream.State != workflow.StepFulfilled || upstream.OutputRef == "" || upstream.OutputHash == "" {
			return contract.AcquireCapabilityRequest{}, "", "", workflow.ErrWorkflowArtifact
		}
		input = contract.Input{Ref: upstream.OutputRef, ContentType: upstream.ContentType, SHA256: strings.TrimPrefix(upstream.OutputHash, "sha256:"), AccessTokenRef: run.Input.AccessTokenRef}
		inputRef = input.Ref
	}
	deadline := run.DeadlineAt
	if step.TimeoutSeconds > 0 {
		candidate := m.now().Add(time.Duration(step.TimeoutSeconds) * time.Second)
		if candidate.Before(deadline) {
			deadline = candidate
		}
	}
	if !deadline.After(m.now()) {
		return contract.AcquireCapabilityRequest{}, "", "", workflow.ErrWorkflowDeadline
	}
	budget := step.MaxBudgetMinor
	if budget > run.Budget.AvailableMinor {
		budget = run.Budget.AvailableMinor
	}
	if budget <= 0 {
		return contract.AcquireCapabilityRequest{}, "", "", workflow.ErrWorkflowBudget
	}
	protocols := append([]string(nil), step.Capability.RequiredProtocolVersions...)
	if len(protocols) == 0 {
		protocols = []string{contract.DefaultProtocolVersion}
	}
	request := contract.AcquireCapabilityRequest{
		RequestID:       "workflow:" + run.WorkflowRunID + ":step:" + step.StepID + ":attempt:" + strconv.Itoa(stepRun.Attempt+1),
		ParentSessionID: run.WorkflowRunID,
		ParentEpisodeID: parentEpisodeID(stepRuns, step),
		WorkflowID:      definition.WorkflowID, WorkflowVersion: definition.Version, WorkflowRunID: run.WorkflowRunID, WorkflowStepID: step.StepID,
		RequesterDID:    run.RequesterDID,
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: step.Capability.TaskType, Description: definition.Name + " / " + step.StepID, SemanticConstraints: append([]contract.KeyValue(nil), step.Capability.SemanticConstraints...)},
		Input:           input,
		Constraints:     contract.Constraints{BudgetLimitMinor: budget, Currency: definition.Currency, DeadlineAt: deadline, SupportedProtocolVersions: protocols, MaxTotalAttempts: step.MaxTotalAttempts, MaxPaymentAttempts: step.MaxPaymentAttempts, MaxDeliveryAttempts: step.MaxDeliveryAttempts},
		ExpectedOutput:  step.ExpectedOutput, Validator: step.Validator,
	}
	if err := request.ValidateAt(m.now()); err != nil {
		return contract.AcquireCapabilityRequest{}, "", "", err
	}
	inputHash, err := hashValue(input)
	if err != nil {
		return contract.AcquireCapabilityRequest{}, "", "", err
	}
	return request, inputRef, inputHash, nil
}

func (m *Manager) rebuildBudget(ctx context.Context, run *workflow.WorkflowRun, steps []*workflow.WorkflowStepRun) (workflow.WorkflowBudgetSnapshot, error) {
	entries := make([]*ledger.LedgerEntry, 0)
	if m.facts != nil {
		for _, step := range steps {
			if step == nil || step.ChildEpisodeID == "" {
				continue
			}
			values, err := m.facts.ListLedgerEntries(ctx, step.ChildEpisodeID)
			if err != nil {
				return workflow.WorkflowBudgetSnapshot{}, err
			}
			entries = append(entries, values...)
		}
	}
	if len(entries) == 0 {
		return initialBudget(run.Budget.Currency, run.Budget.BudgetLimitMinor), nil
	}
	projection, err := ledger.BuildProjection(run.Budget.Currency, run.Budget.BudgetLimitMinor, true, entries)
	if err != nil {
		return workflow.WorkflowBudgetSnapshot{}, err
	}
	return workflow.WorkflowBudgetSnapshot{Currency: projection.Currency, BudgetLimitMinor: projection.BudgetLimitMinor, SettledMinor: projection.SettledAmount, RefundedMinor: projection.RefundedAmount, ConsumedMinor: projection.ConsumedAmount, SunkCostMinor: projection.SunkCost, AvailableMinor: projection.AvailableBudget}, nil
}

func (m *Manager) newEvent(ctx context.Context, run *workflow.WorkflowRun, eventType, stepID, childID, key string) (*workflow.WorkflowEvent, error) {
	values, err := m.store.ListEvents(ctx, run.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	sequence := uint64(len(values) + 1)
	payloadHash, err := hashValue(struct{ Type, Step, Child, State string }{eventType, stepID, childID, run.State})
	if err != nil {
		return nil, err
	}
	return &workflow.WorkflowEvent{EventID: fmt.Sprintf("we:%s:%d", run.WorkflowRunID, sequence), WorkflowRunID: run.WorkflowRunID, Sequence: sequence, Type: eventType, StepID: stepID, ChildEpisodeID: childID, WorkflowVersion: run.WorkflowVersion, DefinitionHash: run.DefinitionHash, OccurredAt: m.now(), FactsRef: fmt.Sprintf("workflow-event://%s/%d", run.WorkflowRunID, sequence), PayloadHash: payloadHash, IdempotencyKey: "workflow:" + run.WorkflowRunID + ":" + key}, nil
}

func (m *Manager) RecordParentDecision(ctx context.Context, runID string, request application.ParentDecisionRequest) (*episode.CommerceEpisode, bool, error) {
	status, err := m.GetStatus(ctx, runID)
	if err != nil {
		return nil, false, err
	}
	for _, step := range status.Steps {
		if step.ChildEpisodeID == "" {
			continue
		}
		child, childErr := m.episodes.GetEpisode(ctx, step.ChildEpisodeID)
		if childErr != nil {
			return nil, false, childErr
		}
		if child.State != episode.StateAwaitingParent {
			continue
		}
		if store, ok := m.facts.(repository.S5Store); ok {
			approval, approvalErr := store.GetParentApprovalRequest(ctx, request.ApprovalID)
			if approvalErr != nil || approval == nil || approval.EpisodeID != child.EpisodeID {
				return nil, false, errors.New("parent approval does not belong to active workflow child")
			}
		}
		result, decisionErr := m.episodes.RecordParentDecision(ctx, request)
		if decisionErr != nil {
			return nil, false, decisionErr
		}
		if m.childRunner != nil {
			m.childRunner.Enqueue(ctx, child.EpisodeID)
		}
		m.Enqueue(ctx, runID)
		return result.Episode, result.Replayed, nil
	}
	return nil, false, errors.New("workflow has no active child awaiting parent")
}

func findStep(definition workflow.WorkflowDefinition, stepID string) (workflow.WorkflowStepDefinition, bool) {
	for _, step := range definition.Steps {
		if step.StepID == stepID {
			return step, true
		}
	}
	return workflow.WorkflowStepDefinition{}, false
}

func findStepRun(steps []*workflow.WorkflowStepRun, stepID string) (*workflow.WorkflowStepRun, bool) {
	for _, step := range steps {
		if step != nil && step.StepID == stepID {
			return step, true
		}
	}
	return nil, false
}

func parentEpisodeID(steps []*workflow.WorkflowStepRun, step workflow.WorkflowStepDefinition) string {
	if step.InputBinding.Source != workflow.WorkflowStepOutput {
		return ""
	}
	upstream, ok := findStepRun(steps, step.InputBinding.SourceStepID)
	if !ok {
		return ""
	}
	return upstream.ChildEpisodeID
}

func nextReadyStep(definition workflow.WorkflowDefinition, steps []*workflow.WorkflowStepRun) (*workflow.WorkflowStepRun, bool) {
	order, err := workflow.TopologicalOrder(definition)
	if err != nil {
		return nil, false
	}
	for _, stepID := range order {
		step, ok := findStepRun(steps, stepID)
		if !ok || step.State != workflow.StepPending {
			continue
		}
		definitionStep, _ := findStep(definition, stepID)
		ready := true
		for _, dependency := range definitionStep.DependsOn {
			dep, exists := findStepRun(steps, dependency)
			if !exists || dep.State != workflow.StepFulfilled {
				ready = false
				break
			}
		}
		if ready {
			return step, true
		}
	}
	return nil, false
}

func allStepsFulfilled(steps []*workflow.WorkflowStepRun) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if step == nil || step.State != workflow.StepFulfilled {
			return false
		}
	}
	return true
}

func initialBudget(currency string, limit int64) workflow.WorkflowBudgetSnapshot {
	return workflow.WorkflowBudgetSnapshot{Currency: strings.ToUpper(strings.TrimSpace(currency)), BudgetLimitMinor: limit, AvailableMinor: limit}
}

func normalizeRunRequest(request workflow.WorkflowRunRequest) workflow.WorkflowRunRequest {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.WorkflowID = strings.TrimSpace(request.WorkflowID)
	request.WorkflowVersion = strings.TrimSpace(request.WorkflowVersion)
	request.RequesterDID = strings.TrimSpace(request.RequesterDID)
	request.ParentSessionID = strings.TrimSpace(request.ParentSessionID)
	request.Input.URI = strings.TrimSpace(request.Input.URI)
	request.Input.Ref = strings.TrimSpace(request.Input.Ref)
	request.Input.ContentType = normalizeType(request.Input.ContentType)
	request.Input.SHA256 = strings.ToLower(strings.TrimSpace(request.Input.SHA256))
	request.Input.AccessTokenRef = strings.TrimSpace(request.Input.AccessTokenRef)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.DeadlineAt = request.DeadlineAt.UTC().Truncate(time.Nanosecond)
	return request
}

func normalizeType(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func (m *Manager) now() time.Time {
	if m.clock == nil {
		return time.Now().UTC()
	}
	return m.clock().UTC()
}

func hashValue(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
