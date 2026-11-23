package embed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rostamlabs/rembed"
)

const (
	// queryPrefix and documentPrefix implement the e5 asymmetric contract.
	// rembed embeds exactly the text it receives, so this package applies both
	// prefixes before the call. Never use one prefix for both sides.
	queryPrefix    = "query: "
	documentPrefix = "passage: "

	// RembedDomainVersion marks the neural embedding-space contract. Bump it
	// whenever prefixes, quantization or pooling change. loadStore rejects an
	// index whose EmbedderVersion differs, so a bump forces one re-index
	// instead of silently comparing incompatible vectors.
	RembedDomainVersion = "v2"
)

var (
	_ Embedder      = (*rembedEmbedder)(nil)
	_ TokenEmbedder = (*rembedEmbedder)(nil)
	_ TokenCounter  = (*rembedEmbedder)(nil)
	_ QueryPrefixer = (*rembedEmbedder)(nil)
)

// rembedEmbedder adapts rembed's in-process pure-Go neural encoder to the
// Embedder contract. rembed.Embedder is documented safe for concurrent use and
// this wrapper adds no mutable state, so it is safe for concurrent use too.
//
// Callers must go through EmbedQuery and EmbedDocuments. Calling Embed or
// EmbedBatch directly bypasses the e5 prefixes and produces unusable vectors.
type rembedEmbedder struct {
	model     *rembed.Embedder
	version   string
	maxTokens int
}

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

	// Weight-only int8 cuts weight traffic about 4x with cosine >= 0.999
	// against fp32. rembed falls back to fp32 on CPUs without AVX2+FMA.
	m, err := rembed.Load(ref, rembed.WithInt8())
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

// Version identifies the retrieval domain as the model ref plus the domain
// version. It is derived from the ref alone so it is stable and deterministic.
func (e *rembedEmbedder) Version() string { return e.version }

// QueryPrefix returns the e5 query-side instruction.
func (e *rembedEmbedder) QueryPrefix() string { return queryPrefix }

// DocumentPrefix returns the e5 passage-side instruction.
func (e *rembedEmbedder) DocumentPrefix() string { return documentPrefix }

// Embed encodes one text by delegating to EmbedBatch and requiring exactly one
// vector back. The caller supplies any required prefix.
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

// EmbedBatch encodes all texts in a single rembed.Embed call, preserving input
// order. Prefixes are applied by EmbedDocuments and EmbedQuery, never here.
func (e *rembedEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vecs, err := e.model.Embed(context.Background(), texts)
	if err != nil {
		return nil, fmt.Errorf("embed: rembed batch of %d texts: %w", len(texts), err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("embed: rembed returned %d vectors for %d texts", len(vecs), len(texts))
	}
	return vecs, nil
}

// EmbedTokensWithPrefix exposes raw token vectors for local MaxSim reranking.
// Prefixes must be applied here: rembed embeds exactly the text it receives.
func (e *rembedEmbedder) EmbedTokensWithPrefix(ctx context.Context, text, prefix string) (TokenEmbeddings, error) {
	out, err := e.model.EmbedTokens(ctx, []string{prefix + text})
	if err != nil {
		return TokenEmbeddings{}, fmt.Errorf("embed: token batch of 1 text: %w", err)
	}
	if len(out) != 1 {
		return TokenEmbeddings{}, fmt.Errorf("embed: rembed returned %d token results, want 1", len(out))
	}
	return TokenEmbeddings{IDs: out[0].IDs, Vectors: out[0].Vectors}, nil
}

func (e *rembedEmbedder) Close() error { return e.model.Close() }

// CountForEmbedding counts the exact text that will be embedded: the document
// prefix is prepended by EmbedDocuments, so it is included here.
// rembed.Tokenize is unstable; this is the only caller in kbcli.
func (e *rembedEmbedder) CountForEmbedding(text string) int {
	return len(e.model.Tokenize(documentPrefix + text))
}

// MaxTokens returns the cached per-input limit probed at load time.
func (e *rembedEmbedder) MaxTokens() int { return e.maxTokens }

// rembedVersion builds the retrieval-domain key for a neural model ref. The
// domain version makes a prefix or quantization change invalidate old indexes.
func rembedVersion(ref string) string {
	return "rembed:" + ref + "#" + RembedDomainVersion
}
