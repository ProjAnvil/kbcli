package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// fakeStore implements the full index.Store contract. Search returns a fixed
// list so the vector side of the pipeline is deterministic.
type fakeStore struct {
	mu      sync.Mutex
	chunks  []index.Chunk
	hits    []index.Hit
	rev     uint64
	root    string
	sig     string
	err     error
	calls   int
	lastK   int
	queries []string
}

func (s *fakeStore) Add(cs []index.Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, cs...)
	s.rev++
	return nil
}
func (s *fakeStore) Search(q string, k int) ([]index.Hit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastK = k
	s.queries = append(s.queries, q)
	if s.err != nil {
		return nil, s.err
	}
	out := make([]index.Hit, len(s.hits))
	copy(out, s.hits)
	return out, nil
}

// lastRequestedK and searchedQueries return snapshots so tests never read the
// store while a search could still be running.
func (s *fakeStore) lastRequestedK() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastK
}

func (s *fakeStore) searchedQueries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}
func (s *fakeStore) Save(string) error                   { return nil }
func (s *fakeStore) Sources() map[string]index.FileState { return map[string]index.FileState{} }
func (s *fakeStore) SetSource(index.FileState)           {}
func (s *fakeStore) DropSource(string)                   {}
func (s *fakeStore) Len() int                            { return len(s.chunks) }
func (s *fakeStore) EmbedderVersion() string             { return "local-v1" }
func (s *fakeStore) Root() string                        { return s.root }
func (s *fakeStore) Chunks() []index.Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]index.Chunk, len(s.chunks))
	copy(out, s.chunks)
	return out
}
func (s *fakeStore) Revision() uint64           { return s.rev }
func (s *fakeStore) BuildSignature() string     { return s.sig }
func (s *fakeStore) SetBuildSignature(v string) { s.sig = v }

func mkChunk(id, source, body string, ordinal int) index.Chunk {
	return index.Chunk{
		ID:      id,
		Text:    body,
		Body:    body,
		Source:  source,
		Ordinal: ordinal,
	}
}

func mkHit(id, source, body string, score float64) index.Hit {
	return index.Hit{Rank: 1, Score: score, Chunk: mkChunk(id, source, body, 0)}
}

func TestTokenize(t *testing.T) {
	got := strings.Join(tokenize("Hello WORLD 42!"), "|")
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") || !strings.Contains(got, "42") {
		t.Fatalf("ASCII tokenization = %q", got)
	}
	cjk := tokenize("向量检索")
	joined := strings.Join(cjk, "|")
	for _, want := range []string{"向量检索", "向", "量", "检", "索", "向量", "量检", "检索"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("CJK tokenization %q missing %q", joined, want)
		}
	}
	if a, b := tokenize("Retry Backoff"), tokenize("Retry Backoff"); strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("tokenize is not deterministic")
	}
	if got := tokenize(""); len(got) != 0 {
		t.Fatalf("empty text produced %d tokens", len(got))
	}
}

func TestBM25RanksLexicalMatch(t *testing.T) {
	idx := newBM25Index([]index.Chunk{
		mkChunk("a", "a.md", "the client retries requests with exponential backoff", 0),
		mkChunk("b", "b.md", "tokens expire after one hour and must be refreshed", 1),
		mkChunk("c", "c.md", "caching layer stores compiled templates on disk", 2),
	}, 1.5, 0.75, 1)
	hits := idx.search("exponential backoff retry", 3)
	if len(hits) == 0 {
		t.Fatal("BM25 returned no hits")
	}
	if hits[0].Chunk.ID != "a" {
		t.Fatalf("BM25 top hit = %q, want a", hits[0].Chunk.ID)
	}
	if hits[0].Score <= 0 || hits[0].Score > 1 {
		t.Fatalf("BM25 score = %f, want (0,1]", hits[0].Score)
	}
	if hits[0].Rank != 1 {
		t.Fatalf("BM25 rank = %d, want 1", hits[0].Rank)
	}
	empty := newBM25Index(nil, 1.5, 0.75, 1).search("anything", 3)
	if empty == nil || len(empty) != 0 {
		t.Fatal("empty corpus must return a non-nil empty slice")
	}
}

func TestFuseRRF(t *testing.T) {
	a := []index.Hit{mkHit("a", "a.md", "A", 0.9), mkHit("b", "b.md", "B", 0.8)}
	b := []index.Hit{mkHit("b", "b.md", "B", 0.7), mkHit("c", "c.md", "C", 0.6)}
	out := fuseRRF([][]index.Hit{a, b}, []float64{0.5, 0.5}, 60)
	if len(out) != 3 {
		t.Fatalf("fused %d hits, want 3", len(out))
	}
	if out[0].Chunk.ID != "b" {
		t.Fatalf("RRF top hit = %q, want b (present in both lists)", out[0].Chunk.ID)
	}
	for i, h := range out {
		if h.Rank != i+1 {
			t.Fatalf("rank at %d = %d", i, h.Rank)
		}
		if h.Score < 0 || h.Score > 1 {
			t.Fatalf("unnormalized score %f", h.Score)
		}
	}
	if got := fuseRRF(nil, nil, 0); got == nil || len(got) != 0 {
		t.Fatal("empty rankings must return a non-nil empty slice")
	}
	if got := fuseRRF([][]index.Hit{a}, nil, 0); len(got) != 2 {
		t.Fatalf("mismatched weights should still fuse, got %d", len(got))
	}
}

func TestTrimToBudget(t *testing.T) {
	parts := []string{"question", strings.Repeat("证据", 2000), "tail"}
	got := trimToBudget(parts, 100)
	if len(got) == 0 || got[0] != "question" {
		t.Fatalf("question was not preserved: %v", got)
	}
	if totalRunes(got) > 100+20 {
		t.Fatalf("budget exceeded: %d", totalRunes(got))
	}
	if got := trimToBudget(nil, 100); got == nil || len(got) != 0 {
		t.Fatal("nil parts must return a non-nil empty slice")
	}
	if got := trimToBudget([]string{"only"}, 1); len(got) != 1 {
		t.Fatal("the first part must survive even when it exceeds the budget")
	}
}

func TestFormatSources(t *testing.T) {
	if got := formatSources(nil, 100); !strings.Contains(got, "No knowledge base") {
		t.Fatalf("empty sentinel = %q", got)
	}
	got := formatSources([]index.Hit{mkHit("a", "docs/a.md", strings.Repeat("x", 50), 0.8123)}, 10)
	if !strings.Contains(got, "[1] docs/a.md score=0.812") {
		t.Fatalf("citation header = %q", got)
	}
	if strings.Contains(got, strings.Repeat("x", 11)) {
		t.Fatal("per-chunk cap was not applied")
	}
}

func TestRetrieverModes(t *testing.T) {
	chunks := []index.Chunk{
		mkChunk("a", "a.md", "exponential backoff retry policy", 0),
		mkChunk("b", "b.md", "token refresh endpoint", 1),
	}
	vectorHits := []index.Hit{
		{Rank: 1, Score: 0.9, Chunk: chunks[0]},
		{Rank: 2, Score: 0.4, Chunk: chunks[1]},
	}
	cases := []struct {
		name   string
		method string
		wantID string
	}{
		{"vector", config.RetrievalVector, "a"},
		{"bm25", config.RetrievalBM25, "a"},
		{"hybrid", config.RetrievalHybrid, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.RetrievalMethod = tc.method
			cfg.BM25TopK = 20
			store := &fakeStore{chunks: chunks, hits: vectorHits, root: "/kb", rev: 1}
			r, err := New(Options{Config: cfg, Store: store})
			if err != nil {
				t.Fatal(err)
			}
			hits, err := r.Search(context.Background(), "exponential backoff", 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) == 0 {
				t.Fatal("no hits")
			}
			if hits[0].Chunk.ID != tc.wantID {
				t.Fatalf("top hit = %q, want %q", hits[0].Chunk.ID, tc.wantID)
			}
			if hits[0].Rank != 1 {
				t.Fatalf("rank = %d, want 1", hits[0].Rank)
			}
		})
	}
}

func TestMinScoreAndContextExpand(t *testing.T) {
	chunks := []index.Chunk{
		mkChunk("a", "a.md", "exponential backoff retry policy", 0),
		mkChunk("a2", "a.md", "adjacent paragraph about retry limits", 1),
		mkChunk("b", "b.md", "token refresh endpoint", 0),
	}
	vectorHits := []index.Hit{
		{Rank: 1, Score: 0.9, Chunk: chunks[0]},
		{Rank: 2, Score: 0.1, Chunk: chunks[2]},
	}
	cfg := config.Default()
	cfg.MinScore = 0.5
	cfg.ContextExpand = 1
	// This test covers min_score and context expansion only. Reranking is on by
	// default and replaces vector scores with unnormalized cross-encoder logits
	// whenever the model is present, so disable it to keep the assertions on a
	// stable 0..1 scale and independent of the local model cache.
	cfg.RerankEnabled = false
	store := &fakeStore{chunks: chunks, hits: vectorHits, root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := r.Search(context.Background(), "retry", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Chunk.ID == "b" {
			t.Fatal("min_score did not drop the low-scoring vector hit")
		}
	}
	parentScore := 0.0
	foundParent := false
	for _, h := range hits {
		if h.Chunk.ID == "a" {
			parentScore = h.Score
			foundParent = true
		}
	}
	if !foundParent {
		t.Fatal("parent hit missing")
	}
	foundExpansion := false
	for _, h := range hits {
		if h.Chunk.ID == "a2" {
			foundExpansion = true
			if h.Score >= parentScore {
				t.Fatalf("expanded chunk score %g should be below its parent %g", h.Score, parentScore)
			}
		}
	}
	if !foundExpansion {
		t.Fatal("adjacent chunk expansion did not run")
	}
}

// A production RAG pipeline fetches a broad candidate pool before reranking.
// TopK must only bound the final evidence set, not the vector search itself.
func TestCandidateKWidensVectorSearch(t *testing.T) {
	chunks := []index.Chunk{}
	hits := []index.Hit{}
	for i := 0; i < 10; i++ {
		c := mkChunk(string(rune('a'+i)), "docs.md", "body", i)
		chunks = append(chunks, c)
		hits = append(hits, index.Hit{Rank: i + 1, Score: 1 - float64(i)/20, Chunk: c})
	}
	cfg := config.Default()
	cfg.RerankEnabled = false
	cfg.CandidateK = 40
	cfg.RetrievalMethod = config.RetrievalVector
	store := &fakeStore{chunks: chunks, hits: hits, root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Search(context.Background(), "question", 4)
	if err != nil {
		t.Fatal(err)
	}
	if k := store.lastRequestedK(); k != 40 {
		t.Fatalf("vector search k = %d, want candidate_k 40", k)
	}
	if len(got) != 4 {
		t.Fatalf("final hits = %d, want top_k 4", len(got))
	}
}

// min_score is applied after reranking, on the normalized 0..1 scale, so a
// hit the reranker rescues is not discarded by a pre-rerank threshold.
func TestMinScoreRunsAfterRerankNormalization(t *testing.T) {
	chunks := []index.Chunk{
		mkChunk("a", "a.md", "high", 0),
		mkChunk("b", "b.md", "low", 0),
		mkChunk("c", "c.md", "medium", 0),
	}
	// Install a fake cross-encoder whose raw logits are 0.9, -5, 0.5.
	// sigmoid(-5) is far below the 0.5 threshold.
	fake := &fakeAikitCE{scores: []float32{0.9, -5, 0.5}}
	installFakeAikit(t, fake, nil)
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankAikit
	cfg.RerankModelPath = "/models/cross-encoder"
	cfg.RerankTopN = 10
	cfg.MinScore = 0.5
	cfg.CandidateK = 40
	store := &fakeStore{chunks: chunks, hits: []index.Hit{
		{Rank: 1, Score: 0.6, Chunk: chunks[0]},
		{Rank: 2, Score: 0.6, Chunk: chunks[1]},
		{Rank: 3, Score: 0.6, Chunk: chunks[2]},
	}, root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Search(context.Background(), "question", 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, h := range got {
		ids[h.Chunk.ID] = true
		if h.Score < cfg.MinScore {
			t.Fatalf("hit %q score %g is below min_score %g", h.Chunk.ID, h.Score, cfg.MinScore)
		}
	}
	if !ids["a"] || !ids["c"] {
		t.Fatalf("reranked hits a and c must survive, got %v", ids)
	}
	if ids["b"] {
		t.Fatal("the negative-logit hit b must be filtered out")
	}
}

// Context expansion adds neighbours as extra evidence. The old pipeline
// appended them and then truncated back to k, silently dropping them.
func TestContextExpandSurvivesFinalTake(t *testing.T) {
	chunks := []index.Chunk{
		mkChunk("a", "a.md", "primary", 0),
		mkChunk("a2", "a.md", "adjacent", 1),
		mkChunk("b", "b.md", "secondary", 0),
	}
	cfg := config.Default()
	cfg.RerankEnabled = false
	cfg.ContextExpand = 1
	cfg.CandidateK = 40
	store := &fakeStore{chunks: chunks, hits: []index.Hit{
		{Rank: 1, Score: 0.9, Chunk: chunks[0]},
		{Rank: 2, Score: 0.8, Chunk: chunks[2]},
	}, root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Search(context.Background(), "primary", 2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range got {
		if h.Chunk.ID == "a2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expanded neighbour was truncated away: %+v", got)
	}
}

// A condensed rewrite must not replace the verbatim query's recall. The
// retriever runs a second search on the original question and fuses both.
func TestCondenseRewriteKeepsOriginalQueryRecall(t *testing.T) {
	chunks := []index.Chunk{mkChunk("a", "a.md", "retries", 0)}
	cfg := config.Default()
	cfg.RerankEnabled = false
	cfg.CondenseEnabled = true
	cfg.CandidateK = 40
	store := &fakeStore{chunks: chunks, hits: []index.Hit{
		{Rank: 1, Score: 0.8, Chunk: chunks[0]},
	}, root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.SearchWithHistory(context.Background(), "what about it", 2,
		[]Turn{{Role: "user", Content: "tell me about retries"}})
	if err != nil {
		t.Fatal(err)
	}
	queries := store.searchedQueries()
	if len(queries) != 2 {
		t.Fatalf("searches = %d, want 2 (condensed + original): %v", len(queries), queries)
	}
	if queries[1] != "what about it" {
		t.Fatalf("second search query = %q, want the verbatim question", queries[1])
	}
	if queries[0] == queries[1] {
		t.Fatal("condensed rewrite did not produce a distinct query")
	}
}

// Passing each citation as its own budget part means trimming drops whole
// trailing snippets. A numbered passage must never be sliced mid-marker.
func TestTrimToBudgetKeepsCitationNumbersIntact(t *testing.T) {
	var hits []index.Hit
	for i := 0; i < 6; i++ {
		hits = append(hits, mkHit(string(rune('a'+i)), "doc.md", strings.Repeat("word ", 80), 1-float64(i)/10))
	}
	parts := []string{"system prompt", "Question:\nq", "Evidence:\n"}
	parts = append(parts, FormatCitationParts(hits, nil, 400)...)
	kept := TrimToBudget(parts, 700)
	if len(kept) < 3 {
		t.Fatalf("budget trimmed away the question: %v", kept)
	}
	seen := 0
	for _, p := range kept[3:] {
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "[") {
			t.Fatalf("citation part lost its number: %q", p)
		}
		closing := strings.Index(p, "]")
		if closing < 2 {
			t.Fatalf("citation number was cut in half: %q", p)
		}
		seen++
	}
	if seen == 0 || seen > len(hits) {
		t.Fatalf("kept %d citation parts out of %d", seen, len(hits))
	}
	if seen == len(hits) {
		t.Fatal("budget did not trim any citation")
	}
}

func TestQueryTransformFailOpen(t *testing.T) {
	for _, transform := range []string{
		config.TransformHyDE, config.TransformMultiQuery,
		config.TransformDecomposition, config.TransformStepBack,
	} {
		t.Run(transform, func(t *testing.T) {
			cfg := config.Default()
			cfg.QueryTransform = transform
			cfg.CondenseEnabled = true
			chunks := []index.Chunk{mkChunk("a", "a.md", "retry policy", 0)}
			store := &fakeStore{chunks: chunks, hits: []index.Hit{
				{Rank: 1, Score: 0.8, Chunk: chunks[0]},
			}, root: "/kb", rev: 1}
			r, err := New(Options{Config: cfg, Store: store, Complete: nil})
			if err != nil {
				t.Fatal(err)
			}
			hits, err := r.SearchWithHistory(context.Background(), "what about it", 2, []Turn{{Role: "user", Content: "tell me about retries"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) == 0 {
				t.Fatal("fail-open produced no hits")
			}
		})
	}
}

func TestMultiQueryFusionAndCondense(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformMultiQuery
	cfg.MultiQueryCount = 3
	cfg.CondenseEnabled = true
	chunks := []index.Chunk{mkChunk("a", "a.md", "retry policy", 0)}
	store := &fakeStore{chunks: chunks, hits: []index.Hit{
		{Rank: 1, Score: 0.8, Chunk: chunks[0]},
	}, root: "/kb", rev: 1}

	complete := func(_ context.Context, system, _ string) (string, error) {
		if strings.Contains(system, "standalone") {
			return "PASS", nil
		}
		return "retry policy\nexponential backoff\nrequest retries\n", nil
	}
	r, err := New(Options{Config: cfg, Store: store, Complete: complete})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Search(context.Background(), "retries", 2); err != nil {
		t.Fatal(err)
	}
	// 1 original + 3 variants = 4 rawSearch calls.
	if store.calls != 4 {
		t.Fatalf("rawSearch calls = %d, want 4", store.calls)
	}

	rewritten := func(_ context.Context, system, _ string) (string, error) {
		if strings.Contains(system, "standalone") {
			return "What is the retry policy?", nil
		}
		return "one variant", nil
	}
	q, ok := condenseQuery(context.Background(), "what about it", []Turn{{Role: "user", Content: "retries"}}, rewritten, config.LangEn)
	if !ok || q != "What is the retry policy" {
		t.Fatalf("condense = %q, %v", q, ok)
	}
}

func TestRerankFailOpen(t *testing.T) {
	chunks := []index.Chunk{mkChunk("a", "a.md", "retry policy", 0)}
	store := &fakeStore{chunks: chunks, hits: []index.Hit{
		{Rank: 1, Score: 0.8, Chunk: chunks[0]},
	}, root: "/kb", rev: 1}
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankCrossEncoder
	cfg.RerankBaseURL = "http://127.0.0.1:1"
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := r.Search(context.Background(), "retry", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("fail-open lost hits: %d", len(hits))
	}

	direct, err := rerankHits(context.Background(), "retry", nil, cfg, nil)
	if err != nil || len(direct) != 0 {
		t.Fatalf("empty rerank = %v, %v", direct, err)
	}
}

func TestCrossEncoderRerank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, ok := body["documents"]; !ok {
			t.Error("missing documents")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.97},
				{"index": 0, "relevance_score": 0.12},
			},
		})
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankCrossEncoder
	cfg.RerankBaseURL = srv.URL
	cfg.RerankModel = "bge-reranker"
	cfg.RerankAPIKey = "secret"
	hits := []index.Hit{
		mkHit("a", "a.md", "A", 0.9),
		mkHit("b", "b.md", "B", 0.8),
	}
	out, err := rerankHits(context.Background(), "query", hits, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Chunk.ID != "b" || out[0].Score != 0.97 {
		t.Fatalf("cross-encoder order = %+v", out)
	}
	if out[0].Rank != 1 || out[1].Rank != 2 {
		t.Fatal("ranks were not reassigned")
	}
}

func TestLLMRerank(t *testing.T) {
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankLLM
	hits := []index.Hit{
		mkHit("a", "a.md", "A", 0.9),
		mkHit("b", "b.md", "B", 0.8),
		mkHit("c", "c.md", "C", 0.7),
	}
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "3, 1", nil
	}
	out, err := rerankHits(context.Background(), "query", hits, cfg, complete)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Chunk.ID != "c" || out[1].Chunk.ID != "a" || out[2].Chunk.ID != "b" {
		t.Fatalf("llm rerank order = %+v", out)
	}
	garbage := func(_ context.Context, _, _ string) (string, error) { return "no indices", nil }
	same, err := rerankHits(context.Background(), "query", hits, cfg, garbage)
	if err != nil || same[0].Chunk.ID != "a" {
		t.Fatalf("garbage llm output did not fail open: %v %v", same, err)
	}
}

func TestVectorErrorFailsOpenToFallback(t *testing.T) {
	cfg := config.Default()
	store := &fakeStore{err: errors.New("store down"), root: "/kb", rev: 1}
	r, err := New(Options{Config: cfg, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := r.Search(context.Background(), "anything", 2)
	// transformedSearch falls back to rawSearch, which also errors; the
	// retriever must still return a non-nil slice and no panic.
	if err != nil {
		t.Fatalf("retriever returned an error instead of an empty result: %v", err)
	}
	if hits == nil {
		t.Fatal("hits must not be nil")
	}
}
