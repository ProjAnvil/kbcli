package embed

import (
	"math"
	"reflect"
	"sync"
	"testing"
)

// l2 returns the Euclidean norm of a vector, used to assert normalization.
func l2(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

func TestLocalEmbedDeterminism(t *testing.T) {
	e := newLocal()
	first, err := e.Embed("The Quick Brown Fox")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	second, err := e.Embed("The Quick Brown Fox")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("Embed is not deterministic for identical input")
	}
}

// The embedder lowercases text, so casing must not change the vector.
func TestLocalEmbedCaseInsensitive(t *testing.T) {
	e := newLocal()
	upper, err := e.Embed("Hello WORLD 42")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	lower, err := e.Embed("hello world 42")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if !reflect.DeepEqual(upper, lower) {
		t.Fatal("Embed should be case-insensitive (text is lowercased)")
	}
}

func TestLocalEmbedVectorLength(t *testing.T) {
	cases := []string{
		"short",
		"a longer sentence with several distinct words",
		"",
		"mixed ASCII and 世界 unicode",
	}
	e := newLocal()
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			v, err := e.Embed(text)
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if len(v) != Dim {
				t.Errorf("len = %d, want %d", len(v), Dim)
			}
		})
	}
}

func TestLocalEmbedL2Norm(t *testing.T) {
	cases := []string{
		"short",
		"a longer sentence with several distinct words",
		"mixed ASCII and 世界 unicode",
	}
	const tolerance = 1e-4
	e := newLocal()
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			v, err := e.Embed(text)
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if norm := l2(v); math.Abs(norm-1) > tolerance {
				t.Errorf("L2 norm = %f, want ~1 (tolerance %g)", norm, tolerance)
			}
		})
	}
}

func TestLocalEmbedEmptyString(t *testing.T) {
	e := newLocal()
	v, err := e.Embed("")
	if err != nil {
		t.Fatalf("Embed(\"\"): %v", err)
	}
	if len(v) != Dim {
		t.Fatalf("len = %d, want %d", len(v), Dim)
	}
	// No features are produced, so normalization leaves an all-zero vector.
	if norm := l2(v); norm != 0 {
		t.Errorf("empty-string norm = %f, want 0", norm)
	}
}

func TestLocalEmbedDistinctTexts(t *testing.T) {
	e := newLocal()
	a, err := e.Embed("the quick brown fox jumps over the lazy dog")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	b, err := e.Embed("completely unrelated content about database indexing and vector search")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if reflect.DeepEqual(a, b) {
		t.Fatal("distinct texts must produce distinct vectors")
	}
}

func TestLocalEmbedBatchOrderAndLength(t *testing.T) {
	texts := []string{"alpha", "beta", "gamma", "", "hello world"}
	e := newLocal()
	vecs, err := e.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("len(EmbedBatch) = %d, want %d", len(vecs), len(texts))
	}
	for i, text := range texts {
		want, err := e.Embed(text)
		if err != nil {
			t.Fatalf("Embed(%q): %v", text, err)
		}
		if !reflect.DeepEqual(vecs[i], want) {
			t.Errorf("batch[%d] does not match individual Embed(%q): order not preserved", i, text)
		}
	}
}

func TestLocalEmbedBatchEmpty(t *testing.T) {
	e := newLocal()
	vecs, err := e.EmbedBatch(nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil): %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("len = %d, want 0", len(vecs))
	}
}

func TestLocalClose(t *testing.T) {
	if err := newLocal().Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// The local hashing embedder has no model token limit, so it must not satisfy
// TokenCounter and must keep the existing rune-based chunking behaviour.
func TestLocalEmbedderIsNotTokenCounter(t *testing.T) {
	if tc, ok := TokenCounterOf(newLocal()); ok {
		t.Fatalf("local embedder unexpectedly implements TokenCounter: %v", tc)
	}
}

// The embedder must be safe for concurrent use; results must match serial
// embedding regardless of how calls interleave.
func TestLocalEmbedConcurrent(t *testing.T) {
	e := newLocal()
	texts := []string{"alpha", "beta", "gamma", "delta"}
	want := make([][]float32, len(texts))
	for i, text := range texts {
		var err error
		want[i], err = e.Embed(text)
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan string, 64)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j, text := range texts {
				got, err := e.Embed(text)
				if err != nil {
					errCh <- err.Error()
					return
				}
				if !reflect.DeepEqual(got, want[j]) {
					errCh <- "concurrent Embed result differs from serial result"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
}
