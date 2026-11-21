package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// load reads the persisted session. A missing file yields the fresh in-memory
// session; a corrupt file is an error rather than silent data loss.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("session: read %s: %w", s.path, err)
	}
	var loaded Session
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("session: parse %s: %w", s.path, err)
	}
	if loaded.Turns == nil {
		loaded.Turns = make([]Turn, 0, 16)
	}
	if loaded.Facts == nil {
		loaded.Facts = make([]Fact, 0, 8)
	}
	// The workspace identity always comes from the current path, never from a
	// stale file that was copied between workspaces.
	loaded.Workspace = s.data.Workspace
	if loaded.ID == "" {
		loaded.ID = s.data.ID
	}
	if loaded.CreatedAt.IsZero() {
		loaded.CreatedAt = s.data.CreatedAt
	}
	s.data = loaded
	return nil
}

// Save persists the session atomically. Concurrent saves are serialized and
// use unique temp names, so the fire-and-forget fact writer and the immediate
// turn writer never clobber each other.
func (s *Store) Save() error {
	s.mu.RLock()
	if !s.opts.Enabled {
		s.mu.RUnlock()
		return nil
	}
	snapshot := s.data
	snapshot.Turns = append([]Turn(nil), s.data.Turns...)
	snapshot.Facts = append([]Fact(nil), s.data.Facts...)
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ensureDir(s.path); err != nil {
		return fmt.Errorf("session: create dir: %w", err)
	}
	tmp := s.path + ".tmp-" + newID()
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("session: create temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("session: chmod temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("session: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("session: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("session: close temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		cleanup()
		return fmt.Errorf("session: rename %s: %w", filepath.Base(s.path), err)
	}
	return nil
}
