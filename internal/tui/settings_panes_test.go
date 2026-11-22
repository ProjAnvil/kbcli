package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

func TestRailToDetailAndBack(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	if m.pane != paneRail {
		t.Fatalf("settings should open on the rail, got pane=%d", m.pane)
	}
	if len(m.groups) != 7 {
		t.Fatalf("groups = %d, want 7", len(m.groups))
	}

	got, _ := pressKey(m, "down")
	m = got.(*model)
	if m.groupCursor != 1 {
		t.Fatalf("down did not move the rail, groupCursor=%d", m.groupCursor)
	}

	got, _ = pressKey(m, "right")
	m = got.(*model)
	if m.pane != paneDetail {
		t.Fatal("right did not enter the detail pane")
	}
	start, _ := m.groupBounds()
	if m.cursor < start || m.groupCursor != 1 {
		t.Fatalf("detail cursor=%d outside group starting at %d", m.cursor, start)
	}

	got, _ = pressKey(m, "esc")
	m = got.(*model)
	if m.pane != paneRail {
		t.Fatal("esc did not return to the rail")
	}
	if m.screen != screenSettings {
		t.Fatal("esc from detail must not leave the settings screen")
	}
}

func TestBackspaceIsBackAliasButNotDelete(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.enterDetail()

	got, _ := pressKey(m, "backspace")
	m = got.(*model)
	if m.pane != paneRail {
		t.Fatal("backspace in the detail pane did not go back")
	}

	// In the inline editor backspace must still delete a character.
	m.enterDetail()
	selectField(t, m, "model")
	got, cmd := pressKey(m, "e")
	m = got.(*model)
	if !m.editMode {
		t.Fatal("e did not open the inline editor")
	}
	_ = cmd
	before := m.editInput.Value()
	got, _ = pressKey(m, "backspace")
	m = got.(*model)
	if !m.editMode {
		t.Fatal("backspace left the inline editor")
	}
	if len(m.editInput.Value()) >= len(before) {
		t.Fatal("backspace did not delete a character in edit mode")
	}
}

func TestLeftIsInertOnRailAndAdjustsInDetail(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	before := m.draft.TopK

	m.pane = paneRail
	got, _ := pressKey(m, "left")
	m = got.(*model)
	if m.draft.TopK != before {
		t.Fatalf("left mutated the draft on the rail: %d", m.draft.TopK)
	}

	m.enterDetail()
	got, _ = pressKey(m, "right")
	m = got.(*model)
	if m.draft.TopK != before+1 {
		t.Fatalf("right did not nudge the value in detail: %d", m.draft.TopK)
	}
}

func TestCursorNeverCrossesGroupBoundary(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.enterDetail()
	start, end := m.groupBounds()
	wantEnd := 0
	for _, f := range m.flat {
		if f.Group == m.flat[0].Group {
			wantEnd++
		}
	}
	if start != 0 || end != wantEnd {
		t.Fatalf("Foundation bounds = %d..%d, want 0..%d", start, end, wantEnd)
	}
	m.cursor = end - 1
	got, _ := pressKey(m, "down")
	m = got.(*model)
	if m.cursor != end-1 {
		t.Fatalf("cursor crossed into the next group: %d", m.cursor)
	}
	m.cursor = start
	got, _ = pressKey(m, "up")
	m = got.(*model)
	if m.cursor != start {
		t.Fatalf("cursor crossed above the group: %d", m.cursor)
	}
}

func TestBackToRailPreservesUnsavedDraft(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	got, _ := pressKey(m, "right")
	m = got.(*model)
	if !m.dirty {
		t.Fatal("editing did not mark the draft dirty")
	}
	want := m.draft.TopK

	got, _ = pressKey(m, "backspace")
	m = got.(*model)
	got, _ = pressKey(m, "right")
	m = got.(*model)
	if m.draft.TopK != want {
		t.Fatalf("round trip lost the edit: %d, want %d", m.draft.TopK, want)
	}
}

func TestDirtyExitGuard(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	got, _ := pressKey(m, "right")
	m = got.(*model)
	got, _ = pressKey(m, "backspace") // detail -> rail
	m = got.(*model)

	got, _ = pressKey(m, "esc")
	m = got.(*model)
	if m.screen != screenSettings {
		t.Fatal("first exit attempt must warn, not leave the screen")
	}
	if !strings.Contains(m.View(), "Unsaved") && !strings.Contains(m.status, "Unsaved") {
		t.Fatal("no unsaved-changes hint shown")
	}
	got, _ = pressKey(m, "esc")
	m = got.(*model)
	if m.screen != screenChat {
		t.Fatal("second exit attempt did not leave the settings screen")
	}
}

func TestStaleSettingsLoadIsDiscarded(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	want := m.draft.TopK
	changed := config.Default()
	changed.TopK = 42
	got, _ := m.Update(settingsLoadedMsg{
		draft: changed,
		info:  m.info,
		gen:   m.loadGen + 99,
	})
	m = got.(*model)
	if m.draft.TopK != want {
		t.Fatalf("stale reload overwrote the draft: %d", m.draft.TopK)
	}
}

func TestPostSaveReloadDoesNotClobberDirtyDraft(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "top_k")
	m.draft.TopK = 33
	m.dirty = true
	changed := config.Default()
	changed.TopK = 7
	got, _ := m.Update(settingsLoadedMsg{draft: changed, info: m.info, gen: m.loadGen})
	m = got.(*model)
	if m.draft.TopK != 33 {
		t.Fatalf("late reload clobbered an unsaved edit: %d", m.draft.TopK)
	}
}

func TestNarrowTerminalFallsBackToSingleList(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.applySize(70, 40)
	if !m.narrow {
		t.Fatal("70 columns must trigger the narrow fallback")
	}
	selectField(t, m, "top_k")
	before := m.draft.TopK
	got, _ := pressKey(m, "right")
	m = got.(*model)
	if m.draft.TopK != before+1 {
		t.Fatal("narrow mode lost left/right value adjustment")
	}
	got, _ = pressKey(m, "esc")
	m = got.(*model)
	if m.screen != screenSettings {
		t.Fatal("narrow mode: first exit attempt must warn about the dirty draft")
	}
	got, _ = pressKey(m, "esc")
	m = got.(*model)
	if m.screen != screenChat {
		t.Fatal("narrow mode: second exit attempt did not leave the settings screen")
	}
}

func TestLanguageSwitchRebindsCatalogImmediately(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	selectField(t, m, "language")
	got, _ := pressKey(m, "enter")
	m = got.(*model)
	if m.lang != i18n.LangZh {
		t.Fatalf("language switch did not rebind the catalog: %q", m.lang)
	}
	if !strings.Contains(m.View(), "kbcli 设置") {
		t.Fatal("Chinese header not rendered immediately after the switch")
	}
	if !m.dirty {
		t.Fatal("language switch must mark the draft dirty")
	}
}

func TestPaneSpecificHelp(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	rail := m.settingsHelp()
	m.enterDetail()
	detail := m.settingsHelp()
	if rail == detail {
		t.Fatal("rail and detail help lines are identical")
	}
	if !strings.Contains(detail, "left/right value") {
		t.Fatalf("detail help does not document value adjustment: %q", detail)
	}
	if !strings.Contains(rail, "open detail") {
		t.Fatalf("rail help does not document how to enter detail: %q", rail)
	}
}

func TestRailViewShowsAllGroups(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	view := m.View()
	for _, g := range m.groups {
		if !strings.Contains(view, g) {
			t.Fatalf("rail missing group %q", g)
		}
	}
	if strings.Contains(view, "· UNSAVED") {
		t.Fatal("clean draft rendered an unsaved marker")
	}
}

func TestCtrlCStillQuits(t *testing.T) {
	opts, _ := settingsOpts()
	m := settingsModel(t, opts)
	m.enterDetail()
	_, cmd := pressKey(m, "ctrl+c")
	if cmd == nil {
		t.Fatal("ctrl+c returned no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not produce tea.QuitMsg")
	}
}
