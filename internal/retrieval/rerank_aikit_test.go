package retrieval

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// fakeAikitCE records what the local reranker passed in and returns canned
// scores, so no model files are downloaded during tests.
type fakeAikitCE struct {
	dir         string
	query       string
	docs        []string
	concurrency int
	scores      []float32
	err         error
	closed      bool
	calls       int
}

func (f *fakeAikitCE) ScoreBatch(query string, docs []string, concurrency int) ([]float32, error) {
	f.calls++
	f.query = query
	f.docs = append([]string(nil), docs...)
	f.concurrency = concurrency
	return f.scores, f.err
}

func (f *fakeAikitCE) Close() error {
	f.closed = true
	return nil
}

func installFakeAikit(t *testing.T, fake *fakeAikitCE, loadErr error) {
	t.Helper()
	old := loadAikitCrossEncoder
	loadAikitCrossEncoder = func(dir string) (aikitCrossEncoder, error) {
		if loadErr != nil {
			return nil, loadErr
		}
		fake.dir = dir
		return fake, nil
	}
	t.Cleanup(func() { loadAikitCrossEncoder = old })
}

func aikitHits() []index.Hit {
	return []index.Hit{
		{Rank: 1, Score: 0.9, Chunk: index.Chunk{ID: "a", Body: "body a", Text: "text a"}},
		{Rank: 2, Score: 0.8, Chunk: index.Chunk{ID: "b", Body: "body b", Text: "text b"}},
		{Rank: 3, Score: 0.7, Chunk: index.Chunk{ID: "c", Body: "body c", Text: "text c"}},
	}
}

func aikitConfig() *config.Config {
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankAikit
	cfg.RerankModelPath = "/models/cross-encoder"
	cfg.RerankTopN = 10
	return cfg
}

func assertAikitIDs(t *testing.T, hits []index.Hit, want ...string) {
	t.Helper()
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		got = append(got, h.Chunk.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestAikitRerankAppliesScoresInInputOrder(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{0.9, 0.1, 0.5}}
	installFakeAikit(t, fake, nil)
	cfg := aikitConfig()

	out, err := rerankHits(context.Background(), "the query", aikitHits(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.dir != cfg.RerankModelPath {
		t.Fatalf("model path = %q, want %q", fake.dir, cfg.RerankModelPath)
	}
	if fake.query != "the query" {
		t.Fatalf("query = %q, want %q", fake.query, "the query")
	}
	if fake.concurrency != 0 {
		t.Fatalf("concurrency = %d, want 0", fake.concurrency)
	}
	if len(fake.docs) != 3 || fake.docs[0] != "body a" || fake.docs[1] != "body b" || fake.docs[2] != "body c" {
		t.Fatalf("documents = %v", fake.docs)
	}
	if fake.calls != 1 {
		t.Fatalf("ScoreBatch calls = %d, want 1", fake.calls)
	}
	if !fake.closed {
		t.Fatal("cross encoder was not closed")
	}
	assertAikitIDs(t, out, "a", "b", "c")
	for i, want := range []float32{0.9, 0.1, 0.5} {
		if diff := math.Abs(out[i].Score - float64(want)); diff > 1e-6 {
			t.Fatalf("score at %d = %v, want %v", i, out[i].Score, want)
		}
		if out[i].Rank != i+1 {
			t.Fatalf("rank at %d = %d, want %d", i, out[i].Rank, i+1)
		}
	}
}

func TestAikitRerankRespectsTopN(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{0.9, 0.1, 0.5}}
	installFakeAikit(t, fake, nil)
	cfg := aikitConfig()
	cfg.RerankTopN = 2

	out, err := rerankHits(context.Background(), "q", aikitHits(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b")
	if out[0].Rank != 1 || out[1].Rank != 2 {
		t.Fatalf("ranks were not reassigned: %+v", out)
	}
}

func TestAikitRerankFailsOpenOnScoreError(t *testing.T) {
	fake := &fakeAikitCE{err: errors.New("inference failed")}
	installFakeAikit(t, fake, nil)

	out, err := rerankHits(context.Background(), "q", aikitHits(), aikitConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b", "c")
	if !fake.closed {
		t.Fatal("cross encoder was not closed on error")
	}
}

func TestAikitRerankFailsOpenOnLoadError(t *testing.T) {
	installFakeAikit(t, &fakeAikitCE{}, errors.New("no model"))
	cfg := aikitConfig()

	out, err := rerankHits(context.Background(), "q", aikitHits(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b", "c")
}

func TestAikitRerankFailsOpenOnEmptyPath(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{1, 1, 1}}
	installFakeAikit(t, fake, nil)
	cfg := aikitConfig()
	cfg.RerankModelPath = "  "

	out, err := rerankHits(context.Background(), "q", aikitHits(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b", "c")
	if fake.calls != 0 {
		t.Fatal("ScoreBatch must not run without a model path")
	}
}

func TestAikitRerankFailsOpenOnScoreCountMismatch(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{0.9, 0.1}}
	installFakeAikit(t, fake, nil)

	out, err := rerankHits(context.Background(), "q", aikitHits(), aikitConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b", "c")
}

func TestAikitRerankFailsOpenOnCanceledContext(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{1, 1, 1}}
	installFakeAikit(t, fake, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := rerankHits(ctx, "q", aikitHits(), aikitConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAikitIDs(t, out, "a", "b", "c")
	if fake.calls != 0 {
		t.Fatal("ScoreBatch must not run on a canceled context")
	}
}

func TestAikitRerankTruncatesBodyAndFallsBackToText(t *testing.T) {
	fake := &fakeAikitCE{scores: []float32{0.9, 0.1}}
	installFakeAikit(t, fake, nil)
	hits := []index.Hit{
		{Chunk: index.Chunk{ID: "long", Body: strings.Repeat("x", 2500)}},
		{Chunk: index.Chunk{ID: "fallback", Text: "from text"}},
	}

	if _, err := rerankHits(context.Background(), "q", hits, aikitConfig(), nil); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(fake.docs[0]) != 2000 {
		t.Fatalf("long document has %d runes, want 2000", utf8.RuneCountInString(fake.docs[0]))
	}
	if fake.docs[1] != "from text" {
		t.Fatalf("fallback document = %q, want %q", fake.docs[1], "from text")
	}
}
