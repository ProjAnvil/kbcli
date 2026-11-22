package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/i18n"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/projanvil/langchain-golang/core/messages"
)

func TestCitationLedgerKeepsStableNumbers(t *testing.T) {
	l := newCitationLedger()
	first := makeHits(2)
	nums1, all1 := l.Register(first)
	if len(nums1) != 2 || nums1[0] != 1 || nums1[1] != 2 {
		t.Fatalf("first numbers = %v, want [1 2]", nums1)
	}
	if len(all1) != 2 {
		t.Fatalf("first evidence list = %d, want 2", len(all1))
	}

	// A follow-up lookup returns a new chunk and an already-cited one. The
	// known chunk must keep its original number instead of being renumbered.
	followUp := []index.Hit{
		{Chunk: index.Chunk{ID: "id-3", Source: "doc3.md", Body: "body 3"}},
		first[0],
	}
	nums2, all2 := l.Register(followUp)
	if nums2[0] != 3 || nums2[1] != 1 {
		t.Fatalf("follow-up numbers = %v, want [3 1]", nums2)
	}
	if len(all2) != 3 {
		t.Fatalf("full evidence list = %d, want 3", len(all2))
	}

	l.Reset()
	nums3, all3 := l.Register(first)
	if nums3[0] != 1 || nums3[1] != 2 || len(all3) != 2 {
		t.Fatalf("after Reset numbers = %v, list = %d", nums3, len(all3))
	}
}

func TestSearchToolSharesRunnerLedger(t *testing.T) {
	ledger := newCitationLedger()
	ledger.Register(makeHits(2))
	sink := newSink()
	newHit := index.Hit{Chunk: index.Chunk{ID: "id-3", Source: "doc3.md", Body: "body 3"}}
	tool, err := newSearchToolWithLedger(testConfig(), &fakeRetriever{hits: []index.Hit{newHit}}, sink, ledger)
	if err != nil {
		t.Fatalf("newSearchToolWithLedger: %v", err)
	}
	res, err := tool.Invoke(context.Background(), map[string]any{"tool_input": "follow up"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(res.Content, "[3] doc3.md") {
		t.Fatalf("tool must continue the ledger numbering, got %q", res.Content)
	}
	all := sink.Get()
	if len(all) != 3 {
		t.Fatalf("sink must publish the full evidence list, got %d", len(all))
	}
	if all[2].Chunk.ID != "id-3" {
		t.Fatalf("sink order does not match ledger: %+v", all)
	}
}

// With no retrieved evidence kbcli must refuse instead of asking an ungrounded
// model to answer, and it must mark the answer as degraded.
func TestAskRefusesWhenNoEvidence(t *testing.T) {
	stub := &stubInvoker{msgs: []messages.Message{messages.AI("hallucinated")}}
	r := &runner{
		cfg:       testConfig(),
		retriever: &fakeRetriever{},
		sink:      newSink(),
		agent:     stub,
	}
	answer, hits, err := r.Ask(context.Background(), nil, "unknown topic")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != i18n.LangEn.T("answer.no_evidence_en") {
		t.Fatalf("answer = %q, want the refusal", answer)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(hits))
	}
	if stub.got != nil {
		t.Fatal("agent must not be invoked without evidence")
	}
	meta := r.LastMeta()
	if !meta.Degraded || meta.Reason != "no_evidence" {
		t.Fatalf("meta = %+v, want degraded/no_evidence", meta)
	}
}

func TestAskMarksDegradedOnRetrieverError(t *testing.T) {
	r := &runner{
		cfg:       testConfig(),
		retriever: &fakeRetriever{err: errors.New("db down")},
		sink:      newSink(),
		agent:     &stubInvoker{},
	}
	if _, _, err := r.Ask(context.Background(), nil, "q"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	meta := r.LastMeta()
	if !meta.Degraded || meta.Reason != "retrieval_failed" {
		t.Fatalf("meta = %+v, want degraded/retrieval_failed", meta)
	}
}

func TestAskMarksDegradedOnLLMFailure(t *testing.T) {
	stub := &stubInvoker{err: errors.New("model offline")}
	r := &runner{
		cfg:       testConfig(),
		retriever: &fakeRetriever{hits: makeHits(2)},
		sink:      newSink(),
		agent:     stub,
	}
	answer, _, err := r.Ask(context.Background(), nil, "q")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(answer, "LLM unavailable") {
		t.Fatalf("answer = %q, want the extractive fallback", answer)
	}
	meta := r.LastMeta()
	if !meta.Degraded || meta.Reason != "llm_failed" {
		t.Fatalf("meta = %+v, want degraded/llm_failed", meta)
	}
}
