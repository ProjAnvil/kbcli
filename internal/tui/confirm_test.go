package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/projanvil/kbcli/internal/agent"
	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/index"
	"github.com/projanvil/kbcli/internal/scan"
)

type confirmCapture struct {
	indexCalls int
	indexRoots []string
	scanCalls  int
	refreshed  int
}

func confirmOpts(risk scan.Risk, warnings []string) (Options, *confirmCapture) {
	cap := &confirmCapture{}
	opts := baseOpts()
	rep := scan.Report{
		Root:       "/kb/docs",
		Files:      12,
		Dirs:       3,
		Bytes:      4096,
		MaxDepth:   3,
		Extensions: []scan.ExtCount{{Ext: ".md", Count: 12}},
		Risk:       risk,
		Warnings:   warnings,
	}
	opts.IndexReport = &rep
	opts.Preflight = func(root string) (scan.Report, error) {
		cap.scanCalls++
		r := rep
		r.Root = root
		r.Risk = scan.RiskOK
		r.Warnings = nil
		return r, nil
	}
	opts.RunIndex = func(root string, progress func(IndexProgress)) (int, error) {
		cap.indexCalls++
		cap.indexRoots = append(cap.indexRoots, root)
		progress(IndexProgress{Phase: "indexing", FilesDone: 1, FilesTotal: 12, Chunks: 2, Current: "a.md"})
		return 2, nil
	}
	opts.Refresh = func() (index.Store, agent.Answerer) {
		cap.refreshed++
		return nil, nil
	}
	return opts, cap
}

func confirmDir(t *testing.T, m *model) *model {
	t.Helper()
	got, _ := pressKey(m, "c")
	m = got.(*model)
	if m.confirm.dirCheckMode {
		name := filepath.Base(filepath.Clean(m.confirm.report.Root))
		for _, r := range name {
			got, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			m = got.(*model)
		}
		got, _ = pressKey(m, "enter")
		m = got.(*model)
	}
	if !m.confirm.dirConfirmed {
		t.Fatal("directory was not confirmed")
	}
	return m
}

func TestConfirmScreenShownWhenNoIndex(t *testing.T) {
	opts, _ := confirmOpts(scan.RiskOK, nil)
	m := testModel(t, opts)
	if m.screen != screenConfirm {
		t.Fatal("first run without an index must show the confirmation screen")
	}
	view := m.View()
	for _, want := range []string{"first index confirmation", "/kb/docs", "12 files", "Risk: low", "[ c ] Confirm this directory", "Confirm the directory above"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirm view missing %q: %s", want, view)
		}
	}
}

func TestConfirmCancelNeverIndexes(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskOK, nil)
	m := testModel(t, opts)
	got, _ := pressKey(m, "n")
	m = got.(*model)
	if m.screen != screenChat {
		t.Fatal("n did not return to the chat screen")
	}
	if cap.indexCalls != 0 {
		t.Fatal("cancel started an index")
	}
}

func TestConfirmYesStartsIndexAndSpinner(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskOK, nil)
	m := confirmDir(t, testModel(t, opts))
	got, cmd := pressKey(m, "y")
	m = got.(*model)
	if cmd == nil {
		t.Fatal("y returned no async command")
	}
	if !m.confirm.indexing {
		t.Fatal("indexing flag was not set")
	}
	if !strings.Contains(m.View(), "indexing") {
		t.Fatal("spinner/progress line missing")
	}
	_ = cap

	// The modal screen must ignore keys while indexing.
	got, _ = pressKey(m, "y")
	m = got.(*model)
	if !m.confirm.indexing {
		t.Fatal("indexing was cancelled by a keypress")
	}

	got, _ = m.Update(indexProgressMsg{p: IndexProgress{Phase: "indexing", FilesDone: 5, FilesTotal: 12, Chunks: 7, Current: "b.md"}})
	m = got.(*model)
	if m.confirm.progress.FilesDone != 5 {
		t.Fatal("progress message was not applied")
	}

	got, _ = m.Update(indexDoneMsg{n: 7})
	m = got.(*model)
	if m.screen != screenChat {
		t.Fatal("successful index did not return to chat")
	}
	if m.confirm.indexing {
		t.Fatal("indexing flag not cleared")
	}
	if !strings.Contains(strings.Join(chatTexts(m), "\n"), "Index ready: 7 chunks") {
		t.Fatal("completion status line missing")
	}
}

func TestConfirmHighRiskNeedsTwoYes(t *testing.T) {
	opts, _ := confirmOpts(scan.RiskHigh, []string{"many_files"})
	m := confirmDir(t, testModel(t, opts))
	got, cmd := pressKey(m, "y")
	m = got.(*model)
	if cmd != nil || m.confirm.indexing {
		t.Fatal("first y must only arm the confirmation")
	}
	if !m.confirm.armed || !strings.Contains(m.View(), "Press y again") {
		t.Fatal("second-confirmation hint missing")
	}
	got, cmd = pressKey(m, "y")
	m = got.(*model)
	if cmd == nil || !m.confirm.indexing {
		t.Fatal("second y did not start indexing")
	}
}

func TestConfirmCriticalBlocksYes(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskCritical, []string{"protected:home directory"})
	m := testModel(t, opts)
	view := m.View()
	if strings.Contains(view, "[ y ] Start indexing") {
		t.Fatal("critical risk must hide the yes button")
	}
	if !strings.Contains(view, "blocked") {
		t.Fatal("blocked explanation missing")
	}
	m = confirmDir(t, m)
	got, _ := pressKey(m, "y")
	m = got.(*model)
	if cap.indexCalls != 0 || m.confirm.indexing {
		t.Fatal("y started an index despite critical risk")
	}
	if m.confirm.err == nil {
		t.Fatal("y did not report the block")
	}
}

func TestConfirmChooseSubdirectory(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskCritical, []string{"protected:home directory"})
	m := testModel(t, opts)
	got, _ := pressKey(m, "p")
	m = got.(*model)
	if !m.confirm.pathMode {
		t.Fatal("p did not open the path input")
	}
	// Type a subdirectory and submit it.
	for _, r := range "docs" {
		got, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = got.(*model)
	}
	got, _ = pressKey(m, "enter")
	m = got.(*model)
	if m.confirm.pathMode {
		t.Fatal("enter did not close the path input")
	}
	if cap.scanCalls != 1 {
		t.Fatal("entering a path did not re-run the preflight")
	}
	if m.confirm.report.Risk == scan.RiskCritical {
		t.Fatal("subdirectory report should no longer be critical")
	}
	if !strings.Contains(m.View(), "[ c ] Confirm this directory") {
		t.Fatal("directory confirmation not required again after changing the root")
	}
}

func TestConfirmIndexFailureStaysOnScreen(t *testing.T) {
	opts, _ := confirmOpts(scan.RiskOK, nil)
	opts.RunIndex = func(string, func(IndexProgress)) (int, error) {
		return 0, errors.New("disk full")
	}
	m := confirmDir(t, testModel(t, opts))
	got, _ := pressKey(m, "y")
	m = got.(*model)
	got, _ = m.Update(indexDoneMsg{err: errors.New("disk full")})
	m = got.(*model)
	if m.screen != screenConfirm {
		t.Fatal("failed index must stay on the confirmation screen")
	}
	if !strings.Contains(m.View(), "Indexing failed") {
		t.Fatal("failure message missing")
	}
}

func TestConfirmChinese(t *testing.T) {
	opts, _ := confirmOpts(scan.RiskOK, nil)
	zh := config.Default()
	zh.Language = config.LangZh
	opts.Config = zh
	m := testModel(t, opts)
	view := m.View()
	for _, want := range []string{"首次索引确认", "目录：", "扫描：", "风险：低", "[ c ] 确认此目录", "索引前请先确认上方目录"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Chinese confirm view missing %q", want)
		}
	}
}

func chatTexts(m *model) []string {
	out := make([]string, 0, len(m.chatLines))
	for _, l := range m.chatLines {
		out = append(out, l.text)
	}
	return out
}

func TestConfirmTargetParsesDirectory(t *testing.T) {
	cases := []struct {
		line string
		ok   bool
		dir  string
	}{
		{":index", true, ""},
		{"index", true, ""},
		{":index ./docs", true, "./docs"},
		{"index /tmp/docs", true, "/tmp/docs"},
		{":indexer", false, ""},
		{"reindex", false, ""},
	}
	for _, tc := range cases {
		ok, dir := confirmTarget(tc.line)
		if ok != tc.ok || dir != tc.dir {
			t.Fatalf("confirmTarget(%q) = %v, %q; want %v, %q", tc.line, ok, dir, tc.ok, tc.dir)
		}
	}
}

func TestStartupWithoutIndexShowsGuideOnly(t *testing.T) {
	opts := baseOpts()
	opts.Store = nil
	opts.IndexReport = nil
	m := testModel(t, opts)
	if m.screen != screenChat {
		t.Fatal("startup without an index must go straight to chat")
	}
	if !strings.Contains(m.View(), "No index in this directory") {
		t.Fatal("command hint missing from the chat screen")
	}
	if strings.Contains(m.View(), "[ c ] Confirm this directory") {
		t.Fatal("startup must not open the confirmation dialog")
	}
}

func TestDirectoryCheckRejectsWrongName(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskHigh, []string{"many_files"})
	m := testModel(t, opts)
	got, _ := pressKey(m, "c")
	m = got.(*model)
	if !m.confirm.dirCheckMode {
		t.Fatal("large corpus must require a typed directory name")
	}
	for _, r := range "wrong" {
		got, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = got.(*model)
	}
	got, _ = pressKey(m, "enter")
	m = got.(*model)
	if m.confirm.dirConfirmed {
		t.Fatal("wrong directory name was accepted")
	}
	if m.confirm.err == nil {
		t.Fatal("mismatch error missing")
	}
	if cap.indexCalls != 0 {
		t.Fatal("index started despite a mismatched directory")
	}
}

func TestYBeforeDirectoryConfirmationIsRejected(t *testing.T) {
	opts, cap := confirmOpts(scan.RiskOK, nil)
	m := testModel(t, opts)
	got, _ := pressKey(m, "y")
	m = got.(*model)
	if cap.indexCalls != 0 || m.confirm.indexing {
		t.Fatal("y indexed before the directory was confirmed")
	}
	if m.confirm.err == nil {
		t.Fatal("missing confirmation-first error")
	}
}

func TestIndexCommandUsesWorkspaceRoot(t *testing.T) {
	var scanned string
	opts := baseOpts()
	opts.WorkspaceRoot = "/kb/docs"
	opts.Preflight = func(root string) (scan.Report, error) {
		scanned = root
		return scan.Report{Root: root, Files: 3, Risk: scan.RiskOK}, nil
	}
	m := testModel(t, opts)
	m.input.SetValue(":index")
	got, _ := pressKey(m, "enter")
	m = got.(*model)
	if m.screen != screenConfirm {
		t.Fatal(":index did not open the confirmation screen")
	}
	if scanned != "/kb/docs" {
		t.Fatalf(":index scanned %q, want the workspace root /kb/docs", scanned)
	}
}

func TestIndexCommandResolvesDirectoryArgument(t *testing.T) {
	var scanned string
	opts := baseOpts()
	opts.WorkspaceRoot = "/kb/docs"
	opts.Preflight = func(root string) (scan.Report, error) {
		scanned = root
		return scan.Report{Root: root, Files: 1, Risk: scan.RiskOK}, nil
	}
	m := testModel(t, opts)
	m.input.SetValue(":index ./api")
	got, _ := pressKey(m, "enter")
	m = got.(*model)
	if m.screen != screenConfirm {
		t.Fatal(":index ./api did not open the confirmation screen")
	}
	if scanned != filepath.Join("/kb/docs", "api") {
		t.Fatalf(":index ./api scanned %q", scanned)
	}
}
