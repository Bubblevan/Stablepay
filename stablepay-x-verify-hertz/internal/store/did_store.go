package store

import (
	"sync"

	"stablepay-x-verify-hertz/internal/model"
)

type DIDStore struct {
	mu   sync.RWMutex
	data map[string]model.DIDIdentity
}

func NewDIDStore() *DIDStore {
	return &DIDStore{data: make(map[string]model.DIDIdentity)}
}

func (s *DIDStore) Save(identity model.DIDIdentity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[identity.DID] = identity
}

func (s *DIDStore) Get(did string) (model.DIDIdentity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[did]
	return v, ok
}
