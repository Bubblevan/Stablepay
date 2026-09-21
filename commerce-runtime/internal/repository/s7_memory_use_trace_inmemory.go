package repository

import (
	"context"
	"reflect"
	"sort"

	"github.com/stablepay/commerce-runtime/internal/memory"
)

func (s *InMemoryStore) SaveMemoryUseTrace(ctx context.Context, value *memory.MemoryUseTrace) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return memory.ErrInvalidMemoryUseTrace
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.memoryUseTraces[value.MemoryUseTraceID]; ok {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return ErrMemoryUseTraceConflict
	}
	s.memoryUseTraces[value.MemoryUseTraceID] = value.Clone()
	return nil
}

func (s *InMemoryStore) GetMemoryUseTrace(ctx context.Context, id string) (*memory.MemoryUseTrace, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.memoryUseTraces[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) ListMemoryUseTraces(ctx context.Context, episodeID string) ([]*memory.MemoryUseTrace, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*memory.MemoryUseTrace, 0)
	for _, value := range s.memoryUseTraces {
		if err := value.Validate(); err != nil {
			return nil, err
		}
		if value.EpisodeID == episodeID {
			result = append(result, value.Clone())
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].MemoryUseTraceID < result[j].MemoryUseTraceID
	})
	return result, nil
}

var _ memory.MemoryUseTraceStore = (*InMemoryStore)(nil)
