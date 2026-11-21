package agent

import (
	"sync"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// sink is a mutex-protected, defensive-copy evidence store. It keeps the
// latest hits published by the runner/tool so the TUI can read them while
// retrieval runs.
type sink struct {
	mu   sync.RWMutex
	hits []index.Hit
}

// newSink returns an empty Sink. The stored slice is always non-nil so callers
// can range over Get without a nil check.
func newSink() Sink {
	return &sink{hits: []index.Hit{}}
}

// Set stores a defensive copy of hits. Chunk holds reference fields (Vector,
// Metadata), so clone them too: the caller and the TUI must never share
// mutable backing data that a concurrent Set could race with.
func (s *sink) Set(hits []index.Hit) {
	cp := cloneHits(hits)
	s.mu.Lock()
	s.hits = cp
	s.mu.Unlock()
}

// Get returns a defensive copy of the stored hits.
func (s *sink) Get() []index.Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneHits(s.hits)
}

// cloneHits deep-copies the slice and each Chunk's reference fields so that
// neither the producer nor the consumer can mutate shared state.
func cloneHits(hits []index.Hit) []index.Hit {
	out := make([]index.Hit, len(hits))
	for i, h := range hits {
		c := h.Chunk
		if c.Vector != nil {
			c.Vector = append([]float32(nil), c.Vector...)
		}
		if c.Metadata != nil {
			m := make(map[string]string, len(c.Metadata))
			for k, v := range c.Metadata {
				m[k] = v
			}
			c.Metadata = m
		}
		h.Chunk = c
		out[i] = h
	}
	return out
}
