package procurementmvp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type state struct {
	Plans     map[string]Plan     `json:"plans"`
	Quotes    map[string]Quote    `json:"quotes"`
	Purchases map[string]Purchase `json:"purchases"`
}
type Store struct {
	mu   sync.Mutex
	path string
	data state
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, data: state{Plans: map[string]Plan{}, Quotes: map[string]Quote{}, Purchases: map[string]Purchase{}}}
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &s.data)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.data.Plans == nil || s.data.Quotes == nil || s.data.Purchases == nil {
		return nil, errors.New("invalid procurement store")
	}
	return s, nil
}
func (s *Store) transaction(fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	var next state
	if err = json.Unmarshal(before, &next); err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	after, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if string(before) == string(after) {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "procurement-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(after)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, s.path)
	}
	if err != nil {
		return err
	}
	s.data = next
	return nil
}
