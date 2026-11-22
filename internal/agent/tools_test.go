package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ProjAnvil/kbcli/internal/index"
)

func TestSearchToolFormatsHitsAndPublishes(t *testing.T) {
	ret := &fakeRetriever{hits: makeHits(2)}
	sink := newSink()
	tool, err := newSearchTool(testConfig(), ret, sink)
	if err != nil {
		t.Fatalf("newSearchTool: %v", err)
	}
	res, err := tool.Invoke(context.Background(), map[string]any{"tool_input": "what?"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// The tool uses the same citation renderer as the grounded prompt, so
	// numbers and formatting can never drift between the two evidence paths.
	want := "[1] doc1.md score=0.900\nbody 1\n\n[2] doc2.md score=0.800\nbody 2"
	if res.Content != want {
		t.Fatalf("formatted content =\n%q\nwant\n%q", res.Content, want)
	}
	got := sink.Get()
	if len(got) != 2 || got[0].Chunk.Source != "doc1.md" {
		t.Fatalf("sink not published with hits: %#v", got)
	}
	// The tool drives Search (no history) with the configured TopK.
	if ret.searchCalls != 1 || ret.gotQuery != "what?" || ret.gotK != testConfig().TopK {
		t.Fatalf("retriever args = (%q, %d), calls = %d", ret.gotQuery, ret.gotK, ret.searchCalls)
	}
}

func TestSearchToolEmptyResult(t *testing.T) {
	tool, err := newSearchTool(testConfig(), &fakeRetriever{}, newSink())
	if err != nil {
		t.Fatalf("newSearchTool: %v", err)
	}
	res, err := tool.Invoke(context.Background(), map[string]any{"tool_input": "q"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Content != "(no matching evidence)" {
		t.Fatalf("empty content = %q", res.Content)
	}
}

func TestSearchToolWrapsError(t *testing.T) {
	boom := errors.New("boom")
	tool, err := newSearchTool(testConfig(), &fakeRetriever{err: boom}, newSink())
	if err != nil {
		t.Fatalf("newSearchTool: %v", err)
	}
	if _, err := tool.Invoke(context.Background(), map[string]any{"tool_input": "q"}); !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

func TestSearchToolTruncatesBodyByRunes(t *testing.T) {
	long := strings.Repeat("界", 1000) // multi-byte runes
	h := index.Hit{Score: 0.5, Chunk: index.Chunk{Source: "big.md", Body: long}}
	tool, err := newSearchTool(testConfig(), &fakeRetriever{hits: []index.Hit{h}}, newSink())
	if err != nil {
		t.Fatalf("newSearchTool: %v", err)
	}
	res, err := tool.Invoke(context.Background(), map[string]any{"tool_input": "q"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	lines := strings.Split(res.Content, "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected content %q", res.Content)
	}
	if n := utf8.RuneCountInString(lines[1]); n != maxEvidenceRunes {
		t.Fatalf("body length = %d runes, want %d", n, maxEvidenceRunes)
	}
}

func TestEvidenceBodyFallsBackToText(t *testing.T) {
	h := index.Hit{Chunk: index.Chunk{Body: "", Text: "from text"}}
	if got := evidenceBody(h); got != "from text" {
		t.Fatalf("evidenceBody = %q, want from text", got)
	}
}
