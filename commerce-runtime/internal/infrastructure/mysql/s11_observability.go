package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"gorm.io/gorm"
)

type DecisionOutcomeTraceModel struct {
	DecisionOutcomeTraceID string    `gorm:"column:decision_outcome_trace_id;type:varchar(255);primaryKey"`
	EpisodeID              string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_decision_outcome_episode"`
	ModelDecisionTraceID   string    `gorm:"column:model_decision_trace_id;type:varchar(255);not null"`
	ProposalID             string    `gorm:"column:proposal_id;type:varchar(255);not null"`
	ProposedAction         string    `gorm:"column:proposed_action;type:varchar(64);not null"`
	Stage                  string    `gorm:"column:stage;type:varchar(32);not null"`
	ReachedRuntimeGuard    bool      `gorm:"column:reached_runtime_guard;not null"`
	GuardAccepted          bool      `gorm:"column:guard_accepted;not null"`
	ErrorCode              string    `gorm:"column:error_code;type:varchar(128)"`
	CreatedAt              time.Time `gorm:"column:created_at;not null"`
	FactsRef               string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash            string    `gorm:"column:payload_hash;type:char(71);not null"`
	SideEffectsBefore      []byte    `gorm:"column:side_effects_before;type:json"`
	SideEffectsAfter       []byte    `gorm:"column:side_effects_after;type:json"`
}

func (DecisionOutcomeTraceModel) TableName() string { return "decision_outcome_traces" }

func decisionOutcomeToModel(value *trace.DecisionOutcomeTrace) (*DecisionOutcomeTraceModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, trace.ErrInvalidDecisionOutcomeTrace
	}
	before, err := json.Marshal(value.SideEffectsBefore)
	if err != nil {
		return nil, err
	}
	after, err := json.Marshal(value.SideEffectsAfter)
	if err != nil {
		return nil, err
	}
	return &DecisionOutcomeTraceModel{
		DecisionOutcomeTraceID: value.DecisionOutcomeTraceID,
		EpisodeID:              value.EpisodeID,
		ModelDecisionTraceID:   value.ModelDecisionTraceID,
		ProposalID:             value.ProposalID,
		ProposedAction:         value.ProposedAction,
		Stage:                  string(value.Stage),
		ReachedRuntimeGuard:    value.ReachedRuntimeGuard,
		GuardAccepted:          value.GuardAccepted,
		ErrorCode:              value.ErrorCode,
		CreatedAt:              value.CreatedAt,
		FactsRef:               value.FactsRef,
		PayloadHash:            value.PayloadHash,
		SideEffectsBefore:      before,
		SideEffectsAfter:       after,
	}, nil
}

func modelToDecisionOutcome(value DecisionOutcomeTraceModel) (*trace.DecisionOutcomeTrace, error) {
	result := &trace.DecisionOutcomeTrace{
		DecisionOutcomeTraceID: value.DecisionOutcomeTraceID,
		EpisodeID:              value.EpisodeID,
		ModelDecisionTraceID:   value.ModelDecisionTraceID,
		ProposalID:             value.ProposalID,
		ProposedAction:         value.ProposedAction,
		Stage:                  trace.DecisionStage(value.Stage),
		ReachedRuntimeGuard:    value.ReachedRuntimeGuard,
		GuardAccepted:          value.GuardAccepted,
		ErrorCode:              value.ErrorCode,
		CreatedAt:              value.CreatedAt,
		FactsRef:               value.FactsRef,
		PayloadHash:            value.PayloadHash,
	}
	if len(value.SideEffectsBefore) > 0 && string(value.SideEffectsBefore) != "null" {
		var snapshot trace.GuardSideEffectSnapshot
		if err := json.Unmarshal(value.SideEffectsBefore, &snapshot); err != nil {
			return nil, err
		}
		result.SideEffectsBefore = &snapshot
	}
	if len(value.SideEffectsAfter) > 0 && string(value.SideEffectsAfter) != "null" {
		var snapshot trace.GuardSideEffectSnapshot
		if err := json.Unmarshal(value.SideEffectsAfter, &snapshot); err != nil {
			return nil, err
		}
		result.SideEffectsAfter = &snapshot
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) SaveDecisionOutcomeTrace(ctx context.Context, value *trace.DecisionOutcomeTrace) error {
	model, err := decisionOutcomeToModel(value)
	if err != nil {
		return err
	}
	var existing DecisionOutcomeTraceModel
	lookup := s.db.WithContext(ctx).Where("decision_outcome_trace_id = ?", model.DecisionOutcomeTraceID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToDecisionOutcome(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrDecisionOutcomeConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrDecisionOutcomeConflict
		}
		return err
	}
	return nil
}

func (s *Store) ListDecisionOutcomeTraces(ctx context.Context, episodeID string) ([]*trace.DecisionOutcomeTrace, error) {
	var models []DecisionOutcomeTraceModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("created_at ASC, decision_outcome_trace_id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*trace.DecisionOutcomeTrace, 0, len(models))
	for _, model := range models {
		value, err := modelToDecisionOutcome(model)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].DecisionOutcomeTraceID < result[j].DecisionOutcomeTraceID
	})
	return result, nil
}

func (s *Store) SnapshotSideEffects(ctx context.Context, episodeID string) (trace.GuardSideEffectSnapshot, error) {
	var snapshot trace.GuardSideEffectSnapshot
	count := func(model any, field string, target *int) error {
		var value int64
		if err := s.db.WithContext(ctx).Model(model).Where(field+" = ?", episodeID).Count(&value).Error; err != nil {
			return err
		}
		*target = int(value)
		return nil
	}
	if err := count(&PaymentIntentModel{}, "episode_id", &snapshot.PaymentIntentCount); err != nil {
		return snapshot, err
	}
	var settlements int64
	if err := s.db.WithContext(ctx).Model(&LedgerEntryModel{}).Where("episode_id = ? AND type = ?", episodeID, "PAYMENT_SETTLED").Count(&settlements).Error; err != nil {
		return snapshot, err
	}
	snapshot.SettlementCount = int(settlements)
	if err := count(&MerchantInvocationModel{}, "episode_id", &snapshot.MerchantInvocationCount); err != nil {
		return snapshot, err
	}
	if err := count(&DeliveryArtifactModel{}, "episode_id", &snapshot.DeliveryArtifactCount); err != nil {
		return snapshot, err
	}
	if err := count(&BudgetAmendmentModel{}, "episode_id", &snapshot.BudgetAmendmentCount); err != nil {
		return snapshot, err
	}
	if err := count(&ParentDecisionFactModel{}, "episode_id", &snapshot.ParentDecisionCount); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
