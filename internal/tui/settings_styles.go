package tui

import "github.com/charmbracelet/lipgloss"

// Styles for the settings screen. They reuse the adaptive palette defined in
// styles.go. Every colour resolves against the terminal background, so text
// stays readable in both light and dark terminals. lipgloss setters return
// fresh values, so these are read-only and never mutated at render time.
var (
	settingsHeaderStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	settingsSubHeaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	settingsGroupStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorHighlight).
				Bold(true)

	settingsRowStyle = lipgloss.NewStyle().
				Foreground(colorText)

	settingsSelectedStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Background(colorHighlight).
				Bold(true)

	// settingsIdxStyle marks fields that invalidate the vector index. It uses
	// the user colour so the IDX tag is visually distinct on both themes.
	settingsIdxStyle = lipgloss.NewStyle().
				Foreground(colorUser).
				Bold(true)

	settingsInheritedStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	settingsOverrideStyle = lipgloss.NewStyle().
				Foreground(colorUser).
				Bold(true)

	settingsStaleStyle = lipgloss.NewStyle().
				Foreground(colorError).
				Background(colorHighlight).
				Bold(true)

	settingsErrorStyle = lipgloss.NewStyle().
				Foreground(colorError).
				Bold(true)

	settingsStatusStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	settingsHelpStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	// Master-detail panes. The rail uses the plain text colour; the active
	// group and the breadcrumb use the accent colour for orientation.
	settingsRailStyle = lipgloss.NewStyle().
				Foreground(colorText)

	settingsRailActiveStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	settingsBreadcrumbStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	settingsFocusHelpStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Faint(true)
)
