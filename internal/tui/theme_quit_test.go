package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestAdaptivePaletteHasBothThemes(t *testing.T) {
	palette := map[string]lipgloss.AdaptiveColor{
		"text":      colorText,
		"muted":     colorMuted,
		"accent":    colorAccent,
		"border":    colorBorder,
		"highlight": colorHighlight,
		"error":     colorError,
		"user":      colorUser,
	}
	for name, c := range palette {
		if c.Light == "" || c.Dark == "" {
			t.Fatalf("%s missing a theme value: %+v", name, c)
		}
		if c.Light == c.Dark {
			t.Fatalf("%s does not change between themes", name)
		}
	}
	if !strings.HasPrefix(colorText.Dark, "#e") {
		t.Fatalf("dark text is not light: %s", colorText.Dark)
	}
	if !strings.HasPrefix(colorHighlight.Dark, "#1") {
		t.Fatalf("dark highlight is not dark: %s", colorHighlight.Dark)
	}
}

func TestStylesResolvePerBackground(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	oldDark := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
		lipgloss.SetHasDarkBackground(oldDark)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)

	lipgloss.SetHasDarkBackground(false)
	light := assistantStyle.Render("x")
	lipgloss.SetHasDarkBackground(true)
	dark := assistantStyle.Render("x")

	if light == dark {
		t.Fatal("assistant style did not change with terminal background")
	}
	if !strings.Contains(light, "31;35;40") {
		t.Fatalf("light text ANSI missing: %q", light)
	}
	if !strings.Contains(dark, "230;237;243") {
		t.Fatalf("dark text ANSI missing: %q", dark)
	}

	lipgloss.SetHasDarkBackground(false)
	lightEvidence := evidenceViewStyle.Render("x")
	lipgloss.SetHasDarkBackground(true)
	darkEvidence := evidenceViewStyle.Render("x")
	if !strings.Contains(lightEvidence, "48;2;246;248;250") {
		t.Fatalf("light highlight ANSI missing: %q", lightEvidence)
	}
	if !strings.Contains(darkEvidence, "48;2;22;27;34") {
		t.Fatalf("dark highlight ANSI missing: %q", darkEvidence)
	}
}

func TestQuitCommand(t *testing.T) {
	for _, line := range []string{":q", "q", ":quit", "quit", "EXIT", " :Q "} {
		if !quitCommand(line) {
			t.Fatalf("%q should quit", line)
		}
	}
	for _, line := range []string{"question", ":settings", ":index", "quick", ""} {
		if quitCommand(line) {
			t.Fatalf("%q must not quit", line)
		}
	}
}

func TestSubmitQuitReturnsQuitMsg(t *testing.T) {
	opts := baseOpts()
	m := testModel(t, opts)
	m.input.SetValue(":q")
	cmd := m.submit()
	if cmd == nil {
		t.Fatal("submit(:q) returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("submit(:q) did not return tea.QuitMsg")
	}
}

func TestBareQCallsQuitOnlyWithEmptyInput(t *testing.T) {
	opts := baseOpts()
	m := testModel(t, opts)

	_, cmd := pressKey(m, "q")
	if cmd == nil {
		t.Fatal("bare q with empty input returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("bare q with empty input did not quit")
	}

	m.input.SetValue("q")
	_, cmd = pressKey(m, "q")
	if cmd != nil {
		t.Fatal("q while typing must not quit")
	}
}
