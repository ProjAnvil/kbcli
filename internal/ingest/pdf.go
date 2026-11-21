package ingest

import (
	"fmt"
	"os"
	"strings"

	pdf "github.com/ledongthuc/pdf"
	"github.com/projanvil/kbcli/internal/index"
)

// chunkPDF extracts row-ordered text and splits PER PAGE so every chunk can
// cite a 1-based page number. ledongthuc/pdf needs a real file path, so the
// bytes are written to a temp file that is removed afterwards. Page text is the
// concatenation of word.S per row plus "\n"; pages are kept separate (the
// reference builds one document with a blank line between pages, but per-page
// splitting is required here for evidence citation).
func chunkPDF(data []byte, meta chunkMeta, opts Options) ([]index.Chunk, error) {
	tmp, err := os.CreateTemp("", "kbcli-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("create temp pdf: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("write temp pdf: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp pdf: %w", err)
	}

	f, r, err := pdf.Open(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	pages := r.NumPage()
	if limit := maxPDFPages(opts); pages > limit {
		pages = limit
	}
	size := chunkSize(opts)
	chunks := make([]index.Chunk, 0)
	ordinal := 0
	for i := 1; i <= pages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		rows, err := page.GetTextByRow()
		if err != nil {
			continue
		}
		var sb strings.Builder
		for _, row := range rows {
			for _, word := range row.Content {
				sb.WriteString(word.S)
			}
			sb.WriteByte('\n')
		}
		text := strings.TrimSpace(sb.String())
		if text == "" {
			continue
		}
		pageMeta := meta
		pageMeta.Page = i
		for _, p := range splitFallback(text, size) {
			chunks = append(chunks, makeChunk(pageMeta, ordinal, p.heading, p.body))
			ordinal++
		}
	}
	return chunks, nil
}
