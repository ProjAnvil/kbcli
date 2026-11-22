package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/intent"
	"github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/langchain-golang/core/runnables"
	"github.com/projanvil/langchain-golang/langchain/agents"
)

// eventStreamStub is a pull-based stream of canned StreamEvents.
type eventStreamStub struct {
	events []agents.StreamEvent
	i      int
	err    error
	closed bool
}

func (s *eventStreamStub) Next(ctx context.Context) (agents.StreamEvent, bool, error) {
	if s.err != nil {
		return agents.StreamEvent{}, false, s.err
	}
	if s.i >= len(s.events) {
		return agents.StreamEvent{}, false, nil
	}
	ev := s.events[s.i]
	s.i++
	return ev, true, nil
}

func (s *eventStreamStub) Close() error {
	s.closed = true
	return nil
}

// streamStub implements both invoker and streamInvoker. It records which path
// the runner took so fallback tests can assert Invoke was used.
type streamStub struct {
	events      []agents.StreamEvent
	streamErr   error
	invokeMsgs  []messages.Message
	invokeErr   error
	streamCalls int
	invokeCalls int
	lastStream  *eventStreamStub
}

func (s *streamStub) Invoke(ctx context.Context, msgs []messages.Message) ([]messages.Message, error) {
	s.invokeCalls++
	return s.invokeMsgs, s.invokeErr
}

func (s *streamStub) StreamEvents(ctx context.Context, msgs []messages.Message) (runnables.Stream[agents.StreamEvent], error) {
	s.streamCalls++
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	s.lastStream = &eventStreamStub{events: s.events}
	return s.lastStream, nil
}

func streamMessagePtr(m messages.Message) *messages.Message { return &m }

// deltaEvents builds a realistic model-node event sequence with one terminal
// event carrying the assembled answer.
func deltaEvents(parts ...string) []agents.StreamEvent {
	full := strings.Join(parts, "")
	evs := []agents.StreamEvent{{Type: agents.StreamNodeStart, Node: "model"}}
	for _, p := range parts {
		evs = append(evs, agents.StreamEvent{Type: agents.StreamModelDelta, Node: "model", Text: p})
	}
	return append(evs,
		agents.StreamEvent{Type: agents.StreamModelEnd, Node: "model", Message: streamMessagePtr(messages.AI(full))},
		agents.StreamEvent{Type: agents.StreamEnd, Message: streamMessagePtr(messages.AI(full))},
	)
}

func streamRunner(stub *streamStub) *runner {
	return &runner{
		cfg:       testConfig(),
		retriever: &fakeRetriever{hits: makeHits(2)},
		sink:      newSink(),
		agent:     stub,
		streamer:  stub,
	}
}

func TestAskStreamDeliversDeltasInOrder(t *testing.T) {
	stub := &streamStub{events: deltaEvents("Hel", "lo ", "world")}
	r := streamRunner(stub)

	var got []string
	answer, hits, err := r.AskStream(context.Background(), nil, "q", func(d string) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	want := []string{"Hel", "lo ", "world"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %q, want %q", got, want)
	}
	if answer != "Hello world" {
		t.Fatalf("answer = %q, want %q", answer, "Hello world")
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if stub.invokeCalls != 0 {
		t.Fatalf("Invoke called %d times on the streaming path", stub.invokeCalls)
	}
	if stub.lastStream == nil || !stub.lastStream.closed {
		t.Fatal("stream was not closed after draining")
	}
}

func TestAskStreamFallsBackOnStreamStartError(t *testing.T) {
	stub := &streamStub{
		streamErr:  errors.New("stream down"),
		invokeMsgs: []messages.Message{messages.AI("invoke answer")},
	}
	r := streamRunner(stub)

	var got []string
	answer, hits, err := r.AskStream(context.Background(), nil, "q", func(d string) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("AskStream must fail open: %v", err)
	}
	if answer != "invoke answer" {
		t.Fatalf("answer = %q, want the Invoke fallback", answer)
	}
	if len(got) != 0 {
		t.Fatalf("deltas on failed stream = %q", got)
	}
	if len(hits) != 2 || stub.invokeCalls != 1 {
		t.Fatalf("hits=%d invokeCalls=%d, want 2/1", len(hits), stub.invokeCalls)
	}
}

func TestAskStreamFallsBackOnEmptyStream(t *testing.T) {
	stub := &streamStub{
		events:     []agents.StreamEvent{{Type: agents.StreamEnd, Message: streamMessagePtr(messages.AI(""))}},
		invokeMsgs: []messages.Message{messages.AI("recovered")},
	}
	r := streamRunner(stub)

	answer, _, err := r.AskStream(context.Background(), nil, "q", func(string) {})
	if err != nil {
		t.Fatalf("AskStream must fail open: %v", err)
	}
	if answer != "recovered" || stub.invokeCalls != 1 {
		t.Fatalf("answer=%q invokeCalls=%d, want recovered/1", answer, stub.invokeCalls)
	}
}

func TestAskStreamUsesTerminalMessageWithoutDeltas(t *testing.T) {
	stub := &streamStub{events: []agents.StreamEvent{{
		Type:    agents.StreamEnd,
		Message: streamMessagePtr(messages.AI("terminal only")),
	}}}
	r := streamRunner(stub)

	var got []string
	answer, _, err := r.AskStream(context.Background(), nil, "q", func(d string) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if answer != "terminal only" || len(got) != 0 {
		t.Fatalf("answer=%q deltas=%q", answer, got)
	}
	if stub.invokeCalls != 0 {
		t.Fatal("Invoke must not run when the terminal event carries the answer")
	}
}

func TestAskStreamWithoutStreamerFallsBackToInvoke(t *testing.T) {
	stub := &streamStub{invokeMsgs: []messages.Message{messages.AI("plain invoke")}}
	r := &runner{
		cfg:       testConfig(),
		retriever: &fakeRetriever{hits: makeHits(1)},
		sink:      newSink(),
		agent:     stub,
	}

	answer, _, err := r.AskStream(context.Background(), nil, "q", func(string) {})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if answer != "plain invoke" || stub.invokeCalls != 1 || stub.streamCalls != 0 {
		t.Fatalf("answer=%q invoke=%d stream=%d", answer, stub.invokeCalls, stub.streamCalls)
	}
}

func TestAskStreamDirectKeepsShortAnswer(t *testing.T) {
	cfg := testConfig()
	cfg.IntentMode = config.IntentAdaptive
	cfg.Language = config.LangEn
	completion := func(ctx context.Context, system, user string) (string, error) {
		if strings.Contains(system, "Classify the user message") {
			return `{"intent":"direct","reason":"test reason"}`, nil
		}
		return "Hello there", nil
	}
	stub := &streamStub{invokeMsgs: []messages.Message{messages.AI("unused")}}
	r := &runner{
		cfg:        cfg,
		retriever:  &fakeRetriever{hits: makeHits(1)},
		sink:       newSink(),
		agent:      stub,
		streamer:   stub,
		classifier: intent.New(cfg, intent.CompleteFunc(completion)),
		complete:   completion,
	}

	var got []string
	answer, hits, err := r.AskStream(context.Background(), nil, "hello", func(d string) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if answer != "Hello there" || len(hits) != 0 {
		t.Fatalf("direct answer/hits = %q/%d", answer, len(hits))
	}
	if len(got) != 0 || stub.streamCalls != 0 || stub.invokeCalls != 0 {
		t.Fatal("direct route must not start the streaming or invoke path")
	}
}
