package index

import (
	"fmt"
	"math"
	"sort"
)

// defaultTopK is used when callers pass k <= 0.
const defaultTopK = 6

// Search embeds the query and returns the k nearest chunks by L2 distance.
//
// Retrieval is a brute-force linear scan: every stored vector is compared to
// the query vector, so a query costs O(n*d) where n is the number of chunks
// and d the vector width. This is deliberately simple (no index-build step,
// exact results) and stays responsive up to roughly 200k chunks at 384 dims
// on a laptop. Beyond that an approximate-nearest-neighbour structure
// (HNSW/IVF) would be needed, at the cost of extra complexity and a recall
// trade-off. Following the Sentia convention, score = 1/(1+dist): higher is
// better. An empty query is a valid string and is embedded normally.
func (s *store) Search(query string, k int) ([]Hit, error) {
	if k <= 0 {
		k = defaultTopK
	}

	dim := s.emb.Dim()
	// Snapshot under a short read lock, then release it before calling the
	// embedder (which may be slow or do I/O). Vector backing arrays are
	// immutable, so a shallow struct copy is safe to read after unlocking.
	s.mu.RLock()
	snapshot := make([]Chunk, len(s.chunks))
	copy(snapshot, s.chunks)
	s.mu.RUnlock()

	if len(snapshot) == 0 {
		return []Hit{}, nil
	}

	qvec, err := s.emb.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("index: embed query: %w", err)
	}
	if len(qvec) != dim {
		return nil, fmt.Errorf("index: query vector has dim %d, want %d", len(qvec), dim)
	}

	type scored struct {
		chunk Chunk
		dist  float64
		score float64
	}
	candidates := make([]scored, 0, len(snapshot))
	for _, c := range snapshot {
		if len(c.Vector) != dim {
			continue // skip chunks with mismatched vector dims
		}
		var sum float64
		for j := 0; j < dim; j++ {
			d := float64(qvec[j]) - float64(c.Vector[j])
			sum += d * d
		}
		dist := math.Sqrt(sum)
		candidates = append(candidates, scored{
			chunk: c,
			dist:  dist,
			score: 1.0 / (1.0 + dist),
		})
	}

	// Higher score first; break ties by ID for deterministic ordering.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].chunk.ID < candidates[j].chunk.ID
	})

	if k > len(candidates) {
		k = len(candidates)
	}
	hits := make([]Hit, 0, k)
	for i := 0; i < k; i++ {
		c := candidates[i]
		hits = append(hits, Hit{
			Rank:  i + 1,
			Score: c.score,
			Dist:  c.dist,
			Chunk: cloneChunk(c.chunk), // defensive copy
		})
	}
	return hits, nil
}
