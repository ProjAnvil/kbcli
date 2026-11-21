package embed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rostamlabs/rembed"
)

// queryPrefix is the e5-family instruction prefix. Queries and documents are
// embedded through the same method, so the symmetric "query: " prefix is used
// on both sides (valid per e5 usage notes and matches Sentia).
const queryPrefix = "query: "

// rembedEmbedder adapts rembed's in-process pure-Go neural encoder to the
// Embedder contract. rembed.Embedder is documented safe for concurrent use and
// this wrapper adds no mutable state, so it is safe for concurrent use too.
type rembedEmbedder struct {
	model     *rembed.Embedder
	version   string
	maxTokens int
}

var (
	_ Embedder     = (*rembedEmbedder)(nil)
	_ TokenCounter = (*rembedEmbedder)(nil)
)

// newRembed loads the neural model identified by ref. Nothing is downloaded at
// package init; rembed.Load may fetch weights from the Hugging Face Hub on the
// first explicit New(rembed, ...) call.
//
// Cache layout: when neither REMBED_CACHE nor HF_HOME is set, REMBED_CACHE is
// set to <user cache dir>/rembed before Load so downloaded weights are reused
// across runs. This mirrors rembed's own default but is set explicitly via the
// environment so the location is predictable for operators. If either variable
// is already configured, it is left untouched.
func newRembed(ref string) (*rembedEmbedder, error) {
	if os.Getenv("REMBED_CACHE") == "" && os.Getenv("HF_HOME") == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("embed: resolve user cache dir for rembed: %w", err)
		}
		if err := os.Setenv("REMBED_CACHE", filepath.Join(cache, "rembed")); err != nil {
			return nil, fmt.Errorf("embed: set REMBED_CACHE: %w", err)
		}
	}

	m, err := rembed.Load(ref)
	if err != nil {
		return nil, fmt.Errorf("embed: load rembed model %q: %w", ref, err)
	}
	if got := m.Dim(); got != Dim {
		_ = m.Close()
		return nil, fmt.Errorf("embed: rembed model %q has dimension %d, want %d", ref, got, Dim)
	}
	// Probe the model's real input limit once. rembed.Tokenize truncates at the
	// model's MaxSeqLen, so a long input saturates at the exact limit. This
	// avoids importing rembed internal constants and works for custom models.
	// rembed.Tokenize is documented as not a stable part of the embedding API;
	// all use of it in kbcli is isolated to this file.
	maxTokens := len(m.Tokenize(strings.Repeat("a ", 2048)))
	if maxTokens <= 0 {
		_ = m.Close()
		return nil, fmt.Errorf("embed: rembed model %q reported no token limit", ref)
	}
	return &rembedEmbedder{model: m, version: rembedVersion(ref), maxTokens: maxTokens}, nil
}

func (e *rembedEmbedder) Dim() int { return Dim }

// Version identifies the retrieval domain as "rembed:" plus the model ref. It
// is derived from the ref alone so it is stable and deterministic.
func (e *rembedEmbedder) Version() string { return e.version }

// Embed encodes one text by delegating to EmbedBatch and requiring exactly one
// vector back.
func (e *rembedEmbedder) Embed(text string) ([]float32, error) {
	vecs, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed: rembed returned %d vectors, want 1", len(vecs))
	}
	return vecs[0], nil
}

// EmbedBatch encodes all texts in a single rembed.Embed call, prefixing each
// with queryPrefix and preserving input order.
func (e *rembedEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, text := range texts {
		prefixed[i] = queryPrefix + text
	}
	vecs, err := e.model.Embed(context.Background(), prefixed)
	if err != nil {
		return nil, fmt.Errorf("embed: rembed batch of %d texts: %w", len(texts), err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("embed: rembed returned %d vectors for %d texts", len(vecs), len(texts))
	}
	return vecs, nil
}

func (e *rembedEmbedder) Close() error { return e.model.Close() }

// CountForEmbedding counts the exact text that will be embedded: rembed's
// EmbedBatch prepends queryPrefix to every text, so it is included here.
// rembed.Tokenize is unstable; this is the only caller in kbcli.
func (e *rembedEmbedder) CountForEmbedding(text string) int {
	return len(e.model.Tokenize(queryPrefix + text))
}

// MaxTokens returns the cached per-input limit probed at load time.
func (e *rembedEmbedder) MaxTokens() int { return e.maxTokens }

// rembedVersion builds the retrieval-domain key for a neural model ref.
func rembedVersion(ref string) string { return "rembed:" + ref }
