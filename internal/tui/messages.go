package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/projanvil/kbcli/internal/agent"
	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/i18n"
	"github.com/projanvil/kbcli/internal/index"
)

// answerMsg carries the result of one agent question back to Update.
type answerMsg struct {
	answer string
	hits   []index.Hit
	err    error
	meta   agent.AnswerMeta
}

// commandResultMsg carries the status line returned by a slash command.
type commandResultMsg struct {
	status string
	err    error
	line   string
}

// settingsLoadedMsg carries the config draft and workspace metadata loaded for
// the settings screen. A nil draft is replaced by config.Default on receipt.
type settingsLoadedMsg struct {
	draft *config.Config
	info  SettingsInfo
	err   error
	gen   uint64
}

// settingsSavedMsg carries the result of persisting the settings draft.
type settingsSavedMsg struct {
	result ApplyResult
	err    error
}

// reindexResultMsg carries the status of a reindex triggered from the settings
// screen. It is deliberately separate from commandResultMsg so the chat screen
// command handler is never disturbed while settings are open.
type reindexResultMsg struct {
	status string
	err    error
}

// errMsg wraps a standalone error that should be shown in the chat pane.
type errMsg struct{ err error }

// First-run index confirmation messages. The index runs inside a tea.Cmd, so
// progress arrives through a channel and a tick drives the spinner.
type indexProgressMsg struct{ p IndexProgress }
type indexDoneMsg struct {
	n   int
	err error
}
type spinTickMsg struct{}

func (m *model) waitIndexProgress() tea.Cmd {
	ch := m.confirm.ch
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return indexProgressMsg{p: p}
	}
}

func spinTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinTickMsg{}
	})
}

func (m *model) startIndex() tea.Cmd {
	root := m.confirm.report.Root
	ch := m.confirm.ch
	run := m.opts.RunIndex
	m.confirm.indexing = true
	m.confirm.progress = IndexProgress{Phase: "scanning"}
	return tea.Batch(
		spinTick(),
		m.waitIndexProgress(),
		func() tea.Msg {
			if run == nil {
				return indexDoneMsg{err: fmt.Errorf("tui: indexing unavailable")}
			}
			n, err := run(root, func(p IndexProgress) {
				select {
				case ch <- p:
				default:
				}
			})
			return indexDoneMsg{n: n, err: err}
		},
	)
}

// Error lets errMsg satisfy the error interface for wrapping/logging.
func (e errMsg) Error() string {
	if e.err == nil {
		return "unknown error"
	}
	return e.err.Error()
}

// askCmd runs the answerer off the Update loop. history is a snapshot supplied
// by the caller so the goroutine never races with subsequent turns.
func askCmd(opts Options, timeout time.Duration, history []agent.Turn, question string) tea.Cmd {
	return func() tea.Msg {
		if opts.Runner == nil {
			lang := i18n.LangEn
			if opts.Config != nil {
				lang = i18n.Parse(opts.Config.Language)
			}
			return answerMsg{err: errors.New(lang.T("chat.no_index"))}
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		answer, hits, err := opts.Runner.Ask(ctx, history, question)
		var meta agent.AnswerMeta
		if reporter, ok := opts.Runner.(agent.MetaAnswerer); ok {
			meta = reporter.LastMeta()
		}
		if err != nil {
			return answerMsg{err: fmt.Errorf("tui: ask: %w", err), meta: meta}
		}
		return answerMsg{answer: answer, hits: hits, meta: meta}
	}
}

// commandCmd runs a slash command off the Update loop so slow commands never
// block rendering.
func commandCmd(opts Options, line string) tea.Cmd {
	return func() tea.Msg {
		if opts.OnCommand == nil {
			return commandResultMsg{line: line, err: fmt.Errorf("tui: no command handler configured")}
		}
		status, err := opts.OnCommand(line)
		if err != nil {
			return commandResultMsg{line: line, status: status, err: fmt.Errorf("tui: command %q: %w", line, err)}
		}
		return commandResultMsg{line: line, status: status}
	}
}

// settingsLoadCmd fetches the effective config draft off the Update loop.
func settingsLoadCmd(opts Options, gen uint64) tea.Cmd {
	return func() tea.Msg {
		if opts.LoadSettings == nil {
			return settingsLoadedMsg{err: fmt.Errorf("tui: settings unavailable"), gen: gen}
		}
		cfg, info, err := opts.LoadSettings()
		if err != nil {
			return settingsLoadedMsg{err: fmt.Errorf("tui: load settings: %w", err), gen: gen}
		}
		return settingsLoadedMsg{draft: cfg, info: info, gen: gen}
	}
}

// settingsSaveCmd persists a snapshot of the draft off the Update loop. The
// draft and overrides are copied by the caller so this goroutine never reads
// state that a later keypress might mutate.
func settingsSaveCmd(opts Options, draft *config.Config, mode string, overrides map[string]bool) tea.Cmd {
	return func() tea.Msg {
		if opts.SaveSettings == nil {
			return settingsSavedMsg{err: fmt.Errorf("tui: settings unavailable")}
		}
		result, err := opts.SaveSettings(draft, mode, overrides)
		if err != nil {
			return settingsSavedMsg{result: result, err: fmt.Errorf("tui: save settings: %w", err)}
		}
		return settingsSavedMsg{result: result}
	}
}

// reindexCmd runs a reindex from the settings screen and reports back through
// reindexResultMsg, keeping the chat command handler untouched.
func reindexCmd(opts Options) tea.Cmd {
	return func() tea.Msg {
		if opts.OnCommand == nil {
			return reindexResultMsg{err: fmt.Errorf("tui: no command handler configured")}
		}
		status, err := opts.OnCommand("reindex")
		if err != nil {
			return reindexResultMsg{status: status, err: fmt.Errorf("tui: reindex: %w", err)}
		}
		return reindexResultMsg{status: status}
	}
}
