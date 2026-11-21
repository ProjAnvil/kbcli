package retrieval

import (
	"sort"

	"github.com/projanvil/kbcli/internal/index"
)

// defaultRRFK is Porto's reciprocal-rank smoothing constant.
const defaultRRFK = 60

type fusedHit struct {
	hit   index.Hit
	score float64
}

// fuseRRF merges ranked lists with weighted reciprocal rank fusion. Porto
// amplifies the candidate pool before this step and uses 1/(k+rank) per list.
// Scores are normalized by the maximum into (0, 1].
func fuseRRF(rankings [][]index.Hit, weights []float64, k int) []index.Hit {
	if len(rankings) == 0 {
		return []index.Hit{}
	}
	if k <= 0 {
		k = defaultRRFK
	}
	if len(weights) != len(rankings) {
		weights = make([]float64, len(rankings))
		for i := range weights {
			weights[i] = 1
		}
	}

	byID := make(map[string]*fusedHit)
	var order []string
	for i, list := range rankings {
		w := weights[i]
		for j, h := range list {
			id := h.Chunk.ID
			rank := h.Rank
			if rank <= 0 {
				rank = j + 1
			}
			acc := byID[id]
			if acc == nil {
				acc = &fusedHit{hit: h}
				byID[id] = acc
				order = append(order, id)
			}
			acc.score += w / float64(k+rank)
		}
	}

	out := make([]fusedHit, 0, len(order))
	maxScore := 0.0
	for _, id := range order {
		acc := byID[id]
		if acc.score > maxScore {
			maxScore = acc.score
		}
		out = append(out, *acc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].hit.Chunk.ID < out[j].hit.Chunk.ID
		}
		return out[i].score > out[j].score
	})

	hits := make([]index.Hit, 0, len(out))
	for i, f := range out {
		score := 0.0
		if maxScore > 0 {
			score = f.score / maxScore
		}
		h := f.hit
		h.Rank = i + 1
		h.Score = score
		hits = append(hits, h)
	}
	return hits
}

// take returns at most n hits. A non-positive n means no truncation.
func take(hits []index.Hit, n int) []index.Hit {
	if n <= 0 || n >= len(hits) {
		return hits
	}
	return hits[:n]
}

// rankHits assigns a dense 1-based rank after any ordering change.
func rankHits(hits []index.Hit) {
	for i := range hits {
		hits[i].Rank = i + 1
	}
}
