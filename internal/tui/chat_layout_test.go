package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestChatContentWrapsInsidePane(t *testing.T) {
	m := testModel(t, baseOpts())
	tail := "FINALMARKER"
	long := strings.Repeat("segment ", 60) + tail
	m.appendChat("kb  > "+long, lineAssistant)

	content := m.chatVP.View()
	if !strings.Contains(content, tail) {
		t.Fatal("wrapped chat content lost its tail")
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a wrapped transcript, got %d lines", len(lines))
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > m.leftInner {
			t.Fatalf("visible line width %d exceeds pane width %d: %q", w, m.leftInner, line)
		}
	}
}

func TestCtrlBracketTogglesEvidencePane(t *testing.T) {
	m := testModel(t, baseOpts())
	fullChatWidth := m.chatVP.Width
	fullEvidenceWidth := m.evidenceVP.Width
	if fullEvidenceWidth < 2 {
		t.Fatal("evidence pane must start visible")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m = next.(*model)
	if !m.evidenceCollapsed {
		t.Fatal("ctrl+] did not collapse the evidence pane")
	}
	if m.chatVP.Width != m.width-2 {
		t.Fatalf("collapsed chat width = %d, want %d", m.chatVP.Width, m.width-2)
	}
	if m.chatVP.Width <= fullChatWidth {
		t.Fatal("collapsed chat pane must be wider than the split pane")
	}
	view := m.View()
	if strings.Contains(view, "EVIDENCE") {
		t.Fatal("collapsed view still renders the evidence header")
	}
	if !strings.Contains(view, "ctrl+]") {
		t.Fatal("collapsed view must advertise how to restore evidence")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m = next.(*model)
	if m.evidenceCollapsed {
		t.Fatal("second ctrl+] did not restore the evidence pane")
	}
	if m.chatVP.Width != fullChatWidth || m.evidenceVP.Width != fullEvidenceWidth {
		t.Fatalf("restore widths = (%d, %d), want (%d, %d)",
			m.chatVP.Width, m.evidenceVP.Width, fullChatWidth, fullEvidenceWidth)
	}
	if !strings.Contains(m.View(), "EVIDENCE") {
		t.Fatal("restored view lost the evidence header")
	}
}

func TestCtrlBracketMovesFocusToChat(t *testing.T) {
	m := testModel(t, baseOpts())
	m.focus = focusEvidence
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if got := next.(*model).focus; got != focusChat {
		t.Fatalf("focus after collapse = %v, want chat", got)
	}
}
