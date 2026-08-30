package store

import (
	"sync"
	"time"

	"stablepay-x-verify-hertz/internal/model"
)

// XOAuthSessionStore stores OAuth session state
type XOAuthSessionStore struct {
	mu   sync.RWMutex
	data map[string]*model.XOAuthSession // key: state
	byDID map[string]string // key: did, value: state
}

func NewXOAuthSessionStore() *XOAuthSessionStore {
	return &XOAuthSessionStore{
		data:  make(map[string]*model.XOAuthSession),
		byDID: make(map[string]string),
	}
}

func (s *XOAuthSessionStore) Save(session *model.XOAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[session.State] = session
	s.byDID[session.DID] = session.State
}

func (s *XOAuthSessionStore) GetByState(state string) (*model.XOAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[state]
	return v, ok
}

func (s *XOAuthSessionStore) GetByDID(did string) (*model.XOAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.byDID[did]
	if !ok {
		return nil, false
	}
	v, ok := s.data[state]
	return v, ok
}

func (s *XOAuthSessionStore) Update(session *model.XOAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[session.State] = session
}

func (s *XOAuthSessionStore) Delete(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.data[state]; ok {
		delete(s.byDID, session.DID)
		delete(s.data, state)
	}
}

func (s *XOAuthSessionStore) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for state, session := range s.data {
		if now.After(session.ExpiresAt) {
			delete(s.byDID, session.DID)
			delete(s.data, state)
		}
	}
}

// XAccountStore stores X user accounts
type XAccountStore struct {
	mu   sync.RWMutex
	data map[string]*model.XAccount // key: x_user_id
	byUsername map[string]string // key: username, value: x_user_id
}

func NewXAccountStore() *XAccountStore {
	return &XAccountStore{
		data:       make(map[string]*model.XAccount),
		byUsername: make(map[string]string),
	}
}

func (s *XAccountStore) Save(account *model.XAccount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[account.XUserID] = account
	s.byUsername[account.Username] = account.XUserID
}

func (s *XAccountStore) GetByXUserID(xUserID string) (*model.XAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[xUserID]
	return v, ok
}

func (s *XAccountStore) GetByUsername(username string) (*model.XAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	xUserID, ok := s.byUsername[username]
	if !ok {
		return nil, false
	}
	v, ok := s.data[xUserID]
	return v, ok
}

func (s *XAccountStore) Update(account *model.XAccount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[account.XUserID] = account
}

// XBindingStore stores bindings between DID and X accounts
type XBindingStore struct {
	mu          sync.RWMutex
	byDID       map[string]*model.XBinding // key: did
	byXUserID   map[string]*model.XBinding // key: x_user_id
	byUsername  map[string]*model.XBinding // key: username
}

func NewXBindingStore() *XBindingStore {
	return &XBindingStore{
		byDID:      make(map[string]*model.XBinding),
		byXUserID:  make(map[string]*model.XBinding),
		byUsername: make(map[string]*model.XBinding),
	}
}

func (s *XBindingStore) Save(binding *model.XBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byDID[binding.DID] = binding
	s.byXUserID[binding.XUserID] = binding
	s.byUsername[binding.Username] = binding
}

func (s *XBindingStore) GetByDID(did string) (*model.XBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.byDID[did]
	return v, ok
}

func (s *XBindingStore) GetByXUserID(xUserID string) (*model.XBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.byXUserID[xUserID]
	return v, ok
}

func (s *XBindingStore) GetByUsername(username string) (*model.XBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.byUsername[username]
	return v, ok
}

func (s *XBindingStore) List() []*model.XBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.XBinding, 0, len(s.byDID))
	for _, item := range s.byDID {
		out = append(out, item)
	}
	return out
}
