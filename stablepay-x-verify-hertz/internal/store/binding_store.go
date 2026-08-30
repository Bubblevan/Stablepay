package store

import (
	"sync"

	"stablepay-x-verify-hertz/internal/model"
)

type BindingStore struct {
	mu           sync.RWMutex
	didToBinding map[string]model.Binding
	xToDID       map[string]string
}

func NewBindingStore() *BindingStore {
	return &BindingStore{
		didToBinding: make(map[string]model.Binding),
		xToDID:       make(map[string]string),
	}
}

func (s *BindingStore) GetByDID(did string) (model.Binding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.didToBinding[did]
	return v, ok
}

func (s *BindingStore) GetDIDByHandle(handle string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.xToDID[handle]
	return v, ok
}

func (s *BindingStore) Save(binding model.Binding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.didToBinding[binding.DID] = binding
	s.xToDID[binding.TwitterHandle] = binding.DID
}

func (s *BindingStore) List() []model.Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Binding, 0, len(s.didToBinding))
	for _, item := range s.didToBinding {
		out = append(out, item)
	}
	return out
}
