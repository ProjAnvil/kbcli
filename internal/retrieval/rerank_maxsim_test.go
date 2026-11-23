package retrieval

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
)

type tokenFake struct {
	tokens map[string]embed.TokenEmbeddings
}

func (f *tokenFake) Dim() int                        { return 2 }
func (f *tokenFake) Version() string                 { return "test" }
func (f *tokenFake) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }
func (f *tokenFake) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0}
	}
	return out, nil
}
func (f *tokenFake) Close() error { return nil }
func (f *tokenFake) EmbedTokensWithPrefix(_ context.Context, text, prefix string) (embed.TokenEmbeddings, error) {
	key := prefix + text
	if got, ok := f.tokens[key]; ok {
		return got, nil
	}
	return f.tokens["default"], nil
}

func TestLocalMaxSimReranksWithSameEmbedder(t *testing.T) {
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankLocalMaxSim
	cfg.RerankTopN = 2

	fake := &tokenFake{tokens: map[string]embed.TokenEmbeddings{
		"query: retry": {
			IDs:     []int64{3, 4},
			Vectors: [][]float32{{1, 0}, {0, 1}},
		},
		"passage: exact": {
			IDs:     []int64{3, 4},
			Vectors: [][]float32{{1, 0}, {0, 1}},
		},
		"passage: partial": {
			IDs:     []int64{3},
			Vectors: [][]float32{{1, 0}},
		},
		"default": {
			IDs:     []int64{3},
			Vectors: [][]float32{{-1, 0}},
		},
	}}
	hits := []index.Hit{
		{Rank: 1, Chunk: index.Chunk{Body: "partial"}},
		{Rank: 2, Chunk: index.Chunk{Body: "exact"}},
	}

	out, err := rerankHitsWithEmbedder(context.Background(), "retry", hits, cfg, nil, fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !strings.Contains(out[0].Chunk.Body, "exact") {
		t.Fatalf("maxsim order = %+v, want exact first", out)
	}
	if out[0].Score <= out[1].Score {
		t.Fatalf("scores = %g/%g, want exact higher", out[0].Score, out[1].Score)
	}
}

func TestLocalMaxSimFailsOpenWithoutTokenEmbedder(t *testing.T) {
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankLocalMaxSim
	hits := []index.Hit{{Rank: 1, Chunk: index.Chunk{Body: "x"}}}

	out, err := rerankHits(context.Background(), "q", hits, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("fail-open hits = %+v", out)
	}
}
