package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/i18n"
)

// settingsCapture records interactions with the stub option callbacks so a
// test can assert what SaveSettings / OnCommand received without real disk I/O.
type settingsCapture struct {
	loadCalled      int
	saveCalled      int
	savedDraft      *config.Config
	savedMode       string
	savedOverrides  map[string]bool
	onCommandCalled []string
}

// settingsOpts builds Options with stub Load/Save/OnCommand. The global and
// draft intentionally differ on top_k so tests can observe override clears and
// snapshots. config.Default() needs no disk, so no HOME/TempDir setup is used.
func settingsOpts() (Options, *settingsCapture) {
	cap := &settingsCapture{}
	global := config.Default()
	global.TopK = 3
	draft := config.Default()
	draft.TopK = 20

	opts := baseOpts()
	opts.LoadSettings = func() (*config.Config, SettingsInfo, error) {
		cap.loadCalled++
		return draft.Clone(), SettingsInfo{
			Root:        "/kb",
			Mode:        "workspace",
			Overrides:   map[string]bool{"top_k": true},
			Global:      global,
			FilePath:    "/home/.kbcli/settings.json",
			IndexStale:  false,
			StaleFields: nil,
		}, nil
	}
	opts.SaveSettings = func(d *config.Config, mode string, overrides map[string]bool) (ApplyResult, error) {
		cap.saveCalled++
		cap.savedDraft = d
		cap.savedMode = mode
		cap.savedOverrides = overrides
		return ApplyResult{Message: "settings saved"}, nil
	}
	opts.OnCommand = func(line string) (string, error) {
		cap.onCommandCalled = append(cap.onCommandCalled, line)
		return "ok: " + line, nil
	}
	return opts, cap
}

// settingsModel drives a sized model into the settings screen and completes the
// async load, returning a model with a fully populated draft/flat/overrides.
func settingsModel(t *testing.T, opts Options) *model {
	t.Helper()
	m := testModel(t, opts)
	m.input.SetValue(":settings")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("opening :settings returned no async cmd")
	}
	mm := got.(*model)
	if mm.screen != screenSettings {
		t.Fatal("screen did not switch to settings")
	}
	loaded, ok := cmd().(settingsLoadedMsg)
	if !ok {
		t.Fatal("settings cmd did not yield settingsLoadedMsg")
	}
	final, _ := mm.Update(loaded)
	fm := final.(*model)
	if fm.loading {
		t.Fatal("loading was not cleared after settingsLoadedMsg")
	}
	if len(fm.flat) != len(config.Fields()) {
		t.Fatal("flat rows were not built from config.Fields()")
	}
	return fm
}

// selectField moves the settings cursor onto the row for key.
func selectField(t *testing.T, m *model, key string) {
	t.Helper()
	for i, f := range m.flat {
		if f.Key == key {
			m.cursor = i
			// Per-field keys only apply in the detail pane; put the model
			// there and select the group that owns this field.
			for gi, g := range m.groups {
				if g == f.Group {
					m.groupCursor = gi
					break
				}
			}
			m.pane = paneDetail
			return
		}
	}
	t.Fatalf("field %q is not in flat rows", key)
}

// pressKey sends a single key to the model and returns the resulting model and
// cmd. Named keys map to their tea.KeyType; anything else becomes KeyRunes.
func pressKey(m *model, key string) (tea.Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	got, cmd := m.Update(msg)
	return got, cmd
}

func TestSettingsCommandSwitchesScreen(t *testing.T) {
	opts, cap := settingsOpts()
	m := testModel(t, opts)
	m.input.SetValue(":settings")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(*model)
	if mm.screen != screenSettings {
		t.Fatal(":settings did not switch to the settings screen")
	}
	if cmd == nil {
		t.Fatal(":settings did not trigger an async load cmd")
	}
	if len(cap.onCommandCalled) != 0 {
		t.Fatal(":settings must not be routed through opts.OnCommand")
	}
	for _, line := range []string{"settings", "config", ":settings", ":config"} {
		if !settingsCommand(line) {
			t.Fatalf("settingsCommand(%q) = false, want true", line)
		}
	}
}

func TestSettingsBareConfigOpensScreen(t *testing.T) {
	opts, _ := settingsOpts()
	m := testModel(t, opts)
	m.input.SetValue("config")
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got.(*model).screen != screenSettings {
		t.Fatal("bare config did not open the settings screen")
	}
}

func TestSettingsEscReturnsToChatWithoutQuitting(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	got, cmd := pressKey(m, "esc")
	mm := got.(*model)
	if mm.screen != screenChat {
		t.Fatal("esc did not return to the chat screen")
	}
	if cmd != nil {
		t.Fatal("esc from settings must not return tea.Quit or any cmd")
	}
}

func TestSettingsCtrlCQuitsFromSettings(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	_, cmd := pressKey(m, "ctrl+c")
	if cmd == nil {
		t.Fatal("ctrl+c must quit even from the settings screen")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not produce tea.QuitMsg")
	}
}

func TestSettingsTypingDoesNotChangeChatInput(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.input.SetValue("draft question")
	before := m.input.Value()
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello world")})
	if got.(*model).input.Value() != before {
		t.Fatal("typing in settings leaked into the chat input")
	}
}

func TestSettingsCursorMovesWithinFlatRows(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.enterDetail() // focus the first group's fields
	_, groupEnd := m.groupBounds()
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	got, _ := pressKey(m, "down")
	m = got.(*model)
	if m.cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.cursor)
	}
	got, _ = pressKey(m, "up")
	m = got.(*model)
	if m.cursor != 0 {
		t.Fatalf("up cursor = %d, want 0", m.cursor)
	}
	// Clamp at the top.
	got, _ = pressKey(m, "up")
	m = got.(*model)
	if m.cursor != 0 {
		t.Fatalf("cursor clamped above zero: %d", m.cursor)
	}
	// Cursor is clamped to the active group, not the whole catalog.
	m.cursor = len(m.flat) - 1
	got, _ = pressKey(m, "down")
	m = got.(*model)
	if m.cursor != groupEnd-1 {
		t.Fatalf("cursor escaped the group: %d, want %d", m.cursor, groupEnd-1)
	}
}

func TestSettingsLeftRightAdjustAndClamp(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k") // int, Min 1 Max 50 Step 1, draft 20

	got, _ := pressKey(m, "right")
	m = got.(*model)
	if v, _ := m.draft.GetField("top_k"); v.(int) != 21 {
		t.Fatalf("right did not add Step: got %v, want 21", v)
	}
	got, _ = pressKey(m, "left")
	m = got.(*model)
	if v, _ := m.draft.GetField("top_k"); v.(int) != 20 {
		t.Fatalf("left did not subtract Step: got %v, want 20", v)
	}

	// Clamp at Max.
	m.draft.SetField("top_k", "50")
	for i := 0; i < 3; i++ {
		got, _ = pressKey(m, "right")
		m = got.(*model)
	}
	if v, _ := m.draft.GetField("top_k"); v.(int) != 50 {
		t.Fatalf("right did not clamp at Max: got %v, want 50", v)
	}
	// Clamp at Min.
	m.draft.SetField("top_k", "1")
	for i := 0; i < 3; i++ {
		got, _ = pressKey(m, "left")
		m = got.(*model)
	}
	if v, _ := m.draft.GetField("top_k"); v.(int) != 1 {
		t.Fatalf("left did not clamp at Min: got %v, want 1", v)
	}

	// Float field: Step 0.05 from 0.5 -> 0.55.
	selectField(t, m, "hybrid_vector_weight")
	if err := m.draft.SetField("hybrid_vector_weight", "0.5"); err != nil {
		t.Fatalf("set float: %v", err)
	}
	got, _ = pressKey(m, "right")
	m = got.(*model)
	if v, _ := m.draft.GetField("hybrid_vector_weight"); v.(float64) != 0.55 {
		t.Fatalf("float right = %v, want 0.55", v)
	}
}

func TestSettingsEnterTogglesBoolAndCyclesEnum(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)

	selectField(t, m, "rerank_enabled") // bool, default off
	got, _ := pressKey(m, "enter")
	m = got.(*model)
	if v, _ := m.draft.GetField("rerank_enabled"); !v.(bool) {
		t.Fatal("enter did not toggle bool on")
	}
	got, _ = pressKey(m, "enter")
	m = got.(*model)
	if v, _ := m.draft.GetField("rerank_enabled"); v.(bool) {
		t.Fatal("enter did not toggle bool back off")
	}

	selectField(t, m, "retrieval_method") // enum: vector, bm25, hybrid
	for _, want := range []string{"bm25", "hybrid", "vector"} {
		got, _ = pressKey(m, "enter")
		m = got.(*model)
		if v, _ := m.draft.GetField("retrieval_method"); v.(string) != want {
			t.Fatalf("enum cycle = %v, want %s", v, want)
		}
	}
}

func TestSettingsSpaceAlsoToggles(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "condense_enabled") // bool, default on
	before, _ := m.draft.GetField("condense_enabled")
	got, _ := pressKey(m, " ")
	m = got.(*model)
	after, _ := m.draft.GetField("condense_enabled")
	if after.(bool) == before.(bool) {
		t.Fatal("space did not toggle the bool")
	}
}

func TestSettingsMTogglesMode(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	if m.info.Mode != "workspace" {
		t.Fatalf("initial mode = %q, want workspace", m.info.Mode)
	}
	got, _ := pressKey(m, "m")
	m = got.(*model)
	if m.info.Mode != "global" {
		t.Fatal("m did not switch to global")
	}
	got, _ = pressKey(m, "m")
	m = got.(*model)
	if m.info.Mode != "workspace" {
		t.Fatal("m did not switch back to workspace")
	}
}

func TestSettingsXClearsOverrideRestoresGlobal(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	if v, _ := m.draft.GetField("top_k"); v.(int) != 20 {
		t.Fatalf("precondition draft top_k = %v, want 20", v)
	}
	if !m.overrideMark["top_k"] {
		t.Fatal("precondition: top_k should be marked as override")
	}
	got, _ := pressKey(m, "x")
	m = got.(*model)
	if m.overrideMark["top_k"] {
		t.Fatal("x did not clear the override mark")
	}
	if v, _ := m.draft.GetField("top_k"); v.(int) != 3 {
		t.Fatalf("x did not restore the global value: got %v, want 3", v)
	}
	// In Global mode x is a documented no-op.
	got, _ = pressKey(m, "m") // -> global
	m = got.(*model)
	got, _ = pressKey(m, "x")
	m = got.(*model)
	if !strings.Contains(m.status, "Global") {
		t.Fatalf("x in global mode should report a no-op status, got %q", m.status)
	}
}

func TestSettingsSCallsSaveWithSnapshotAndShowsMessage(t *testing.T) {
	opts, cap := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	got, _ := pressKey(m, "right") // draft.TopK = 21
	m = got.(*model)

	got, cmd := pressKey(m, "s")
	m = got.(*model)
	if cmd == nil {
		t.Fatal("s did not return an async tea.Cmd")
	}
	if cap.saveCalled != 0 {
		t.Fatal("SaveSettings must not run synchronously inside Update")
	}
	saved, ok := cmd().(settingsSavedMsg)
	if !ok {
		t.Fatal("s cmd did not yield settingsSavedMsg")
	}
	if cap.saveCalled != 1 {
		t.Fatalf("SaveSettings called %d times, want 1", cap.saveCalled)
	}
	if cap.savedMode != "workspace" {
		t.Fatalf("SaveSettings mode = %q, want workspace", cap.savedMode)
	}
	if cap.savedDraft == nil {
		t.Fatal("SaveSettings received a nil draft")
	}
	if v, _ := cap.savedDraft.GetField("top_k"); v.(int) != 21 {
		t.Fatalf("SaveSettings draft top_k = %v, want 21", v)
	}
	if !cap.savedOverrides["top_k"] {
		t.Fatal("SaveSettings overrides did not include top_k")
	}
	// The draft passed to SaveSettings must be an independent clone.
	m.draft.SetField("top_k", "7")
	if v, _ := cap.savedDraft.GetField("top_k"); v.(int) != 21 {
		t.Fatal("SaveSettings draft was not a snapshot clone")
	}

	final, reloadCmd := m.Update(saved)
	fm := final.(*model)
	if fm.status != "settings saved" {
		t.Fatalf("status = %q, want the saved message", fm.status)
	}
	if reloadCmd == nil {
		t.Fatal("settingsSavedMsg should trigger an async reload")
	}
}

func TestSettingsSaveErrorShownAsStatus(t *testing.T) {
	opts, _ := settingsOpts()
	opts.SaveSettings = func(d *config.Config, mode string, overrides map[string]bool) (ApplyResult, error) {
		return ApplyResult{}, errors.New("disk full")
	}
	m := settingsModel(t, opts)
	got, cmd := pressKey(m, "s")
	m = got.(*model)
	saved := cmd().(settingsSavedMsg)
	final, _ := m.Update(saved)
	fm := final.(*model)
	if fm.statusErr == nil {
		t.Fatal("a save error should be stored in statusErr, not crash")
	}
	if !strings.Contains(fm.View(), "error:") {
		t.Fatal("save error was not rendered in the settings status line")
	}
}

func TestSettingsReindexRequiredShowsStaleBanner(t *testing.T) {
	opts, _ := settingsOpts()
	opts.SaveSettings = func(d *config.Config, mode string, overrides map[string]bool) (ApplyResult, error) {
		return ApplyResult{ReindexRequired: true, ChangedFields: []string{"chunk_size"}, Message: "saved; reindex needed"}, nil
	}
	m := settingsModel(t, opts)
	got, cmd := pressKey(m, "s")
	m = got.(*model)
	saved := cmd().(settingsSavedMsg)
	final, _ := m.Update(saved)
	view := final.(*model).View()
	if !strings.Contains(view, "INDEX STALE") {
		t.Fatal("a reindex-required save did not show the stale banner")
	}
	if !strings.Contains(view, "chunk_size") {
		t.Fatal("stale banner did not list the changed fields")
	}
}

func TestSettingsReindexAppendsToChat(t *testing.T) {
	opts, cap := settingsOpts()
	m := settingsModel(t, opts)
	got, cmd := pressKey(m, "r")
	m = got.(*model)
	if cmd == nil {
		t.Fatal("r did not return an async cmd")
	}
	res, ok := cmd().(reindexResultMsg)
	if !ok {
		t.Fatal("r cmd did not yield reindexResultMsg")
	}
	found := false
	for _, l := range cap.onCommandCalled {
		if l == "reindex" {
			found = true
		}
	}
	if !found {
		t.Fatal("reindex was not routed through OnCommand")
	}
	final, _ := m.Update(res)
	fm := final.(*model)
	if !chatHas(fm, "ok: reindex") {
		t.Fatal("reindex status was not appended to chat")
	}
	if fm.status == "" {
		t.Fatal("reindex status was not shown in the settings status line")
	}
}

func TestSettingsEditModeCommitsValue(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "model") // FieldString, default "gpt-4o-mini"
	got, _ := pressKey(m, "e")
	m = got.(*model)
	if !m.editMode || m.editKey != "model" {
		t.Fatal("e did not enter inline edit mode for model")
	}
	m.editInput.SetValue("gpt-4o")
	got, _ = pressKey(m, "enter")
	m = got.(*model)
	if m.editMode {
		t.Fatal("enter did not exit inline edit mode")
	}
	if v, _ := m.draft.GetField("model"); v.(string) != "gpt-4o" {
		t.Fatalf("edit did not commit: got %q, want gpt-4o", v.(string))
	}
}

func TestSettingsSecretEditKeepsOldValue(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.draft.SetField("api_key", "secret-1234")
	selectField(t, m, "api_key") // Secret FieldString
	got, _ := pressKey(m, "e")
	m = got.(*model)
	if m.editInput.Value() != "" {
		t.Fatal("secret edit input must be pre-filled empty")
	}
	got, _ = pressKey(m, "enter") // submit empty
	m = got.(*model)
	if v, _ := m.draft.GetField("api_key"); v.(string) != "secret-1234" {
		t.Fatalf("empty secret submit changed the value: got %q", v.(string))
	}
}

func TestSettingsStaleBannerRenders(t *testing.T) {
	opts, _ := settingsOpts()
	opts.LoadSettings = func() (*config.Config, SettingsInfo, error) {
		return config.Default(), SettingsInfo{
			Root:        "/kb",
			Mode:        "workspace",
			Overrides:   map[string]bool{},
			Global:      config.Default(),
			FilePath:    "/home/.kbcli/settings.json",
			IndexStale:  true,
			StaleFields: []string{"chunk_size", "embedder"},
		}, nil
	}
	m := settingsModel(t, opts)
	view := m.View()
	if !strings.Contains(view, "INDEX STALE") {
		t.Fatal("stale banner missing from settings view")
	}
	if !strings.Contains(view, "chunk_size") || !strings.Contains(view, "embedder") {
		t.Fatal("stale banner did not list the stale fields")
	}
	if !strings.Contains(view, "press r to reindex") {
		t.Fatal("stale banner missing the reindex hint")
	}
}

func TestSettingsViewContainsGroupsAndMode(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	// The catalog exceeds a 40-row terminal, so the view renders a scrolling
	// window. Use a tall terminal here to assert the full group set.
	m.height = 100
	view := m.View()
	for _, want := range []string{
		"kbcli settings", "mode:", "Workspace",
		"Foundation", "Ingestion", "Retrieval", "Rerank", "Query Transform",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("settings view missing %q", want)
		}
	}
}

// The settings store retains override keys even when the workspace is in
// Global mode, but does not apply them there. The W marker must therefore be
// gated on the active mode so inactive overrides are not shown as live ones.
func TestSettingsOverrideMarkerInactiveInGlobalMode(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts) // workspace mode, top_k marked as override
	idx := -1
	for i, f := range m.flat {
		if f.Key == "top_k" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("top_k missing from flat rows")
	}
	f := m.flat[idx]
	if !strings.Contains(m.renderFieldRow(idx, f), "W") {
		t.Fatal("workspace mode should render the W override marker for top_k")
	}

	got, _ := pressKey(m, "m") // -> global
	m = got.(*model)
	if m.info.Mode != "global" {
		t.Fatal("m did not switch to global")
	}
	if !m.overrideMark["top_k"] {
		t.Fatal("the stored override should be retained, not dropped")
	}
	if strings.Contains(m.renderFieldRow(idx, f), "W") {
		t.Fatal("global mode must not render an inactive override as W")
	}
	if !strings.Contains(m.renderFieldRow(idx, f), "G") {
		t.Fatal("global mode should render the inherited G marker")
	}
}

func TestSettingsViewChinese(t *testing.T) {
	opts, _ := settingsOpts()
	zh := config.Default()
	zh.Language = config.LangZh
	opts.Config = zh
	opts.LoadSettings = func() (*config.Config, SettingsInfo, error) {
		return zh.Clone(), SettingsInfo{
			Root:      "/kb",
			Mode:      "workspace",
			Overrides: map[string]bool{"language": true},
			Global:    config.Default(),
			FilePath:  "/home/.kbcli/settings.json",
		}, nil
	}
	m := settingsModel(t, opts)
	m.height = 100
	view := m.View()
	for _, want := range []string{
		"kbcli 设置", "模式：", "工作区", "基础", "摄取", "检索", "重排", "查询变换", "会话",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Chinese settings view missing %q", want)
		}
	}
	// Field labels live in the detail pane of the currently selected group.
	for gi, g := range m.groups {
		m.groupCursor = gi
		m.enterDetail()
		v := m.View()
		switch g {
		case "Ingestion":
			if !strings.Contains(v, "分块大小") {
				t.Fatal("Chinese chunk_size label missing")
			}
		case "Query Transform":
			if !strings.Contains(v, "指代消解") {
				t.Fatal("Chinese condense label missing")
			}
		case "Session":
			if !strings.Contains(v, "语言") || !strings.Contains(v, "会话记忆") {
				t.Fatal("Chinese session labels missing")
			}
		}
	}
	if strings.Contains(view, "INDEX STALE") {
		t.Fatal("English stale text leaked into the Chinese screen")
	}
}

func TestSettingsLanguageSwitchUpdatesCatalog(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	if m.lang != i18n.LangEn {
		t.Fatalf("initial lang = %q", m.lang)
	}
	m.draft.Language = config.LangZh
	got, _ := m.Update(settingsSavedMsg{result: ApplyResult{Message: "saved"}})
	m = got.(*model)
	if m.lang != i18n.LangZh {
		t.Fatal("language was not rebound after a settings save")
	}
}
