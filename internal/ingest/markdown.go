package ingest

import (
	"fmt"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/projanvil/langchain-golang/core/documents"
	"github.com/projanvil/langchain-golang/textsplitters"
)

// chunkMarkdown splits Markdown with the remote langchain-golang splitters.
// It header-splits first with stripHeaders=true so headings stay in metadata
// (and therefore Chunk.Heading) rather than in Chunk.Body, then recursively
// splits any section longer than the chunk size while keeping short sections
// intact.
func chunkMarkdown(text string, meta chunkMeta, opts Options) ([]index.Chunk, error) {
	size := chunkSize(opts)
	headers := []textsplitters.Header{
		{Marker: "#", Name: "h1"},
		{Marker: "##", Name: "h2"},
		{Marker: "###", Name: "h3"},
		{Marker: "####", Name: "h4"},
	}
	headerSplitter := textsplitters.NewMarkdownHeader(headers, false, true)

	overlap := opts.ChunkOverlap
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	recursive, err := textsplitters.NewMarkdown(textsplitters.Config{
		ChunkSize:    size,
		ChunkOverlap: overlap,
	})
	if err != nil {
		return nil, fmt.Errorf("create markdown splitter: %w", err)
	}

	passages := markdownPassages(headerSplitter.SplitText(text), recursive, size)
	return buildChunks(passages, meta, 0), nil
}

// markdownPassages turns header-split documents into passages. Overlong
// sections are recursively split; short sections stay intact. Every passage
// keeps the joined active-heading metadata of its section.
func markdownPassages(docs []documents.Document, recursive *textsplitters.RecursiveCharacterTextSplitter, size int) []passage {
	var passages []passage
	for _, doc := range docs {
		heading := joinHeadings(doc.Metadata)
		body := doc.PageContent
		if strings.TrimSpace(body) == "" {
			continue
		}
		if runeLen(body) > size {
			for _, piece := range recursive.SplitText(body) {
				if piece != "" {
					passages = append(passages, passage{heading: heading, body: piece})
				}
			}
		} else {
			passages = append(passages, passage{heading: heading, body: body})
		}
	}
	return passages
}

// joinHeadings builds the "H1 > H2" style heading from active h1..h4 values.
func joinHeadings(m map[string]any) string {
	var parts []string
	for _, key := range []string{"h1", "h2", "h3", "h4"} {
		if v, ok := m[key].(string); ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " > ")
}
