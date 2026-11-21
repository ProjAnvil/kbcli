package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// settingsHelpLine is the footer shown on the settings screen.
const settingsHelpLine = "up/down select · left/right adjust · enter toggle enum/bool · e edit text · x clear override · m switch Global/Workspace · s save · r reindex · esc back"

// settingsSplitMinWidth is the terminal width below which the rail is dropped
// and the original single-column list is used.
const settingsSplitMinWidth = 80

type settingsPane int

const (
	paneRail settingsPane = iota
	paneDetail
)

// settingsCommand reports whether line opens the settings screen. Both the
// colon form (:settings, :config) and the bare form (settings, config) work.
func settingsCommand(line string) bool {
	l := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":")))
	return l == "settings" || l == "config"
}

// openSettings switches to the settings screen and starts an async load.
func (m *model) openSettings() tea.Cmd {
	m.screen = screenSettings
	m.loading = true
	m.status = ""
	m.statusErr = nil
	m.cursor = 0
	m.editMode = false
	m.editKey = ""
	m.reindexRequired = false
	m.reindexChanged = nil
	m.pane = paneRail
	m.groupCursor = 0
	m.groupCursorMem = make(map[string]int)
	m.dirty = false
	m.exitArmed = false
	m.loadGen++
	if m.narrow {
		m.pane = paneDetail
	}
	return settingsLoadCmd(m.opts, m.loadGen)
}

// updateSettings handles every key while the settings screen is active. It is
// reached before the chat KeyMsg switch, so the global esc -> tea.Quit and the
// chat input forwarding never run while settings are open.
func (m *model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, from any screen.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if m.editMode {
		return m.updateSettingsEdit(msg)
	}
	if m.narrow {
		return m.updateSettingsNarrow(msg)
	}

	key := msg.String()
	// Commands are global: they work from either pane.
	switch key {
	case "s":
		return m, m.saveSettings()
	case "r":
		return m, m.startReindex(screenSettings)
	case "m":
		m.toggleMode()
		return m, nil
	}

	if m.pane == paneRail {
		switch key {
		case "up", "k":
			m.moveGroup(-1)
		case "down", "j":
			m.moveGroup(1)
		case "right", "l", "enter", " ", "tab":
			m.enterDetail()
		// Backspace is a back alias only while browsing; the inline editor
		// still receives it as a delete key.
		case "esc", "q", "backspace":
			m.requestExit()
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		m.moveCursorInGroup(-1)
	case "down", "j":
		m.moveCursorInGroup(1)
	// left/right stay the numeric nudge. 21 of the 40 fields are numeric, so
	// spending these keys on pane navigation would break the common action.
	case "left", "h":
		m.adjustField(-1)
	case "right", "l":
		m.adjustField(1)
	case "enter", " ":
		m.toggleOrCycle()
	case "e":
		return m, m.beginEdit()
	case "x":
		m.clearOverride()
	case "esc", "backspace", "tab":
		m.backToRail()
	case "q":
		m.requestExit()
	}
	return m, nil
}

// updateSettingsNarrow is the <80-column fallback: one scrolling list with
// the original key map, so narrow terminals lose no functionality.
func (m *model) updateSettingsNarrow(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "backspace":
		m.requestExit()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "h":
		m.adjustField(-1)
	case "right", "l":
		m.adjustField(1)
	case "enter", " ":
		m.toggleOrCycle()
	case "e":
		return m, m.beginEdit()
	case "x":
		m.clearOverride()
	case "m":
		m.toggleMode()
	case "s":
		return m, m.saveSettings()
	case "r":
		return m, m.startReindex(screenSettings)
	}
	return m, nil
}

func (m *model) buildGroups() {
	seen := make(map[string]bool)
	m.groups = nil
	for _, f := range m.flat {
		if !seen[f.Group] {
			seen[f.Group] = true
			m.groups = append(m.groups, f.Group)
		}
	}
	if m.groupCursor < 0 || m.groupCursor >= len(m.groups) {
		m.groupCursor = 0
	}
	if m.groupCursorMem == nil {
		m.groupCursorMem = make(map[string]int)
	}
}

func (m *model) groupBounds() (int, int) {
	if len(m.groups) == 0 || m.groupCursor >= len(m.groups) {
		return 0, len(m.flat)
	}
	want := m.groups[m.groupCursor]
	start := -1
	for i, f := range m.flat {
		if f.Group == want {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			return start, i
		}
	}
	if start < 0 {
		return 0, len(m.flat)
	}
	return start, len(m.flat)
}

func (m *model) moveGroup(delta int) {
	if len(m.groups) == 0 {
		return
	}
	if m.pane == paneDetail {
		m.groupCursorMem[m.groups[m.groupCursor]] = m.cursor
	}
	m.groupCursor += delta
	if m.groupCursor < 0 {
		m.groupCursor = 0
	}
	if m.groupCursor >= len(m.groups) {
		m.groupCursor = len(m.groups) - 1
	}
	start, end := m.groupBounds()
	if mem, ok := m.groupCursorMem[m.groups[m.groupCursor]]; ok && mem >= start && mem < end {
		m.cursor = mem
	} else {
		m.cursor = start
	}
	m.exitArmed = false
}

func (m *model) enterDetail() {
	start, end := m.groupBounds()
	if mem, ok := m.groupCursorMem[m.groups[m.groupCursor]]; ok && mem >= start && mem < end {
		m.cursor = mem
	} else {
		m.cursor = start
	}
	m.pane = paneDetail
	m.exitArmed = false
}

// backToRail only flips focus. It never reloads the draft, so unsaved edits
// survive a round trip between the panes.
func (m *model) backToRail() {
	if len(m.groups) > 0 && m.groupCursor < len(m.groups) {
		m.groupCursorMem[m.groups[m.groupCursor]] = m.cursor
	}
	m.pane = paneRail
	m.exitArmed = false
}

func (m *model) requestExit() {
	if m.dirty && !m.exitArmed {
		m.exitArmed = true
		m.status = m.lang.T("settings.unsaved_hint")
		return
	}
	m.screen = screenChat
	m.exitArmed = false
}

func (m *model) moveCursorInGroup(delta int) {
	start, end := m.groupBounds()
	if start >= end {
		return
	}
	m.cursor += delta
	if m.cursor < start {
		m.cursor = start
	}
	if m.cursor >= end {
		m.cursor = end - 1
	}
}

// updateSettingsEdit forwards keys to the inline textinput while a field is
// being edited. enter commits, esc cancels.
func (m *model) updateSettingsEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editMode = false
		m.editKey = ""
		m.statusErr = nil
		return m, nil
	case "enter":
		m.commitEdit()
		return m, nil
	default:
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}
}

// beginEdit opens the inline editor for free-text, numeric and enum fields.
// Secrets are pre-filled empty so their value is never shown.
func (m *model) beginEdit() tea.Cmd {
	f, ok := m.currentField()
	if !ok {
		return nil
	}
	switch f.Kind {
	case config.FieldString, config.FieldInt, config.FieldFloat, config.FieldEnum:
		// editable as free text
	default:
		m.status = m.lang.T("settings.edit_hint")
		m.statusErr = nil
		return nil
	}
	ti := textinput.New()
	ti.Prompt = "edit: "
	ti.CharLimit = 256
	editWidth := m.width - 4
	if !m.narrow && m.detailW > 4 {
		editWidth = m.detailW - 4
	}
	if editWidth > 0 {
		ti.Width = editWidth
	}
	if !f.Secret && m.draft != nil {
		ti.SetValue(m.draft.ValueString(f.Key))
	}
	m.editInput = ti
	m.editMode = true
	m.editKey = f.Key
	return m.editInput.Focus()
}

// commitEdit applies the inline editor value to the draft. A secret submitted
// empty keeps its previous value.
func (m *model) commitEdit() {
	f, ok := config.FieldByKey(m.editKey)
	if !ok {
		m.editMode = false
		m.editKey = ""
		return
	}
	if m.draft == nil {
		m.statusErr = fmt.Errorf("tui: no settings draft loaded")
		m.status = ""
		m.editMode = false
		m.editKey = ""
		return
	}
	val := m.editInput.Value()
	if val == "" && f.Secret {
		m.status = m.fieldLabel(f) + " unchanged"
		m.statusErr = nil
	} else if err := m.draft.SetField(m.editKey, val); err != nil {
		m.statusErr = err
		m.status = ""
	} else {
		m.statusErr = nil
		m.status = m.fieldLabel(f) + " = " + m.draft.ValueString(m.editKey)
		m.markDirty(m.editKey)
	}
	m.editMode = false
	m.editKey = ""
}

// fieldLabel localizes a field label while keeping the English catalog value
// byte-for-byte stable.
func (m *model) fieldLabel(f config.Field) string {
	if m.lang == i18n.LangZh {
		return m.lang.FieldLabel(f.Key)
	}
	return f.Label
}

func (m *model) currentField() (config.Field, bool) {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return config.Field{}, false
	}
	return m.flat[m.cursor], true
}

func (m *model) moveCursor(delta int) {
	if len(m.flat) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.flat) {
		m.cursor = len(m.flat) - 1
	}
}

// adjustField nudges a numeric field by its Step, clamping to Min/Max.
func (m *model) adjustField(dir int) {
	f, ok := m.currentField()
	if !ok || (f.Kind != config.FieldInt && f.Kind != config.FieldFloat) || m.draft == nil {
		return
	}
	v, err := m.draft.GetField(f.Key)
	if err != nil {
		return
	}
	var cur float64
	switch x := v.(type) {
	case int:
		cur = float64(x)
	case float64:
		cur = x
	default:
		return
	}
	next := cur + float64(dir)*f.Step
	if next < f.Min {
		next = f.Min
	}
	if next > f.Max {
		next = f.Max
	}
	var value string
	if f.Kind == config.FieldInt {
		value = strconv.Itoa(int(math.Round(next)))
	} else {
		// Trim accumulated float error so steps like 0.05 stay readable.
		next = math.Round(next*1e6) / 1e6
		value = strconv.FormatFloat(next, 'f', -1, 64)
	}
	if err := m.draft.SetField(f.Key, value); err != nil {
		m.statusErr = err
		m.status = ""
		return
	}
	m.statusErr = nil
	m.status = m.fieldLabel(f) + " = " + m.draft.ValueString(f.Key)
	m.markDirty(f.Key)
}

// toggleOrCycle toggles a bool or advances an enum to its next option.
func (m *model) toggleOrCycle() {
	f, ok := m.currentField()
	if !ok || m.draft == nil {
		return
	}
	switch f.Kind {
	case config.FieldBool:
		v, err := m.draft.GetField(f.Key)
		if err != nil {
			return
		}
		on, _ := v.(bool)
		value := "off"
		if !on {
			value = "on"
		}
		if err := m.draft.SetField(f.Key, value); err != nil {
			m.statusErr = err
			m.status = ""
			return
		}
	case config.FieldEnum:
		if len(f.Options) == 0 {
			return
		}
		cur := m.draft.ValueString(f.Key)
		idx := 0
		for i, opt := range f.Options {
			if opt == cur {
				idx = i
				break
			}
		}
		next := f.Options[(idx+1)%len(f.Options)]
		if err := m.draft.SetField(f.Key, next); err != nil {
			m.statusErr = err
			m.status = ""
			return
		}
	default:
		return
	}
	m.statusErr = nil
	m.status = m.fieldLabel(f) + " = " + m.draft.ValueString(f.Key)
	m.markDirty(f.Key)
}

// clearOverride removes the workspace override and restores the inherited
// global value. In Global mode there are no overrides, so it is a no-op.
func (m *model) clearOverride() {
	if m.info.Mode != "workspace" {
		m.status = "no workspace overrides in Global mode"
		m.statusErr = nil
		return
	}
	f, ok := m.currentField()
	if !ok {
		return
	}
	delete(m.overrideMark, f.Key)
	if m.info.Global != nil && m.draft != nil {
		v, err := m.info.Global.GetField(f.Key)
		if err == nil {
			if err := m.draft.SetField(f.Key, fieldValueString(v)); err != nil {
				m.statusErr = err
				m.status = ""
				return
			}
		}
	}
	m.statusErr = nil
	m.status = m.fieldLabel(f) + " reset to inherited value"
	m.dirty = true
	m.exitArmed = false
}

func (m *model) toggleMode() {
	if m.info.Mode == "workspace" {
		m.info.Mode = "global"
	} else {
		m.info.Mode = "workspace"
	}
	m.status = m.lang.Tf("settings.mode", m.modeLabel())
	m.statusErr = nil
	m.dirty = true
	m.exitArmed = false
}

// markOverride records that a field is now a workspace override. Edits made in
// Global mode change the global config directly and leave no override.
func (m *model) markOverride(key string) {
	if m.info.Mode == "workspace" {
		m.overrideMark[key] = true
	}
}

// markDirty records an unsaved edit, arms the exit guard, and rebinds the
// catalog immediately when the language field itself changes.
func (m *model) markDirty(key string) {
	m.markOverride(key)
	m.dirty = true
	m.exitArmed = false
	if key == "language" && m.draft != nil {
		m.lang = i18n.Parse(m.draft.Language)
	}
}

func (m *model) saveSettings() tea.Cmd {
	draft := m.draft.Clone()
	overrides := copyOverrides(m.overrideMark)
	return settingsSaveCmd(m.opts, draft, m.info.Mode, overrides)
}

func (m *model) onSettingsLoaded(msg settingsLoadedMsg) {
	// A stale reload must never overwrite a draft the user is still editing.
	// gen == 0 means the message came from a test or an older caller.
	if msg.gen != 0 && msg.gen != m.loadGen {
		return
	}
	m.loading = false
	if msg.err != nil {
		m.statusErr = msg.err
		m.status = ""
		return
	}
	// Keep unsaved edits when the post-save reload lands late.
	if !m.dirty {
		m.draft = msg.draft
		if m.draft == nil {
			m.draft = config.Default()
		}
		m.lang = i18n.Parse(m.draft.Language)
	}
	m.info = msg.info
	m.flat = config.Fields()
	m.buildGroups()
	m.overrideMark = copyOverrides(msg.info.Overrides)
	if m.pane == paneRail {
		start, _ := m.groupBounds()
		m.cursor = start
	}
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		m.cursor = 0
	}
	m.statusErr = nil
}

func (m *model) onSettingsSaved(msg settingsSavedMsg) tea.Cmd {
	if msg.err != nil {
		m.statusErr = msg.err
		m.status = ""
		return nil
	}
	if msg.result.Message != "" {
		m.status = msg.result.Message
	}
	// Language may have just changed; rebind the catalog immediately so the
	// user sees the new language without restarting the TUI.
	if m.draft != nil {
		m.lang = i18n.Parse(m.draft.Language)
	}
	m.statusErr = nil
	m.dirty = false
	m.exitArmed = false
	if msg.result.ReindexRequired {
		m.reindexRequired = true
		m.reindexChanged = append([]string(nil), msg.result.ChangedFields...)
	}
	// A save can swap the runner/store, so rebind the live references.
	if m.opts.Refresh != nil {
		store, runner := m.opts.Refresh()
		m.opts.Store = store
		m.opts.Runner = runner
	}
	// Reload asynchronously so the draft reflects exactly what was persisted.
	return settingsLoadCmd(m.opts, m.loadGen)
}

func (m *model) settingsStale() (bool, []string) {
	if !m.info.IndexStale && !m.reindexRequired {
		return false, nil
	}
	seen := make(map[string]bool)
	var fields []string
	for _, f := range m.info.StaleFields {
		if !seen[f] {
			seen[f] = true
			fields = append(fields, f)
		}
	}
	for _, f := range m.reindexChanged {
		if !seen[f] {
			seen[f] = true
			fields = append(fields, f)
		}
	}
	return true, fields
}

func (m *model) settingsView() string {
	var b strings.Builder
	b.WriteString(settingsHeaderStyle.Render(m.lang.Tf("settings.header", m.info.Root)))
	b.WriteRune('\n')
	b.WriteString(settingsSubHeaderStyle.Render(m.lang.Tf("settings.mode", m.modeLabel()) + " · " + m.info.FilePath))
	b.WriteRune('\n')

	if stale, fields := m.settingsStale(); stale {
		b.WriteString(settingsStaleStyle.Render(m.lang.Tf("settings.stale", strings.Join(fields, ", "))))
		b.WriteRune('\n')
	}
	if m.loading {
		b.WriteString(settingsStatusStyle.Render(m.lang.T("settings.loading")))
		b.WriteRune('\n')
	}

	// Reserve rows for the two headers, optional stale/loading lines, the
	// inline editor, status and help footer.
	paneHeight := m.height - 8
	if paneHeight < 6 {
		paneHeight = 6
	}
	b.WriteString(m.settingsBody(paneHeight))
	b.WriteRune('\n')

	if m.editMode {
		b.WriteString(m.editInput.View())
		b.WriteRune('\n')
	}
	if m.statusErr != nil {
		b.WriteString(settingsErrorStyle.Render(m.lang.Tf("chat.error", m.statusErr.Error())))
		b.WriteRune('\n')
	} else if m.status != "" {
		b.WriteString(settingsStatusStyle.Render(m.status))
		b.WriteRune('\n')
	}

	help := m.settingsHelp()
	if m.lang == i18n.LangZh {
		// CJK glyphs occupy two terminal cells.
		help = truncate(help, m.width/2)
	} else {
		help = truncate(help, m.width)
	}
	b.WriteString(settingsHelpStyle.Render(help))
	return b.String()
}

func (m *model) settingsHelp() string {
	if m.narrow {
		if m.lang == i18n.LangZh {
			return m.lang.T("settings.help")
		}
		return settingsHelpLine
	}
	if m.pane == paneRail {
		return m.lang.T("settings.help_rail")
	}
	return m.lang.T("settings.help_detail")
}

// settingsBody renders either the two-pane master-detail view or, below 80
// columns, the original single scrolling list.
func (m *model) settingsBody(h int) string {
	if m.narrow {
		return m.settingsLegacyBody(h)
	}
	rail := lipgloss.NewStyle().Width(m.railW).Height(h).Render(m.renderRail(h))
	detail := lipgloss.NewStyle().Width(m.detailW).Height(h).Render(m.renderDetail(h))
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, detail)
}

func (m *model) renderRail(h int) string {
	var b strings.Builder
	for i, g := range m.groups {
		label := g
		if m.lang == i18n.LangZh {
			label = m.lang.GroupLabel(g)
		}
		label = truncate(label, m.railW-3)
		prefix := "  "
		style := settingsRailStyle
		if i == m.groupCursor {
			prefix = "> "
			style = settingsRailActiveStyle
		}
		if i > 0 {
			b.WriteRune('\n')
		}
		b.WriteString(style.Render(prefix + label))
	}
	for n := len(m.groups); n < h; n++ {
		b.WriteRune('\n')
	}
	return b.String()
}

func (m *model) renderDetail(h int) string {
	start, end := m.groupBounds()
	group := ""
	if len(m.groups) > 0 && m.groupCursor < len(m.groups) {
		group = m.groups[m.groupCursor]
		if m.lang == i18n.LangZh {
			group = m.lang.GroupLabel(group)
		}
	}

	var b strings.Builder
	breadcrumb := m.lang.Tf("settings.group_count", end-start)
	if m.dirty {
		breadcrumb += " · " + m.lang.T("settings.unsaved_mark")
	}
	b.WriteString(settingsBreadcrumbStyle.Render(truncate(group+" · "+breadcrumb, m.detailW)))

	rows := h - 2 // breadcrumb + focused-field help
	if rows < 1 {
		rows = 1
	}
	lo, hi := start, end
	if end-start > rows {
		lo = m.cursor - rows/2
		if lo < start {
			lo = start
		}
		if lo > end-rows {
			lo = end - rows
		}
		hi = lo + rows
	}
	for i := lo; i < hi; i++ {
		b.WriteRune('\n')
		b.WriteString(m.renderFieldRowCompact(i, m.flat[i], m.detailW))
	}
	for n := hi - lo; n < rows; n++ {
		b.WriteRune('\n')
	}
	b.WriteRune('\n')
	if f, ok := m.currentField(); ok {
		hint := f.Help
		if hint == "" && (f.Kind == config.FieldInt || f.Kind == config.FieldFloat) {
			hint = fmt.Sprintf("%g..%g step %g", f.Min, f.Max, f.Step)
		}
		hint = truncate(hint, m.detailW-4)
		b.WriteString(settingsFocusHelpStyle.Render(hint))
	}
	return b.String()
}

// renderFieldRowCompact is the detail-pane row. Numeric ranges moved to the
// focused-field help line so a 80-column terminal never wraps a row.
func (m *model) renderFieldRowCompact(i int, f config.Field, width int) string {
	selected := i == m.cursor && m.pane == paneDetail
	prefix := "  "
	if selected {
		prefix = "> "
	}
	marker := "G"
	if m.info.Mode == "workspace" && m.overrideMark[f.Key] {
		marker = "W"
	}
	value := ""
	if m.draft != nil {
		value = m.draft.ValueString(f.Key)
	}
	label := f.Label
	idxTag := " IDX"
	if m.lang == i18n.LangZh {
		label = m.lang.FieldLabel(f.Key)
		idxTag = " " + m.lang.T("settings.idx_tag")
	}
	tag := ""
	if f.IndexAffect {
		tag = idxTag
	}
	plain := prefix + marker + "  " + fmt.Sprintf("%-18s %s", label, value) + tag
	plain = truncate(plain, width)
	style := settingsRowStyle
	if selected {
		style = settingsSelectedStyle
	}
	return style.Render(plain)
}

// settingsLegacyBody is the <80-column fallback: one windowed list with
// inline group headers, identical to the original rendering.
func (m *model) settingsLegacyBody(h int) string {
	var b strings.Builder
	// Reserve rows for inline group headers and the above/below indicators.
	// Without this the single list grows past the terminal and pushes the
	// header off the top of the screen.
	available := h - 8
	if available < 5 {
		available = 5
	}
	start, end := 0, len(m.flat)
	if available < len(m.flat) {
		start = m.cursor - available/2
		if start < 0 {
			start = 0
		}
		if start > len(m.flat)-available {
			start = len(m.flat) - available
		}
		end = start + available
	}
	if start > 0 {
		b.WriteString(settingsStatusStyle.Render("… " + strconv.Itoa(start) + " more above"))
		b.WriteRune('\n')
	}
	lastGroup := ""
	for i := start; i < end; i++ {
		f := m.flat[i]
		if f.Group != lastGroup {
			group := f.Group
			if m.lang == i18n.LangZh {
				group = m.lang.GroupLabel(f.Group)
			}
			b.WriteString(settingsGroupStyle.Render(group))
			b.WriteRune('\n')
			lastGroup = f.Group
		}
		b.WriteString(m.renderFieldRow(i, f))
		b.WriteRune('\n')
	}
	if end < len(m.flat) {
		b.WriteString(settingsStatusStyle.Render("… " + strconv.Itoa(len(m.flat)-end) + " more below"))
		b.WriteRune('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) renderFieldRow(i int, f config.Field) string {
	selected := i == m.cursor
	prefix := "  "
	if selected {
		prefix = "> "
	}
	// "G" marks a value inherited from global, "W" an active workspace
	// override. The settings store retains override keys even in Global mode
	// but does not apply them there, so only render "W" when the workspace
	// mode is actually active; otherwise those inactive marks would look like
	// live overrides.
	marker := settingsInheritedStyle.Render("G")
	if m.info.Mode == "workspace" && m.overrideMark[f.Key] {
		marker = settingsOverrideStyle.Render("W")
	}
	value := ""
	if m.draft != nil {
		value = m.draft.ValueString(f.Key)
	}
	label := f.Label
	idxTag := " IDX"
	if m.lang == i18n.LangZh {
		label = m.lang.FieldLabel(f.Key)
		idxTag = " " + m.lang.T("settings.idx_tag")
	}
	hint := ""
	if f.Kind == config.FieldInt || f.Kind == config.FieldFloat {
		hint = fmt.Sprintf(" [%g..%g step %g]", f.Min, f.Max, f.Step)
	}
	bodyStyle := settingsRowStyle
	if selected {
		bodyStyle = settingsSelectedStyle
	}
	row := prefix + marker + "  " + bodyStyle.Render(fmt.Sprintf("%-20s %s%s", label, value, hint))
	if f.IndexAffect {
		row += settingsIdxStyle.Render(idxTag)
	}
	return row
}

// modeLabel keeps the English wording stable while allowing a localized
// label on the settings screen.
func (m *model) modeLabel() string {
	if m.lang == i18n.LangZh {
		if m.info.Mode == "workspace" {
			return m.lang.T("settings.mode_workspace")
		}
		return m.lang.T("settings.mode_global")
	}
	return modeLabel(m.info.Mode)
}

func modeLabel(mode string) string {
	if mode == "workspace" {
		return "Workspace"
	}
	return "Global"
}

func copyOverrides(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// fieldValueString converts a GetField value into the string form SetField
// expects. It is used when restoring an inherited global value.
func fieldValueString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case bool:
		if x {
			return "on"
		}
		return "off"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
