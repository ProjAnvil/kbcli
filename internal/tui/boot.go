package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// bootLogo is the ASCII wordmark shown while startup provisions the reranker
// model. It is plain ASCII so it renders in every terminal, including the
// minimal ones that lack Unicode block glyphs.
const bootLogo = ` _  __ ___ _   _ _     _
| |/ /| _ \ | | (_)___| |__
| ' / |  _/ |_| | / __| '_ \
| . \ | |_|  _  | \__ \ | | |
|_|\_\|___|_| |_|_|___/_| |_|`

// bootBarWidth is the fixed rune width of the progress bar. It fits inside
// minWidth (40) once the surrounding labels are accounted for.
const bootBarWidth = 24

var (
	bootLogoStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	bootBarStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	bootErrStyle  = lipgloss.NewStyle().Foreground(colorError).Bold(true)
)

func bootPhaseLabel(lang i18n.Lang, phase string) string {
	switch phase {
	case "checking":
		return lang.T("boot.phase_checking")
	case "copying":
		return lang.T("boot.phase_copying")
	case "downloading":
		return lang.T("boot.phase_downloading")
	case "done":
		return lang.T("boot.phase_done")
	default:
		return phase
	}
}

// bootBar renders a byte-level progress bar. A model that is only being
// checked or copied still reports total bytes, so the same ratio applies.
func bootBar(p ModelProgress) string {
	filled := 0
	if p.Total > 0 {
		filled = int(int64(bootBarWidth) * p.Downloaded / p.Total)
	}
	if filled < 0 {
		filled = 0
	}
	if filled > bootBarWidth {
		filled = bootBarWidth
	}
	return bootBarStyle.Render(strings.Repeat("=", filled) +
		strings.Repeat("-", bootBarWidth-filled))
}

// bootView is the full-screen modal shown until the managed reranker model is
// present. Chat input stays blocked: on failure the view keeps the error and
// the hint so the user can quit with ctrl+c or fix the model and retry.
func (m *model) bootView() string {
	p := m.bootProgress
	frame := spinnerFrames[m.bootFrame%len(spinnerFrames)]

	var b strings.Builder
	b.WriteString(bootLogoStyle.Render(bootLogo))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render(m.lang.T("boot.title")))
	b.WriteString("\n\n")
	b.WriteString(settingsStatusStyle.Render(m.lang.Tf("boot.progress",
		frame, bootPhaseLabel(m.lang, p.Phase), p.FilesDone, p.FilesTotal,
		float64(p.Downloaded)/(1<<20), float64(p.Total)/(1<<20))))
	b.WriteString("\n")
	b.WriteString(bootBar(p))
	b.WriteString("\n")
	if p.File != "" {
		b.WriteString(helpStyle.Render(truncate(p.File, m.width)))
		b.WriteString("\n")
	}
	if m.bootErr != nil {
		b.WriteString("\n")
		b.WriteString(bootErrStyle.Render(m.lang.Tf("boot.failed", m.bootErr.Error())))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.lang.T("boot.hint")))
	return b.String()
}

// bootPercent returns the downloaded fraction as a whole percent, clamped to
// 0..100. Exposed for tests that assert progress never exceeds the bar.
func bootPercent(p ModelProgress) int {
	if p.Total <= 0 {
		return 0
	}
	n := int(100 * p.Downloaded / p.Total)
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}
