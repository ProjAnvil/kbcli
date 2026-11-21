package embed

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// localEmbedder is the zero-dependency default: a deterministic hashing model
// that blends word unigrams with character 3-grams and L2-normalizes the
// result. It needs no model download. The behaviour is ported verbatim from
// the Sentia backend so index files stay compatible with its conventions.
//
// The type holds no state and every call allocates its own vector, so it is
// safe for concurrent use.
type localEmbedder struct{}

var _ Embedder = (*localEmbedder)(nil)

func newLocal() *localEmbedder { return &localEmbedder{} }

func (e *localEmbedder) Dim() int { return Dim }

// Version returns the retrieval-domain key for hashing vectors.
func (e *localEmbedder) Version() string { return LocalVersion }

// Embed encodes text into a fixed-width, L2-normalized vector.
func (e *localEmbedder) Embed(text string) ([]float32, error) {
	v := make([]float32, Dim)
	text = strings.ToLower(text)

	// Word unigrams: split on ASCII punctuation/whitespace. ASCII letters and
	// digits stay inside tokens; non-ASCII runes are kept too (the predicate
	// only splits on ASCII separators). Each token is weighted 1.0.
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r < 128
	})
	for _, w := range words {
		addHash(v, "w:"+w, 1.0)
	}

	// Character 3-grams over runes add literal, typo-tolerant signal. Each is
	// weighted 0.4 so word matches dominate but literal overlap still counts.
	runes := []rune(text)
	for i := 0; i+3 <= len(runes); i++ {
		addHash(v, "c:"+string(runes[i:i+3]), 0.4)
	}

	l2Normalize(v)
	return v, nil
}

// EmbedBatch encodes each text via Embed, preserving input order.
func (e *localEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, fmt.Errorf("embed: local text %d: %w", i, err)
		}
		out[i] = vec
	}
	return out, nil
}

// Close is a no-op: the hashing embedder holds no resources.
func (e *localEmbedder) Close() error { return nil }

// addHash accumulates a weighted feature into v at a bucket chosen by a
// 64-bit FNV-1a hash of the key.
func addHash(v []float32, key string, weight float64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	idx := int(h.Sum64() % uint64(len(v)))
	v[idx] += float32(weight)
}

// l2Normalize scales v to unit length, leaving an all-zero vector unchanged.
func l2Normalize(v []float32) {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
}
