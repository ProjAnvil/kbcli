package agent

import (
	"context"
	"fmt"
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

// newSearchTool builds the kb_search tool with its own citation ledger. It is
// kept as a thin wrapper so callers that do not share an answer's ledger (for
// example unit tests) keep the original three-argument constructor.
func newSearchTool(cfg *config.Config, ret retrieval.Retriever, sink Sink) (coretools.Tool, error) {
	return newSearchToolWithLedger(cfg, ret, sink, newCitationLedger())
}

// newSearchToolWithLedger builds a kb_search tool that shares the runner's
// citation ledger. Every call runs a fresh retrieval, registers the new hits
// under stable [n] numbers, publishes the FULL ordered evidence list to the
// Sink (so the TUI never shows a different ranking than the model cites), and
// returns the formatted snippets as the tool result.
func newSearchToolWithLedger(cfg *config.Config, ret retrieval.Retriever, sink Sink, ledger *citationLedger) (coretools.Tool, error) {
	if ledger == nil {
		ledger = newCitationLedger()
	}
	fn := func(ctx context.Context, query string) (coretools.Result, error) {
		hits, err := ret.Search(ctx, query, cfg.TopK)
		if err != nil {
			return coretools.Result{}, fmt.Errorf("agent: kb_search %q: %w", query, err)
		}
		numbers, all := ledger.Register(hits)
		sink.Set(all)
		if len(hits) == 0 {
			return coretools.Result{Content: "(no matching evidence)"}, nil
		}
		return coretools.Result{Content: retrieval.FormatCitations(hits, numbers, maxEvidenceRunes)}, nil
	}
	t, err := coretools.NewSimple(toolName, toolDescription, fn)
	if err != nil {
		return nil, fmt.Errorf("agent: build %s tool: %w", toolName, err)
	}
	return t, nil
}

// formatEvidence renders up to limit hits with the SAME numbering format as
// the grounded prompt. An empty result yields a stable sentinel string.
func formatEvidence(hits []index.Hit, limit int) string {
	if len(hits) == 0 {
		return "(no matching evidence)"
	}
	if limit > len(hits) {
		limit = len(hits)
	}
	return retrieval.FormatCitations(hits[:limit], sequentialNumbers(limit), maxEvidenceRunes)
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
