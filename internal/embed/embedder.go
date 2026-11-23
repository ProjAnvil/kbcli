// Package embed provides pluggable in-process text vectorization.
//
// The default local embedder is a deterministic hashing model that needs no
// model download. The rembed provider runs a purely Go neural encoder in the
// same process. Both emit L2-normalized 384-dimensional vectors, matching the
// Sentia backend so index files stay compatible with its conventions.
package embed

import "context"

const (
	// Dim is the fixed vector width of every provider.
	Dim = 384
	// ProviderLocal selects the zero-dependency hashing embedder.
	ProviderLocal = "local"
	// ProviderRembed selects the in-process neural embedder.
	ProviderRembed = "rembed"
	// LocalVersion is the retrieval-domain key for hashing vectors.
	LocalVersion = "local-v1"
	// DefaultRembedRef is the model used when no reference is configured.
	DefaultRembedRef = "intfloat/multilingual-e5-small"
)

// Embedder turns text into a fixed-width vector. Implementations must be safe
// for concurrent use because indexing embeds chunks in parallel.
type Embedder interface {
	// Dim returns the vector width.
	Dim() int
	// Version identifies the retrieval domain. Vectors from different
	// versions must never be compared with each other.
	Version() string
	// Embed encodes one string.
	Embed(text string) ([]float32, error)
	// EmbedBatch encodes several strings, preserving input order.
	EmbedBatch(texts []string) ([][]float32, error)
	// Close releases model resources. It is a no-op for the local provider.
	Close() error
}

// TokenEmbeddings holds one text's token ids and final hidden states.
// Local rerankers use these for token-level MaxSim scoring.
type TokenEmbeddings struct {
	IDs     []int64
	Vectors [][]float32
}

// TokenEmbedder is an optional capability for local token-level reranking.
type TokenEmbedder interface {
	EmbedTokensWithPrefix(ctx context.Context, text, prefix string) (TokenEmbeddings, error)
}

// TokenEmbedderOf reports whether e supports token-level reranking.
func TokenEmbedderOf(e Embedder) (TokenEmbedder, bool) {
	te, ok := e.(TokenEmbedder)
	return te, ok
}

// TokenCounter is an optional capability of an Embedder that can count the
// exact tokenized length of the text that will be embedded, plus the model's
// real per-input token limit. Embedders that do not implement it (such as the
// local hashing embedder) keep their existing rune-based chunking unchanged.
type TokenCounter interface {
	// CountForEmbedding returns the token count of text as it will actually
	// be embedded, including any internal prefix the provider prepends.
	CountForEmbedding(text string) int
	// MaxTokens is the maximum number of input tokens the model accepts.
	MaxTokens() int
}

// TokenCounterOf reports whether e implements TokenCounter.
func TokenCounterOf(e Embedder) (TokenCounter, bool) {
	tc, ok := e.(TokenCounter)
	return tc, ok
}

// New builds an embedder for the named provider. modelRef is ignored by the
// local provider. Implemented in provider.go.
func New(provider, modelRef string) (Embedder, error) {
	return newForProvider(provider, modelRef)
}
