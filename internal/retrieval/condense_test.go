package retrieval

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

func TestHasAnaphora(t *testing.T) {
	yes := []string{
		"它要重新索引吗？", "该配置怎么改？", "上述方案有问题吗",
		"这个和前面的区别", "does it need a rebuild?", "what about them?",
		"why is that slower", "it's broken",
	}
	for _, q := range yes {
		if !hasAnaphora(q) {
			t.Fatalf("missed anaphora in %q", q)
		}
	}
	no := []string{"exponential backoff retry policy", "如何配置 chunk_size", "what is gRPC"}
	for _, q := range no {
		if hasAnaphora(q) {
			t.Fatalf("false positive in %q", q)
		}
	}
}

func TestRuleCondenseAnchorsPreviousUserTurn(t *testing.T) {
	history := []Turn{
		{Role: "user", Content: "kbcli 的 chunk_size 默认是多少？"},
		{Role: "assistant", Content: "默认 600 runes。"},
	}
	got, ok := ruleCondense("它改了要重新索引吗？", history)
	if !ok {
		t.Fatal("rule condense did not fire")
	}
	if !strings.Contains(got, "chunk_size") || !strings.Contains(got, "它改了要重新索引吗") {
		t.Fatalf("rewritten = %q", got)
	}
	if got, ok := ruleCondense("chunk_size 怎么配置", history); ok {
		t.Fatalf("standalone query was rewritten to %q", got)
	}
	if got, ok := ruleCondense("它呢？", nil); ok {
		t.Fatalf("empty history rewrote to %q", got)
	}
}

func TestCondensePromptIsBilingual(t *testing.T) {
	history := []Turn{{Role: "user", Content: "我们讨论 chunk_size"}}
	for _, tc := range []struct {
		lang string
		want string
	}{
		{config.LangEn, "PASS"},
		{config.LangZh, "它"},
	} {
		var seen string
		complete := func(_ context.Context, system, _ string) (string, error) {
			seen = system
			return "PASS", nil
		}
		_, ok := condenseQuery(context.Background(), "它呢？", history, complete, tc.lang)
		if ok {
			t.Fatal("PASS must not count as a rewrite")
		}
		if !strings.Contains(seen, tc.want) {
			t.Fatalf("lang=%s prompt missing %q: %q", tc.lang, tc.want, seen)
		}
	}
}

func TestCondenseQueryStripsQuotesAndTrailingPunctuation(t *testing.T) {
	history := []Turn{{Role: "user", Content: "retry policy"}}
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "\"What is the retry policy?\"", nil
	}
	got, ok := condenseQuery(context.Background(), "它呢？", history, complete, config.LangEn)
	if !ok || got != "What is the retry policy" {
		t.Fatalf("cleaned = %q, %v", got, ok)
	}
	if _, ok := condenseQuery(context.Background(), "它呢？", history, nil, config.LangZh); ok {
		t.Fatal("nil complete must fail open")
	}
}

func TestResolveQueryPrefersLLMThenRule(t *testing.T) {
	history := []Turn{{Role: "user", Content: "chunk_size 的默认值？"}}
	cfg := config.Default()
	cfg.CondenseEnabled = true

	llm := func(_ context.Context, _, _ string) (string, error) {
		return "kbcli chunk_size 默认值是多少", nil
	}
	q, meta := resolveQuery(context.Background(), "它呢？", history, cfg, llm)
	if !meta.Applied || meta.Method != "llm" || meta.Rewritten != "kbcli chunk_size 默认值是多少" || q != meta.Rewritten {
		t.Fatalf("llm meta = %+v, q=%q", meta, q)
	}

	pass := func(_ context.Context, _, _ string) (string, error) { return "PASS", nil }
	q, meta = resolveQuery(context.Background(), "它呢？", history, cfg, pass)
	if !meta.Applied || meta.Method != "rule" || !strings.Contains(q, "chunk_size") {
		t.Fatalf("rule meta = %+v, q=%q", meta, q)
	}

	cfg.CondenseEnabled = false
	q, meta = resolveQuery(context.Background(), "它呢？", history, cfg, nil)
	if meta.Applied || q != "它呢？" {
		t.Fatalf("disabled meta = %+v, q=%q", meta, q)
	}
}

func TestRetrieverReportsLastCondense(t *testing.T) {
	cfg := config.Default()
	cfg.CondenseEnabled = true
	store := &fakeStore{
		chunks: []index.Chunk{mkChunk("a", "a.md", "chunk_size defaults to 600 runes", 0)},
		hits: []index.Hit{
			{Rank: 1, Score: 0.8, Chunk: mkChunk("a", "a.md", "chunk_size defaults to 600 runes", 0)},
		},
		root: "/kb",
		rev:  1,
	}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	reporter, ok := r.(CondenseReporter)
	if !ok {
		t.Fatal("retriever does not implement CondenseReporter")
	}
	_, err = r.SearchWithHistory(context.Background(), "它呢？", 2, []Turn{
		{Role: "user", Content: "chunk_size 的默认值？"},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := reporter.LastCondense()
	if !meta.Applied || meta.Method != "rule" {
		t.Fatalf("last condense = %+v", meta)
	}

	_, err = r.Search(context.Background(), "chunk_size", 2)
	if err != nil {
		t.Fatal(err)
	}
	if reporter.LastCondense().Applied {
		t.Fatal("a standalone search must clear the previous rewrite")
	}
}
