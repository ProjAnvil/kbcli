package embed

// QueryPrefixer is an optional capability of asymmetric encoders. The e5
// family is trained with "query: " on queries and "passage: " on documents.
// Applying the query prefix to both sides puts documents in the query
// representation space and degrades recall without raising an error. Providers
// that do not implement QueryPrefixer use the text unchanged.
type QueryPrefixer interface {
	// QueryPrefix returns the prefix prepended to a search query.
	QueryPrefix() string
	// DocumentPrefix returns the prefix prepended to indexed chunk text.
	DocumentPrefix() string
}

// EmbedQuery encodes one query with the provider's query-side prefix.
func EmbedQuery(e Embedder, text string) ([]float32, error) {
	return e.Embed(prefixFor(e, true) + text)
}

// EmbedDocuments encodes several chunk texts with the provider's
// document-side prefix, preserving input order.
func EmbedDocuments(e Embedder, texts []string) ([][]float32, error) {
	p := prefixFor(e, false)
	if p == "" {
		return e.EmbedBatch(texts)
	}
	prefixed := make([]string, len(texts))
	for i, text := range texts {
		prefixed[i] = p + text
	}
	return e.EmbedBatch(prefixed)
}

// DocumentText returns the exact text that EmbedDocuments will encode. Token
// counters use it so chunk budgets include the document-side prefix.
func DocumentText(e Embedder, text string) string {
	return prefixFor(e, false) + text
}

func prefixFor(e Embedder, query bool) string {
	p, ok := e.(QueryPrefixer)
	if !ok {
		return ""
	}
	if query {
		return p.QueryPrefix()
	}
	return p.DocumentPrefix()
}
