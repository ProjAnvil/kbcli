package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	reservedRows = 5

	helpLine      = "enter send · :settings / :reindex / :update / :help / :clear / :q · tab switch · pgup/pgdn scroll · q or ctrl+c quit"
	thinkingLabel = "  ⠋ thinking..."
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

	chatVP     viewport.Model
	evidenceVP viewport.Model
	input      textinput.Model

	chatLines []chatLine
	history   []agent.Turn
	evidence  []index.Hit

	busy  bool
	focus focus
	ready bool

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
		lang:         lang,
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
	return m
}

func (m *model) Init() tea.Cmd {
	return m.input.Focus()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applySize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		// Reindex is modal on every screen. This must precede confirm/settings
		// routing so no key (not even settings' r) can start a second build.
		if m.reindexing {
			if msg.String() == "ctrl+c" {
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
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "q":
			// Bare q quits only when the chat input is empty, so it never
			// eats a letter the user is typing.
			if strings.TrimSpace(m.input.Value()) == "" {
				return m, tea.Quit
			}
		case "tab":
			m.toggleFocus()
			return m, nil
		case "pgup", "pgdown", "up", "down":
			m.scrollFocused(msg.String())
			return m, nil
		case "enter":
			return m, m.submit()
		}
		// Unhandled keys (printable runes, backspace) fall through to the
		// text input forwarding below.

	case answerMsg:
		m.onAnswer(msg)
		return m, nil

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
	m.refreshChat()
	m.chatVP.GotoBottom()
	// Snapshot the history so the Cmd goroutine never races with later turns.
	hist := append([]agent.Turn(nil), m.history...)
	return askCmd(m.opts, m.timeout, hist, question)
}

func (m *model) onAnswer(msg answerMsg) {
	m.busy = false
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
	// Show Porto's anaphora rewrite and session memory as status lines so the
	// optimizations are observable, not silent.
	if msg.meta.Condensed {
		m.appendChat(m.lang.Tf("chat.condensed", msg.meta.Method, msg.meta.Original, msg.meta.Rewritten), lineStatus)
	}
	if msg.meta.FactsCount > 0 || msg.meta.HasSummary {
		m.appendChat(m.lang.Tf("session.title", msg.meta.FactsCount), lineStatus)
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

func (m *model) refreshChat() {
	var b strings.Builder
	for i, ln := range m.chatLines {
		b.WriteString(styleLine(ln))
		if i != len(m.chatLines)-1 {
			b.WriteRune('\n')
		}
	}
	if m.busy {
		if b.Len() > 0 {
			b.WriteRune('\n')
		}
		b.WriteString(thinkingStyle.Render(m.lang.T("chat.thinking")))
	}
	m.chatVP.SetContent(b.String())
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

	leftBox := w * 62 / 100 // chat pane is ~62% of the terminal width
	rightBox := w - leftBox
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
	rightW := m.rightInner + 2
	leftHeader := headerStyle.Width(leftW).Render(truncate(m.chatHeader(), leftW))
	rightHeader := headerStyle.Width(rightW).Render(truncate(m.evidenceHeader(), rightW))
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, leftHeader, rightHeader)

	leftPane, rightPane := paneStyle, paneStyle
	if m.focus == focusChat {
		leftPane = paneFocusedStyle
	} else {
		rightPane = paneFocusedStyle
	}
	leftBox := leftPane.Render(m.chatVP.View())
	rightBox := rightPane.Render(m.evidenceVP.View())
	panesRow := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	inputRow := inputStyle.Render(m.input.View())
	helpRow := helpStyle.Render(truncate(m.lang.T("chat.help"), m.width))

	return lipgloss.JoinVertical(lipgloss.Left, headerRow, panesRow, inputRow, helpRow)
}
