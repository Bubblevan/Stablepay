package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/workflow"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkflowDefinitionModel struct {
	WorkflowID     string    `gorm:"column:workflow_id;type:varchar(128);primaryKey"`
	Version        string    `gorm:"column:version;type:varchar(64);primaryKey"`
	DefinitionHash string    `gorm:"column:definition_hash;type:char(71);not null;uniqueIndex:uk_workflow_definition_hash"`
	DefinitionJSON []byte    `gorm:"column:definition_json;type:longtext;not null"`
	FactsRef       string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (WorkflowDefinitionModel) TableName() string { return "workflow_definitions" }

type WorkflowRunModel struct {
	WorkflowRunID       string    `gorm:"column:workflow_run_id;type:varchar(128);primaryKey"`
	RequestID           string    `gorm:"column:request_id;type:varchar(128);not null;uniqueIndex:uk_workflow_run_request_id"`
	RequestSnapshotHash string    `gorm:"column:request_snapshot_hash;type:char(71);not null"`
	WorkflowID          string    `gorm:"column:workflow_id;type:varchar(128);not null;index:idx_workflow_run_definition"`
	WorkflowVersion     string    `gorm:"column:workflow_version;type:varchar(64);not null;index:idx_workflow_run_definition"`
	DefinitionHash      string    `gorm:"column:definition_hash;type:char(71);not null"`
	RequesterDID        string    `gorm:"column:requester_did;type:varchar(128);not null"`
	ParentSessionID     string    `gorm:"column:parent_session_id;type:varchar(128)"`
	State               string    `gorm:"column:state;type:varchar(32);not null;index:idx_workflow_run_state"`
	InputJSON           []byte    `gorm:"column:input_json;type:json;not null"`
	Currency            string    `gorm:"column:currency;type:varchar(16);not null"`
	BudgetLimitMinor    int64     `gorm:"column:budget_limit_minor;not null"`
	SettledMinor        int64     `gorm:"column:settled_minor;not null"`
	RefundedMinor       int64     `gorm:"column:refunded_minor;not null"`
	ConsumedMinor       int64     `gorm:"column:consumed_minor;not null"`
	SunkCostMinor       int64     `gorm:"column:sunk_cost_minor;not null"`
	AvailableMinor      int64     `gorm:"column:available_minor;not null"`
	DeadlineAt          time.Time `gorm:"column:deadline_at;not null"`
	Version             uint64    `gorm:"column:version;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;not null"`
}

func (WorkflowRunModel) TableName() string { return "workflow_runs" }

type WorkflowStepRunModel struct {
	WorkflowRunID  string     `gorm:"column:workflow_run_id;type:varchar(128);primaryKey;index:idx_workflow_step_run"`
	StepID         string     `gorm:"column:step_id;type:varchar(128);primaryKey"`
	State          string     `gorm:"column:state;type:varchar(32);not null"`
	ChildEpisodeID string     `gorm:"column:child_episode_id;type:varchar(128);index:idx_workflow_step_child"`
	ChildRequestID string     `gorm:"column:child_request_id;type:varchar(255);index:idx_workflow_step_request"`
	InputRef       string     `gorm:"column:input_ref;type:varchar(512)"`
	InputHash      string     `gorm:"column:input_hash;type:char(71)"`
	OutputRef      string     `gorm:"column:output_ref;type:varchar(512)"`
	OutputHash     string     `gorm:"column:output_hash;type:char(71)"`
	ContentType    string     `gorm:"column:content_type;type:varchar(255)"`
	DeliveryID     string     `gorm:"column:delivery_id;type:varchar(128)"`
	Attempt        int        `gorm:"column:attempt;not null"`
	Version        uint64     `gorm:"column:version;not null"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
}

func (WorkflowStepRunModel) TableName() string { return "workflow_step_runs" }

type WorkflowEventModel struct {
	EventID         string    `gorm:"column:event_id;type:varchar(255);primaryKey"`
	WorkflowRunID   string    `gorm:"column:workflow_run_id;type:varchar(128);not null;uniqueIndex:uk_workflow_event_sequence;index:idx_workflow_event_run"`
	Sequence        uint64    `gorm:"column:sequence;not null;uniqueIndex:uk_workflow_event_sequence"`
	Type            string    `gorm:"column:type;type:varchar(64);not null"`
	StepID          string    `gorm:"column:step_id;type:varchar(128)"`
	ChildEpisodeID  string    `gorm:"column:child_episode_id;type:varchar(128)"`
	WorkflowVersion string    `gorm:"column:workflow_version;type:varchar(64);not null"`
	DefinitionHash  string    `gorm:"column:definition_hash;type:char(71);not null"`
	OccurredAt      time.Time `gorm:"column:occurred_at;not null"`
	FactsRef        string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash     string    `gorm:"column:payload_hash;type:char(71);not null"`
	IdempotencyKey  string    `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex:uk_workflow_event_idempotency"`
}

func (WorkflowEventModel) TableName() string { return "workflow_events" }

func (s *Store) SaveDefinition(ctx context.Context, value *workflow.WorkflowDefinition) error {
	if value == nil || value.Validate() != nil {
		return workflow.ErrInvalidDefinition
	}
	model, err := definitionToModel(value)
	if err != nil {
		return err
	}
	var existing WorkflowDefinitionModel
	query := s.db.WithContext(ctx).Where("workflow_id = ? AND version = ?", value.WorkflowID, value.Version).First(&existing)
	if query.Error == nil {
		if existing.DefinitionHash == value.DefinitionHash {
			return nil
		}
		return workflow.ErrDefinitionConflict
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return query.Error
	}
	return s.db.WithContext(ctx).Create(model).Error
}

func (s *Store) GetDefinition(ctx context.Context, workflowID, version string) (*workflow.WorkflowDefinition, error) {
	var model WorkflowDefinitionModel
	err := s.db.WithContext(ctx).Where("workflow_id = ? AND version = ?", workflowID, version).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToDefinition(model)
}

func (s *Store) GetLatestDefinition(ctx context.Context, workflowID string) (*workflow.WorkflowDefinition, error) {
	values, err := s.ListDefinitions(ctx, workflowID)
	if err != nil || len(values) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, repository.ErrNotFound
	}
	return values[len(values)-1], nil
}

func (s *Store) ListDefinitions(ctx context.Context, workflowID string) ([]*workflow.WorkflowDefinition, error) {
	var models []WorkflowDefinitionModel
	query := s.db.WithContext(ctx)
	if strings.TrimSpace(workflowID) != "" {
		query = query.Where("workflow_id = ?", workflowID)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	values := make([]*workflow.WorkflowDefinition, 0, len(models))
	for _, model := range models {
		value, err := modelToDefinition(model)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Version < values[j].Version })
	return values, nil
}

func (s *Store) CreateAggregate(ctx context.Context, run *workflow.WorkflowRun, steps []*workflow.WorkflowStepRun, event *workflow.WorkflowEvent) error {
	if run == nil || run.Validate() != nil || event == nil || event.Validate() != nil || event.WorkflowRunID != run.WorkflowRunID || event.Sequence != 1 || len(steps) == 0 {
		return workflow.ErrInvalidWorkflowRun
	}
	runModel, err := workflowRunToModel(run)
	if err != nil {
		return err
	}
	tx := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(runModel).Error; err != nil {
			return err
		}
		for _, step := range steps {
			if step == nil || step.WorkflowRunID != run.WorkflowRunID || step.Validate() != nil {
				return workflow.ErrInvalidWorkflowRun
			}
			model, stepErr := workflowStepToModel(step)
			if stepErr != nil {
				return stepErr
			}
			if err := tx.Create(model).Error; err != nil {
				return err
			}
		}
		model := workflowEventToModel(event)
		return tx.Create(model).Error
	})
	if tx != nil {
		return tx
	}
	return nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (*workflow.WorkflowRun, error) {
	var model WorkflowRunModel
	err := s.db.WithContext(ctx).Where("workflow_run_id = ?", runID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToWorkflowRun(model)
}
func (s *Store) FindRunByRequestID(ctx context.Context, requestID string) (*workflow.WorkflowRun, error) {
	var model WorkflowRunModel
	err := s.db.WithContext(ctx).Where("request_id = ?", requestID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToWorkflowRun(model)
}

func (s *Store) UpdateRunCAS(ctx context.Context, runID string, expected uint64, next *workflow.WorkflowRun) error {
	if next == nil || next.WorkflowRunID != runID || next.Version != expected+1 || next.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	model, err := workflowRunToModel(next)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&WorkflowRunModel{}).Where("workflow_run_id = ? AND version = ?", runID, expected).Updates(map[string]any{"state": model.State, "input_json": model.InputJSON, "currency": model.Currency, "budget_limit_minor": model.BudgetLimitMinor, "settled_minor": model.SettledMinor, "refunded_minor": model.RefundedMinor, "consumed_minor": model.ConsumedMinor, "sunk_cost_minor": model.SunkCostMinor, "available_minor": model.AvailableMinor, "deadline_at": model.DeadlineAt, "version": model.Version, "updated_at": model.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return workflow.ErrWorkflowVersionConflict
	}
	return nil
}

func (s *Store) SaveStepRun(ctx context.Context, value *workflow.WorkflowStepRun) error {
	if value == nil || value.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	model, err := workflowStepToModel(value)
	if err != nil {
		return err
	}
	var existing WorkflowStepRunModel
	lookup := s.db.WithContext(ctx).Where("workflow_run_id = ? AND step_id = ?", value.WorkflowRunID, value.StepID).First(&existing).Error
	if lookup == nil {
		if existing.Version == value.Version && existing.State == value.State && existing.ChildEpisodeID == value.ChildEpisodeID && existing.OutputHash == value.OutputHash {
			return nil
		}
		return workflow.ErrWorkflowVersionConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	return s.db.WithContext(ctx).Create(model).Error
}
func (s *Store) GetStepRun(ctx context.Context, runID, stepID string) (*workflow.WorkflowStepRun, error) {
	var model WorkflowStepRunModel
	err := s.db.WithContext(ctx).Where("workflow_run_id = ? AND step_id = ?", runID, stepID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToWorkflowStep(model), nil
}
func (s *Store) ListStepRuns(ctx context.Context, runID string) ([]*workflow.WorkflowStepRun, error) {
	var models []WorkflowStepRunModel
	if err := s.db.WithContext(ctx).Where("workflow_run_id = ?", runID).Order("step_id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	values := make([]*workflow.WorkflowStepRun, 0, len(models))
	for _, model := range models {
		values = append(values, modelToWorkflowStep(model))
	}
	return values, nil
}

func (s *Store) CommitWorkflowTransition(ctx context.Context, transition workflow.WorkflowTransition) error {
	if transition.NextRun == nil || transition.Event == nil || transition.NextRun.WorkflowRunID != transition.WorkflowRunID || transition.NextRun.Version != transition.ExpectedRunVersion+1 || transition.Event.WorkflowRunID != transition.WorkflowRunID || transition.NextRun.Validate() != nil || transition.Event.Validate() != nil {
		return workflow.ErrInvalidWorkflowRun
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingEvent WorkflowEventModel
		if err := tx.Where("workflow_run_id = ? AND idempotency_key = ?", transition.WorkflowRunID, transition.Event.IdempotencyKey).First(&existingEvent).Error; err == nil {
			if existingEvent.PayloadHash == transition.Event.PayloadHash {
				return nil
			}
			return workflow.ErrWorkflowEventConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var current WorkflowRunModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("workflow_run_id = ?", transition.WorkflowRunID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != transition.ExpectedRunVersion {
			return workflow.ErrWorkflowVersionConflict
		}
		var latestEvent WorkflowEventModel
		if err := tx.Where("workflow_run_id = ?", transition.WorkflowRunID).Order("sequence DESC").First(&latestEvent).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		} else if transition.Event.Sequence != latestEvent.Sequence+1 {
			return workflow.ErrWorkflowEventConflict
		}
		nextModel, err := workflowRunToModel(transition.NextRun)
		if err != nil {
			return err
		}
		if err := tx.Model(&WorkflowRunModel{}).Where("workflow_run_id = ? AND version = ?", transition.WorkflowRunID, transition.ExpectedRunVersion).Updates(map[string]any{"state": nextModel.State, "budget_limit_minor": nextModel.BudgetLimitMinor, "settled_minor": nextModel.SettledMinor, "refunded_minor": nextModel.RefundedMinor, "consumed_minor": nextModel.ConsumedMinor, "sunk_cost_minor": nextModel.SunkCostMinor, "available_minor": nextModel.AvailableMinor, "version": nextModel.Version, "updated_at": nextModel.UpdatedAt}).Error; err != nil {
			return err
		}
		if transition.NextStep != nil {
			if transition.NextStep.WorkflowRunID != transition.WorkflowRunID || transition.NextStep.Version != transition.ExpectedStepVersion+1 || transition.NextStep.Validate() != nil {
				return workflow.ErrInvalidWorkflowRun
			}
			var currentStep WorkflowStepRunModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("workflow_run_id = ? AND step_id = ?", transition.WorkflowRunID, transition.NextStep.StepID).First(&currentStep).Error; err != nil {
				return err
			}
			if currentStep.Version != transition.ExpectedStepVersion {
				return workflow.ErrWorkflowVersionConflict
			}
			stepModel, err := workflowStepToModel(transition.NextStep)
			if err != nil {
				return err
			}
			if err := tx.Model(&WorkflowStepRunModel{}).Where("workflow_run_id = ? AND step_id = ? AND version = ?", transition.WorkflowRunID, transition.NextStep.StepID, transition.ExpectedStepVersion).Updates(stepModel).Error; err != nil {
				return err
			}
		}
		return tx.Create(workflowEventToModel(transition.Event)).Error
	})
}

func (s *Store) SaveEvent(ctx context.Context, value *workflow.WorkflowEvent) error {
	if value == nil || value.Validate() != nil {
		return workflow.ErrWorkflowEventConflict
	}
	return s.db.WithContext(ctx).Create(workflowEventToModel(value)).Error
}
func (s *Store) GetEventByIdempotencyKey(ctx context.Context, runID, key string) (*workflow.WorkflowEvent, error) {
	var model WorkflowEventModel
	err := s.db.WithContext(ctx).Where("workflow_run_id = ? AND idempotency_key = ?", runID, key).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToWorkflowEvent(model), nil
}
func (s *Store) ListEvents(ctx context.Context, runID string) ([]*workflow.WorkflowEvent, error) {
	var models []WorkflowEventModel
	if err := s.db.WithContext(ctx).Where("workflow_run_id = ?", runID).Order("sequence ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	values := make([]*workflow.WorkflowEvent, 0, len(models))
	for _, model := range models {
		values = append(values, modelToWorkflowEvent(model))
	}
	return values, nil
}
func (s *Store) ListRunnableRuns(ctx context.Context) ([]*workflow.WorkflowRun, error) {
	var models []WorkflowRunModel
	if err := s.db.WithContext(ctx).Where("state NOT IN ?", []string{workflow.WorkflowFulfilled, workflow.WorkflowFailed, workflow.WorkflowAborted, workflow.WorkflowExpired}).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	values := make([]*workflow.WorkflowRun, 0, len(models))
	for _, model := range models {
		value, err := modelToWorkflowRun(model)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func definitionToModel(value *workflow.WorkflowDefinition) (*WorkflowDefinitionModel, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowDefinitionModel{WorkflowID: value.WorkflowID, Version: value.Version, DefinitionHash: value.DefinitionHash, DefinitionJSON: payload, FactsRef: value.FactsRef, CreatedAt: value.CreatedAt}, nil
}
func modelToDefinition(value WorkflowDefinitionModel) (*workflow.WorkflowDefinition, error) {
	var result workflow.WorkflowDefinition
	if err := json.Unmarshal(value.DefinitionJSON, &result); err != nil {
		return nil, err
	}
	if result.DefinitionHash == "" {
		result.DefinitionHash = value.DefinitionHash
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}
func workflowRunToModel(value *workflow.WorkflowRun) (*WorkflowRunModel, error) {
	payload, err := json.Marshal(value.Input)
	if err != nil {
		return nil, err
	}
	return &WorkflowRunModel{WorkflowRunID: value.WorkflowRunID, RequestID: value.RequestID, RequestSnapshotHash: value.RequestSnapshotHash, WorkflowID: value.WorkflowID, WorkflowVersion: value.WorkflowVersion, DefinitionHash: value.DefinitionHash, RequesterDID: value.RequesterDID, ParentSessionID: value.ParentSessionID, State: value.State, InputJSON: payload, Currency: value.Budget.Currency, BudgetLimitMinor: value.Budget.BudgetLimitMinor, SettledMinor: value.Budget.SettledMinor, RefundedMinor: value.Budget.RefundedMinor, ConsumedMinor: value.Budget.ConsumedMinor, SunkCostMinor: value.Budget.SunkCostMinor, AvailableMinor: value.Budget.AvailableMinor, DeadlineAt: value.DeadlineAt, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, nil
}
func modelToWorkflowRun(value WorkflowRunModel) (*workflow.WorkflowRun, error) {
	var input contract.Input
	if err := json.Unmarshal(value.InputJSON, &input); err != nil {
		return nil, err
	}
	result := &workflow.WorkflowRun{WorkflowRunID: value.WorkflowRunID, RequestID: value.RequestID, RequestSnapshotHash: value.RequestSnapshotHash, WorkflowID: value.WorkflowID, WorkflowVersion: value.WorkflowVersion, DefinitionHash: value.DefinitionHash, RequesterDID: value.RequesterDID, ParentSessionID: value.ParentSessionID, State: value.State, Input: input, Budget: workflow.WorkflowBudgetSnapshot{Currency: value.Currency, BudgetLimitMinor: value.BudgetLimitMinor, SettledMinor: value.SettledMinor, RefundedMinor: value.RefundedMinor, ConsumedMinor: value.ConsumedMinor, SunkCostMinor: value.SunkCostMinor, AvailableMinor: value.AvailableMinor}, DeadlineAt: value.DeadlineAt, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}
func workflowStepToModel(value *workflow.WorkflowStepRun) (*WorkflowStepRunModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, workflow.ErrInvalidWorkflowRun
	}
	return &WorkflowStepRunModel{WorkflowRunID: value.WorkflowRunID, StepID: value.StepID, State: value.State, ChildEpisodeID: value.ChildEpisodeID, ChildRequestID: value.ChildRequestID, InputRef: value.InputRef, InputHash: value.InputHash, OutputRef: value.OutputRef, OutputHash: value.OutputHash, ContentType: value.ContentType, DeliveryID: value.DeliveryID, Attempt: value.Attempt, Version: value.Version, StartedAt: timePtr(value.StartedAt), CompletedAt: value.CompletedAt}, nil
}
func modelToWorkflowStep(value WorkflowStepRunModel) *workflow.WorkflowStepRun {
	result := &workflow.WorkflowStepRun{WorkflowRunID: value.WorkflowRunID, StepID: value.StepID, State: value.State, ChildEpisodeID: value.ChildEpisodeID, ChildRequestID: value.ChildRequestID, InputRef: value.InputRef, InputHash: value.InputHash, OutputRef: value.OutputRef, OutputHash: value.OutputHash, ContentType: value.ContentType, DeliveryID: value.DeliveryID, Attempt: value.Attempt, Version: value.Version, CompletedAt: value.CompletedAt}
	if value.StartedAt != nil {
		result.StartedAt = *value.StartedAt
	}
	return result
}
func workflowEventToModel(value *workflow.WorkflowEvent) *WorkflowEventModel {
	return &WorkflowEventModel{EventID: value.EventID, WorkflowRunID: value.WorkflowRunID, Sequence: value.Sequence, Type: value.Type, StepID: value.StepID, ChildEpisodeID: value.ChildEpisodeID, WorkflowVersion: value.WorkflowVersion, DefinitionHash: value.DefinitionHash, OccurredAt: value.OccurredAt, FactsRef: value.FactsRef, PayloadHash: value.PayloadHash, IdempotencyKey: value.IdempotencyKey}
}
func modelToWorkflowEvent(value WorkflowEventModel) *workflow.WorkflowEvent {
	return &workflow.WorkflowEvent{EventID: value.EventID, WorkflowRunID: value.WorkflowRunID, Sequence: value.Sequence, Type: value.Type, StepID: value.StepID, ChildEpisodeID: value.ChildEpisodeID, WorkflowVersion: value.WorkflowVersion, DefinitionHash: value.DefinitionHash, OccurredAt: value.OccurredAt, FactsRef: value.FactsRef, PayloadHash: value.PayloadHash, IdempotencyKey: value.IdempotencyKey}
}
func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

var _ workflow.Repository = (*Store)(nil)
