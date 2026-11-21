// Package retrieval ports the Porto RAG retrieval optimizations to Go:
// BM25 sparse search with CJK bigram tokenization, weighted RRF fusion,
// query transforms, reranking, and context budget trimming.
package retrieval

import (
	"context"

	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/index"
)

// CompleteFunc is one LLM completion. A nil CompleteFunc disables every
// LLM-backed optimization and the pipeline fails open to plain retrieval.
type CompleteFunc func(ctx context.Context, system, user string) (string, error)

// Options configures a Retriever.
type Options struct {
	Config   *config.Config
	Store    index.Store
	Complete CompleteFunc
}

// CondenseMeta describes one anaphora-resolution step. Method is "llm",
// "rule" or empty when no rewrite happened.
type CondenseMeta struct {
	Applied   bool
	Original  string
	Rewritten string
	Method    string
}

// CondenseReporter is an optional Retriever extension. The agent uses it to
// surface the rewrite in the TUI without changing the Retriever interface.
type CondenseReporter interface {
	LastCondense() CondenseMeta
}

// Turn is one chat message used by the condense query transform.
type Turn struct {
	Role    string
	Content string
}

// Retriever returns ranked evidence for a query.
type Retriever interface {
	// Search runs the full optimization pipeline for a standalone query.
	Search(ctx context.Context, query string, k int) ([]index.Hit, error)
	// SearchWithHistory also enables the condense transform, which resolves
	// pronouns from recent turns. Role is "user" or "assistant".
	SearchWithHistory(ctx context.Context, query string, k int, history []Turn) ([]index.Hit, error)
}

// New builds a retriever. Implemented in retriever.go.
func New(opts Options) (Retriever, error) {
	return newRetriever(opts)
}

// Tokenize splits text into lowercase word tokens plus, for CJK runs, single
// characters and adjacent bigrams. Implemented in bm25.go.
func Tokenize(text string) []string {
	return tokenize(text)
}

// FuseRRF merges ranked lists with weighted reciprocal rank fusion. Rankings
// and weights must have the same length. Implemented in rrf.go.
func FuseRRF(rankings [][]index.Hit, weights []float64, k int) []index.Hit {
	return fuseRRF(rankings, weights, k)
}

// TrimToBudget drops and truncates parts from the end until the joined text
// fits budget runes. Implemented in context.go.
func TrimToBudget(parts []string, budget int) []string {
	return trimToBudget(parts, budget)
}

// FormatSources renders hits as numbered citations for the agent prompt.
// Implemented in context.go.
func FormatSources(hits []index.Hit, perChunk int) string {
	return formatSources(hits, perChunk)
}
