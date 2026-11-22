package retrieval

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// optHits builds deterministic hits whose chunk IDs follow the argument order.
func optHits(ids ...string) []index.Hit {
	out := make([]index.Hit, 0, len(ids))
	for i, id := range ids {
		out = append(out, index.Hit{
			Rank:  i + 1,
			Score: float64(len(ids)-i) / 10,
			Chunk: index.Chunk{ID: id, Body: "body of " + id, Text: "text of " + id},
		})
	}
	return out
}

func assertOptIDs(t *testing.T, hits []index.Hit, want ...string) {
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

func TestOptTransformNoneSearchesOriginalOnce(t *testing.T) {
	cfg := config.Default()
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return optHits("a"), nil
	}
	got, err := transformedSearch(context.Background(), "original", nil, cfg, nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "original" {
		t.Fatalf("rawSearch calls = %v, want [original]", calls)
	}
	assertOptIDs(t, got, "a")
}

func TestOptHyDEPassesGeneratedDocument(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformHyDE
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "A hypothetical passage about retry policy.", nil
	}
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return optHits("a"), nil
	}
	if _, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "A hypothetical passage about retry policy." {
		t.Fatalf("hyde rawSearch calls = %v", calls)
	}
}

func TestOptHyDEFailsOpen(t *testing.T) {
	for name, complete := range map[string]CompleteFunc{
		"empty": func(_ context.Context, _, _ string) (string, error) { return "  ", nil },
		"error": func(_ context.Context, _, _ string) (string, error) { return "", errors.New("llm down") },
		"nil":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			cfg.QueryTransform = config.TransformHyDE
			var calls []string
			raw := func(_ context.Context, q string) ([]index.Hit, error) {
				calls = append(calls, q)
				return optHits("a"), nil
			}
			got, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0] != "retry" {
				t.Fatalf("fallback calls = %v, want [retry]", calls)
			}
			assertOptIDs(t, got, "a")
		})
	}
}

func TestOptMultiQueryCallsAndFuses(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformMultiQuery
	cfg.MultiQueryCount = 3
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "- retry policy\n- backoff rules\n- request retries\n", nil
	}
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return optHits("a"), nil
	}
	got, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"retry", "retry policy", "backoff rules", "request retries"}
	if len(calls) != len(want) {
		t.Fatalf("rawSearch calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("rawSearch calls = %v, want %v", calls, want)
		}
	}
	if got == nil {
		t.Fatal("fused hits must not be nil")
	}
}

func TestOptMultiQueryNoLinesFallsBack(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformMultiQuery
	complete := func(_ context.Context, _, _ string) (string, error) { return " \n \n", nil }
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return optHits("a"), nil
	}
	if _, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "retry" {
		t.Fatalf("fallback calls = %v, want [retry]", calls)
	}
}

func TestOptDecompositionDedupePreservesOrder(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformDecomposition
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "sub one\nsub two\n", nil
	}
	results := map[string][]index.Hit{
		"retry":   optHits("a", "b"),
		"sub one": optHits("b", "c"),
		"sub two": optHits("c", "d"),
	}
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return results[q], nil
	}
	got, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw)
	if err != nil {
		t.Fatal(err)
	}
	assertOptIDs(t, got, "a", "b", "c", "d")
	for i, h := range got {
		if h.Rank != i+1 {
			t.Fatalf("rank at %d = %d, want %d", i, h.Rank, i+1)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("rawSearch calls = %v, want 3", calls)
	}
}

func TestOptStepBackFusesAbstractThenOriginal(t *testing.T) {
	cfg := config.Default()
	cfg.QueryTransform = config.TransformStepBack
	complete := func(_ context.Context, _, _ string) (string, error) {
		return "What is the general retry design?", nil
	}
	var calls []string
	raw := func(_ context.Context, q string) ([]index.Hit, error) {
		calls = append(calls, q)
		return optHits("a"), nil
	}
	got, err := transformedSearch(context.Background(), "retry", nil, cfg, complete, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"What is the general retry design?", "retry"}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("step-back calls = %v, want %v", calls, want)
	}
	if got == nil {
		t.Fatal("fused hits must not be nil")
	}
}

func TestOptNilCompleteFallsBackForEveryTransform(t *testing.T) {
	for _, transform := range []string{
		config.TransformHyDE,
		config.TransformMultiQuery,
		config.TransformDecomposition,
		config.TransformStepBack,
	} {
		t.Run(transform, func(t *testing.T) {
			cfg := config.Default()
			cfg.QueryTransform = transform
			var calls []string
			raw := func(_ context.Context, q string) ([]index.Hit, error) {
				calls = append(calls, q)
				return optHits("a"), nil
			}
			got, err := transformedSearch(context.Background(), "retry", nil, cfg, nil, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0] != "retry" {
				t.Fatalf("fallback calls = %v, want [retry]", calls)
			}
			assertOptIDs(t, got, "a")
		})
	}
}

func TestOptTransformReportsRawSearchError(t *testing.T) {
	cfg := config.Default()
	raw := func(_ context.Context, _ string) ([]index.Hit, error) {
		return nil, errors.New("store down")
	}
	_, err := transformedSearch(context.Background(), "retry", nil, cfg, nil, raw)
	if err == nil {
		t.Fatal("expected an error when the original search fails")
	}
}

func TestOptRerankDisabledIsIdentity(t *testing.T) {
	cfg := config.Default()
	cfg.RerankEnabled = false
	hits := optHits("a", "b")
	hits[0].Rank, hits[1].Rank = 3, 1
	out, err := rerankHits(context.Background(), "q", hits, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOptIDs(t, out, "a", "b")
	if out[0].Rank != 3 || out[1].Rank != 1 {
		t.Fatalf("disabled rerank changed ranks: %+v", out)
	}
}

func TestOptCrossEncoderServerFailureFailsOpen(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"json": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		},
		"empty": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"results":[]}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			cfg := config.Default()
			cfg.RerankEnabled = true
			cfg.RerankBaseURL = srv.URL
			hits := optHits("a", "b")
			out, err := rerankHits(context.Background(), "q", hits, cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertOptIDs(t, out, "a", "b")
		})
	}
}

func TestOptCrossEncoderTopNTruncatesAndRanks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"index":2,"relevance_score":0.9},{"index":0,"relevance_score":0.5},{"index":1,"relevance_score":0.1}]}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.RerankEnabled = true
	cfg.RerankType = config.RerankCrossEncoder
	cfg.RerankBaseURL = srv.URL
	cfg.RerankTopN = 2
	out, err := rerankHits(context.Background(), "q", optHits("a", "b", "c"), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOptIDs(t, out, "c", "a")
	if out[0].Rank != 1 || out[1].Rank != 2 {
		t.Fatalf("ranks were not reassigned: %+v", out)
	}
}
