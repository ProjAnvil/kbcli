package ingest

import (
	"regexp"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// markdownStrategy header-splits Markdown (including .mdx), keeping the heading
// path in Chunk.Heading and capping each section at SectionSize.
type markdownStrategy struct{}

func (markdownStrategy) Name() string { return "markdown" }
func (markdownStrategy) Extensions() []string {
	return []string{".md", ".markdown", ".mdx"}
}
func (markdownStrategy) Chunk(src Source) ([]index.Chunk, error) {
	return chunkMarkdownWithSize(src.Text, src.Meta, sectionSize(src.Opts), src.Opts.ChunkOverlap)
}

// plainTextStrategy keeps the legacy single-size fallback for plain text. It
// deliberately reads ChunkSize (not SectionSize) through chunkText.
type plainTextStrategy struct{}

func (plainTextStrategy) Name() string { return "plain_text" }
func (plainTextStrategy) Extensions() []string {
	return []string{".txt", ".text"}
}
func (plainTextStrategy) Chunk(src Source) ([]index.Chunk, error) {
	return chunkText(src.Text, src.Ext, src.Meta, src.Opts)
}

// csvStrategy chunks CSV by row groups, repeating the header line at the top
// of every chunk so each chunk stays a valid, self-describing table fragment.
type csvStrategy struct{}

func (csvStrategy) Name() string         { return "csv" }
func (csvStrategy) Extensions() []string { return []string{".csv"} }

func (csvStrategy) Chunk(src Source) ([]index.Chunk, error) {
	size := sectionSize(src.Opts)

	// Normalize line endings, then keep only non-empty lines so stray blank
	// rows never become phantom data.
	normalized := strings.ReplaceAll(src.Text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	var lines []string
	for _, ln := range strings.Split(normalized, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return []index.Chunk{}, nil
	}

	header := lines[0]
	rows := lines[1:]

	// Reserve one rune for the newline between the header and the first row.
	rowBudget := size - runeLen(header) - 1
	if rowBudget < 1 {
		rowBudget = 1
	}

	var passages []passage
	cur := make([]string, 0)
	curLen := 0 // rune length of strings.Join(cur, "\n")

	flush := func() {
		if len(cur) > 0 {
			passages = append(passages, passage{heading: "", body: header + "\n" + strings.Join(cur, "\n")})
			cur = cur[:0]
			curLen = 0
		}
	}

	// addRow appends one row, starting a new group when the cap would be
	// exceeded. Callers pre-split overlong rows so a single row is always
	// within rowBudget here.
	addRow := func(row string) {
		rLen := runeLen(row)
		add := rLen
		if len(cur) > 0 {
			add++ // newline separator between rows
		}
		if curLen+add > rowBudget && len(cur) > 0 {
			flush()
		}
		if len(cur) > 0 {
			curLen++ // separator added below via strings.Join
		}
		cur = append(cur, row)
		curLen += rLen
	}

	for _, row := range rows {
		if runeLen(row) <= rowBudget {
			addRow(row)
			continue
		}
		// A single overlong row must never overflow the cap: hard-split it
		// into rune pieces and treat each piece as its own row.
		flush()
		for _, piece := range hardSplit(row, rowBudget) {
			addRow(piece)
		}
	}
	flush()

	if len(passages) == 0 {
		// Header-only input: emit one chunk carrying just the header.
		passages = append(passages, passage{heading: "", body: header})
	}
	return buildChunks(passages, src.Meta, 0)
}

// htmlStrategy turns HTML headings into Markdown heading lines before stripping
// tags, so the fallback splitter can lift them into Chunk.Heading.
type htmlStrategy struct{}

func (htmlStrategy) Name() string         { return "html" }
func (htmlStrategy) Extensions() []string { return []string{".html", ".htm"} }

func (htmlStrategy) Chunk(src Source) ([]index.Chunk, error) {
	text := htmlHeadingsToMarkdown(src.Text)
	text = stripHTML(text)
	return buildChunks(splitFallback(text, sectionSize(src.Opts)), src.Meta, 0)
}

// htmlHeadingPattern matches h1..h4 elements. RE2 has no backreferences, so the
// closing tag is matched loosely; stripHTML removes any inner tags afterwards.
var htmlHeadingPattern = regexp.MustCompile(`(?is)<h([1-4])[^>]*>(.*?)</h[1-4]>`)

// htmlHeadingsToMarkdown rewrites <h1>..<h4> elements into Markdown heading
// lines. Inner markup is preserved here and removed by stripHTML later.
func htmlHeadingsToMarkdown(s string) string {
	return htmlHeadingPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := htmlHeadingPattern.FindStringSubmatch(match)
		level := int(parts[1][0] - '0')
		return "\n" + strings.Repeat("#", level) + " " + parts[2] + "\n"
	})
}

// pdfStrategy extracts per-page PDF text, capping each page at SectionSize.
type pdfStrategy struct{}

func (pdfStrategy) Name() string         { return "pdf" }
func (pdfStrategy) Extensions() []string { return []string{".pdf"} }

func (pdfStrategy) Chunk(src Source) ([]index.Chunk, error) {
	return chunkPDF(src.Data, src.Meta, withSize(src.Opts, sectionSize(src.Opts)))
}

// docxStrategy converts DOCX to Markdown, then header-splits it so DOCX
// headings land in Chunk.Heading instead of the body.
type docxStrategy struct{}

func (docxStrategy) Name() string         { return "docx" }
func (docxStrategy) Extensions() []string { return []string{".docx"} }

func (docxStrategy) Chunk(src Source) ([]index.Chunk, error) {
	md, err := docxToMarkdown(src.Data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(md) == "" {
		return []index.Chunk{}, nil
	}
	return chunkMarkdownWithSize(md, src.Meta, sectionSize(src.Opts), src.Opts.ChunkOverlap)
}
