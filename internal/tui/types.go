// Package tui renders the two-pane knowledge-base chat interface and its
// settings screen.
package tui

import (
	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/scan"
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

// ModelProgress is one checkpoint emitted while the managed reranker model is
// downloaded or copied at startup. It mirrors rerankmodel.Progress without
// coupling the TUI to that package.
type ModelProgress struct {
	Phase      string
	File       string
	Downloaded int64
	Total      int64
	FilesDone  int
	FilesTotal int
}

// IndexFunc builds an index for root. It runs inside a tea.Cmd goroutine; the
// progress callback must be non-blocking.
type IndexFunc func(root string, progress func(IndexProgress)) (int, error)

// ReindexFunc rebuilds the index for the current workspace. It runs inside a
// tea.Cmd goroutine; the progress callback must be non-blocking. rembed
// exposes no byte-level progress, so only phase-level checkpoints are emitted.
type ReindexFunc func(progress func(IndexProgress)) (int, error)

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
	// ReindexWithProgress rebuilds the live index and reports phase-level
	// progress. While it runs the TUI is modal and chat input is blocked.
	ReindexWithProgress ReindexFunc
	// EnsureModel provisions the managed reranker model. It is non-nil only
	// when a download or copy is actually needed. When set, the TUI opens on
	// a boot screen with an ASCII logo and a byte-level progress bar, and
	// blocks chat until the call returns.
	EnsureModel func(progress func(ModelProgress)) error
	// NewWindow builds a fresh runner and evidence sink for a new window.
	// Each window needs its own session memory; sharing one runner makes
	// conversations overwrite each other. When nil, the first runner is reused.
	NewWindow func(id int) (agent.Answerer, agent.Sink)
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
