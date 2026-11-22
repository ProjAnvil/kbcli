package retrieval

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

type retriever struct {
	cfg      *config.Config
	store    index.Store
	complete CompleteFunc

	mu     sync.Mutex
	idx    *bm25Index
	metaMu sync.RWMutex
	meta   CondenseMeta
}

// LastCondense reports the anaphora-resolution result of the last search.
func (r *retriever) LastCondense() CondenseMeta {
	r.metaMu.RLock()
	defer r.metaMu.RUnlock()
	return r.meta
}

func newRetriever(opts Options) (Retriever, error) {
	if opts.Config == nil {
		return nil, errors.New("retrieval: config is nil")
	}
	if opts.Store == nil {
		return nil, errors.New("retrieval: store is nil")
	}
	cfg := opts.Config.Clone()
	cfg.Normalize()
	return &retriever{cfg: cfg, store: opts.Store, complete: opts.Complete}, nil
}

// ensureBM25 rebuilds the sparse index when the vector store revision changes.
func (r *retriever) ensureBM25() *bm25Index {
	rev := r.store.Revision()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idx != nil && r.idx.rev == rev {
		return r.idx
	}
	r.idx = newBM25Index(r.store.Chunks(), r.cfg.BM25K1, r.cfg.BM25B, rev)
	return r.idx
}

func (r *retriever) Search(ctx context.Context, query string, k int) ([]index.Hit, error) {
	return r.search(ctx, query, k, nil, false)
}

// SearchQuick implements the quick_rag route: verbatim query, no condense and
// no query transform. Scoring, reranking and context expansion still apply.
func (r *retriever) SearchQuick(ctx context.Context, query string, k int) ([]index.Hit, error) {
	return r.search(ctx, query, k, nil, true)
}

func (r *retriever) SearchWithHistory(ctx context.Context, query string, k int, history []Turn) ([]index.Hit, error) {
	return r.search(ctx, query, k, history, false)
}

func (r *retriever) search(ctx context.Context, query string, k int, history []Turn, quick bool) ([]index.Hit, error) {
	if k <= 0 {
		k = r.cfg.TopK
	}
	if k <= 0 {
		k = 6
	}
	candK := k
	if r.cfg.RetrievalMethod == config.RetrievalHybrid && r.cfg.BM25TopK > candK {
		candK = r.cfg.BM25TopK
	}

	rawSearch := func(ctx context.Context, q string) ([]index.Hit, error) {
		switch r.cfg.RetrievalMethod {
		case config.RetrievalBM25:
			return r.ensureBM25().search(q, candK), nil
		case config.RetrievalHybrid:
			vectorHits, err := r.store.Search(q, candK)
			if err != nil {
				return nil, fmt.Errorf("retrieval: vector search: %w", err)
			}
			sparseHits := r.ensureBM25().search(q, candK)
			w := r.cfg.HybridVectorWeight
			fused := fuseRRF(
				[][]index.Hit{vectorHits, sparseHits},
				[]float64{w, 1 - w},
				r.cfg.RRFK,
			)
			return take(fused, candK), nil
		default:
			hits, err := r.store.Search(q, candK)
			if err != nil {
				return nil, fmt.Errorf("retrieval: vector search: %w", err)
			}
			return hits, nil
		}
	}

	// Porto condense gate: resolve pronouns and ellipsis before any retrieval.
	// A nil LLM falls back to a rule-based anchor rewrite, so anaphora handling
	// also works offline. The quick_rag route skips this gate entirely.
	q := query
	if !quick {
		var meta CondenseMeta
		q, meta = resolveQuery(ctx, query, history, r.cfg, r.complete)
		r.metaMu.Lock()
		r.meta = meta
		r.metaMu.Unlock()
	}

	transformCfg := r.cfg.Clone()
	transformCfg.CondenseEnabled = false
	if quick {
		transformCfg.QueryTransform = config.TransformNone
	}
	hits, err := transformedSearch(ctx, q, history, transformCfg, r.complete, rawSearch)
	if err != nil || hits == nil {
		hits, _ = rawSearch(ctx, q)
	}
	hits = nonNil(hits)

	if r.cfg.MinScore > 0 {
		filtered := make([]index.Hit, 0, len(hits))
		for _, h := range hits {
			if h.Score >= r.cfg.MinScore {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}

	if r.cfg.RerankEnabled {
		if reranked, rerankErr := rerankHits(ctx, q, hits, r.cfg, r.complete); rerankErr == nil && reranked != nil {
			hits = reranked
		}
	}

	if r.cfg.ContextExpand > 0 {
		hits = r.expand(hits)
	}

	hits = take(hits, k)
	rankHits(hits)
	return nonNil(hits), nil
}

// expand appends neighbouring chunks of the same source. Porto does not do
// this; it is a kbcli extension that restores context around a narrow hit.
func (r *retriever) expand(hits []index.Hit) []index.Hit {
	if len(hits) == 0 {
		return hits
	}
	all := r.store.Chunks()
	bySource := make(map[string][]index.Chunk)
	for _, c := range all {
		bySource[c.Source] = append(bySource[c.Source], c)
	}

	seen := make(map[string]bool, len(hits))
	for _, h := range hits {
		seen[h.Chunk.ID] = true
	}
	out := append([]index.Hit{}, hits...)
	for _, h := range hits {
		for _, c := range bySource[h.Chunk.Source] {
			if seen[c.ID] {
				continue
			}
			delta := c.Ordinal - h.Chunk.Ordinal
			if delta < -r.cfg.ContextExpand || delta > r.cfg.ContextExpand {
				continue
			}
			seen[c.ID] = true
			out = append(out, index.Hit{
				Score: h.Score * 0.5,
				Dist:  h.Dist,
				Chunk: c,
			})
		}
	}
	return out
}
