package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// fakeStreamAnswerer emits canned deltas then a final answer. Embedding
// stubAnswerer keeps Ask available so it satisfies both Answerer interfaces.
type fakeStreamAnswerer struct {
	stubAnswerer
	deltas    []string
	answer    string
	hits      []index.Hit
	err       error
	gotDeltas []string
}

func (f *fakeStreamAnswerer) AskStream(ctx context.Context, history []agent.Turn, question string, onDelta func(string)) (string, []index.Hit, error) {
	f.called = true
	f.gotQuestion = question
	f.gotHistory = history
	for _, d := range f.deltas {
		f.gotDeltas = append(f.gotDeltas, d)
		onDelta(d)
	}
	if f.err != nil {
		return "", f.hits, f.err
	}
	return f.answer, f.hits, nil
}

// driveStream runs the producer half of askStreamCmd and feeds every delivered
// message into Update until the terminal done message arrives.
func driveStream(t *testing.T, m *model, cmd tea.Cmd) *model {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("ask stream cmd = %T, want a two-command batch", cmd())
	}
	if msg := batch[1](); msg != nil {
		t.Fatalf("stream producer returned a message %T", msg)
	}
	wait := batch[0]
	for {
		msg := wait()
		if msg == nil {
			t.Fatal("stream ended without a done message")
		}
		var next tea.Cmd
		var got tea.Model
		got, next = m.Update(msg)
		m = got.(*model)
		if _, done := msg.(answerDoneMsg); done {
			return m
		}
		if next == nil {
			t.Fatalf("message %T did not re-arm the stream waiter", msg)
		}
		wait = next
	}
}

func TestStreamDeltasRenderIncrementally(t *testing.T) {
	hits := []index.Hit{{Rank: 1, Score: 0.9, Chunk: index.Chunk{Source: "/kb/stream.md"}}}
	opts := baseOpts()
	fake := &fakeStreamAnswerer{
		deltas: []string{"Hel", "lo ", "world"},
		answer: "Hello world",
		hits:   hits,
	}
	opts.Runner = fake
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	if !m.busy {
		t.Fatal("busy was not set before streaming")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("ask stream cmd = %T", cmd())
	}
	if msg := batch[1](); msg != nil {
		t.Fatalf("stream producer returned a message %T", msg)
	}
	if !reflect.DeepEqual(fake.gotHistory, []agent.Turn{{Role: "user", Content: "question"}}) {
		t.Fatalf("history snapshot = %+v", fake.gotHistory)
	}
	wait := batch[0]

	// First delta: only partial text is visible, no history write yet.
	msg := wait()
	delta, ok := msg.(answerDeltaMsg)
	if !ok {
		t.Fatalf("first stream message = %T, want answerDeltaMsg", msg)
	}
	got, next := m.Update(delta)
	m = got.(*model)
	if m.streamText != "Hel" || !m.streaming {
		t.Fatalf("stream text/state = %q/%v", m.streamText, m.streaming)
	}
	if view := m.chatVP.View(); !strings.Contains(view, "Hel") || strings.Contains(view, "thinking") {
		t.Fatalf("live answer did not replace the spinner: %q", view)
	}
	if len(m.history) != 1 {
		t.Fatalf("assistant turn written before done: %+v", m.history)
	}

	// Second delta: text grows on the same live line.
	msg = next()
	got, next = m.Update(msg.(answerDeltaMsg))
	m = got.(*model)
	if m.streamText != "Hello " {
		t.Fatalf("stream text after second delta = %q", m.streamText)
	}
	if len(m.history) != 1 {
		t.Fatal("assistant turn written before done")
	}

	// Third delta completes the streamed text.
	msg = next()
	got, next = m.Update(msg.(answerDeltaMsg))
	m = got.(*model)
	if m.streamText != "Hello world" {
		t.Fatalf("stream text after third delta = %q", m.streamText)
	}

	// Done: full answer enters history once, live text is cleared.
	msg = next()
	done, ok := msg.(answerDoneMsg)
	if !ok {
		t.Fatalf("terminal stream message = %T, want answerDoneMsg", msg)
	}
	got, _ = m.Update(done)
	m = got.(*model)
	if m.busy || m.streaming || m.streamText != "" {
		t.Fatalf("done did not reset busy/stream state: %+v", m)
	}
	if len(m.history) != 2 || m.history[1].Role != "assistant" || m.history[1].Content != "Hello world" {
		t.Fatalf("history after done = %+v", m.history)
	}
	if !chatHas(m, "Hello world") {
		t.Fatal("final answer missing from chat")
	}
	assistantLines := 0
	for _, line := range m.chatLines {
		if strings.Contains(line.text, "Hello world") {
			assistantLines++
		}
	}
	if assistantLines != 1 {
		t.Fatalf("assistant answer rendered %d times, want 1", assistantLines)
	}
	if !strings.Contains(m.evidenceVP.View(), "/kb/stream.md") {
		t.Fatal("evidence pane not refreshed from the done message")
	}
	if !reflect.DeepEqual(fake.gotDeltas, []string{"Hel", "lo ", "world"}) {
		t.Fatalf("onDelta order = %q", fake.gotDeltas)
	}
}

func TestStreamErrorClearsBusyAndShowsError(t *testing.T) {
	opts := baseOpts()
	fake := &fakeStreamAnswerer{
		deltas: []string{"partial"},
		err:    errors.New("offline"),
	}
	opts.Runner = fake
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)

	m = driveStream(t, m, cmd)
	if m.busy || m.streaming || m.streamText != "" {
		t.Fatalf("error did not reset busy/stream state: %+v", m)
	}
	if len(m.history) != 1 {
		t.Fatalf("failed answer must not enter history: %+v", m.history)
	}
	if !chatHas(m, "offline") {
		t.Fatal("stream error was not shown in chat")
	}
}

func TestManualDeltaAndDoneWithoutChannel(t *testing.T) {
	// A delta with no channel must not schedule another waiter; tests and
	// callers can still deliver a done message directly.
	m := testModel(t, baseOpts())
	got, _ := m.Update(answerDeltaMsg{text: "partial answer"})
	m = got.(*model)
	if m.streamText != "partial answer" || !m.streaming {
		t.Fatalf("manual delta state = %q/%v", m.streamText, m.streaming)
	}
	got, _ = m.Update(answerDoneMsg{answer: "final answer"})
	m = got.(*model)
	if m.streaming || m.streamText != "" || len(m.history) != 1 {
		t.Fatalf("manual done state = %+v", m)
	}
	if !chatHas(m, "final answer") {
		t.Fatal("manual done answer missing from chat")
	}
}
