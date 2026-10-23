package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// enableStore persists which plugin ids an admin has enabled.
type enableStore struct {
	mu   sync.Mutex
	path string
	ids  map[string]bool
}

func newEnableStore(path string) (*enableStore, error) {
	s := &enableStore{path: path, ids: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read plugin state: %w", err)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("decode plugin state: %w", err)
	}
	for _, id := range ids {
		s.ids[id] = true
	}
	return s, nil
}

func (s *enableStore) enabled(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ids[id]
}

func (s *enableStore) set(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		s.ids[id] = true
	} else {
		delete(s.ids, id)
	}
	return s.flushLocked()
}

func (s *enableStore) flushLocked() error {
	ids := make([]string, 0, len(s.ids))
	for id := range s.ids {
		ids = append(ids, id)
	}
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create plugin state dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write plugin state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace plugin state: %w", err)
	}
	return nil
}
