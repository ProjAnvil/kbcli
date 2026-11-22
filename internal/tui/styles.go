package tui

import "github.com/charmbracelet/lipgloss"

// Palette with one value per terminal background. lipgloss resolves
// AdaptiveColor at render time using the terminal background reported by
// OSC 11, or COLORFGBG when the terminal cannot answer. Light values keep the
// original look. Dark values follow GitHub's dark theme, so text stays light
// on dark surfaces.
var (
	colorText      = lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#e6edf3"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9198a1"}
	colorAccent    = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#58a6ff"}
	colorBorder    = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#30363d"}
	colorHighlight = lipgloss.AdaptiveColor{Light: "#f6f8fa", Dark: "#161b22"}
	colorError     = lipgloss.AdaptiveColor{Light: "#b42318", Dark: "#f85149"}
	colorUser      = lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#79c0ff"}
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

	// evidenceViewStyle is applied to the whole evidence viewport. Its text
	// and highlight background switch together with the terminal theme.
	evidenceViewStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorHighlight)

	inputStyle = lipgloss.NewStyle().Foreground(colorText)
	helpStyle  = lipgloss.NewStyle().Foreground(colorMuted)

	// tabActiveStyle and tabInactiveStyle render one browser-style tab row.
	// Padding is horizontal only, so each tab stays a single line.
	tabActiveStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Background(colorHighlight).
				Padding(0, 1)
)
