package retrieval

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// isCJK reports whether r is in the CJK unified ideograph block. Porto's
// tokenizer uses single characters plus adjacent bigrams for these runs so
// Chinese queries match without a word segmenter.
func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// tokenize splits text into lowercase word tokens. When a token contains CJK
// characters, each character and each adjacent bigram is emitted too. This is
// the portable Go form of Porto's TOKEN_RE + CJK bigram tokenizer.
func tokenize(text string) []string {
	runes := []rune(strings.ToLower(text))
	var out []string
	start := -1

	flush := func(end int) {
		if start < 0 {
			return
		}
		word := string(runes[start:end])
		start = -1
		if word == "" {
			return
		}
		out = append(out, word)

		var cjk []rune
		for _, r := range []rune(word) {
			if isCJK(r) {
				cjk = append(cjk, r)
			}
		}
		if len(cjk) == 0 {
			return
		}
		for _, r := range cjk {
			out = append(out, string(r))
		}
		for i := 0; i+1 < len(cjk); i++ {
			out = append(out, string(cjk[i:i+2]))
		}
	}

	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(runes))
	return out
}

type bm25Posting struct {
	doc int
	tf  float64
}

type bm25Doc struct {
	chunk index.Chunk
	len   int
}

// bm25Index is an in-memory sparse index built from the vector store's chunk
// snapshot. It is rebuilt whenever the store revision changes.
type bm25Index struct {
	docs     []bm25Doc
	postings map[string][]bm25Posting
	df       map[string]int
	avgdl    float64
	k1       float64
	b        float64
	rev      uint64
}

func newBM25Index(chunks []index.Chunk, k1, b float64, rev uint64) *bm25Index {
	idx := &bm25Index{
		docs:     make([]bm25Doc, 0, len(chunks)),
		postings: make(map[string][]bm25Posting),
		df:       make(map[string]int),
		k1:       k1,
		b:        b,
		rev:      rev,
	}
	if k1 <= 0 {
		idx.k1 = 1.5
	}
	if b < 0 || b > 1 {
		idx.b = 0.75
	}

	totalLen := 0
	for _, c := range chunks {
		text := c.Body
		if text == "" {
			text = c.Text
		}
		tokens := tokenize(text)
		pos := len(idx.docs)
		idx.docs = append(idx.docs, bm25Doc{chunk: c, len: len(tokens)})
		totalLen += len(tokens)

		tf := make(map[string]int, len(tokens))
		for _, t := range tokens {
			tf[t]++
		}
		for t, n := range tf {
			idx.postings[t] = append(idx.postings[t], bm25Posting{doc: pos, tf: float64(n)})
			idx.df[t]++
		}
	}
	if len(idx.docs) > 0 {
		idx.avgdl = float64(totalLen) / float64(len(idx.docs))
	}
	return idx
}

// search scores the corpus with Lucene BM25 and normalizes scores into (0, 1]
// so min_score behaves the same on vector and sparse results.
func (idx *bm25Index) search(query string, k int) []index.Hit {
	if idx == nil || len(idx.docs) == 0 || k <= 0 {
		return []index.Hit{}
	}
	n := float64(len(idx.docs))
	scored := make(map[int]float64)
	for _, term := range tokenize(query) {
		postings := idx.postings[term]
		if len(postings) == 0 {
			continue
		}
		df := float64(idx.df[term])
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for _, p := range postings {
			dl := float64(idx.docs[p.doc].len)
			denom := p.tf + idx.k1*(1-idx.b+idx.b*dl/maxFloat(idx.avgdl, 1))
			if denom <= 0 {
				continue
			}
			scored[p.doc] += idf * (p.tf * (idx.k1 + 1)) / denom
		}
	}

	positions := make([]int, 0, len(scored))
	for pos := range scored {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(i, j int) bool {
		a, b := positions[i], positions[j]
		if scored[a] == scored[b] {
			return idx.docs[a].chunk.ID < idx.docs[b].chunk.ID
		}
		return scored[a] > scored[b]
	})
	if len(positions) > k {
		positions = positions[:k]
	}

	maxScore := 0.0
	for _, pos := range positions {
		if scored[pos] > maxScore {
			maxScore = scored[pos]
		}
	}
	hits := make([]index.Hit, 0, len(positions))
	for i, pos := range positions {
		score := 0.0
		if maxScore > 0 {
			score = scored[pos] / maxScore
		}
		hits = append(hits, index.Hit{
			Rank:  i + 1,
			Score: score,
			Chunk: idx.docs[pos].chunk,
		})
	}
	return hits
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
