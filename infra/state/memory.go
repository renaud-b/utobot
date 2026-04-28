package state

import (
	"encoding/json"
	"fmt"
	"sync"
)

type MemoryStore struct {
	mu   sync.RWMutex
	data []byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) LoadContext() ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.data) == 0 {
		return nil, false, nil
	}

	return append([]byte(nil), s.data...), true, nil
}

func (s *MemoryStore) SaveContext(data []byte) error {
	if len(data) == 0 {
		data = []byte("null")
	}
	if !json.Valid(data) {
		return fmt.Errorf("context must be valid JSON")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = append([]byte(nil), data...)
	return nil
}

func (s *MemoryStore) UpdateContext(fn func([]byte) ([]byte, error)) error {
	if fn == nil {
		return fmt.Errorf("update function is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	updated, err := fn(append([]byte(nil), s.data...))
	if err != nil {
		return err
	}
	if len(updated) == 0 {
		updated = []byte("null")
	}
	if !json.Valid(updated) {
		return fmt.Errorf("context must be valid JSON")
	}

	s.data = append([]byte(nil), updated...)
	return nil
}

func (s *MemoryStore) Close() error {
	return nil
}
