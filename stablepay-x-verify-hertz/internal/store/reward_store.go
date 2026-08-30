package store

import (
	"sync"

	"stablepay-x-verify-hertz/internal/model"
)

type RewardStore struct {
	mu       sync.RWMutex
	rewards  map[string]model.RewardRecord
	balances map[string]float64
}

func NewRewardStore() *RewardStore {
	return &RewardStore{
		rewards:  make(map[string]model.RewardRecord),
		balances: make(map[string]float64),
	}
}

func (s *RewardStore) Save(record model.RewardRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rewards[record.DID] = record
	s.balances[record.DID] += record.Amount
}

func (s *RewardStore) GetByDID(did string) (model.RewardRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.rewards[did]
	return v, ok
}

func (s *RewardStore) GetBalance(did string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.balances[did]
}
