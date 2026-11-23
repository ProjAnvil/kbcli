package retrieval

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

func TestTextLanguage(t *testing.T) {
	if got := textLanguage("What is the retry policy?"); got != config.KBLanguageEn {
		t.Fatalf("english = %q", got)
	}
	if got := textLanguage("重试策略是什么？"); got != config.KBLanguageZh {
		t.Fatalf("chinese = %q", got)
	}
	if got := textLanguage("123?"); got != "" {
		t.Fatalf("no-letter = %q", got)
	}
}

func TestCorpusLanguageFromChunks(t *testing.T) {
	chunks := []index.Chunk{
		{Body: "retry policy timeout"},
		{Body: "重试策略 超时 退避 指数退避 最大重试次数 业务异常 技术异常 幂等校验"},
		{Body: "重试次数 和 指数退避 熔断降级 人工处理 告警通知 失败队列 补偿事务"},
		{Body: "retry limit"},
	}
	if got := corpusLanguage(chunks); got != config.KBLanguageZh {
		t.Fatalf("corpus language = %q, want zh", got)
	}
}

func TestCorpusLanguageUsesChunkMetadata(t *testing.T) {
	chunks := []index.Chunk{
		{Metadata: map[string]string{"language": "en-US"}, Body: "系统设计面试"},
		{Metadata: map[string]string{"lang": "en"}, Body: "系统设计面试"},
		{Body: "重试策略 超时 退避 指数退避 最大重试次数 业务异常 技术异常 幂等校验"},
	}
	if got := corpusLanguage(chunks); got != config.KBLanguageEn {
		t.Fatalf("corpus language = %q, want metadata override en", got)
	}
}

func TestQuickSearchStillTranslates(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTranslation = config.QueryTranslationOn
	cfg.RerankEnabled = false
	store := &fakeStore{
		chunks: []index.Chunk{{Body: "系统设计面试沟通策略", Metadata: map[string]string{"language": "en"}}},
		hits:   []index.Hit{{Rank: 1, Chunk: index.Chunk{ID: "a", Body: "hit"}}},
		root:   "/kb",
		rev:    1,
	}
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "system design interview", nil
	}
	r, err := newRetriever(Options{Config: cfg, Store: store, Complete: complete})
	if err != nil {
		t.Fatal(err)
	}
	quick, ok := r.(QuickRetriever)
	if !ok {
		t.Fatal("retriever lacks QuickRetriever")
	}
	if _, err := quick.SearchQuick(context.Background(), "什么是系统设计", 3); err != nil {
		t.Fatal(err)
	}
	queries := store.searchedQueries()
	translated := false
	for _, q := range queries {
		if q == "system design interview" {
			translated = true
		}
	}
	if !translated {
		t.Fatalf("quick search queries = %v, want translated query", queries)
	}
	reporter, ok := r.(CondenseReporter)
	if !ok {
		t.Fatal("retriever lacks CondenseReporter")
	}
	if meta := reporter.LastCondense(); !meta.Translated || meta.SearchQuery != "system design interview" {
		t.Fatalf("translation meta = %+v", meta)
	}
}

func TestTranslateQueryPreservesLiteral(t *testing.T) {
	var gotSystem, gotUser string
	complete := func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user
		return "retry policy RETRY_TIMEOUT_MS", nil
	}
	out, err := translateQuery(context.Background(), "重试策略 RETRY_TIMEOUT_MS", config.KBLanguageZh, config.KBLanguageEn, complete)
	if err != nil {
		t.Fatal(err)
	}
	if out != "retry policy RETRY_TIMEOUT_MS" {
		t.Fatalf("translation = %q", out)
	}
	if !strings.Contains(gotSystem, "Preserve product names") || gotUser != "重试策略 RETRY_TIMEOUT_MS" {
		t.Fatalf("prompt = %q / %q", gotSystem, gotUser)
	}
}
