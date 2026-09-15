package repository

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/stablepay/commerce-runtime/internal/episode"
)

// InMemoryStore is a deterministic repository for unit tests and local
// demonstrations. The mutex models the atomic boundary of the SQL transaction.
type InMemoryStore struct {
	mu        sync.RWMutex
	episodes  map[string]*episode.CommerceEpisode
	byRequest map[string]string
	events    map[string][]*episode.EpisodeEvent
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		episodes:  make(map[string]*episode.CommerceEpisode),
		byRequest: make(map[string]string),
		events:    make(map[string][]*episode.EpisodeEvent),
	}
}

func (s *InMemoryStore) Create(ctx context.Context, value *episode.CommerceEpisode) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; ok {
		return ErrRequestIDConflict
	}
	if existingID, ok := s.byRequest[value.RequestID]; ok && existingID != value.EpisodeID {
		return ErrRequestIDConflict
	}
	s.episodes[value.EpisodeID] = value.Clone()
	s.byRequest[value.RequestID] = value.EpisodeID
	return nil
}

func (s *InMemoryStore) Get(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.episodes[episodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) FindByRequestID(ctx context.Context, requestID string) (*episode.CommerceEpisode, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	episodeID, ok := s.byRequest[requestID]
	if !ok {
		return nil, ErrNotFound
	}
	return s.episodes[episodeID].Clone(), nil
}

func (s *InMemoryStore) UpdateOptimistic(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateLocked(episodeID, expectedVersion, next)
}

func (s *InMemoryStore) Append(ctx context.Context, value *episode.EpisodeEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; !ok {
		return ErrNotFound
	}
	return s.appendLocked(value)
}

func (s *InMemoryStore) ListByEpisode(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.events[episodeID]
	result := make([]*episode.EpisodeEvent, 0, len(values))
	for _, value := range values {
		result = append(result, value.Clone())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, nil
}

func (s *InMemoryStore) FindByIdempotencyKey(ctx context.Context, episodeID, key string) (*episode.EpisodeEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.events[episodeID] {
		if value.Action.IdempotencyKey == key {
			return value.Clone(), nil
		}
	}
	return nil, ErrNotFound
}

func (s *InMemoryStore) CommitTransition(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode, event *episode.EpisodeEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if next == nil || event == nil || event.EpisodeID != episodeID || next.EpisodeID != episodeID {
		return errors.New("transition records do not refer to the same episode")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Sequence != expectedVersion {
		return ErrEventSequenceConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.events[episodeID] {
		if existing.Action.IdempotencyKey == event.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	previous := s.episodes[episodeID].Clone()
	if err := s.updateLocked(episodeID, expectedVersion, next); err != nil {
		return err
	}
	if err := s.appendLocked(event); err != nil {
		// Both maps are only changed under this lock. Restore the projection if
		// the append hits a uniqueness conflict after the optimistic update.
		s.episodes[episodeID] = previous
		return err
	}
	return nil
}

func (s *InMemoryStore) updateLocked(episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	current, ok := s.episodes[episodeID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion {
		return ErrVersionConflict
	}
	if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
		return episode.ErrImmutableContract
	}
	if next.Version != expectedVersion+1 {
		return ErrVersionConflict
	}
	s.episodes[episodeID] = next.Clone()
	return nil
}

func (s *InMemoryStore) appendLocked(value *episode.EpisodeEvent) error {
	if value.Sequence != uint64(len(s.events[value.EpisodeID])+1) {
		return ErrEventSequenceConflict
	}
	for _, existing := range s.events[value.EpisodeID] {
		if existing.Sequence == value.Sequence {
			return ErrEventSequenceConflict
		}
		if existing.Action.IdempotencyKey == value.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	s.events[value.EpisodeID] = append(s.events[value.EpisodeID], value.Clone())
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
