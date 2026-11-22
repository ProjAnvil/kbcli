package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// answerMsg carries the result of one agent question back to Update. win is
// the window index that asked; zero keeps single-window tests unchanged.
type answerMsg struct {
	answer string
	hits   []index.Hit
	err    error
	meta   agent.AnswerMeta
	win    int
}

// answerStreamCh carries streamed answer items out of the producer goroutine.
// Deltas and the terminal result share one channel so ordering is preserved.
type answerStreamCh chan tea.Msg

// answerDeltaMsg carries one streamed text delta. ch lets Update pull the next
// item; a nil ch is allowed in tests that drive a single delta by hand.
type answerDeltaMsg struct {
	text string
	ch   answerStreamCh
	win  int
}

// answerDoneMsg carries the final result of a streamed answer. It is always
// the last item on the stream channel.
type answerDoneMsg struct {
	answer string
	hits   []index.Hit
	err    error
	meta   agent.AnswerMeta
	win    int
}

// waitAnswerStream pulls one item from the stream channel. Update re-arms it
// after every delta so the terminal result is delivered as its own tea.Msg.
func waitAnswerStream(ch answerStreamCh) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// prefixTimeoutMsg cancels a pending window prefix. A short delay keeps the
// prefix from lingering when the user changes their mind.
type prefixTimeoutMsg struct{}

func prefixTimeout() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{}
	})
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

// Reindex modal messages. A buffered channel carries phase-level progress out
// of the reindex goroutine; a tick drives the spinner. The modal is closed by
// reindexDoneMsg on both success and error so the UI can never get stuck.
type reindexProgressMsg struct{ p IndexProgress }
type reindexSpinTickMsg struct{}
type reindexDoneMsg struct {
	n   int
	err error
}

func (m *model) waitReindexProgress() tea.Cmd {
	ch := m.reindexCh
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return reindexProgressMsg{p: p}
	}
}

func reindexSpinTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return reindexSpinTickMsg{}
	})
}

// startReindex switches the model into the modal reindex view immediately so
// no subsequent key can launch a second build. from is the screen to restore
// when the build finishes.
func (m *model) startReindex(from screen) tea.Cmd {
	if m.reindexing {
		return nil
	}
	m.reindexing = true
	m.reindexFrom = from
	m.reindexErr = nil
	m.reindexFrame = 0
	m.reindexProgress = IndexProgress{Phase: "loading_model"}
	if m.reindexCh == nil {
		m.reindexCh = make(chan IndexProgress, 64)
	}
	ch := m.reindexCh
	run := m.opts.ReindexWithProgress
	return tea.Batch(
		reindexSpinTick(),
		m.waitReindexProgress(),
		func() tea.Msg {
			if run == nil {
				return reindexDoneMsg{err: fmt.Errorf("tui: reindex unavailable")}
			}
			n, err := run(func(p IndexProgress) {
				select {
				case ch <- p:
				default:
				}
			})
			return reindexDoneMsg{n: n, err: err}
		},
	)
}

// Startup model provisioning messages. A buffered channel carries byte-level
// progress out of the EnsureModel goroutine; a tick drives the spinner. The
// modal is closed by bootDoneMsg on both success and error.
type bootProgressMsg struct{ p ModelProgress }
type bootSpinTickMsg struct{}
type bootDoneMsg struct{ err error }

func (m *model) waitBootProgress() tea.Cmd {
	ch := m.bootCh
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return bootProgressMsg{p: p}
	}
}

func bootSpinTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return bootSpinTickMsg{}
	})
}

// startBoot switches the model into the modal boot view immediately. Only
// ctrl+c is accepted until bootDoneMsg, so chat cannot open with a missing
// model.
func (m *model) startBoot() tea.Cmd {
	m.booting = true
	m.bootErr = nil
	m.bootFrame = 0
	if m.bootCh == nil {
		m.bootCh = make(chan ModelProgress, 128)
	}
	ch := m.bootCh
	run := m.opts.EnsureModel
	return tea.Batch(
		bootSpinTick(),
		m.waitBootProgress(),
		func() tea.Msg {
			if run == nil {
				return bootDoneMsg{err: fmt.Errorf("tui: model setup unavailable")}
			}
			err := run(func(p ModelProgress) {
				select {
				case ch <- p:
				default:
				}
			})
			return bootDoneMsg{err: err}
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

// askStreamCmd runs a streaming answerer off the Update loop. A buffered
// channel turns each delta into its own tea.Msg and delivers the final result
// last, so partial text never overtakes the completion.
func askStreamCmd(opts Options, timeout time.Duration, sa agent.StreamAnswerer, history []agent.Turn, question string, win int) tea.Cmd {
	ch := make(answerStreamCh, 256)
	produce := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		answer, hits, err := sa.AskStream(ctx, history, question, func(delta string) {
			if delta == "" {
				return
			}
			select {
			case ch <- answerDeltaMsg{text: delta, ch: ch, win: win}:
			case <-ctx.Done():
			}
		})
		var meta agent.AnswerMeta
		if reporter, ok := opts.Runner.(agent.MetaAnswerer); ok {
			meta = reporter.LastMeta()
		}
		done := answerDoneMsg{answer: answer, hits: hits, meta: meta, win: win}
		if err != nil {
			done.err = fmt.Errorf("tui: ask: %w", err)
		}
		ch <- done
		close(ch)
		return nil
	}
	// The waiter reads while the producer fills the channel. Both commands
	// run concurrently, so deltas never block the producer on a full buffer.
	return tea.Batch(waitAnswerStream(ch), produce)
}

// askCmd runs the answerer off the Update loop. history is a snapshot supplied
// by the caller so the goroutine never races with subsequent turns. When the
// runner supports streaming, deltas arrive as separate messages.
func askCmd(opts Options, timeout time.Duration, history []agent.Turn, question string, win int) tea.Cmd {
	if sa, ok := opts.Runner.(agent.StreamAnswerer); ok && sa != nil {
		return askStreamCmd(opts, timeout, sa, history, question, win)
	}
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
			return answerMsg{err: fmt.Errorf("tui: ask: %w", err), meta: meta, win: win}
		}
		return answerMsg{answer: answer, hits: hits, meta: meta, win: win}
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
