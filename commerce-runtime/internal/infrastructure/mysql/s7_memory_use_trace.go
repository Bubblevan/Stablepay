package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"gorm.io/gorm"
)

type MemoryUseTraceModel struct {
	MemoryUseTraceID     string    `gorm:"column:memory_use_trace_id;type:varchar(128);primaryKey"`
	EpisodeID            string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_memory_use_trace_episode"`
	ModelDecisionTraceID string    `gorm:"column:model_decision_trace_id;type:varchar(255);not null"`
	ContextHash          string    `gorm:"column:context_hash;type:char(71);not null"`
	RetrievedMemoryRefs  []byte    `gorm:"column:retrieved_memory_refs;type:json"`
	CitedMemoryRefs      []byte    `gorm:"column:cited_memory_refs;type:json"`
	ProposedAction       string    `gorm:"column:proposed_action;type:varchar(64);not null"`
	GuardAccepted        bool      `gorm:"column:guard_accepted;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	FactsRef             string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash          string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (MemoryUseTraceModel) TableName() string { return "memory_use_traces" }

func memoryUseTraceToModel(value *memory.MemoryUseTrace) (*MemoryUseTraceModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, memory.ErrInvalidMemoryUseTrace
	}
	retrieved, err := json.Marshal(value.RetrievedMemoryRefs)
	if err != nil {
		return nil, err
	}
	cited, err := json.Marshal(value.CitedMemoryRefs)
	if err != nil {
		return nil, err
	}
	return &MemoryUseTraceModel{MemoryUseTraceID: value.MemoryUseTraceID, EpisodeID: value.EpisodeID, ModelDecisionTraceID: value.ModelDecisionTraceID, ContextHash: value.ContextHash, RetrievedMemoryRefs: retrieved, CitedMemoryRefs: cited, ProposedAction: value.ProposedAction, GuardAccepted: value.GuardAccepted, CreatedAt: value.CreatedAt, FactsRef: value.FactsRef, PayloadHash: value.PayloadHash}, nil
}

func modelToMemoryUseTrace(row MemoryUseTraceModel) (*memory.MemoryUseTrace, error) {
	value := &memory.MemoryUseTrace{MemoryUseTraceID: row.MemoryUseTraceID, EpisodeID: row.EpisodeID, ModelDecisionTraceID: row.ModelDecisionTraceID, ContextHash: row.ContextHash, ProposedAction: row.ProposedAction, GuardAccepted: row.GuardAccepted, CreatedAt: row.CreatedAt, FactsRef: row.FactsRef, PayloadHash: row.PayloadHash}
	if len(row.RetrievedMemoryRefs) > 0 {
		if err := json.Unmarshal(row.RetrievedMemoryRefs, &value.RetrievedMemoryRefs); err != nil {
			return nil, err
		}
	}
	if len(row.CitedMemoryRefs) > 0 {
		if err := json.Unmarshal(row.CitedMemoryRefs, &value.CitedMemoryRefs); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Store) SaveMemoryUseTrace(ctx context.Context, value *memory.MemoryUseTrace) error {
	model, err := memoryUseTraceToModel(value)
	if err != nil {
		return err
	}
	var existing MemoryUseTraceModel
	lookup := s.db.WithContext(ctx).Where("memory_use_trace_id = ?", model.MemoryUseTraceID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToMemoryUseTrace(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrMemoryUseTraceConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrMemoryUseTraceConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetMemoryUseTrace(ctx context.Context, id string) (*memory.MemoryUseTrace, error) {
	var row MemoryUseTraceModel
	if err := s.db.WithContext(ctx).Where("memory_use_trace_id = ?", id).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToMemoryUseTrace(row)
}

func (s *Store) ListMemoryUseTraces(ctx context.Context, episodeID string) ([]*memory.MemoryUseTrace, error) {
	var rows []MemoryUseTraceModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("created_at ASC, memory_use_trace_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*memory.MemoryUseTrace, 0, len(rows))
	for _, row := range rows {
		value, err := modelToMemoryUseTrace(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].MemoryUseTraceID < result[j].MemoryUseTraceID
	})
	return result, nil
}
