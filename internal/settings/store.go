package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
)

// tmpSeq makes each atomic-write temp file unique. A process id alone is not
// enough when two goroutines save at once, and Windows refuses to share an
// open temp file.
var tmpSeq atomic.Uint64

// store is the file-backed Store implementation. All mutable state is guarded
// by mu; path is fixed at open time and is never modified afterwards.
type store struct {
	mu   sync.RWMutex
	path string
	file File
}

// openStore loads ~/.kbcli/settings.json. A missing file yields an empty
// in-memory store; a malformed file or an unknown schema version is an error.
func openStore() (Store, error) {
	path, err := config.WorkspacePath()
	if err != nil {
		return nil, fmt.Errorf("settings: resolve workspace path: %w", err)
	}
	s := &store{path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.file = File{Version: Version, Workspaces: map[string]Workspace{}}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("settings: parse %s: %w", path, err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("settings: unsupported settings version %d in %s (want %d)", f.Version, path, Version)
	}
	if f.Workspaces == nil {
		f.Workspaces = map[string]Workspace{}
	}
	s.file = f
	return s, nil
}

// Path returns the absolute settings.json path.
func (s *store) Path() string {
	return s.path
}

// File returns a defensive copy of the whole file.
func (s *store) File() File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFile(s.file)
}

// Get returns a defensive copy of one workspace record. A missing root yields
// a ModeGlobal workspace without inserting anything into the store.
func (s *store) Get(root string) Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.file.Workspaces[normalizeRoot(root)]
	if !ok {
		return Workspace{Mode: ModeGlobal, Overrides: map[string]any{}}
	}
	return cloneWorkspace(ws)
}

// Set persists one workspace record atomically, holding the write lock across
// the entire read-modify-write so concurrent callers cannot interleave.
func (s *store) Set(root string, ws Workspace) error {
	key := normalizeRoot(root)
	s.mu.Lock()
	defer s.mu.Unlock()

	if ws.Mode == "" {
		ws.Mode = ModeGlobal
	}
	if ws.Mode != ModeGlobal && ws.Mode != ModeWorkspace {
		return fmt.Errorf("settings: invalid workspace mode %q (want %q or %q)", ws.Mode, ModeGlobal, ModeWorkspace)
	}
	ws.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	newFile := cloneFile(s.file)
	newFile.Workspaces[key] = cloneWorkspace(ws)
	if err := s.writeAtomic(newFile); err != nil {
		return err
	}
	// Commit the in-memory state only after the disk write succeeded.
	s.file = newFile
	return nil
}

// Delete removes one workspace record. Deleting an absent root is a no-op so
// it never creates a file for a store that was loaded from a missing path.
func (s *store) Delete(root string) error {
	key := normalizeRoot(root)
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.file.Workspaces[key]; !ok {
		return nil
	}
	newFile := cloneFile(s.file)
	delete(newFile.Workspaces, key)
	if err := s.writeAtomic(newFile); err != nil {
		return err
	}
	s.file = newFile
	return nil
}

// List returns a defensive copy of every workspace record.
func (s *store) List() map[string]Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Workspace, len(s.file.Workspaces))
	for k, ws := range s.file.Workspaces {
		out[k] = cloneWorkspace(ws)
	}
	return out
}

// Resolve merges the global config with the workspace overrides into a fresh
// Config. It never mutates the caller's global config.
func (s *store) Resolve(global *config.Config, root string) (*config.Config, error) {
	cfg := global.Clone()
	cfg.Normalize()
	key := normalizeRoot(root)
	ws := s.Get(key)
	if ws.Mode != ModeWorkspace {
		return cfg, nil
	}
	if err := applyOverrides(cfg, ws.Overrides); err != nil {
		return nil, fmt.Errorf("settings: resolve workspace %q: %w", key, err)
	}
	cfg.ApplyProfile()
	return cfg, nil
}

// OverrideKeys reports which fields the workspace stores as overrides. Keys are
// reported even when the workspace is in global mode, where they are currently
// inactive but still useful for the UI to display.
func (s *store) OverrideKeys(root string) map[string]bool {
	ws := s.Get(normalizeRoot(root))
	out := make(map[string]bool, len(ws.Overrides))
	for k := range ws.Overrides {
		out[k] = true
	}
	return out
}

// writeAtomic serializes f to a temp file in the same directory, then renames
// it over the real path. The temp file is removed on any failure.
func (s *store) writeAtomic(f File) error {
	f.Version = Version
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("settings: create directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode settings file: %w", err)
	}
	data = append(data, '\n')

	tmp := fmt.Sprintf("%s.tmp-%d-%d", s.path, os.Getpid(), tmpSeq.Add(1))
	if err := writeAndSync(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("settings: rename %s to %s: %w", tmp, s.path, err)
	}
	return nil
}

// writeAndSync creates path with owner-only permissions, writes data, then
// syncs and closes it so the subsequent rename observes durable content.
func writeAndSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("settings: create temp file %s: %w", path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("settings: chmod temp file %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("settings: write temp file %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("settings: sync temp file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("settings: close temp file %s: %w", path, err)
	}
	return nil
}

// normalizeRoot produces a stable key for a workspace directory.
func normalizeRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}

// cloneFile deep-copies a File, including every workspace's Overrides map.
func cloneFile(f File) File {
	out := File{Version: f.Version, Workspaces: make(map[string]Workspace, len(f.Workspaces))}
	for k, ws := range f.Workspaces {
		out.Workspaces[k] = cloneWorkspace(ws)
	}
	return out
}

// cloneWorkspace deep-copies a Workspace, including its Overrides map.
func cloneWorkspace(ws Workspace) Workspace {
	out := ws
	out.Overrides = cloneOverrides(ws.Overrides)
	return out
}

// cloneOverrides deep-copies an Overrides map. A nil source yields a fresh,
// non-nil empty map so callers can always mutate the result safely.
func cloneOverrides(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue recursively copies the container types encoding/json produces so
// nested overrides cannot be mutated through a defensive copy.
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	default:
		return v
	}
}
