package embed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	model   *rembed.Embedder
	version string
}

var _ Embedder = (*rembedEmbedder)(nil)

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
	return &rembedEmbedder{model: m, version: rembedVersion(ref)}, nil
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

// rembedVersion builds the retrieval-domain key for a neural model ref.
func rembedVersion(ref string) string { return "rembed:" + ref }
