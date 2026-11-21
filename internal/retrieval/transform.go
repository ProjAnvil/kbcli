package retrieval

import (
	"context"
	"strconv"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// callLLM runs one completion and trims the result. Every caller treats an
// error or empty output as a signal to fail open.
func callLLM(ctx context.Context, complete CompleteFunc, system, user string) (string, error) {
	if complete == nil {
		return "", nil
	}
	out, err := complete(ctx, system, user)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// parseLines extracts clean, numbered-free lines from an LLM list response.
func parseLines(s string) []string {
	var out []string
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
			if i := strings.IndexAny(line, ".):"); i >= 0 && i <= 3 {
				line = strings.TrimSpace(line[i+1:])
			}
		}
		line = strings.Trim(line, "\"'` ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// mergeDedupe preserves first-seen order across result lists (Porto's
// decomposition merge) and reassigns ranks.
func mergeDedupe(lists [][]index.Hit) []index.Hit {
	seen := make(map[string]bool)
	var out []index.Hit
	for _, list := range lists {
		for _, h := range list {
			if seen[h.Chunk.ID] {
				continue
			}
			seen[h.Chunk.ID] = true
			out = append(out, h)
		}
	}
	rankHits(out)
	return out
}

// transformedSearch implements Porto's B4/B5 query transforms. Every LLM
// failure falls back to a single rawSearch on the original query.
func transformedSearch(
	ctx context.Context,
	query string,
	history []Turn,
	cfg *config.Config,
	complete CompleteFunc,
	rawSearch func(context.Context, string) ([]index.Hit, error),
) ([]index.Hit, error) {
	fallback := func() ([]index.Hit, error) {
		hits, err := rawSearch(ctx, query)
		if err != nil {
			return nil, err
		}
		return nonNil(hits), nil
	}
	if cfg == nil || rawSearch == nil {
		return []index.Hit{}, nil
	}

	// Anaphora resolution already ran in retriever.search, which passes the
	// standalone query here. Keeping it out of this function avoids a second
	// LLM call on every question.
	q := query

	transform := cfg.QueryTransform
	if transform == "" {
		transform = config.TransformNone
	}
	if transform == config.TransformNone || complete == nil {
		hits, err := rawSearch(ctx, q)
		if err != nil {
			return fallback()
		}
		return nonNil(hits), nil
	}

	switch transform {
	case config.TransformHyDE:
		system := "Write a short passage that would answer the question. " +
			"Use a factual, document-like style. Output only the passage."
		doc, err := callLLM(ctx, complete, system, q)
		if err != nil || doc == "" {
			return fallback()
		}
		hits, err := rawSearch(ctx, doc)
		if err != nil || len(hits) == 0 {
			return fallback()
		}
		return hits, nil

	case config.TransformMultiQuery:
		system := "Rewrite the question into exactly " + strconv.Itoa(cfg.MultiQueryCount) +
			" alternative phrasings. Output one per line, no numbering, no explanation."
		out, err := callLLM(ctx, complete, system, q)
		variants := parseLines(out)
		if err != nil || len(variants) == 0 {
			return fallback()
		}
		if len(variants) > cfg.MultiQueryCount {
			variants = variants[:cfg.MultiQueryCount]
		}
		queries := append([]string{q}, variants...)
		lists := make([][]index.Hit, 0, len(queries))
		weights := make([]float64, 0, len(queries))
		for _, v := range queries {
			hits, err := rawSearch(ctx, v)
			if err != nil {
				return fallback()
			}
			lists = append(lists, hits)
			weights = append(weights, 1)
		}
		return fuseRRF(lists, weights, cfg.RRFK), nil

	case config.TransformDecomposition:
		system := "Break the question into short sub-questions. " +
			"Output one per line, no numbering, no explanation."
		out, err := callLLM(ctx, complete, system, q)
		subs := parseLines(out)
		if err != nil || len(subs) == 0 {
			return fallback()
		}
		queries := append([]string{q}, subs...)
		var lists [][]index.Hit
		for _, v := range queries {
			hits, err := rawSearch(ctx, v)
			if err != nil {
				return fallback()
			}
			lists = append(lists, hits)
		}
		return mergeDedupe(lists), nil

	case config.TransformStepBack:
		system := "Write one more general, abstract question that would help " +
			"answer the given question. Output only that question."
		abstract, err := callLLM(ctx, complete, system, q)
		if err != nil || abstract == "" {
			return fallback()
		}
		first, err := rawSearch(ctx, abstract)
		if err != nil {
			return fallback()
		}
		second, err := rawSearch(ctx, q)
		if err != nil {
			return fallback()
		}
		return fuseRRF([][]index.Hit{first, second}, []float64{0.5, 0.5}, cfg.RRFK), nil

	default:
		return fallback()
	}
}

func nonNil(hits []index.Hit) []index.Hit {
	if hits == nil {
		return []index.Hit{}
	}
	return hits
}
