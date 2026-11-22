package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/intent"
	"github.com/projanvil/langchain-golang/core/messages"
)

func intentRunner(t *testing.T, choice string, completeText string) (*runner, *fakeRetriever, *stubInvoker) {
	t.Helper()
	cfg := testConfig()
	cfg.IntentMode = config.IntentAdaptive
	cfg.Language = config.LangEn
	answer := `{"intent":"` + choice + `","reason":"test reason"}`
	completion := func(ctx context.Context, system, user string) (string, error) {
		if strings.Contains(system, "Classify the user message") {
			return answer, nil
		}
		return completeText, nil
	}
	ret := &fakeRetriever{hits: makeHits(2)}
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("grounded [1]")}}
	r := &runner{
		cfg:        cfg,
		retriever:  ret,
		sink:       newSink(),
		agent:      stub,
		classifier: intent.New(cfg, intent.CompleteFunc(completion)),
		complete:   completion,
	}
	return r, ret, stub
}

func TestAskDirectSkipsRetrieval(t *testing.T) {
	r, ret, stub := intentRunner(t, intent.Direct, "Hello there")
	answer, hits, err := r.Ask(context.Background(), nil, "hello")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "Hello there" {
		t.Fatalf("direct answer = %q", answer)
	}
	if len(hits) != 0 || len(r.sink.Get()) != 0 {
		t.Fatal("direct route must publish empty evidence")
	}
	if ret.withHistoryCalls != 0 || ret.quickCalls != 0 || ret.searchCalls != 0 {
		t.Fatal("direct route must not call the retriever")
	}
	if stub.got != nil {
		t.Fatal("direct route must not invoke the RAG agent")
	}
	if got := r.LastMeta(); got.Intent != intent.Direct || got.IntentEngine != intent.EngineLLM {
		t.Fatalf("meta = %+v", got)
	}
}

func TestAskQuickUsesVerbatimSearch(t *testing.T) {
	r, ret, stub := intentRunner(t, intent.QuickRAG, "unused")
	answer, hits, err := r.Ask(context.Background(), nil, "simple fact")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "grounded [1]" || len(hits) != 2 {
		t.Fatalf("answer/hits = %q/%d", answer, len(hits))
	}
	if ret.quickCalls != 1 || ret.withHistoryCalls != 0 {
		t.Fatalf("quick route calls: quick=%d history=%d", ret.quickCalls, ret.withHistoryCalls)
	}
	if stub.got == nil {
		t.Fatal("quick route must still invoke the agent")
	}
}

func TestAskDeepUsesFullPipeline(t *testing.T) {
	r, ret, _ := intentRunner(t, intent.DeepRAG, "unused")
	if _, _, err := r.Ask(context.Background(), nil, "compare architectures"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ret.withHistoryCalls != 1 || ret.quickCalls != 0 {
		t.Fatalf("deep route calls: history=%d quick=%d", ret.withHistoryCalls, ret.quickCalls)
	}
}

func TestAskDirectFallsBackWhenClassifierOffline(t *testing.T) {
	cfg := testConfig()
	cfg.IntentMode = config.IntentAdaptive
	cfg.Language = config.LangEn
	offline := func(ctx context.Context, system, user string) (string, error) {
		return "", context.DeadlineExceeded
	}
	r := &runner{
		cfg:        cfg,
		retriever:  &fakeRetriever{},
		sink:       newSink(),
		classifier: intent.New(cfg, intent.CompleteFunc(offline)),
		complete:   offline,
	}
	answer, hits, err := r.Ask(context.Background(), nil, "hello")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer == "" || len(hits) != 0 {
		t.Fatalf("offline direct answer/hits = %q/%d", answer, len(hits))
	}
	if got := r.LastMeta(); got.IntentEngine != intent.EngineRule || !strings.HasPrefix(got.IntentReason, "llm_unavailable:") {
		t.Fatalf("meta = %+v", got)
	}
}
