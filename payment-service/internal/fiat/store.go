package fiat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// FileStore is a single-process durable store for the local MVP. Production
// composition uses SQLStore and the existing payment-service MySQL connection.
type FileStore struct {
	mu     sync.Mutex
	path   string
	orders map[string]Order
}

func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, orders: map[string]Order{}}
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &s.orders)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.orders == nil {
		return nil, errors.New("invalid fiat order snapshot")
	}
	return s, nil
}
func (s *FileStore) Update(change func(map[string]Order) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(s.orders)
	if err != nil {
		return err
	}
	var next map[string]Order
	if err = json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err = change(next); err != nil {
		return err
	}
	updated, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if string(raw) == string(mustJSON(next)) {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "fiat-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(updated)
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
	s.orders = next
	return nil
}
func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
