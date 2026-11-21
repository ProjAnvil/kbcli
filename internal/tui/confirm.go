package tui

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/projanvil/kbcli/internal/scan"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type confirmState struct {
	report       scan.Report
	armed        bool
	dirConfirmed bool
	dirCheckMode bool
	dirInput     textinput.Model
	pathMode     bool
	pathInput    textinput.Model
	err          error
	indexing     bool
	progress     IndexProgress
	chunks       int
	doneErr      error
	ch           chan IndexProgress
	frame        int
}

// confirmTarget recognizes ":index", "index", ":index <dir>" and
// "index <dir>". The second value is the optional directory argument.
func confirmTarget(line string) (bool, string) {
	l := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":"))
	fields := strings.Fields(l)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "index") {
		return false, ""
	}
	return true, strings.Join(fields[1:], " ")
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	// While indexing the dialog is modal: no key can start a second build.
	if m.confirm.indexing {
		return m, nil
	}
	if m.confirm.pathMode {
		return m.updateConfirmPath(msg)
	}
	if m.confirm.dirCheckMode {
		return m.updateConfirmDirCheck(msg)
	}

	switch msg.String() {
	case "n", "esc":
		m.screen = screenChat
		m.appendChat(m.lang.T("confirm.cancelled"), lineStatus)
		return m, nil
	case "q":
		return m, tea.Quit
	case "p":
		m.confirm.pathMode = true
		m.confirm.dirConfirmed = false
		m.confirm.dirCheckMode = false
		m.confirm.err = nil
		m.confirm.pathInput.SetValue("")
		return m, m.confirm.pathInput.Focus()
	case "c":
		return m, m.confirmDirectory()
	case "y":
		if !m.confirm.dirConfirmed {
			m.confirm.err = errors.New(m.lang.T("confirm.dir_first"))
			return m, nil
		}
		if !m.confirm.report.CanAutoIndex() {
			m.confirm.err = errors.New(m.lang.T("confirm.blocked"))
			return m, nil
		}
		if m.confirm.report.NeedsSecondConfirm() && !m.confirm.armed {
			m.confirm.armed = true
			return m, nil
		}
		return m, m.startIndex()
	}
	return m, nil
}

// confirmDirectory is the second confirmation of the target location. For a
// medium-or-larger corpus the user must type the directory name, not just
// press a key.
func (m *model) confirmDirectory() tea.Cmd {
	m.confirm.err = nil
	if m.confirm.report.Risk >= scan.RiskMedium {
		m.confirm.dirCheckMode = true
		m.confirm.dirInput.SetValue("")
		return m.confirm.dirInput.Focus()
	}
	m.confirm.dirConfirmed = true
	return nil
}

func (m *model) updateConfirmDirCheck(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.confirm.dirCheckMode = false
		return m, nil
	case "enter":
		want := filepath.Base(filepath.Clean(m.confirm.report.Root))
		got := strings.TrimSpace(m.confirm.dirInput.Value())
		if !strings.EqualFold(got, want) {
			m.confirm.err = errors.New(m.lang.T("confirm.dir_mismatch"))
			return m, nil
		}
		m.confirm.dirCheckMode = false
		m.confirm.dirConfirmed = true
		m.confirm.err = nil
		return m, nil
	default:
		var cmd tea.Cmd
		m.confirm.dirInput, cmd = m.confirm.dirInput.Update(msg)
		return m, cmd
	}
}

func (m *model) updateConfirmPath(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.confirm.pathMode = false
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.confirm.pathInput.Value())
		if value == "" {
			m.confirm.pathMode = false
			return m, nil
		}
		root := value
		if !filepath.IsAbs(root) {
			root = filepath.Join(m.confirm.report.Root, root)
		}
		if m.opts.Preflight == nil {
			m.confirm.err = errors.New("preflight unavailable")
			return m, nil
		}
		rep, err := m.opts.Preflight(root)
		if err != nil {
			m.confirm.err = err
			return m, nil
		}
		m.confirm.report = rep
		m.confirm.armed = false
		m.confirm.dirConfirmed = false
		m.confirm.dirCheckMode = false
		m.confirm.pathMode = false
		m.confirm.err = nil
		return m, nil
	default:
		var cmd tea.Cmd
		m.confirm.pathInput, cmd = m.confirm.pathInput.Update(msg)
		return m, cmd
	}
}

func (m *model) onIndexDone(msg indexDoneMsg) tea.Cmd {
	m.confirm.indexing = false
	if msg.err != nil {
		m.confirm.doneErr = msg.err
		return nil
	}
	m.confirm.chunks = msg.n
	m.screen = screenChat
	m.appendChat(m.lang.Tf("confirm.done", msg.n), lineStatus)
	// Build installed a fresh store and runner; rebind them like a reindex.
	if m.opts.Refresh != nil {
		store, runner := m.opts.Refresh()
		m.opts.Store = store
		m.opts.Runner = runner
	}
	return nil
}

func (m *model) confirmView() string {
	r := m.confirm.report
	var b strings.Builder

	b.WriteString(settingsHeaderStyle.Render(m.lang.T("confirm.title")))
	b.WriteRune('\n')
	b.WriteString(m.lang.Tf("confirm.root", r.Root))
	b.WriteRune('\n')
	b.WriteString(m.lang.Tf("confirm.scan", r.Files, scan.HumanBytes(r.Bytes), r.MaxDepth, r.Dirs))
	b.WriteRune('\n')
	if len(r.Extensions) > 0 {
		var parts []string
		for i, e := range r.Extensions {
			if i >= 6 {
				parts = append(parts, "…")
				break
			}
			parts = append(parts, e.Ext+" "+strconv.Itoa(e.Count))
		}
		b.WriteString(m.lang.Tf("confirm.ext", strings.Join(parts, ", ")))
		b.WriteRune('\n')
	}
	b.WriteString(settingsStaleStyle.Render(m.lang.Tf("confirm.risk", m.lang.T("confirm.risk_"+r.RiskLabel()))))
	b.WriteRune('\n')
	if !m.confirm.indexing {
		if m.confirm.dirConfirmed {
			b.WriteString(settingsStatusStyle.Render(m.lang.T("confirm.dir_done")))
		} else {
			b.WriteString(settingsStatusStyle.Render(m.lang.T("confirm.dir_ask")))
		}
		b.WriteRune('\n')
	}

	for _, warn := range r.Warnings {
		line := m.lang.T("confirm.warn_" + warn)
		if reason, ok := strings.CutPrefix(warn, "protected:"); ok {
			line = m.lang.Tf("confirm.warn_protected", reason)
		}
		b.WriteString(settingsErrorStyle.Render("  ! " + line))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')
	if m.confirm.indexing {
		frame := spinnerFrames[m.confirm.frame%len(spinnerFrames)]
		p := m.confirm.progress
		b.WriteString(settingsStatusStyle.Render(m.lang.Tf("confirm.indexing",
			frame, p.FilesDone, p.FilesTotal, p.Chunks, p.Current)))
		b.WriteRune('\n')
	} else if m.confirm.doneErr != nil {
		b.WriteString(settingsErrorStyle.Render(m.lang.Tf("confirm.failed", m.confirm.doneErr)))
		b.WriteRune('\n')
	} else if m.confirm.err != nil {
		b.WriteString(settingsErrorStyle.Render(m.confirm.err.Error()))
		b.WriteRune('\n')
	} else if m.confirm.armed {
		b.WriteString(settingsStaleStyle.Render(m.lang.Tf("confirm.again", r.Files)))
		b.WriteRune('\n')
	}

	if m.confirm.pathMode {
		b.WriteString(m.lang.T("confirm.path_hint"))
		b.WriteRune('\n')
		b.WriteString(m.confirm.pathInput.View())
		b.WriteRune('\n')
	} else if m.confirm.dirCheckMode {
		b.WriteString(m.lang.Tf("confirm.dir_check", filepath.Base(filepath.Clean(r.Root))))
		b.WriteRune('\n')
		b.WriteString(m.confirm.dirInput.View())
		b.WriteRune('\n')
	} else if !m.confirm.indexing {
		if !m.confirm.dirConfirmed {
			b.WriteString(settingsRailActiveStyle.Render(m.lang.T("confirm.dir_confirm")))
			b.WriteString("   ")
		} else if r.CanAutoIndex() {
			b.WriteString(settingsRailActiveStyle.Render(m.lang.T("confirm.yes")))
			b.WriteString("   ")
		}
		b.WriteString(settingsRailStyle.Render(m.lang.T("confirm.no")))
		b.WriteString("   ")
		b.WriteString(settingsRailStyle.Render(m.lang.T("confirm.path")))
		b.WriteRune('\n')
	}
	if !r.CanAutoIndex() {
		b.WriteString(settingsStatusStyle.Render(m.lang.T("confirm.blocked")))
	}
	return b.String()
}

// openConfirm re-runs the preflight for the :index command.
func (m *model) openConfirm(root string) {
	if m.opts.Preflight != nil {
		rep, err := m.opts.Preflight(root)
		if err == nil {
			m.confirm.report = rep
		} else {
			m.confirm.report = scan.Report{
				Root:     root,
				Risk:     scan.RiskCritical,
				Warnings: []string{"scan_failed"},
			}
		}
	}
	m.confirm.armed = false
	m.confirm.dirConfirmed = false
	m.confirm.dirCheckMode = false
	m.confirm.pathMode = false
	m.confirm.indexing = false
	m.confirm.doneErr = nil
	m.confirm.err = nil
	if m.confirm.ch == nil {
		m.confirm.ch = make(chan IndexProgress, 64)
	}
	m.screen = screenConfirm
}
