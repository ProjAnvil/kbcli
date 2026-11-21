package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// stubAnswerer is a controllable agent.Answerer used by the Update tests.
type stubAnswerer struct {
	answer string
	hits   []index.Hit
	err    error

	called      bool
	gotQuestion string
	gotHistory  []agent.Turn
}

func (s *stubAnswerer) Ask(ctx context.Context, history []agent.Turn, question string) (string, []index.Hit, error) {
	s.called = true
	s.gotQuestion = question
	s.gotHistory = history
	return s.answer, s.hits, s.err
}

// stubSink is a minimal agent.Sink.
type stubSink struct{ hits []index.Hit }

func (s *stubSink) Set(h []index.Hit) { s.hits = h }
func (s *stubSink) Get() []index.Hit  { return s.hits }

// stubStore satisfies index.Store without performing any real indexing.
type stubStore struct{ root string }

func (s stubStore) Add(_ []index.Chunk) error                   { return nil }
func (s stubStore) Search(_ string, _ int) ([]index.Hit, error) { return nil, nil }
func (s stubStore) Save(_ string) error                         { return nil }
func (s stubStore) Sources() map[string]index.FileState         { return nil }
func (s stubStore) SetSource(_ index.FileState)                 {}
func (s stubStore) DropSource(_ string)                         {}
func (s stubStore) Len() int                                    { return 0 }
func (s stubStore) EmbedderVersion() string                     { return "" }
func (s stubStore) Root() string                                { return s.root }
func (s stubStore) Chunks() []index.Chunk                       { return nil }
func (s stubStore) Revision() uint64                            { return 0 }
func (s stubStore) BuildSignature() string                      { return "" }
func (s stubStore) SetBuildSignature(_ string)                  {}

func baseOpts() Options {
	return Options{
		Store:     stubStore{root: "/kb"},
		Runner:    &stubAnswerer{},
		Sink:      &stubSink{},
		IndexPath: "/kb/.kbvec",
		OnCommand: func(line string) (string, error) { return "ok: " + line, nil },
		Version:   "1.2.3",
	}
}

// testModel builds a model that has already processed a WindowSizeMsg.
func testModel(t *testing.T, opts Options) *model {
	t.Helper()
	m := newModel(opts)
	got, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return got.(*model)
}

func chatHas(m *model, substr string) bool {
	for _, l := range m.chatLines {
		if strings.Contains(l.text, substr) {
			return true
		}
	}
	return false
}

func TestWindowSize(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{"desktop", 120, 40},
		{"compact", 80, 24},
		{"minimum", 40, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(baseOpts())
			if m.ready {
				t.Fatal("ready must be false before the first WindowSizeMsg")
			}
			got, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			mm := got.(*model)
			if !mm.ready {
				t.Fatal("ready was not set after WindowSizeMsg")
			}
			if mm.width != tc.width || mm.height != tc.height {
				t.Fatalf("size = %dx%d, want %dx%d", mm.width, mm.height, tc.width, tc.height)
			}
			wantLeft := tc.width*62/100 - 2
			if mm.chatVP.Width != wantLeft {
				t.Fatalf("chat width = %d, want %d", mm.chatVP.Width, wantLeft)
			}
			if mm.chatVP.Height != mm.vpHeight || mm.evidenceVP.Height != mm.vpHeight {
				t.Fatal("both viewports must share the resize height")
			}
			if mm.chatVP.Width < 1 || mm.evidenceVP.Width < 1 || mm.vpHeight < 1 {
				t.Fatal("viewport dimensions must be positive")
			}
		})
	}
}

func TestEnterEmptyInput(t *testing.T) {
	for _, input := range []string{"", "   ", "\t"} {
		m := testModel(t, baseOpts())
		m.input.SetValue(input)
		before := len(m.chatLines)
		got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		mm := got.(*model)
		if cmd != nil {
			t.Fatalf("input %q unexpectedly produced a tea.Cmd", input)
		}
		if len(mm.chatLines) != before {
			t.Fatalf("input %q added chat lines", input)
		}
		if mm.busy {
			t.Fatalf("input %q set busy", input)
		}
	}
}

func TestCommandRouting(t *testing.T) {
	commands := []string{":reindex", ":update ./docs", ":help", ":clear", "reindex", "update", "help", "clear"}
	for _, line := range commands {
		t.Run(line, func(t *testing.T) {
			called := ""
			opts := baseOpts()
			opts.OnCommand = func(l string) (string, error) {
				called = l
				return "ok: " + l, nil
			}
			m := testModel(t, opts)
			m.input.SetValue(line)

			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("command did not return a tea.Cmd")
			}
			res, ok := cmd().(commandResultMsg)
			if !ok {
				t.Fatal("command tea.Cmd did not yield commandResultMsg")
			}
			if called != line {
				t.Fatalf("OnCommand received %q, want %q", called, line)
			}
			got, _ := m.Update(res)
			if !chatHas(got.(*model), "ok: "+line) {
				t.Fatal("command status was not appended to chat")
			}
		})
	}
}

func TestQuestionGoesToAsk(t *testing.T) {
	opts := baseOpts()
	ans := opts.Runner.(*stubAnswerer)
	m := testModel(t, opts)
	m.input.SetValue("what is the capital?")

	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(*model)
	if cmd == nil {
		t.Fatal("question did not return an ask tea.Cmd")
	}
	if !mm.busy {
		t.Fatal("busy was not set while asking")
	}
	if !chatHas(mm, "you > what is the capital?") {
		t.Fatal("user turn was not appended immediately")
	}
	if _, ok := cmd().(answerMsg); !ok {
		t.Fatal("ask tea.Cmd did not yield answerMsg")
	}
	if !ans.called || ans.gotQuestion != "what is the capital?" {
		t.Fatal("Runner.Ask was not called with the question")
	}
	if len(ans.gotHistory) != 1 || ans.gotHistory[0].Role != "user" {
		t.Fatal("history snapshot did not contain the user turn")
	}
}

func TestEnterIgnoredWhileBusy(t *testing.T) {
	m := testModel(t, baseOpts())
	m.input.SetValue("first question")
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(*model)
	if !mm.busy {
		t.Fatal("expected busy after the first question")
	}
	before := len(mm.chatLines)
	mm.input.SetValue("second question")
	got2, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm2 := got2.(*model)
	if cmd != nil {
		t.Fatal("enter while busy should not launch another ask")
	}
	if len(mm2.chatLines) != before {
		t.Fatal("enter while busy appended a chat line")
	}
}

func TestAnswerMsg(t *testing.T) {
	hits := []index.Hit{{
		Rank:  1,
		Score: 0.821,
		Chunk: index.Chunk{
			Source:  "/kb/guide.md",
			Heading: "Getting started",
			Page:    3,
			Body:    "first line\nsecond line\nthird line",
		},
	}}
	opts := baseOpts()
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := got.(*model)
	if !mm.busy {
		t.Fatal("busy should be set before the answer arrives")
	}

	final, _ := mm.Update(answerMsg{answer: "the answer", hits: hits})
	fm := final.(*model)
	if fm.busy {
		t.Fatal("busy was not cleared by answerMsg")
	}
	if !chatHas(fm, "kb  > the answer") {
		t.Fatal("assistant turn missing from chat")
	}
	if len(fm.history) != 2 || fm.history[0].Role != "user" || fm.history[1].Role != "assistant" {
		t.Fatalf("conversation history not maintained: %+v", fm.history)
	}
	ev := fm.evidenceVP.View()
	if !strings.Contains(ev, "/kb/guide.md") || !strings.Contains(ev, "#1") {
		t.Fatalf("evidence pane not refreshed from hits: %q", ev)
	}
}

func TestAnswerFallsBackToSink(t *testing.T) {
	opts := baseOpts()
	opts.Sink.(*stubSink).hits = []index.Hit{
		{Rank: 2, Score: 0.5, Chunk: index.Chunk{Source: "/kb/fallback.md"}},
	}
	m := testModel(t, opts)
	final, _ := m.Update(answerMsg{answer: "ok", hits: nil})
	fm := final.(*model)
	if !strings.Contains(fm.evidenceVP.View(), "/kb/fallback.md") {
		t.Fatal("evidence did not fall back to Sink.Get() when hits were empty")
	}
}

func TestView(t *testing.T) {
	m := testModel(t, baseOpts())
	view := m.View()
	for _, want := range []string{"kbcli", "EVIDENCE · recall ranking", "v1.2.3", helpLine, "> "} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q", want)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	m := newModel(baseOpts())
	got, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if !strings.Contains(got.(*model).View(), "too small") {
		t.Fatal("expected a graceful terminal-too-small view")
	}
}

func TestTabSwitchesFocus(t *testing.T) {
	m := testModel(t, baseOpts())
	if m.focus != focusChat {
		t.Fatal("expected initial focus on the chat pane")
	}
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm := got.(*model)
	if mm.focus != focusEvidence {
		t.Fatal("tab did not switch focus to the evidence pane")
	}
	got2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got2.(*model).focus != focusChat {
		t.Fatal("tab did not switch focus back to the chat pane")
	}
}

func TestScrollFocusedViewport(t *testing.T) {
	m := testModel(t, baseOpts())
	for i := 0; i < 60; i++ {
		m.appendChat("filler line", lineStatus)
	}
	m.chatVP.GotoTop()
	before := m.chatVP.YOffset

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	mm := got.(*model)
	if mm.chatVP.YOffset <= before {
		t.Fatal("pgdown did not scroll the focused chat viewport")
	}

	got2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm2 := got2.(*model)
	chatOffset := mm2.chatVP.YOffset
	got3, _ := mm2.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	mm3 := got3.(*model)
	if mm3.chatVP.YOffset != chatOffset {
		t.Fatal("pgdown scrolled chat while evidence was focused")
	}
}
