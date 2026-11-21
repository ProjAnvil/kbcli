package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/projanvil/kbcli/internal/i18n"
	"github.com/projanvil/kbcli/internal/retrieval"
	"github.com/projanvil/langchain-golang/core/messages"
)

func TestLastAIAnswer(t *testing.T) {
	out := []messages.Message{
		messages.AI("first"),
		messages.Human("q"),
		messages.AI(""), // empty AI messages must be skipped
		messages.AI("last answer"),
		messages.System("sys"),
	}
	if got := lastAIAnswer(out); got != "last answer" {
		t.Fatalf("lastAIAnswer = %q, want last answer", got)
	}

	if got := lastAIAnswer([]messages.Message{messages.AI("real"), messages.AI("")}); got != "real" {
		t.Fatalf("lastAIAnswer with trailing empty = %q, want real", got)
	}
	if got := lastAIAnswer([]messages.Message{messages.Human("only")}); got != "" {
		t.Fatalf("lastAIAnswer with no AI = %q, want empty", got)
	}
}

func TestBuildMessagesHistoryTruncation(t *testing.T) {
	history := make([]Turn, 25)
	for i := range history {
		history[i] = Turn{Role: "user", Content: fmt.Sprintf("turn-%d", i)}
	}
	msgs := buildMessages(history, "question")
	// 1 system + 20 kept turns + 1 question.
	if len(msgs) != 22 {
		t.Fatalf("len(msgs) = %d, want 22", len(msgs))
	}
	if msgs[0].Role != messages.RoleSystem {
		t.Fatalf("first message role = %q, want system", msgs[0].Role)
	}
	if msgs[1].Content != "turn-5" {
		t.Fatalf("first kept turn = %q, want turn-5", msgs[1].Content)
	}
	if last := msgs[len(msgs)-1]; last.Role != messages.RoleHuman || last.Content != "question" {
		t.Fatalf("last message = %#v, want human question", last)
	}
}

func TestBuildMessagesRoleMapping(t *testing.T) {
	history := []Turn{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "tool", Content: "ignored"},
	}
	msgs := buildMessages(history, "q")
	// 1 system + 2 recognised turns + 1 question; the "tool" turn is dropped.
	if len(msgs) != 4 {
		t.Fatalf("len(msgs) = %d, want 4", len(msgs))
	}
	if msgs[1].Role != messages.RoleHuman || msgs[1].Content != "u1" {
		t.Fatalf("msgs[1] = %#v", msgs[1])
	}
	if msgs[2].Role != messages.RoleAI || msgs[2].Content != "a1" {
		t.Fatalf("msgs[2] = %#v", msgs[2])
	}
}

func TestFallbackAnswer(t *testing.T) {
	hits := makeHits(5)
	got := fallbackAnswer(hits, errors.New("offline"), i18n.LangEn)
	if !strings.Contains(got, "LLM unavailable (offline); showing retrieved evidence instead.") {
		t.Fatalf("missing fallback note: %q", got)
	}
	for _, c := range []string{"[1]", "[2]", "[3]"} {
		if !strings.Contains(got, c) {
			t.Fatalf("fallback missing citation %s: %q", c, got)
		}
	}
	if strings.Contains(got, "[4]") {
		t.Fatalf("fallback must list only the top 3 hits: %q", got)
	}
}

func TestFallbackAnswerNoHits(t *testing.T) {
	got := fallbackAnswer(nil, errors.New("x"), i18n.LangEn)
	if !strings.Contains(got, "(no matching evidence)") {
		t.Fatalf("fallback with no hits = %q", got)
	}
}

func TestAskReturnsAgentAnswer(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	sink := newSink()
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("grounded [1]")}}
	r := &runner{cfg: testConfig(), retriever: ret, sink: sink, agent: stub}

	answer, hits, err := r.Ask(context.Background(), nil, "what?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "grounded [1]" {
		t.Fatalf("answer = %q", answer)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
	if len(sink.Get()) != 2 {
		t.Fatal("Ask did not publish hits to the sink")
	}
	if ret.withHistoryCalls != 1 {
		t.Fatalf("SearchWithHistory calls = %d, want 1", ret.withHistoryCalls)
	}
	if ret.gotQuery != "what?" || ret.gotK != testConfig().TopK {
		t.Fatalf("retriever args = (%q, %d)", ret.gotQuery, ret.gotK)
	}
	if stub.got == nil || len(stub.got) < 2 || stub.got[0].Role != messages.RoleSystem {
		t.Fatalf("agent did not receive built messages: %#v", stub.got)
	}
}

func TestAskConvertsHistoryToRetrievalTurns(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(1)}
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("ok")}}
	r := &runner{cfg: testConfig(), retriever: ret, sink: newSink(), agent: stub}

	history := []Turn{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	if _, _, err := r.Ask(context.Background(), history, "follow up"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	want := []retrieval.Turn{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	if len(ret.gotHistory) != len(want) {
		t.Fatalf("history turns = %d, want %d", len(ret.gotHistory), len(want))
	}
	for i := range want {
		if ret.gotHistory[i] != want[i] {
			t.Fatalf("turn[%d] = %#v, want %#v", i, ret.gotHistory[i], want[i])
		}
	}
}

func TestAskInjectsEvidenceBeforeHistory(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("ok")}}
	r := &runner{cfg: testConfig(), retriever: ret, sink: newSink(), agent: stub}

	history := []Turn{{Role: "user", Content: "earlier"}}
	if _, _, err := r.Ask(context.Background(), history, "what?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	got := stub.got
	// system + evidence + history + final question.
	if len(got) < 4 {
		t.Fatalf("want system + evidence + history + question, got %d: %#v", len(got), got)
	}
	if got[0].Role != messages.RoleSystem {
		t.Fatalf("first message role = %q, want system", got[0].Role)
	}
	// The budget-trimmed parts (question + evidence) are injected as a Human
	// message before the history so citations reach the model.
	minQ := "Question:\nwhat?"
	if got[1].Role != messages.RoleHuman || !strings.Contains(got[1].Content, minQ) {
		t.Fatalf("question not injected before history: %#v", got[1])
	}
	if len(got[1].Content) <= len(minQ) {
		t.Fatalf("evidence missing from injected message: %q", got[1].Content)
	}
	if got[2].Content != "earlier" {
		t.Fatalf("history turn not after evidence: %#v", got[2])
	}
	last := got[len(got)-1]
	if last.Role != messages.RoleHuman || last.Content != "what?" {
		t.Fatalf("last message = %#v, want final human question", last)
	}
}

func TestAskFallbackOnInvokeError(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	stub := &stubInvoker{err: errors.New("offline")}
	r := &runner{cfg: testConfig(), retriever: ret, sink: newSink(), agent: stub}

	answer, hits, err := r.Ask(context.Background(), nil, "q")
	if err != nil {
		t.Fatalf("Ask must not surface LLM errors: %v", err)
	}
	if !strings.Contains(answer, "LLM unavailable (offline)") {
		t.Fatalf("expected fallback, got %q", answer)
	}
	if len(hits) != 2 {
		t.Fatal("hits must still be returned on fallback")
	}
}

func TestAskFallbackOnEmptyAnswer(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("")}}
	r := &runner{cfg: testConfig(), retriever: ret, sink: newSink(), agent: stub}

	answer, hits, err := r.Ask(context.Background(), nil, "q")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(answer, "LLM unavailable") {
		t.Fatalf("expected fallback on empty answer, got %q", answer)
	}
	if len(hits) != 2 {
		t.Fatal("hits must still be returned on fallback")
	}
}

func TestAskFallbackOnNoAIMessage(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	stub := &stubInvoker{msgs: []messages.Message{messages.Human("no ai")}}
	r := &runner{cfg: testConfig(), retriever: ret, sink: newSink(), agent: stub}

	answer, hits, err := r.Ask(context.Background(), nil, "q")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(answer, "LLM unavailable") || !strings.Contains(answer, "no AI message") {
		t.Fatalf("expected no-AI fallback, got %q", answer)
	}
	if len(hits) != 2 {
		t.Fatal("hits must still be returned on fallback")
	}
}

func TestAskFallbackOnRetrieverError(t *testing.T) {
	boom := errors.New("db down")
	ret := &fakeRetriever{err: boom}
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("unused")}}
	sink := newSink()
	r := &runner{cfg: testConfig(), retriever: ret, sink: sink, agent: stub}

	answer, hits, err := r.Ask(context.Background(), nil, "q")
	if err != nil {
		t.Fatalf("Ask must not surface errors: %v", err)
	}
	if !strings.Contains(answer, "LLM unavailable") || !strings.Contains(answer, "db down") {
		t.Fatalf("expected fallback mentioning cause, got %q", answer)
	}
	if hits == nil {
		t.Fatal("hits must be non-nil on fallback")
	}
	if sink.Get() == nil {
		t.Fatal("sink must hold non-nil hits on fallback")
	}
	if stub.got != nil {
		t.Fatal("agent must not run when retrieval fails")
	}
}

func TestNewCompleteNonNil(t *testing.T) {
	// Building the completion function constructs a ChatModel but makes no
	// network call, so this stays offline.
	if newComplete(testConfig()) == nil {
		t.Fatal("newComplete returned nil")
	}
}
