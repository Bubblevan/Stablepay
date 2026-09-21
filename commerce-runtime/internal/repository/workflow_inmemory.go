package repository

import (
	"context"
	"sort"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

func workflowDefinitionKey(workflowID, version string) string {
	return strings.TrimSpace(workflowID) + "\x00" + strings.TrimSpace(version)
}
func workflowStepKey(runID, stepID string) string {
	return strings.TrimSpace(runID) + "\x00" + strings.TrimSpace(stepID)
}

func cloneWorkflowDefinition(value *workflow.WorkflowDefinition) *workflow.WorkflowDefinition {
	if value == nil {
		return nil
	}
	copy := value.Normalize()
	copy.Steps = append([]workflow.WorkflowStepDefinition(nil), value.Steps...)
	for index := range copy.Steps {
		copy.Steps[index].DependsOn = append([]string(nil), value.Steps[index].DependsOn...)
		copy.Steps[index].Capability.RequiredProtocolVersions = append([]string(nil), value.Steps[index].Capability.RequiredProtocolVersions...)
		copy.Steps[index].Capability.SemanticConstraints = append([]contract.KeyValue(nil), value.Steps[index].Capability.SemanticConstraints...)
		copy.Steps[index].ExpectedOutput.SemanticConstraints = append([]contract.KeyValue(nil), value.Steps[index].ExpectedOutput.SemanticConstraints...)
		copy.Steps[index].Validator.Config = append([]contract.KeyValue(nil), value.Steps[index].Validator.Config...)
	}
	return &copy
}

func (s *InMemoryStore) SaveDefinition(ctx context.Context, value *workflow.WorkflowDefinition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil {
		return workflow.ErrInvalidDefinition
	}
	normalized := value.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	key := workflowDefinitionKey(normalized.WorkflowID, normalized.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.workflowDefinitions[key]; ok {
		if existing.DefinitionHash == normalized.DefinitionHash {
			return nil
		}
		return workflow.ErrDefinitionConflict
	}
	s.workflowDefinitions[key] = cloneWorkflowDefinition(&normalized)
	return nil
}

func (s *InMemoryStore) GetDefinition(ctx context.Context, workflowID, version string) (*workflow.WorkflowDefinition, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.workflowDefinitions[workflowDefinitionKey(workflowID, version)]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneWorkflowDefinition(value), nil
}

func (s *InMemoryStore) GetLatestDefinition(ctx context.Context, workflowID string) (*workflow.WorkflowDefinition, error) {
	values, err := s.ListDefinitions(ctx, workflowID)
	if err != nil || len(values) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return values[len(values)-1], nil
}

func (s *InMemoryStore) ListDefinitions(ctx context.Context, workflowID string) ([]*workflow.WorkflowDefinition, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]*workflow.WorkflowDefinition, 0)
	for _, value := range s.workflowDefinitions {
		if value != nil && (strings.TrimSpace(workflowID) == "" || value.WorkflowID == strings.TrimSpace(workflowID)) {
			values = append(values, cloneWorkflowDefinition(value))
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Version < values[j].Version })
	return values, nil
}

func (s *InMemoryStore) CreateAggregate(ctx context.Context, run *workflow.WorkflowRun, steps []*workflow.WorkflowStepRun, event *workflow.WorkflowEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if run == nil || run.Validate() != nil || event == nil || event.Validate() != nil || event.WorkflowRunID != run.WorkflowRunID || event.Sequence != 1 {
		return workflow.ErrInvalidWorkflowRun
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, ok := s.workflowRunsByRequest[run.RequestID]; ok {
		if existingID == run.WorkflowRunID {
			return nil
		}
		return workflow.ErrWorkflowRequestConflict
	}
	if _, ok := s.workflowRuns[run.WorkflowRunID]; ok {
		return workflow.ErrWorkflowRequestConflict
	}
	if len(steps) == 0 {
		return workflow.ErrInvalidWorkflowRun
	}
	for _, step := range steps {
		if step == nil || step.WorkflowRunID != run.WorkflowRunID || step.Validate() != nil {
			return workflow.ErrInvalidWorkflowRun
		}
		key := workflowStepKey(run.WorkflowRunID, step.StepID)
		if _, exists := s.workflowStepRuns[key]; exists {
			return workflow.ErrWorkflowRequestConflict
		}
	}
	s.workflowRuns[run.WorkflowRunID] = run.Clone()
	s.workflowRunsByRequest[run.RequestID] = run.WorkflowRunID
	for _, step := range steps {
		s.workflowStepRuns[workflowStepKey(run.WorkflowRunID, step.StepID)] = step.Clone()
	}
	s.workflowEvents[run.WorkflowRunID] = []*workflow.WorkflowEvent{cloneWorkflowEvent(event)}
	return nil
}

func (s *InMemoryStore) GetRun(ctx context.Context, runID string) (*workflow.WorkflowRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.workflowRuns[strings.TrimSpace(runID)]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) FindRunByRequestID(ctx context.Context, requestID string) (*workflow.WorkflowRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	runID, ok := s.workflowRunsByRequest[strings.TrimSpace(requestID)]
	if !ok {
		return nil, ErrNotFound
	}
	return s.workflowRuns[runID].Clone(), nil
}

func (s *InMemoryStore) UpdateRunCAS(ctx context.Context, runID string, expected uint64, next *workflow.WorkflowRun) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if next == nil || next.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.workflowRuns[runID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expected {
		return workflow.ErrWorkflowVersionConflict
	}
	if next.WorkflowRunID != runID || next.Version != expected+1 {
		return workflow.ErrWorkflowVersionConflict
	}
	s.workflowRuns[runID] = next.Clone()
	return nil
}

func (s *InMemoryStore) SaveStepRun(ctx context.Context, value *workflow.WorkflowStepRun) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflowRuns[value.WorkflowRunID]; !ok {
		return ErrNotFound
	}
	key := workflowStepKey(value.WorkflowRunID, value.StepID)
	if existing, ok := s.workflowStepRuns[key]; ok {
		if existing.Version == value.Version && existing.State == value.State && existing.ChildEpisodeID == value.ChildEpisodeID && existing.OutputHash == value.OutputHash {
			return nil
		}
		return workflow.ErrWorkflowVersionConflict
	}
	s.workflowStepRuns[key] = value.Clone()
	return nil
}

func (s *InMemoryStore) GetStepRun(ctx context.Context, runID, stepID string) (*workflow.WorkflowStepRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.workflowStepRuns[workflowStepKey(runID, stepID)]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) ListStepRuns(ctx context.Context, runID string) ([]*workflow.WorkflowStepRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]*workflow.WorkflowStepRun, 0)
	for key, value := range s.workflowStepRuns {
		if strings.HasPrefix(key, strings.TrimSpace(runID)+"\x00") {
			values = append(values, value.Clone())
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].StepID < values[j].StepID })
	return values, nil
}

func (s *InMemoryStore) CommitWorkflowTransition(ctx context.Context, transition workflow.WorkflowTransition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if transition.NextRun == nil || transition.Event == nil || transition.NextRun.WorkflowRunID != transition.WorkflowRunID || transition.Event.WorkflowRunID != transition.WorkflowRunID || transition.NextRun.Validate() != nil || transition.Event.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.workflowRuns[transition.WorkflowRunID]
	if !ok {
		return ErrNotFound
	}
	for _, existing := range s.workflowEvents[transition.WorkflowRunID] {
		if existing.IdempotencyKey == transition.Event.IdempotencyKey {
			if existing.PayloadHash == transition.Event.PayloadHash {
				return nil
			}
			return workflow.ErrWorkflowEventConflict
		}
	}
	if current.Version != transition.ExpectedRunVersion || transition.NextRun.Version != transition.ExpectedRunVersion+1 {
		return workflow.ErrWorkflowVersionConflict
	}
	if transition.Event.Sequence != uint64(len(s.workflowEvents[transition.WorkflowRunID])+1) {
		return workflow.ErrWorkflowEventConflict
	}
	if transition.NextStep != nil {
		if transition.NextStep.WorkflowRunID != transition.WorkflowRunID || transition.NextStep.Validate() != nil {
			return workflow.ErrInvalidWorkflowRun
		}
		key := workflowStepKey(transition.WorkflowRunID, transition.NextStep.StepID)
		currentStep, exists := s.workflowStepRuns[key]
		if !exists {
			return ErrNotFound
		}
		if currentStep.Version != transition.ExpectedStepVersion || transition.NextStep.Version != transition.ExpectedStepVersion+1 {
			return workflow.ErrWorkflowVersionConflict
		}
		s.workflowStepRuns[key] = transition.NextStep.Clone()
	}
	s.workflowRuns[transition.WorkflowRunID] = transition.NextRun.Clone()
	s.workflowEvents[transition.WorkflowRunID] = append(s.workflowEvents[transition.WorkflowRunID], cloneWorkflowEvent(transition.Event))
	return nil
}

func (s *InMemoryStore) SaveEvent(ctx context.Context, value *workflow.WorkflowEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return workflow.ErrWorkflowEventConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflowRuns[value.WorkflowRunID]; !ok {
		return ErrNotFound
	}
	for _, existing := range s.workflowEvents[value.WorkflowRunID] {
		if existing.IdempotencyKey == value.IdempotencyKey {
			if existing.PayloadHash == value.PayloadHash {
				return nil
			}
			return workflow.ErrWorkflowEventConflict
		}
	}
	if value.Sequence != uint64(len(s.workflowEvents[value.WorkflowRunID])+1) {
		return workflow.ErrWorkflowEventConflict
	}
	s.workflowEvents[value.WorkflowRunID] = append(s.workflowEvents[value.WorkflowRunID], cloneWorkflowEvent(value))
	return nil
}

func (s *InMemoryStore) GetEventByIdempotencyKey(ctx context.Context, runID, key string) (*workflow.WorkflowEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.workflowEvents[strings.TrimSpace(runID)] {
		if value.IdempotencyKey == strings.TrimSpace(key) {
			return cloneWorkflowEvent(value), nil
		}
	}
	return nil, ErrNotFound
}

func (s *InMemoryStore) ListEvents(ctx context.Context, runID string) ([]*workflow.WorkflowEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]*workflow.WorkflowEvent, 0, len(s.workflowEvents[runID]))
	for _, value := range s.workflowEvents[runID] {
		values = append(values, cloneWorkflowEvent(value))
	}
	return values, nil
}

func (s *InMemoryStore) ListRunnableRuns(ctx context.Context) ([]*workflow.WorkflowRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]*workflow.WorkflowRun, 0)
	for _, value := range s.workflowRuns {
		if value != nil && !workflow.IsTerminalRun(value.State) {
			values = append(values, value.Clone())
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
	return values, nil
}

func cloneWorkflowEvent(value *workflow.WorkflowEvent) *workflow.WorkflowEvent {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var _ workflow.Repository = (*InMemoryStore)(nil)
