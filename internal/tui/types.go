// Package tui renders the two-pane knowledge-base chat interface and its
// settings screen.
package tui

import (
	"github.com/projanvil/kbcli/internal/agent"
	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/index"
	"github.com/projanvil/kbcli/internal/scan"
)

// CommandFunc handles slash-style commands typed into the chat input, such as
// "reindex" and "update <path>". It returns a status line for the chat pane.
type CommandFunc func(line string) (string, error)

// SettingsInfo describes the workspace whose settings are being edited.
type SettingsInfo struct {
	Root        string
	Mode        string
	Overrides   map[string]bool
	Global      *config.Config
	FilePath    string
	IndexStale  bool
	StaleFields []string
}

// ApplyResult is returned after settings are saved.
type ApplyResult struct {
	ReindexRequired bool
	ChangedFields   []string
	Message         string
}

// SettingsLoadFunc returns the effective config draft plus workspace metadata.
type SettingsLoadFunc func() (*config.Config, SettingsInfo, error)

// SettingsSaveFunc persists the draft. overrides marks which fields the
// workspace keeps as its own; unmarked fields stay inherited from global.
type SettingsSaveFunc func(draft *config.Config, mode string, overrides map[string]bool) (ApplyResult, error)

// IndexProgress is one checkpoint emitted while the first index is built.
type IndexProgress struct {
	Phase      string
	FilesDone  int
	FilesTotal int
	Chunks     int
	Current    string
}

// IndexFunc builds an index for root. It runs inside a tea.Cmd goroutine; the
// progress callback must be non-blocking.
type IndexFunc func(root string, progress func(IndexProgress)) (int, error)

// PreflightFunc re-scans a directory when the user chooses a different root.
type PreflightFunc func(root string) (scan.Report, error)

// Options configures a TUI session.
type Options struct {
	Config    *config.Config
	Store     index.Store
	Runner    agent.Answerer
	Sink      agent.Sink
	IndexPath string
	OnCommand CommandFunc
	// Refresh returns the live store and answerer. The TUI calls it after a
	// command rebuilds the index so later questions use the new vectors.
	Refresh func() (index.Store, agent.Answerer)
	// LoadSettings and SaveSettings drive the settings screen. When nil the
	// :settings command reports that settings are unavailable.
	LoadSettings SettingsLoadFunc
	SaveSettings SettingsSaveFunc
	// IndexReport is non-nil only when the session started without a vector
	// file. The confirmation screen is shown before any indexing happens.
	IndexReport *scan.Report
	Preflight   PreflightFunc
	RunIndex    IndexFunc
	// WorkspaceRoot is the directory the session was opened for. The :index
	// command defaults to it, not to the process working directory.
	WorkspaceRoot string
	Version       string
}

// Run starts the bubbletea program and blocks until the user quits.
// Implemented in app.go.
func Run(opts Options) error {
	return run(opts)
}
