package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/config"
)

func keyPress(s string) tea.KeyMsg {
	switch s {
	case "ctrl+x":
		return tea.KeyMsg{Type: tea.KeyCtrlX}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPrefixCreatesSwitchesAndClosesWindows(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	m := testModel(t, opts)
	m.applySize(120, 40)
	m.chatLines = []chatLine{{text: "window one", kind: lineStatus}}

	next, cmd := m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	if !m.prefixPending {
		t.Fatal("ctrl+x did not arm the prefix")
	}
	if cmd == nil {
		t.Fatal("prefix did not arm a timeout")
	}

	next, _ = m.Update(keyPress("t"))
	m = next.(*model)
	if len(m.windowTitles) != 2 || m.active != 1 {
		t.Fatalf("windows = %v active = %d, want two windows at 1", m.windowTitles, m.active)
	}
	if m.prefixPending {
		t.Fatal("prefix stayed armed after a command")
	}
	if chatHas(m, "window one") {
		t.Fatal("new window inherited the old transcript")
	}

	m.appendChat("window two text", lineStatus)
	next, _ = m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("p"))
	m = next.(*model)
	if m.active != 0 {
		t.Fatalf("active = %d, want 0 after previous", m.active)
	}
	if !chatHas(m, "window one") {
		t.Fatal("switching back lost the first transcript")
	}
	if chatHas(m, "window two text") {
		t.Fatal("first window shows text from the second window")
	}

	next, _ = m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("x"))
	m = next.(*model)
	if len(m.windowTitles) != 1 {
		t.Fatalf("close left %d windows, want 1", len(m.windowTitles))
	}
	next, _ = m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("x"))
	m = next.(*model)
	if len(m.windowTitles) != 1 {
		t.Fatal("last window was closed")
	}
}

func TestPrefixUnknownKeyDoesNotReachInput(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	m := testModel(t, opts)
	m.applySize(120, 40)

	next, _ := m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("z"))
	m = next.(*model)
	if m.prefixPending {
		t.Fatal("prefix stayed armed after unknown key")
	}
	if strings.Contains(m.input.Value(), "z") {
		t.Fatalf("unknown prefix key reached input: %q", m.input.Value())
	}
	if len(m.windowTitles) != 1 {
		t.Fatal("unknown prefix key changed windows")
	}
}

func TestAnswerRoutesToOriginatingWindow(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	m := testModel(t, opts)
	m.applySize(120, 40)

	m.newWindow()
	m.appendChat("question two", lineUser)
	next, _ := m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("p"))
	m = next.(*model)

	next, _ = m.Update(answerMsg{answer: "answer for two", win: 1})
	m = next.(*model)
	if chatHas(m, "answer for two") {
		t.Fatal("background answer landed in the active window")
	}
	next, _ = m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("n"))
	m = next.(*model)
	if !chatHas(m, "answer for two") {
		t.Fatal("background answer missing from its window")
	}
}

func TestWindowPickerAndTabBar(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	m := testModel(t, opts)
	m.applySize(120, 40)
	m.newWindow()
	m.newWindow()

	view := m.View()
	if !strings.Contains(view, "1") || !strings.Contains(view, "2") || !strings.Contains(view, "3") {
		t.Fatalf("tab bar missing window labels: %q", view)
	}

	next, _ := m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("w"))
	m = next.(*model)
	if !m.pickerOpen {
		t.Fatal("ctrl+x w did not open the picker")
	}
	view = m.View()
	for _, want := range []string{"select window", "up/down move", "3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q: %q", want, view)
		}
	}

	next, _ = m.Update(keyPress("up"))
	m = next.(*model)
	next, _ = m.Update(keyPress("enter"))
	m = next.(*model)
	if m.pickerOpen {
		t.Fatal("enter did not close the picker")
	}
	if m.active != 1 {
		t.Fatalf("active = %d, want 1 after picker move", m.active)
	}

	next, _ = m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(keyPress("w"))
	m = next.(*model)
	next, _ = m.Update(keyPress("esc"))
	m = next.(*model)
	if m.pickerOpen {
		t.Fatal("esc did not close the picker")
	}
}

func TestPrefixTimeout(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	m := testModel(t, opts)
	next, _ := m.Update(keyPress("ctrl+x"))
	m = next.(*model)
	next, _ = m.Update(prefixTimeoutMsg{})
	m = next.(*model)
	if m.prefixPending {
		t.Fatal("timeout did not clear the prefix")
	}
}
