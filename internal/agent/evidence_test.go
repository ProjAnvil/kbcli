package agent

import (
	"sync"
	"testing"
)

func TestSinkEmptyIsNonNil(t *testing.T) {
	s := newSink()
	got := s.Get()
	if got == nil {
		t.Fatal("Get on a fresh sink must return a non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("fresh sink len = %d, want 0", len(got))
	}
}

func TestSinkDefensiveCopy(t *testing.T) {
	s := newSink()
	original := makeHits(2)
	s.Set(original)

	// Mutating the caller's slice (including Chunk reference fields) must not
	// affect the stored hits.
	original[0].Chunk.Source = "mutated.md"
	original[0].Chunk.Vector[0] = 999
	original[0].Chunk.Metadata["k"] = "mutated"
	got := s.Get()
	if got[0].Chunk.Source != "doc1.md" {
		t.Fatalf("stored source = %q, want doc1.md", got[0].Chunk.Source)
	}
	if got[0].Chunk.Vector[0] != 0 {
		t.Fatalf("stored vector = %v, want 0", got[0].Chunk.Vector[0])
	}
	if got[0].Chunk.Metadata["k"] != "v" {
		t.Fatalf("stored metadata = %q, want v", got[0].Chunk.Metadata["k"])
	}

	// Mutating a Get result must not affect the sink on a subsequent read.
	got[0].Chunk.Source = "changed.md"
	got[0].Chunk.Vector[0] = 123
	again := s.Get()
	if again[0].Chunk.Source != "doc1.md" {
		t.Fatalf("sink mutated through Get result: %q", again[0].Chunk.Source)
	}
	if again[0].Chunk.Vector[0] != 0 {
		t.Fatalf("sink vector mutated through Get result: %v", again[0].Chunk.Vector[0])
	}
}

// TestSinkConcurrent hammers Set/Get from many goroutines. It is designed to
// be clean under `go test -race`.
func TestSinkConcurrent(t *testing.T) {
	s := newSink()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			s.Set(makeHits(n%5 + 1))
		}(i)
		go func() {
			defer wg.Done()
			for _, hit := range s.Get() {
				_ = hit.Chunk.Source
				_ = hit.Score
			}
		}()
	}
	wg.Wait()

	if got := s.Get(); got == nil {
		t.Fatal("Get returned nil after concurrent access")
	}
}
