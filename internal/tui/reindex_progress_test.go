package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// reindexStub emits phase-level progress then returns, without any disk I/O.
type reindexStub struct {
	calls  int
	chunks int
	err    error
}

func (s *reindexStub) run(p func(IndexProgress)) (int, error) {
	s.calls++
	for _, phase := range []string{"loading_model", "scanning", "indexing", "saving"} {
		p(IndexProgress{
			Phase: phase, FilesDone: 2, FilesTotal: 2,
			Chunks: s.chunks, Current: "docs/a.md",
		})
	}
	return s.chunks, s.err
}

// reindexRunner returns the last cmd of a startReindex batch: the one that
// actually invokes ReindexWithProgress.
func reindexRunner(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("startReindex cmd = %T, want tea.BatchMsg", msg)
	}
	if len(batch) == 0 {
		t.Fatal("empty reindex batch")
	}
	return batch[len(batch)-1]
}

func TestReindexModalBlocksKeysAndAdvancesSpinner(t *testing.T) {
	stub := &reindexStub{chunks: 9}
	opts := baseOpts()
	opts.ReindexWithProgress = stub.run
	m := testModel(t, opts)
	m.input.SetValue("draft question")

	m.startReindex(screenChat)
	if !m.reindexing {
		t.Fatal("startReindex did not set the modal immediately")
	}
	if m.reindexFrom != screenChat {
		t.Fatalf("reindexFrom = %v, want screenChat", m.reindexFrom)
	}
	view := m.View()
	for _, want := range []string{"kbcli · reindexing", "loading model", "wait for reindex to finish before chatting"} {
		if !strings.Contains(view, want) {
			t.Fatalf("modal view missing %q: %q", want, view)
		}
	}

	histBefore, chatBefore := len(m.history), len(m.chatLines)
	blocked := []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")},
		tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyPgDown},
		tea.KeyMsg{Type: tea.KeyEsc},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}},
	}
	for _, key := range blocked {
		got, c := m.Update(key)
		if c != nil {
			t.Fatalf("key %v while reindexing returned a cmd", key)
		}
		m = got.(*model)
	}
	if len(m.history) != histBefore || len(m.chatLines) != chatBefore {
		t.Fatal("keys while reindexing changed history or chat")
	}
	if m.input.Value() != "draft question" {
		t.Fatal("keys while reindexing mutated the chat input")
	}
	if stub.calls != 0 {
		t.Fatal("reindex must not run synchronously inside Update")
	}

	before := m.reindexFrame
	got, c := m.Update(reindexSpinTickMsg{})
	m = got.(*model)
	if m.reindexFrame != before+1 {
		t.Fatalf("spin frame = %d, want %d", m.reindexFrame, before+1)
	}
	if c == nil {
		t.Fatal("spin tick must schedule the next tick")
	}

	_, c = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if c == nil {
		t.Fatal("ctrl+c must still quit while reindexing")
	}
	if _, ok := c().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not produce tea.QuitMsg")
	}
}

func TestReindexProgressAndCompletion(t *testing.T) {
	stub := &reindexStub{chunks: 9}
	opts := baseOpts()
	opts.ReindexWithProgress = stub.run
	freshStore := stubStore{root: "/fresh"}
	freshRunner := &stubAnswerer{answer: "fresh"}
	refreshCalls := 0
	opts.Refresh = func() (index.Store, agent.Answerer) {
		refreshCalls++
		return freshStore, freshRunner
	}
	m := testModel(t, opts)

	cmd := m.startReindex(screenChat)
	done := reindexRunner(t, cmd)().(reindexDoneMsg)
	if stub.calls != 1 {
		t.Fatalf("ReindexWithProgress called %d times, want 1", stub.calls)
	}

	pmsg := m.waitReindexProgress()()
	prog, ok := pmsg.(reindexProgressMsg)
	if !ok {
		t.Fatalf("waitReindexProgress = %T, want reindexProgressMsg", pmsg)
	}
	got, _ := m.Update(prog)
	m = got.(*model)
	if m.reindexProgress.Phase != "loading_model" {
		t.Fatalf("progress phase = %q, want loading_model", m.reindexProgress.Phase)
	}
	if !strings.Contains(m.View(), "loading model") {
		t.Fatal("modal did not render the localized phase label")
	}

	final, _ := m.Update(done)
	fm := final.(*model)
	if fm.reindexing {
		t.Fatal("modal not cleared on completion")
	}
	if fm.screen != screenChat {
		t.Fatal("chat screen not restored on completion")
	}
	if refreshCalls != 1 {
		t.Fatalf("Refresh called %d times, want 1", refreshCalls)
	}
	if fm.opts.Store.Root() != "/fresh" || fm.opts.Runner != freshRunner {
		t.Fatal("store/runner were not rebound after reindex")
	}
	if !chatHas(fm, "Reindex done: 9 chunks") {
		t.Fatal("completion status was not appended to chat")
	}
}

func TestReindexErrorRestoresScreenAndReportsError(t *testing.T) {
	stub := &reindexStub{err: errors.New("embed download failed")}
	opts := baseOpts()
	opts.ReindexWithProgress = stub.run
	refreshCalls := 0
	opts.Refresh = func() (index.Store, agent.Answerer) {
		refreshCalls++
		return stubStore{root: "/kb"}, &stubAnswerer{}
	}
	m := testModel(t, opts)

	done := reindexRunner(t, m.startReindex(screenSettings))().(reindexDoneMsg)
	if done.err == nil {
		t.Fatal("test stub did not return an error")
	}
	final, c := m.Update(done)
	fm := final.(*model)
	if c != nil {
		t.Fatal("reindexDoneMsg must not return a cmd")
	}
	if fm.reindexing {
		t.Fatal("error left the UI stuck in the modal")
	}
	if fm.screen != screenSettings {
		t.Fatal("settings screen not restored after a failed reindex")
	}
	if !chatHas(fm, "Reindex failed: embed download failed") {
		t.Fatal("error was not appended to chat")
	}
	if refreshCalls != 1 {
		t.Fatalf("error path must still refresh; Refresh called %d times", refreshCalls)
	}
}

func TestChatReindexCommandBypassesOnCommand(t *testing.T) {
	stub := &reindexStub{chunks: 3}
	called := 0
	opts := baseOpts()
	opts.OnCommand = func(line string) (string, error) { called++; return "ok: " + line, nil }
	opts.ReindexWithProgress = stub.run

	for _, line := range []string{":reindex", "reindex"} {
		m := testModel(t, opts)
		m.input.SetValue(line)
		got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		mm := got.(*model)
		if !mm.reindexing {
			t.Fatalf("%q did not start the modal", line)
		}
		if mm.screen != screenChat {
			t.Fatalf("%q changed the screen", line)
		}
		if cmd == nil {
			t.Fatalf("%q did not return an async cmd", line)
		}
	}
	if called != 0 {
		t.Fatalf("OnCommand called %d times; reindex must bypass it", called)
	}
}

func TestSettingsCannotStartSecondReindex(t *testing.T) {
	stub := &reindexStub{chunks: 1}
	opts, _ := settingsOpts()
	opts.ReindexWithProgress = stub.run
	m := settingsModel(t, opts)

	got, _ := pressKey(m, "r")
	m = got.(*model)
	if !m.reindexing {
		t.Fatal("first r did not start the modal")
	}
	from, ch := m.reindexFrom, m.reindexCh
	got2, cmd2 := pressKey(m, "r")
	m2 := got2.(*model)
	if cmd2 != nil {
		t.Fatal("second r must not start another reindex")
	}
	if !m2.reindexing || m2.reindexFrom != from || m2.reindexCh != ch {
		t.Fatal("second r changed the active reindex state")
	}
	if c := m2.startReindex(screenChat); c != nil {
		t.Fatal("startReindex must be a no-op while already reindexing")
	}
}
