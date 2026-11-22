package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// windowState is a full snapshot of one chat window. The model keeps the
// active window in its own fields; switching saves and restores this struct.
type windowState struct {
	title      string
	chatVP     viewport.Model
	evidenceVP viewport.Model
	input      textinput.Model

	chatLines []chatLine
	history   []agent.Turn
	evidence  []index.Hit

	busy              bool
	focus             focus
	evidenceCollapsed bool
	streamText        string
	streaming         bool

	runner agent.Answerer
	sink   agent.Sink
}

// saveWindow copies the active chat state into a snapshot. Slices are copied
// by value because the model replaces them wholesale on restore; the backing
// arrays are never mutated in place by the chat code.
func (m *model) saveWindow() windowState {
	return windowState{
		title:             m.windowTitle(),
		chatVP:            m.chatVP,
		evidenceVP:        m.evidenceVP,
		input:             m.input,
		chatLines:         m.chatLines,
		history:           m.history,
		evidence:          m.evidence,
		busy:              m.busy,
		focus:             m.focus,
		evidenceCollapsed: m.evidenceCollapsed,
		streamText:        m.streamText,
		streaming:         m.streaming,
		runner:            m.opts.Runner,
		sink:              m.opts.Sink,
	}
}

// loadWindow restores a snapshot into the active model fields. Geometry is
// reapplied by the caller because viewport sizes depend on the terminal.
func (m *model) loadWindow(w windowState) {
	m.chatVP = w.chatVP
	m.evidenceVP = w.evidenceVP
	m.input = w.input
	m.chatLines = w.chatLines
	m.history = w.history
	m.evidence = w.evidence
	m.busy = w.busy
	m.focus = w.focus
	m.evidenceCollapsed = w.evidenceCollapsed
	m.streamText = w.streamText
	m.streaming = w.streaming
	if w.runner != nil {
		m.opts.Runner = w.runner
	}
	if w.sink != nil {
		m.opts.Sink = w.sink
	}
}

func (m *model) windowTitle() string {
	if m.active >= 0 && m.active < len(m.windowTitles) {
		return m.windowTitles[m.active]
	}
	return ""
}

// runPrefixKey dispatches the second key of a window command. Unknown keys
// cancel the prefix and are discarded, so no letter leaks into the input.
func (m *model) runPrefixKey(msg tea.KeyMsg) tea.Cmd {
	m.prefixPending = false
	switch {
	case key.Matches(msg, m.keys.NewWindow):
		return m.newWindow()
	case key.Matches(msg, m.keys.NextWindow):
		return m.switchWindow(m.active + 1)
	case key.Matches(msg, m.keys.PrevWindow):
		return m.switchWindow(m.active - 1)
	case key.Matches(msg, m.keys.CloseWindow):
		return m.closeWindow()
	case key.Matches(msg, m.keys.WindowList):
		m.pickerOpen = true
		m.pickerCursor = m.active
		return nil
	}
	for i := range m.keys.Windows {
		if key.Matches(msg, m.keys.Windows[i]) {
			return m.switchWindow(i)
		}
	}
	return nil
}

// tabBarView renders one bottom row of window tabs. The active tab uses the
// selected style so it stays visible without a cursor.
func (m *model) tabBarView() string {
	parts := make([]string, 0, len(m.windowTitles))
	for i, title := range m.windowTitles {
		tab := " " + title + " "
		if i == m.active {
			parts = append(parts, tabActiveStyle.Render(tab))
		} else {
			parts = append(parts, tabInactiveStyle.Render(tab))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	// Pad to the full width. Bubbletea repaints by diff; a shorter bar would
	// leave tail characters from the previous, longer tab row on screen.
	if w := lipgloss.Width(bar); w < m.width {
		bar += strings.Repeat(" ", m.width-w)
	}
	return bar
}

// updatePicker handles keys while the window picker is open. The picker is a
// toast over the chat view; only navigation and confirm keys are accepted.
func (m *model) updatePicker(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, quitKey), msg.String() == "esc":
		m.pickerOpen = false
		return nil
	case key.Matches(msg, m.keys.Up):
		m.pickerCursor = (m.pickerCursor - 1 + len(m.windowTitles)) % len(m.windowTitles)
		return nil
	case key.Matches(msg, m.keys.Down):
		m.pickerCursor = (m.pickerCursor + 1) % len(m.windowTitles)
		return nil
	case key.Matches(msg, m.keys.Send):
		target := m.pickerCursor
		m.pickerOpen = false
		return m.switchWindow(target)
	}
	return nil
}

// pickerView renders the window picker as a small bordered toast. It has no
// viewport and replaces the chat view only while pickerOpen is true.
func (m *model) pickerView() string {
	var b strings.Builder
	b.WriteString(settingsHeaderStyle.Render(m.lang.T("window.picker_title")))
	b.WriteString("\n\n")
	for i, title := range m.windowTitles {
		line := "  " + title
		if i == m.pickerCursor {
			line = "> " + title
			b.WriteString(settingsSelectedStyle.Render(line))
		} else {
			b.WriteString(settingsRowStyle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.lang.T("window.picker_hint")))
	return paneFocusedStyle.Padding(1, 2).Render(b.String())
}

// newWindow saves the active window, appends a fresh one and focuses it.
func (m *model) newWindow() tea.Cmd {
	if len(m.windowTitles) >= maxWindows {
		m.appendChat(m.lang.Tf("window.limit", maxWindows), lineError)
		return nil
	}
	m.windows[m.active] = m.saveWindow()
	m.windows = append(m.windows, windowState{})
	title := strconv.Itoa(len(m.windowTitles) + 1)
	m.windowTitles = append(m.windowTitles, title)
	m.active = len(m.windowTitles) - 1

	if m.opts.NewWindow != nil {
		runner, sink := m.opts.NewWindow(m.active)
		if runner != nil {
			m.opts.Runner = runner
		}
		if sink != nil {
			m.opts.Sink = sink
		}
	}

	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = m.lang.T("chat.placeholder")
	ti.CharLimit = 512
	m.chatVP = viewport.New(1, 1)
	m.evidenceVP = viewport.New(1, 1)
	m.evidenceVP.Style = evidenceViewStyle
	m.input = ti
	// A new window starts empty. The first window shows the welcome line at
	// startup; later windows must not repeat it.
	m.chatLines = nil
	m.history = make([]agent.Turn, 0, 16)
	m.evidence = nil
	m.busy = false
	m.focus = focusChat
	m.evidenceCollapsed = false
	m.streamText = ""
	m.streaming = false
	if m.width > 0 && m.height > 0 {
		m.applySize(m.width, m.height)
	}
	return m.input.Focus()
}

// switchWindow saves the active window and restores the target. Out-of-range
// indices wrap around, so next at the last window opens the first one.
func (m *model) switchWindow(target int) tea.Cmd {
	n := len(m.windowTitles)
	if n < 2 {
		return nil
	}
	target = (target%n + n) % n
	if target == m.active {
		return nil
	}
	m.windows[m.active] = m.saveWindow()
	m.active = target
	m.loadWindow(m.windows[target])
	m.applySize(m.width, m.height)
	m.refreshChat()
	m.refreshEvidence()
	return m.input.Focus()
}

// closeWindow removes the active window. The last window cannot be closed.
func (m *model) closeWindow() tea.Cmd {
	n := len(m.windowTitles)
	if n < 2 {
		return nil
	}
	// The removed snapshot is dropped. The next active window is the one
	// that followed it, or the new last entry when closing the tail.
	next := m.active
	if next >= n-1 {
		next = n - 2
	}
	m.windows = append(m.windows[:m.active], m.windows[m.active+1:]...)
	m.windowTitles = append(m.windowTitles[:m.active], m.windowTitles[m.active+1:]...)
	m.active = next
	m.loadWindow(m.windows[next])
	m.applySize(m.width, m.height)
	m.refreshChat()
	m.refreshEvidence()
	return m.input.Focus()
}

// routeAnswer applies a message to a window other than the active one. It
// saves the active state, temporarily loads the target, runs fn, then restores
// the active window. Answers never land in the wrong transcript.
func (m *model) routeAnswer(win int, fn func()) {
	if win < 0 || win >= len(m.windowTitles) || win == m.active {
		return
	}
	current := m.active
	m.windows[current] = m.saveWindow()
	m.loadWindow(m.windows[win])
	fn()
	m.windows[win] = m.saveWindow()
	m.loadWindow(m.windows[current])
}
