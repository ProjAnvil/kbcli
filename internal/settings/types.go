// Package settings stores the workspace-to-configuration relation under
// ~/.kbcli/settings.json.
//
// The global configuration still lives in config.yaml. Each workspace (the
// directory where an index was built) gets one record in this file. A
// workspace in global mode inherits everything; a workspace in workspace mode
// inherits global and applies a sparse set of per-field overrides.
package settings

import "github.com/projanvil/kbcli/internal/config"

const (
	// Version is the current settings.json schema version.
	Version = 1
	// ModeGlobal makes a workspace inherit the global configuration verbatim.
	ModeGlobal = "global"
	// ModeWorkspace applies the workspace overrides on top of global.
	ModeWorkspace = "workspace"
)

// Workspace is one workspace's configuration relation.
type Workspace struct {
	Mode      string         `json:"mode"`
	Overrides map[string]any `json:"overrides,omitempty"`
	IndexPath string         `json:"index_path,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

// File is the on-disk shape of settings.json.
type File struct {
	Version    int                  `json:"version"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

// Store reads and writes the workspace relation file. Implementations must be
// safe for concurrent use.
type Store interface {
	// Path returns the absolute settings.json path.
	Path() string
	// File returns a defensive copy of the whole file.
	File() File
	// Get returns one workspace record. A missing root yields ModeGlobal.
	Get(root string) Workspace
	// Set persists one workspace record atomically.
	Set(root string, ws Workspace) error
	// Delete removes one workspace record.
	Delete(root string) error
	// List returns a defensive copy of all records.
	List() map[string]Workspace
	// Resolve merges global with the workspace overrides into a new Config.
	// It never mutates global.
	Resolve(global *config.Config, root string) (*config.Config, error)
	// OverrideKeys reports which fields the workspace overrides.
	OverrideKeys(root string) map[string]bool
}

// Open loads settings.json. A missing file yields an empty in-memory store.
// Implemented in store.go.
func Open() (Store, error) {
	return openStore()
}

// OverridableKeys lists every field a workspace may override. Implemented in
// overrides.go.
func OverridableKeys() []string {
	return overridableKeys()
}

// Apply copies values from overrides into cfg. Unknown keys and bad types are
// errors. Implemented in overrides.go.
func Apply(cfg *config.Config, overrides map[string]any) error {
	return applyOverrides(cfg, overrides)
}

// NeedsReindex reports which index-affecting fields changed. Implemented in
// overrides.go.
func NeedsReindex(before, after *config.Config) []string {
	return config.IndexAffectingChanges(before, after)
}
