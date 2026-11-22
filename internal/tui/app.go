package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
	"github.com/ProjAnvil/kbcli/internal/index"
)

const (
	minWidth  = 40
	minHeight = 10

	// reservedRows are the rows the viewports do not own: one header row,
	// the two border rows (top + bottom) around each pane, the input row and
	// the help row. So viewport height = terminal height - reservedRows.
	reservedRows = 6 // header, two pane borders, input, tab bar, help

	helpLine = "enter send · :settings / :reindex / :update / :help / :q · ctrl+x windows · tab · ctrl+] evidence · q/ctrl+c quit"

	emptyEvidence = "No evidence yet. Ask a question."
	welcomeLine   = "kbcli ready. Ask a question, type :settings to configure, or :help for commands."
)

type focus int

const (
	focusChat focus = iota
	focusEvidence
)

// screen identifies which full-screen view the model is rendering.
type screen int

const (
	screenChat screen = iota
	screenSettings
	screenConfirm
)

type lineKind int

const (
	lineUser lineKind = iota
	lineAssistant
	lineStatus
	lineError
)

type chatLine struct {
	text string
	kind lineKind
}

type model struct {
	opts Options
	keys keyMap

	// Windows hold snapshots of inactive chat sessions. The active window
	// lives in the model fields below, so existing chat code stays unchanged.
	windows       []windowState
	windowTitles  []string
	active        int
	prefixPending bool
	pickerOpen    bool
	pickerCursor  int

	chatVP     viewport.Model
	evidenceVP viewport.Model
	input      textinput.Model

	chatLines []chatLine
	history   []agent.Turn
	evidence  []index.Hit

	busy              bool
	focus             focus
	ready             bool
	evidenceCollapsed bool

	// In-flight streamed assistant text. It is rendered as a live line while
	// deltas arrive, then promoted into history once the done message lands.
	streamText string
	streaming  bool

	// cancel stops the whole in-flight request (intent, retrieval, rerank and
	// generation). reqGen is the active window's request generation; nextGen
	// is a monotonic allocator so a cancelled request's late messages can be
	// dropped. spinFrame drives the animated thinking glyph.
	// spinRunning guards the single shared tick chain. It is global, not
	// per-window, so several busy tabs never spawn duplicate chains.
	cancel      context.CancelFunc
	reqGen      uint64
	nextGen     uint64
	spinFrame   int
	spinRunning bool

	width  int
	height int

	leftInner  int
	rightInner int
	vpHeight   int

	timeout time.Duration
	lang    i18n.Lang

	// Settings screen state. It is kept on the model so the chat screen stays
	// byte-for-byte unchanged when screen == screenChat.
	screen          screen
	draft           *config.Config
	info            SettingsInfo
	cursor          int
	flat            []config.Field
	overrideMark    map[string]bool
	editMode        bool
	editKey         string
	editInput       textinput.Model
	status          string
	statusErr       error
	loading         bool
	reindexRequired bool
	reindexChanged  []string

	// Master-detail settings navigation. The rail owns only group selection;
	// the detail pane owns the focused field. One draft is shared by both.
	pane           settingsPane
	groups         []string
	groupCursor    int
	groupCursorMem map[string]int
	dirty          bool
	exitArmed      bool
	loadGen        uint64
	railW          int
	detailW        int
	narrow         bool

	// First-run index confirmation. It is only populated when the session
	// starts without a vector file.
	confirm confirmState

	// Reindex modal. While reindexing is true every key except ctrl+c is
	// ignored, so chat input is blocked and no second reindex can start.
	reindexing      bool
	reindexProgress IndexProgress
	reindexFrame    int
	reindexCh       chan IndexProgress
	reindexFrom     screen
	reindexErr      error

	// Startup model provisioning. The boot view shows an ASCII logo and a
	// byte-level progress bar. It is modal: only ctrl+c is accepted until the
	// model is present, so chat cannot start with a missing reranker.
	booting      bool
	bootProgress ModelProgress
	bootFrame    int
	bootCh       chan ModelProgress
	bootErr      error

	// First-run API key wizard. It is modal: while setupActive is true the
	// wizard owns every key, so chat, settings, confirm and boot never see a
	// key until setup is saved or skipped. setupActive may be true at the same
	// time as booting; the wizard is shown first and boot starts on close.
	setupActive bool
	setup       setupState
}

// run starts the bubbletea program and blocks until the user quits.
func run(opts Options) error {
	m := newModel(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: run program: %w", err)
	}
	return nil
}

func newModel(opts Options) *model {
	lang := i18n.LangEn
	if opts.Config != nil {
		lang = i18n.Parse(opts.Config.Language)
	}
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = lang.T("chat.placeholder")
	ti.CharLimit = 512

	pi := textinput.New()
	pi.Prompt = lang.T("confirm.path_prompt")
	pi.CharLimit = 512
	di := textinput.New()
	di.Prompt = lang.T("confirm.dir_prompt")
	di.CharLimit = 128

	m := &model{
		opts:         opts,
		keys:         newKeyMap(lang),
		lang:         lang,
		windowTitles: []string{"1"},
		active:       0,
		chatVP:       viewport.New(1, 1),
		evidenceVP:   viewport.New(1, 1),
		input:        ti,
		editInput:    textinput.New(),
		overrideMark: make(map[string]bool),
		screen:       screenChat,
		focus:        focusChat,
		history:      make([]agent.Turn, 0, 16),
		timeout:      120 * time.Second,
	}
	if opts.Config != nil && opts.Config.TimeoutSeconds > 0 {
		m.timeout = time.Duration(opts.Config.TimeoutSeconds) * time.Second
	}
	m.evidenceVP.Style = evidenceViewStyle
	m.chatLines = append(m.chatLines, chatLine{text: lang.T("chat.welcome"), kind: lineStatus})
	m.confirm.pathInput = pi
	m.confirm.dirInput = di
	m.confirm.ch = make(chan IndexProgress, 64)
	if opts.EnsureModel != nil {
		// Model provisioning is modal and starts on the first tick. Screen
		// state underneath is untouched; bootView covers it until done.
		m.booting = true
		m.bootProgress = ModelProgress{Phase: "checking", Total: 1, FilesTotal: 1}
	}
	if opts.IndexReport != nil {
		m.screen = screenConfirm
		m.confirm.report = *opts.IndexReport
	} else if opts.Store == nil {
		// No index: guide the user to an explicit command. Nothing is scanned
		// or built on startup.
		m.chatLines = append(m.chatLines, chatLine{text: lang.T("confirm.guide"), kind: lineStatus})
	}
	m.refreshChat()
	m.refreshEvidence()
	m.windows = []windowState{m.saveWindow()}
	// The first-run wizard takes precedence over every other screen, but it
	// does not cancel boot or confirm state: both resume after it closes.
	if opts.SetupNeeded && opts.SetupApply != nil {
		m.setupActive = true
		m.setup = newSetupState(opts, lang)
	}
	return m
}

func (m *model) Init() tea.Cmd {
	if m.setupActive {
		return m.setupFocus()
	}
	if m.booting {
		return m.startBoot()
	}
	return m.input.Focus()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applySize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		// The first-run setup wizard owns every key while active. It must
		// run before boot and reindex so no key can leak into another modal.
		if m.setupActive {
			return m, m.updateSetup(msg)
		}
		// Boot is the first modal of the session. Only ctrl+c is accepted
		// while the model downloads, so no key can reach chat or settings.
		if m.booting {
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}
			return m, nil
		}
		// Reindex is modal on every screen. This must precede confirm/settings
		// routing so no key (not even settings' r) can start a second build.
		if m.reindexing {
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}
			return m, nil
		}
		// The confirmation and settings screens own every key while active.
		// These must run before the chat switch so esc never quits the app.
		if m.screen == screenConfirm {
			return m.updateConfirm(msg)
		}
		if m.screen == screenSettings {
			return m.updateSettings(msg)
		}
		// The window picker owns every key while open. Enter activates the
		// highlighted window; esc cancels without changing windows.
		if m.pickerOpen {
			return m, m.updatePicker(msg)
		}
		// A pending prefix consumes the next key as a window command. The key
		// never reaches the text input, so typed letters stay untouched.
		if m.prefixPending {
			return m, m.runPrefixKey(msg)
		}
		if key.Matches(msg, m.keys.Prefix) {
			m.prefixPending = true
			return m, prefixTimeout()
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case msg.String() == "esc":
			// While a request runs, esc cancels the whole pipeline instead of
			// quitting. When idle it is a no-op so a second esc after a cancel
			// cannot exit the app by accident.
			if m.busy {
				m.cancelActive()
			}
			return m, nil
		case key.Matches(msg, m.keys.QuitQ):
			// Bare q quits only when the chat input is empty, so it never
			// eats a letter the user is typing.
			if strings.TrimSpace(m.input.Value()) == "" {
				return m, tea.Quit
			}
		case key.Matches(msg, m.keys.Focus):
			m.toggleFocus()
			return m, nil
		case key.Matches(msg, m.keys.Evidence):
			m.toggleEvidence()
			return m, nil
		case key.Matches(msg, m.keys.PageUp):
			m.scrollFocused("pgup")
			return m, nil
		case key.Matches(msg, m.keys.PageDown):
			m.scrollFocused("pgdown")
			return m, nil
		case key.Matches(msg, m.keys.Up):
			m.scrollFocused("up")
			return m, nil
		case key.Matches(msg, m.keys.Down):
			m.scrollFocused("down")
			return m, nil
		case key.Matches(msg, m.keys.Send):
			return m, m.submit()
		}
		// Unhandled keys (printable runes, backspace) fall through to the
		// text input forwarding below.

	case prefixTimeoutMsg:
		m.prefixPending = false
		return m, nil

	case answerMsg:
		if msg.win != m.active {
			m.routeAnswer(msg.win, func() {
				if m.validGen(msg.gen) {
					m.onAnswer(msg)
				}
			})
			return m, nil
		}
		if m.validGen(msg.gen) {
			m.onAnswer(msg)
		}
		return m, nil

	case answerDeltaMsg:
		if msg.win != m.active {
			m.routeAnswer(msg.win, func() {
				if m.validGen(msg.gen) {
					m.onAnswerDelta(msg)
				}
			})
		} else if m.validGen(msg.gen) {
			m.onAnswerDelta(msg)
		}
		// Re-arm the waiter even for stale messages so the producer channel
		// always drains and terminates cleanly after a cancel.
		if msg.ch == nil {
			return m, nil
		}
		return m, waitAnswerStream(msg.ch)

	case answerResetMsg:
		if msg.win != m.active {
			m.routeAnswer(msg.win, func() {
				if m.validGen(msg.gen) {
					m.onAnswerReset()
				}
			})
		} else if m.validGen(msg.gen) {
			m.onAnswerReset()
		}
		if msg.ch == nil {
			return m, nil
		}
		return m, waitAnswerStream(msg.ch)

	case answerDoneMsg:
		if msg.win != m.active {
			m.routeAnswer(msg.win, func() {
				if m.validGen(msg.gen) {
					m.onAnswerDone(msg)
				}
			})
		} else if m.validGen(msg.gen) {
			m.onAnswerDone(msg)
		}
		return m, nil

	case thinkingTickMsg:
		// The chain is owned by the model, not one request: it must survive a
		// tab switch and keep animating every busy tab in the tab bar.
		if !m.anyWindowBusy() {
			m.spinRunning = false
			return m, nil
		}
		m.spinFrame++
		if m.busy && !m.streaming {
			m.refreshChat()
		}
		return m, thinkTick()

	case commandResultMsg:
		m.onCommandResult(msg)
		return m, nil

	case errMsg:
		if msg.err != nil {
			m.appendChat(m.lang.Tf("chat.error", msg.err.Error()), lineError)
		}
		return m, nil

	case settingsLoadedMsg:
		m.onSettingsLoaded(msg)
		return m, nil

	case settingsSavedMsg:
		return m, m.onSettingsSaved(msg)

	case indexProgressMsg:
		if m.screen == screenConfirm {
			m.confirm.progress = msg.p
			return m, m.waitIndexProgress()
		}
		return m, nil

	case spinTickMsg:
		if m.screen != screenConfirm || !m.confirm.indexing {
			return m, nil
		}
		m.confirm.frame++
		return m, spinTick()

	case indexDoneMsg:
		if m.screen != screenConfirm {
			return m, nil
		}
		return m, m.onIndexDone(msg)

	case bootProgressMsg:
		if !m.booting {
			return m, nil
		}
		m.bootProgress = msg.p
		return m, m.waitBootProgress()

	case bootSpinTickMsg:
		if !m.booting {
			return m, nil
		}
		m.bootFrame++
		return m, bootSpinTick()

	case bootDoneMsg:
		if !m.booting {
			return m, nil
		}
		return m, m.onBootDone(msg)

	case setupApplyDoneMsg:
		return m, m.onSetupApplyDone(msg)

	case reindexProgressMsg:
		if !m.reindexing {
			return m, nil
		}
		m.reindexProgress = msg.p
		return m, m.waitReindexProgress()

	case reindexSpinTickMsg:
		if !m.reindexing {
			return m, nil
		}
		m.reindexFrame++
		return m, reindexSpinTick()

	case reindexDoneMsg:
		if !m.reindexing {
			return m, nil
		}
		return m, m.onReindexDone(msg)
	}

	// Forward typing, cursor blinks and any other message to the chat text
	// input only while the chat screen is active. In settings, non-key messages
	// (e.g. the inline editor cursor blink) go to the edit input instead so
	// typing in settings never leaks into the chat input.
	var cmd tea.Cmd
	if m.screen == screenChat {
		m.input, cmd = m.input.Update(msg)
	} else if m.screen == screenSettings && m.editMode {
		m.editInput, cmd = m.editInput.Update(msg)
	} else if m.screen == screenConfirm && m.confirm.pathMode {
		m.confirm.pathInput, cmd = m.confirm.pathInput.Update(msg)
	} else if m.screen == screenConfirm && m.confirm.dirCheckMode {
		m.confirm.dirInput, cmd = m.confirm.dirInput.Update(msg)
	}
	return m, cmd
}

// submit handles the enter key. Commands and questions are always executed in
// a tea.Cmd so Update never blocks. Enter is ignored while an answer runs.
func (m *model) submit() tea.Cmd {
	line := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if line == "" {
		return nil
	}
	if quitCommand(line) {
		return tea.Quit
	}
	// Settings and :index are handled locally: app.HandleCommand does not own
	// the confirmation screen's preflight state.
	if settingsCommand(line) {
		return m.openSettings()
	}
	if reindexCommand(line) {
		m.appendChat("> "+line, lineStatus)
		return m.startReindex(screenChat)
	}
	if ok, target := confirmTarget(line); ok {
		root := m.opts.WorkspaceRoot
		if root == "" {
			root = "."
			if wd, err := os.Getwd(); err == nil {
				root = wd
			}
		}
		if target != "" {
			if filepath.IsAbs(target) {
				root = filepath.Clean(target)
			} else {
				root = filepath.Join(root, target)
			}
		}
		m.appendChat("> "+line, lineStatus)
		m.openConfirm(root)
		return nil
	}
	if isCommand(line) {
		if clearCommand(line) {
			m.clearChat()
		}
		m.appendChat("> "+line, lineStatus)
		return commandCmd(m.opts, line)
	}
	if m.busy {
		return nil
	}
	return m.ask(line)
}

func isCommand(line string) bool {
	if strings.HasPrefix(line, ":") {
		return true
	}
	trimmed := strings.TrimSpace(line)
	fields := strings.Fields(trimmed)
	if len(fields) > 0 && strings.EqualFold(fields[0], "index") {
		return true
	}
	switch trimmed {
	case "reindex", "update", "help", "clear", "settings", "config":
		return true
	}
	return false
}

func quitCommand(line string) bool {
	l := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":")))
	return l == "q" || l == "quit" || l == "exit"
}

func clearCommand(line string) bool {
	return strings.TrimSpace(strings.TrimPrefix(line, ":")) == "clear"
}

func (m *model) ask(question string) tea.Cmd {
	m.appendChat(m.lang.T("chat.you")+question, lineUser)
	m.history = append(m.history, agent.Turn{Role: "user", Content: question})
	m.busy = true
	m.streamText = ""
	m.streaming = false
	m.nextGen++
	m.reqGen = m.nextGen
	gen := m.reqGen

	// One cancellable context spans intent classification, retrieval,
	// reranking and generation, so esc stops the whole post-enter pipeline.
	var ctx context.Context
	var cancel context.CancelFunc
	if m.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), m.timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	m.cancel = cancel

	m.refreshChat()
	m.chatVP.GotoBottom()
	// Snapshot the history so the Cmd goroutine never races with later turns.
	hist := append([]agent.Turn(nil), m.history...)
	return askWithContext(m.opts, ctx, cancel, hist, question, m.active, gen, m.startSpin())
}

// startSpin launches the single shared spinner tick chain. Multiple busy
// windows reuse it instead of each scheduling their own.
func (m *model) startSpin() tea.Cmd {
	if m.spinRunning {
		return nil
	}
	m.spinRunning = true
	return thinkTick()
}

// validGen reports whether a streamed message belongs to the current request.
// gen == 0 is the untracked legacy path and is always accepted.
func (m *model) validGen(gen uint64) bool {
	return gen == 0 || gen == m.reqGen
}

// cancelActive cancels the in-flight request and invalidates every message it
// may still deliver. The user's question stays in history; no assistant turn
// or fabricated fallback is written.
func (m *model) cancelActive() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.nextGen++
	m.reqGen = m.nextGen
	m.busy = false
	m.streaming = false
	m.streamText = ""
	m.appendChat(m.lang.T("chat.cancelled"), lineStatus)
	m.chatVP.GotoBottom()
}

// thinkingLine renders the animated glyph plus the localized esc hint.
func (m *model) thinkingLine() string {
	frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
	return "  " + frame + " " + m.lang.T("chat.thinking")
}

// focusCmd re-arms the shared spinner when a busy window becomes active
// again after a switch, so the glyph never freezes mid-request.
func (m *model) focusCmd() tea.Cmd {
	focus := m.input.Focus()
	if !m.anyWindowBusy() {
		return focus
	}
	spin := m.startSpin()
	if spin == nil {
		return focus
	}
	return tea.Batch(focus, spin)
}

// onAnswerDelta appends one streamed delta to the live assistant text. The
// spinner is hidden as soon as text starts arriving.
func (m *model) onAnswerDelta(msg answerDeltaMsg) {
	if msg.text == "" {
		return
	}
	m.streaming = true
	m.streamText += msg.text
	m.refreshChat()
	m.chatVP.GotoBottom()
}

// onAnswerReset drops tool-round preamble text and shows the spinner again
// while the agent runs its next model round.
func (m *model) onAnswerReset() {
	m.streaming = false
	m.streamText = ""
	m.refreshChat()
}

// onAnswerDone clears the live text and reuses the normal answer path so
// history, evidence and diagnostics are written exactly once.
func (m *model) onAnswerDone(msg answerDoneMsg) {
	m.streaming = false
	m.streamText = ""
	m.onAnswer(answerMsg{answer: msg.answer, hits: msg.hits, err: msg.err, meta: msg.meta})
}

func (m *model) onAnswer(msg answerMsg) {
	m.busy = false
	m.cancel = nil
	if msg.err != nil {
		m.appendChat(m.lang.T("chat.kb")+m.lang.Tf("chat.error", msg.err.Error()), lineError)
	} else {
		answer := msg.answer
		if answer == "" {
			answer = m.lang.T("chat.no_answer")
		}
		m.appendChat(m.lang.T("chat.kb")+answer, lineAssistant)
		m.history = append(m.history, agent.Turn{Role: "assistant", Content: answer})
	}
	hits := msg.hits
	if len(hits) == 0 && m.opts.Sink != nil {
		hits = m.opts.Sink.Get()
	}
	m.setEvidence(hits)
	// Show Porto's intent route alongside the other optimizations so the
	// routing decision is observable, not silent.
	if msg.meta.Intent != "" && msg.meta.IntentEngine != "off" {
		m.appendChat(m.lang.Tf("chat.intent",
			msg.meta.Intent+"/"+msg.meta.IntentEngine, msg.meta.IntentReason), lineStatus)
	}
	if msg.meta.Condensed {
		m.appendChat(m.lang.Tf("chat.condensed", msg.meta.Method, msg.meta.Original, msg.meta.Rewritten), lineStatus)
	}
	if msg.meta.FactsCount > 0 || msg.meta.HasSummary {
		m.appendChat(m.lang.Tf("session.title", msg.meta.FactsCount), lineStatus)
	}
	if msg.meta.Degraded {
		reason := msg.meta.Reason
		if reason == "" {
			reason = "degraded"
		}
		m.appendChat(m.lang.Tf("chat.degraded", reason), lineStatus)
	}
	m.refreshChat()
	m.chatVP.GotoBottom()
}

func (m *model) onCommandResult(msg commandResultMsg) {
	if msg.err != nil {
		m.appendChat(m.lang.Tf("chat.command_error", msg.err.Error()), lineError)
		return
	}
	if msg.status != "" {
		m.appendChat(msg.status, lineStatus)
	}
	// reindex and update replace the index object inside the app. Rebind the
	// live references so the next question hits the rebuilt vectors.
	if m.opts.Refresh != nil {
		store, runner := m.opts.Refresh()
		m.opts.Store = store
		m.opts.Runner = runner
	}
}

// onBootDone closes the boot modal only on success. A failure keeps chat
// blocked and shows the error, so the user can retry after fixing the model.
func (m *model) onBootDone(msg bootDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.bootErr = msg.err
		return nil
	}
	m.booting = false
	m.bootProgress.Phase = "done"
	if m.bootProgress.Total > 0 {
		m.bootProgress.Downloaded = m.bootProgress.Total
	}
	m.bootProgress.FilesDone = m.bootProgress.FilesTotal
	return m.input.Focus()
}

// onReindexDone leaves the modal on either outcome, restores the originating
// screen, rebinds the live store/runner and appends a status or error line.
func (m *model) onReindexDone(msg reindexDoneMsg) tea.Cmd {
	m.reindexing = false
	m.screen = m.reindexFrom
	if msg.err != nil {
		m.reindexErr = msg.err
		m.appendChat(m.lang.Tf("reindex.failed", msg.err), lineError)
	} else {
		m.reindexProgress = IndexProgress{Phase: "done", Chunks: msg.n}
		m.appendChat(m.lang.Tf("reindex.done", msg.n), lineStatus)
		// A successful rebuild clears the settings stale banner.
		m.reindexRequired = false
		m.reindexChanged = nil
		m.info.IndexStale = false
		m.info.StaleFields = nil
	}
	if m.opts.Refresh != nil {
		store, runner := m.opts.Refresh()
		m.opts.Store = store
		m.opts.Runner = runner
	}
	return nil
}

// reindexCommand reports whether line is the chat :reindex / bare reindex
// command. It is intercepted before generic command routing so the rebuild
// runs through the blocking modal instead of opts.OnCommand.
func reindexCommand(line string) bool {
	l := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":")))
	return l == "reindex"
}

// clearChat clears the visible transcript and evidence but keeps the
// conversation history so the agent still has its context.
func (m *model) clearChat() {
	m.chatLines = nil
	m.evidence = nil
	m.refreshChat()
	m.refreshEvidence()
}

func (m *model) setEvidence(hits []index.Hit) {
	m.evidence = hits
	m.refreshEvidence()
	m.evidenceVP.GotoTop()
}

func (m *model) appendChat(text string, kind lineKind) {
	m.chatLines = append(m.chatLines, chatLine{text: text, kind: kind})
	m.refreshChat()
}

// refreshChat rebuilds the chat viewport content. Every line is wrapped to
// the pane width before styling: the viewport truncates lines wider than its
// Width, so without wrapping long answers disappear under the evidence pane.
func (m *model) refreshChat() {
	var b strings.Builder
	first := true
	write := func(s string) {
		if !first {
			b.WriteRune('\n')
		}
		b.WriteString(s)
		first = false
	}
	for _, ln := range m.chatLines {
		for _, wrapped := range wrapChatLine(ln, m.leftInner) {
			write(wrapped)
		}
	}
	if m.streaming {
		// Live answer text replaces the spinner while deltas arrive.
		live := chatLine{text: m.lang.T("chat.kb") + m.streamText, kind: lineAssistant}
		for _, wrapped := range wrapChatLine(live, m.leftInner) {
			write(wrapped)
		}
	} else if m.busy {
		write(thinkingStyle.Render(m.thinkingLine()))
	}
	m.chatVP.SetContent(b.String())
}

// wrapChatLine wraps plain text first, then styles each visual line. Styling
// before wrapping would let the viewport truncate a line whose visible width
// fits but whose ANSI escape sequences make it look longer.
func wrapChatLine(ln chatLine, width int) []string {
	if width <= 0 {
		return []string{styleLine(ln)}
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(ln.text)
	parts := strings.Split(wrapped, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, " ")
		if part == "" {
			part = " "
		}
		out = append(out, styleLine(chatLine{text: part, kind: ln.kind}))
	}
	return out
}

func styleLine(ln chatLine) string {
	switch ln.kind {
	case lineUser:
		return userStyle.Render(ln.text)
	case lineAssistant:
		return assistantStyle.Render(ln.text)
	case lineError:
		return errorStyle.Render(ln.text)
	default:
		return statusStyle.Render(ln.text)
	}
}

func (m *model) refreshEvidence() {
	m.evidenceVP.SetContent(renderEvidence(m.evidence, m.rightInner, m.lang))
}

// renderEvidence builds the plain-text content of the evidence pane. Colors are
// applied uniformly by evidenceViewStyle on the viewport itself.
func renderEvidence(hits []index.Hit, width int, lang i18n.Lang) string {
	if len(hits) == 0 {
		return lang.T("evidence.empty")
	}
	if width < 8 {
		width = 8
	}
	var b strings.Builder
	for i, h := range hits {
		rank := h.Rank
		if rank <= 0 {
			rank = i + 1
		}
		b.WriteString(fmt.Sprintf("#%d  %.3f  %s", rank, h.Score, scoreBar(h.Score)))

		if src := h.Chunk.Source; src != "" {
			b.WriteRune('\n')
			b.WriteString(truncate(src, width))
		}
		if meta := evidenceMeta(h.Chunk, lang); meta != "" {
			b.WriteRune('\n')
			b.WriteString(truncate(meta, width))
		}
		body := h.Chunk.Body
		if body == "" {
			body = h.Chunk.Text
		}
		for _, l := range wrapSnippet(body, width, 6) {
			b.WriteRune('\n')
			b.WriteString(l)
		}
		if i != len(hits)-1 {
			b.WriteRune('\n')
			b.WriteString(strings.Repeat("─", width))
			b.WriteRune('\n')
		}
	}
	return b.String()
}

func scoreBar(score float64) string {
	const cells = 8
	filled := int(score * cells)
	if filled < 0 {
		filled = 0
	}
	if filled > cells {
		filled = cells
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
}

func evidenceMeta(c index.Chunk, lang i18n.Lang) string {
	var parts []string
	if c.Heading != "" {
		parts = append(parts, c.Heading)
	}
	if c.Page > 0 {
		parts = append(parts, lang.Tf("evidence.page", c.Page))
	}
	return strings.Join(parts, lang.T("evidence.heading_separator"))
}

func wrapSnippet(text string, width, maxLines int) []string {
	wrapped := lipgloss.NewStyle().Width(width).Render(text)
	var out []string
	for _, l := range strings.Split(wrapped, "\n") {
		l = strings.TrimRight(l, " ")
		if l == "" {
			continue
		}
		if len(out) >= maxLines {
			break
		}
		out = append(out, l)
	}
	return out
}

// truncate clips a string to at most max runes. It is used for single-line
// labels so lipgloss word wrapping never grows the layout.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// applySize recomputes the two-pane geometry on the first size message and on
// every resize. Both viewports always receive the new dimensions.
func (m *model) applySize(w, h int) {
	m.width = w
	m.height = h
	m.ready = true
	if w < minWidth || h < minHeight {
		return
	}

	leftBox, rightBox := w*62/100, w*38/100 // chat pane is ~62% of the terminal width
	if m.evidenceCollapsed {
		leftBox, rightBox = w, 0
	}
	m.leftInner = leftBox - 2 // minus the left/right border columns
	m.rightInner = rightBox - 2
	if m.leftInner < 1 {
		m.leftInner = 1
	}
	if m.rightInner < 1 {
		m.rightInner = 1
	}
	m.vpHeight = h - reservedRows
	if m.vpHeight < 1 {
		m.vpHeight = 1
	}

	m.chatVP.Width = m.leftInner
	m.chatVP.Height = m.vpHeight
	m.evidenceVP.Width = m.rightInner
	m.evidenceVP.Height = m.vpHeight
	m.input.Width = w - 2
	if m.input.Width < 1 {
		m.input.Width = 1
	}
	if m.setupActive {
		m.setup.input.Width = max(w-4, 20)
	}

	// Settings master-detail geometry. Below 80 columns the rail is dropped
	// and the original single scrolling list takes over.
	m.narrow = w < settingsSplitMinWidth
	m.railW = w * 22 / 100
	if m.railW < 16 {
		m.railW = 16
	}
	if m.railW > 24 {
		m.railW = 24
	}
	if m.railW >= w-20 {
		m.railW = w - 20
	}
	m.detailW = w - m.railW - 1
	if m.detailW < 20 {
		m.detailW = 20
	}
	if m.narrow {
		m.pane = paneDetail
	}

	// Rebuild content that depends on the pane width (evidence rules/snippets).
	m.refreshChat()
	m.refreshEvidence()
}

func (m *model) toggleFocus() {
	if m.focus == focusChat {
		m.focus = focusEvidence
	} else {
		m.focus = focusChat
	}
}

// toggleEvidence collapses the evidence pane and gives the full width to the
// chat pane. Focus moves back to chat, since the evidence pane is not visible.
func (m *model) toggleEvidence() {
	m.evidenceCollapsed = !m.evidenceCollapsed
	if m.evidenceCollapsed && m.focus == focusEvidence {
		m.focus = focusChat
	}
	if m.width > 0 && m.height > 0 {
		m.applySize(m.width, m.height)
	}
}

// scrollFocused scrolls whichever viewport currently owns the focus.
func (m *model) scrollFocused(key string) {
	vp := &m.chatVP
	if m.focus == focusEvidence {
		vp = &m.evidenceVP
	}
	switch key {
	case "pgup":
		vp.PageUp()
	case "pgdown":
		vp.PageDown()
	case "up":
		vp.LineUp(1)
	case "down":
		vp.LineDown(1)
	}
}

func (m *model) chatHeader() string {
	root := m.opts.IndexPath
	if root == "" && m.opts.Store != nil {
		root = m.opts.Store.Root()
	}
	if root == "" {
		root = "."
	}
	ver := m.opts.Version
	if ver == "" {
		ver = "dev"
	}
	if title := m.windowTitle(); title != "" && len(m.windowTitles) > 1 {
		return m.lang.Tf("window.header", root, ver, title)
	}
	return m.lang.Tf("app.header", root, ver)
}

func (m *model) evidenceHeader() string {
	return m.lang.T("evidence.header")
}

func (m *model) View() string {
	if !m.ready {
		return helpStyle.Render(m.lang.T("chat.starting"))
	}
	if m.width < minWidth || m.height < minHeight {
		return helpStyle.Render(m.lang.T("chat.terminal_small"))
	}
	if m.setupActive {
		return m.setupView()
	}
	if m.booting {
		return m.bootView()
	}
	if m.reindexing {
		return m.reindexView()
	}
	if m.screen == screenSettings {
		return m.settingsView()
	}
	if m.screen == screenConfirm {
		return m.confirmView()
	}

	leftW := m.leftInner + 2
	leftHeader := headerStyle.Width(leftW).Render(truncate(m.chatHeader(), leftW))
	headerRow := leftHeader

	leftPane, rightPane := paneStyle, paneStyle
	if m.focus == focusChat {
		leftPane = paneFocusedStyle
	} else {
		rightPane = paneFocusedStyle
	}
	leftBox := leftPane.Render(m.chatVP.View())
	panesRow := leftBox

	var view string
	if m.evidenceCollapsed {
		inputRow := inputStyle.Render(m.input.View())
		helpRow := helpStyle.Render(truncate(m.lang.T("chat.help_collapsed"), m.width))
		view = lipgloss.JoinVertical(lipgloss.Left, headerRow, panesRow, inputRow, m.tabBarView(), helpRow)
	} else {
		rightW := m.rightInner + 2
		rightHeader := headerStyle.Width(rightW).Render(truncate(m.evidenceHeader(), rightW))
		headerRow = lipgloss.JoinHorizontal(lipgloss.Top, leftHeader, rightHeader)
		rightBox := rightPane.Render(m.evidenceVP.View())
		panesRow = lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

		inputRow := inputStyle.Render(m.input.View())
		helpRow := helpStyle.Render(truncate(m.lang.T("chat.help"), m.width))
		view = lipgloss.JoinVertical(lipgloss.Left, headerRow, panesRow, inputRow, m.tabBarView(), helpRow)
	}
	if m.pickerOpen {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.pickerView())
	}
	return view
}
