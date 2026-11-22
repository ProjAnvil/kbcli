package tui

import "strings"

// reindexBar renders a file-level progress bar. Reindex has no byte data,
// so the fill comes from FilesDone / FilesTotal.
func reindexBar(p IndexProgress) string {
	filled := 0
	if p.FilesTotal > 0 {
		filled = bootBarWidth * p.FilesDone / p.FilesTotal
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

// reindexView is the full-screen modal shown during a reindex. It mirrors the
// boot screen: logo, bar, phase, and current file. Chat stays blocked until
// reindexDoneMsg clears the modal. On failure the error stays in view.
func (m *model) reindexView() string {
	p := m.reindexProgress
	frame := spinnerFrames[m.reindexFrame%len(spinnerFrames)]

	var b strings.Builder
	b.WriteString(bootLogoStyle.Render(bootLogo))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render(m.lang.T("reindex.title")))
	b.WriteString("\n\n")
	b.WriteString(settingsStatusStyle.Render(m.lang.Tf("reindex.progress",
		frame, reindexPhaseLabel(m.lang, p.Phase), p.FilesDone, p.FilesTotal, p.Chunks, p.Current)))
	b.WriteString("\n")
	b.WriteString(reindexBar(p))
	b.WriteString("\n")
	if p.Current != "" {
		b.WriteString(helpStyle.Render(truncate(p.Current, m.width)))
		b.WriteString("\n")
	}
	if m.reindexErr != nil {
		b.WriteString("\n")
		b.WriteString(bootErrStyle.Render(m.lang.Tf("chat.reindex_error", m.reindexErr.Error())))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.lang.T("reindex.hint")))
	return b.String()
}
