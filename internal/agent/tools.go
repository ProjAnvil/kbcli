package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/retrieval"
	coretools "github.com/projanvil/langchain-golang/core/tools"
)

const (
	// maxEvidenceRunes bounds each snippet shown to the model.
	maxEvidenceRunes = 400
	toolName         = "kb_search"
	toolDescription  = "Search the local knowledge base for passages relevant to a query. Input is a plain-text query."
)

// newSearchTool builds the kb_search tool. Each call runs a fresh retrieval
// through the optimized retriever, publishes the hits to the Sink (so the TUI
// shows the latest evidence), and returns the formatted snippets as the tool
// result.
func newSearchTool(cfg *config.Config, ret retrieval.Retriever, sink Sink) (coretools.Tool, error) {
	fn := func(ctx context.Context, query string) (coretools.Result, error) {
		hits, err := ret.Search(ctx, query, cfg.TopK)
		if err != nil {
			return coretools.Result{}, fmt.Errorf("agent: kb_search %q: %w", query, err)
		}
		sink.Set(hits)
		return coretools.Result{Content: formatEvidence(hits, len(hits))}, nil
	}
	t, err := coretools.NewSimple(toolName, toolDescription, fn)
	if err != nil {
		return nil, fmt.Errorf("agent: build %s tool: %w", toolName, err)
	}
	return t, nil
}

// formatEvidence renders up to limit hits as numbered, score-tagged snippets.
// An empty result yields a stable sentinel string.
func formatEvidence(hits []index.Hit, limit int) string {
	if len(hits) == 0 {
		return "(no matching evidence)"
	}
	if limit > len(hits) {
		limit = len(hits)
	}
	var b strings.Builder
	for i := 0; i < limit; i++ {
		h := hits[i]
		fmt.Fprintf(&b, "[%d] %s (score %.3f)\n", i+1, h.Chunk.Source, h.Score)
		b.WriteString(truncateRunes(evidenceBody(h), maxEvidenceRunes))
		b.WriteString("\n")
	}
	return b.String()
}

// evidenceBody prefers the display body, falling back to the embedded text
// when a store only populated Text.
func evidenceBody(h index.Hit) string {
	if h.Chunk.Body != "" {
		return h.Chunk.Body
	}
	return h.Chunk.Text
}

// truncateRunes cuts s to at most limit runes (not bytes), so multi-byte text
// is never split mid-character.
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
