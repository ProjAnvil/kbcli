package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/townsendmerino/aikit/encoder"
)

// aikitCrossEncoder is the small surface used by the local reranker. Keeping
// it as an interface lets tests inject a fake instead of downloading a model.
type aikitCrossEncoder interface {
	ScoreBatch(query string, docs []string, concurrency int) ([]float32, error)
	Close() error
}

// loadAikitCrossEncoder is replaceable in tests to avoid model downloads.
var loadAikitCrossEncoder = func(dir string) (aikitCrossEncoder, error) {
	return encoder.LoadCrossEncoder(dir)
}

// rerankHits applies Porto's post-retrieval reranker. Any external failure
// returns the input hits unchanged with a nil error, so chat never breaks when
// a rerank service is misconfigured or offline.
func rerankHits(
	ctx context.Context,
	query string,
	hits []index.Hit,
	cfg *config.Config,
	complete CompleteFunc,
) ([]index.Hit, error) {
	return rerankHitsWithEmbedder(ctx, query, hits, cfg, complete, nil)
}

func rerankHitsWithEmbedder(
	ctx context.Context,
	query string,
	hits []index.Hit,
	cfg *config.Config,
	complete CompleteFunc,
	emb embed.Embedder,
) ([]index.Hit, error) {
	if cfg == nil || !cfg.RerankEnabled || len(hits) == 0 {
		return hits, nil
	}

	var out []index.Hit
	var err error
	switch cfg.RerankType {
	case config.RerankLLM:
		out, err = llmRerank(ctx, query, hits, complete)
	case config.RerankAikit:
		out, err = aikitRerank(ctx, query, hits, cfg)
	case config.RerankLocalMaxSim:
		out, err = localMaxSimRerank(ctx, query, hits, cfg, emb)
	default:
		out, err = crossEncoderRerank(ctx, query, hits, cfg)
	}
	if err != nil || len(out) == 0 {
		return hits, nil
	}

	// Cross-encoder logits and 0..1 relevance scores must share one scale with
	// vector / RRF scores. Without this, min_score and the evidence bar are
	// meaningless after reranking (aikit logits are unbounded). llmRerank only
	// reorders and keeps the original 0..1 scores, so it needs no conversion.
	switch cfg.RerankType {
	case config.RerankAikit:
		for i := range out {
			out[i].Score = sigmoid(out[i].Score)
		}
	case config.RerankLocalMaxSim:
		for i := range out {
			out[i].Score = clamp01(out[i].Score)
		}
	case config.RerankCrossEncoder:
		for i := range out {
			out[i].Score = clamp01(out[i].Score)
		}
	}

	// rankHits only labels ranks. Actually sort by the reranker scores, or a
	// reranker would change the numbers without changing which k hits reach the
	// prompt. llmRerank already returns the LLM's preferred order, so preserve
	// it instead of re-sorting on the untouched vector scores.
	if cfg.RerankType != config.RerankLLM {
		sortByScore(out)
	}
	if cfg.RerankTopN > 0 && cfg.RerankTopN < len(out) {
		out = out[:cfg.RerankTopN]
	}
	rankHits(out)
	return out, nil
}

// sigmoid maps an unbounded cross-encoder logit monotonically into (0, 1).
// It is computed in a numerically stable form for large negative values.
func sigmoid(x float64) float64 {
	if x >= 0 {
		return 1 / (1 + math.Exp(-x))
	}
	e := math.Exp(x)
	return e / (1 + e)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// aikitRerank runs a locally loaded cross-encoder model. An explicit
// cfg.RerankModelPath overrides the managed directory resolved from
// cfg.RerankModel; the model directory must contain config.json,
// model.safetensors and tokenizer files (for example
// cross-encoder/ms-marco-MiniLM-L-6-v2). Any failure is returned so
// rerankHits fails open to the original hit order.
func aikitRerank(ctx context.Context, query string, hits []index.Hit, cfg *config.Config) ([]index.Hit, error) {
	dir := strings.TrimSpace(cfg.RerankModelPath)
	if dir == "" {
		dir = config.ModelDir(cfg.RerankModel)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("retrieval: aikit rerank canceled: %w", err)
	}

	documents := make([]string, 0, len(hits))
	for _, h := range hits {
		text := h.Chunk.Body
		if text == "" {
			text = h.Chunk.Text
		}
		documents = append(documents, truncateRunes(text, 2000))
	}

	ce, err := loadAikitCrossEncoder(dir)
	if err != nil {
		return nil, fmt.Errorf("retrieval: load aikit cross-encoder: %w", err)
	}
	defer func() { _ = ce.Close() }()

	scores, err := ce.ScoreBatch(query, documents, 0)
	if err != nil {
		return nil, fmt.Errorf("retrieval: aikit score batch: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("retrieval: aikit rerank canceled: %w", err)
	}
	if len(scores) != len(hits) {
		return nil, fmt.Errorf("retrieval: aikit returned %d scores for %d hits", len(scores), len(hits))
	}

	out := make([]index.Hit, len(hits))
	copy(out, hits)
	for i := range out {
		out[i].Score = float64(scores[i])
	}
	return out, nil
}

// localMaxSimRerank reuses a token-capable local embedder. It scores each
// query token against every document token and averages the best match.
// No second model is loaded and no token vectors are written to the index.
func localMaxSimRerank(
	ctx context.Context,
	query string,
	hits []index.Hit,
	cfg *config.Config,
	emb embed.Embedder,
) ([]index.Hit, error) {
	if emb == nil {
		return nil, fmt.Errorf("retrieval: local_maxsim needs a token embedder")
	}
	te, ok := embed.TokenEmbedderOf(emb)
	if !ok {
		return nil, fmt.Errorf("retrieval: embedder %q has no token vector capability", cfg.Embedder)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	qTokens, err := te.EmbedTokensWithPrefix(ctx, query, "query: ")
	if err != nil {
		return nil, fmt.Errorf("retrieval: maxsim query tokens: %w", err)
	}
	qVecs := normalizedTokenVectors(qTokens)
	if len(qVecs) == 0 {
		return nil, fmt.Errorf("retrieval: maxsim query has no content tokens")
	}
	out := make([]index.Hit, len(hits))
	copy(out, hits)
	for i, h := range out {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := h.Chunk.Body
		if text == "" {
			text = h.Chunk.Text
		}
		dTokens, err := te.EmbedTokensWithPrefix(ctx, truncateRunes(text, 2000), "passage: ")
		if err != nil {
			return nil, fmt.Errorf("retrieval: maxsim document tokens: %w", err)
		}
		dVecs := normalizedTokenVectors(dTokens)
		if len(dVecs) == 0 {
			out[i].Score = 0
			continue
		}
		total := 0.0
		for _, q := range qVecs {
			best := -1.0
			for _, d := range dVecs {
				if score := dotF32(q, d); score > best {
					best = score
				}
			}
			total += best
		}
		cosine := total / float64(len(qVecs))
		out[i].Score = (cosine + 1) / 2
	}
	return out, nil
}

// normalizedTokenVectors drops XLM-R/e5 special ids and L2-normalizes rows.
func normalizedTokenVectors(tokens embed.TokenEmbeddings) [][]float32 {
	out := make([][]float32, 0, len(tokens.Vectors))
	for i, v := range tokens.Vectors {
		if i < len(tokens.IDs) && tokens.IDs[i] <= 2 {
			continue
		}
		var sum float32
		for _, x := range v {
			sum += x * x
		}
		if sum <= 0 {
			continue
		}
		norm := 1 / float32(math.Sqrt(float64(sum)))
		row := make([]float32, len(v))
		for j, x := range v {
			row[j] = x * norm
		}
		out = append(out, row)
	}
	return out
}

func dotF32(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(a[i] * b[i])
	}
	return sum
}

// crossEncoderRerank posts to an OpenAI-compatible /rerank endpoint. Expected
// response: {"results":[{"index":0,"relevance_score":0.93}, ...]}.
func crossEncoderRerank(ctx context.Context, query string, hits []index.Hit, cfg *config.Config) ([]index.Hit, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.RerankBaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("retrieval: rerank_base_url is empty")
	}

	documents := make([]map[string]string, 0, len(hits))
	for _, h := range hits {
		body := h.Chunk.Body
		if body == "" {
			body = h.Chunk.Text
		}
		documents = append(documents, map[string]string{"text": truncateRunes(body, 2000)})
	}
	topN := cfg.RerankTopN
	if topN <= 0 || topN > len(hits) {
		topN = len(hits)
	}
	payload := map[string]any{
		"model":     cfg.RerankModel,
		"query":     query,
		"documents": documents,
		"top_n":     topN,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("retrieval: encode rerank request: %w", err)
	}

	endpoint, err := url.JoinPath(base, "rerank")
	if err != nil {
		return nil, fmt.Errorf("retrieval: build rerank endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("retrieval: build rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.RerankAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.RerankAPIKey)
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retrieval: call rerank endpoint: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("retrieval: read rerank response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("retrieval: rerank endpoint returned %s", resp.Status)
	}

	var parsed struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("retrieval: decode rerank response: %w", err)
	}
	if len(parsed.Results) == 0 {
		return nil, fmt.Errorf("retrieval: rerank response has no results")
	}

	out := make([]index.Hit, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		if r.Index < 0 || r.Index >= len(hits) {
			continue
		}
		h := hits[r.Index]
		h.Score = r.RelevanceScore
		out = append(out, h)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("retrieval: rerank returned no valid indices")
	}
	return out, nil
}

// llmRerank asks the LLM for a comma-separated ordering of 1-based indices.
func llmRerank(ctx context.Context, query string, hits []index.Hit, complete CompleteFunc) ([]index.Hit, error) {
	if complete == nil {
		return nil, fmt.Errorf("retrieval: no completion function for llm rerank")
	}
	var b strings.Builder
	for i, h := range hits {
		body := h.Chunk.Body
		if body == "" {
			body = h.Chunk.Text
		}
		fmt.Fprintf(&b, "%d: %s\n", i+1, truncateRunes(body, 200))
	}
	system := "Rank the numbered passages by relevance to the question. " +
		"Output only a comma-separated list of 1-based indices, most relevant first."
	user := "Question: " + query + "\n\nPassages:\n" + b.String()
	out, err := callLLM(ctx, complete, system, user)
	if err != nil || out == "" {
		return nil, fmt.Errorf("retrieval: llm rerank failed: %v", err)
	}

	seen := make(map[int]bool)
	var ordered []index.Hit
	for _, token := range strings.FieldsFunc(out, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '[' || r == ']'
	}) {
		n, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || n < 1 || n > len(hits) || seen[n] {
			continue
		}
		seen[n] = true
		ordered = append(ordered, hits[n-1])
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("retrieval: llm rerank returned no valid indices")
	}
	for i, h := range hits {
		if !seen[i+1] {
			ordered = append(ordered, h)
		}
	}
	return ordered, nil
}
