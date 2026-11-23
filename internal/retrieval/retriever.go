package retrieval

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
)

type retriever struct {
	cfg      *config.Config
	store    index.Store
	complete CompleteFunc
	embedder embed.Embedder

	mu     sync.Mutex
	idx    *bm25Index
	metaMu sync.RWMutex
	meta   CondenseMeta

	langMu  sync.RWMutex
	langRev uint64
	lang    string
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
	return &retriever{cfg: cfg, store: opts.Store, complete: opts.Complete, embedder: opts.Embedder}, nil
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

func (r *retriever) corpusLanguage() string {
	if r.store == nil {
		return ""
	}
	if r.cfg.KBLanguage != config.KBLanguageAuto {
		return r.cfg.KBLanguage
	}
	rev := r.store.Revision()
	r.langMu.RLock()
	if r.langRev == rev {
		lang := r.lang
		r.langMu.RUnlock()
		return lang
	}
	r.langMu.RUnlock()

	lang := corpusLanguage(r.store.Chunks())
	r.langMu.Lock()
	r.langRev, r.lang = rev, lang
	r.langMu.Unlock()
	return lang
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
	// Production RAG widens the candidate pool before reranking: TopK only
	// bounds the final evidence set. With the old candK == k the reranker could
	// only reorder the same handful of hits the vector search already picked,
	// so a miss in the first stage could never be recovered downstream.
	candK := r.cfg.CandidateK
	if candK < k {
		candK = k
	}
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

	searchQ := q
	translatedFrom := ""
	translatedTo := ""
	// quick_rag skips condense and query transforms, but query translation is
	// language alignment, not expansion. It must still run in multilingual
	// profiles or simple definitional questions silently fail across languages.
	if r.cfg.QueryTranslation != config.QueryTranslationOff {
		from := textLanguage(q)
		to := r.corpusLanguage()
		r.metaMu.Lock()
		r.meta.QueryLang, r.meta.KBLang = from, to
		r.metaMu.Unlock()
		if from != "" && to != "" && from != to &&
			(r.cfg.QueryTranslation == config.QueryTranslationOn || r.cfg.QueryTranslation == config.QueryTranslationAuto) {
			if translated, terr := translateQuery(ctx, q, from, to, r.complete); terr == nil && translated != "" {
				searchQ = translated
				translatedFrom, translatedTo = from, to
				r.metaMu.Lock()
				r.meta.SearchQuery = translated
				r.meta.Translated = true
				r.meta.TranslateFrom = translatedFrom
				r.meta.TranslateTo = translatedTo
				r.metaMu.Unlock()
			}
		}
	}

	transformCfg := r.cfg.Clone()
	transformCfg.CondenseEnabled = false
	if quick {
		transformCfg.QueryTransform = config.TransformNone
	}
	hits, err := transformedSearch(ctx, searchQ, history, transformCfg, r.complete, rawSearch)
	if err != nil || hits == nil {
		// On a hard failure search the verbatim question, not the condensed
		// rewrite, so a bad anaphora resolution cannot erase the real query.
		hits, _ = rawSearch(ctx, query)
	}
	hits = nonNil(hits)

	// Anaphora resolution rewrites the question into a standalone query, but a
	// wrong rewrite must degrade recall gradually rather than replace it. Fuse
	// the verbatim query back in with a smaller weight whenever q differs.
	if !quick && q != query {
		if original, oerr := rawSearch(ctx, query); oerr == nil && len(original) > 0 {
			hits = fuseRRF([][]index.Hit{hits, original}, []float64{0.7, 0.3}, r.cfg.RRFK)
		}
	}
	if translatedFrom != "" {
		if original, oerr := rawSearch(ctx, q); oerr == nil && len(original) > 0 {
			hits = fuseRRF([][]index.Hit{hits, original}, []float64{0.7, 0.3}, r.cfg.RRFK)
		}
	}

	if r.cfg.RerankEnabled {
		// Rerank in the same language as the evidence search. With a
		// translated query, reranking on the original language reintroduces
		// the query-language bias translation was meant to remove.
		if reranked, rerankErr := rerankHitsWithEmbedder(ctx, searchQ, hits, r.cfg, r.complete, r.embedder); rerankErr == nil && reranked != nil {
			hits = reranked
		}
	}

	// min_score must run AFTER reranking: rerankHits normalizes logits back to
	// 0..1, so the threshold compares like with like. Filtering before rerank
	// also threw away candidates the reranker could have rescued.
	if r.cfg.MinScore > 0 {
		filtered := make([]index.Hit, 0, len(hits))
		for _, h := range hits {
			if h.Score >= r.cfg.MinScore {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}

	// Bound the primary evidence first. Context expansion adds neighbouring
	// chunks as EXTRA evidence; the old code appended them and then truncated
	// straight back to k, discarding every neighbour it had just added.
	hits = take(hits, k)
	if r.cfg.ContextExpand > 0 {
		hits = r.expand(hits)
		sortByScore(hits)
		if cap := k * (2*r.cfg.ContextExpand + 1); cap > k {
			hits = take(hits, cap)
		}
	}
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
