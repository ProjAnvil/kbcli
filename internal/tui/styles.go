package tui

import "github.com/charmbracelet/lipgloss"

// Light palette. The IDE ships a light theme, so every surface is a light
// color and every piece of text is dark. Colors are explicit hex values so the
// look does not depend on the terminal's own ANSI palette.
const (
	colorText      lipgloss.Color = "#1f2328" // primary dark text
	colorMuted     lipgloss.Color = "#6b7280" // secondary / dimmed text
	colorAccent    lipgloss.Color = "#2563eb" // accent blue
	colorBorder    lipgloss.Color = "#d0d7de" // panel border
	colorHighlight lipgloss.Color = "#f6f8fa" // evidence highlight background
	colorError     lipgloss.Color = "#b42318" // dark red for errors
	colorUser      lipgloss.Color = "#0550ae" // dark blue for user turns
)

// Styles are read-only configuration values. lipgloss setters have value
// receivers and return fresh Style values, so nothing here is mutated at
// render time.
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	userStyle      = lipgloss.NewStyle().Foreground(colorUser)
	assistantStyle = lipgloss.NewStyle().Foreground(colorText)
	statusStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle     = lipgloss.NewStyle().Foreground(colorError)
	thinkingStyle  = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	paneFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent)

	// evidenceViewStyle is applied to the whole evidence viewport so the pane
	// has a subtle light highlight while all its text stays dark.
	evidenceViewStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorHighlight)

	inputStyle = lipgloss.NewStyle().Foreground(colorText)
	helpStyle  = lipgloss.NewStyle().Foreground(colorMuted)
)
