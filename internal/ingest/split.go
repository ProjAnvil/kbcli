package ingest

import (
	"strings"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// passage is one chunk of body text plus the heading it belongs to. The split
// functions below produce passages; buildChunks turns them into index.Chunk.
type passage struct {
	heading string
	body    string
}

// splitFallbackChunks is the fallback path's index.Chunk producer: it splits
// text and stamps every chunk with Ordinal, Source, Page and Title from meta.
func splitFallbackChunks(text string, size int, meta chunkMeta) []index.Chunk {
	return buildChunks(splitFallback(text, size), meta, 0)
}

// mdSection is one heading-bounded region; heading is empty for the preamble.
type mdSection struct {
	heading string
	body    string
}

// splitFallback is Sentia's 600-rune structure-aware splitter, ported to emit
// passages. It is the fallback for plain text, PDF, DOCX, CSV and HTML. Rules:
//  1. `#{1,4} ` heading lines open a section whose heading prefixes every chunk.
//  2. Blank-line-separated blocks aggregate greedily up to size; consecutive
//     `|` lines stay one table block, oversized tables split by row groups that
//     repeat the header row.
//  3. A single oversized block hard-splits by runes with no overlap.
//  4. With no headings there is one section with the same block aggregation.
func splitFallback(text string, size int) []passage {
	if text == "" {
		return nil
	}
	if runeLen(text) <= size {
		return []passage{{heading: firstHeading(text), body: text}}
	}
	var out []passage
	for _, sec := range splitSections(text) {
		out = append(out, splitSectionFallback(sec, size)...)
	}
	return out
}

// firstHeading returns the clean text of the first heading line, or "".
func firstHeading(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		if _, ok := headingLine(ln); ok {
			return cleanHeading(strings.TrimSpace(ln))
		}
	}
	return ""
}

// cleanHeading strips the leading '#'/space markers from a heading line.
func cleanHeading(head string) string {
	return strings.TrimSpace(strings.TrimLeft(head, "# "))
}

func splitSections(text string) []mdSection {
	lines := strings.Split(text, "\n")
	var secs []mdSection
	var cur mdSection
	flush := func() {
		if strings.TrimSpace(cur.heading+cur.body) != "" {
			secs = append(secs, cur)
		}
		cur = mdSection{}
	}
	for _, ln := range lines {
		if _, ok := headingLine(ln); ok {
			flush()
			cur.heading = strings.TrimSpace(ln)
			continue
		}
		cur.body += ln + "\n"
	}
	flush()
	return secs
}

func headingLine(ln string) (int, bool) {
	t := strings.TrimLeft(ln, " ")
	if !strings.HasPrefix(t, "#") {
		return 0, false
	}
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n <= 4 && n < len(t) && t[n] == ' ' {
		return n, true
	}
	return 0, false
}

// splitSectionFallback aggregates a section's blocks; the heading prefix rides
// every chunk. It is Sentia's splitSection adapted to also record the heading.
func splitSectionFallback(sec mdSection, size int) []passage {
	head := strings.TrimSpace(sec.heading)
	clean := cleanHeading(head)
	var chunks []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	room := func() int { return size - runeLen(head) - 1 }
	appendText := func(s string) {
		if cur.Len() == 0 && head != "" {
			cur.WriteString(head + "\n")
		}
		cur.WriteString(s)
	}
	for _, block := range splitBlocks(sec.body) {
		groups := []string{block}
		switch {
		case isTable(block) && runeLen(block) > size:
			groups = splitTableGroups(block, size, head)
		case runeLen(block) > size:
			groups = hardSplit(block, size)
		}
		for _, g := range groups {
			if runeLen(cur.String())+runeLen(g)+1 > size && cur.Len() > 0 {
				flush()
			}
			for _, piece := range hardSplit(g, max(room(), 1)) {
				appendText(piece)
				if runeLen(cur.String()) >= size {
					flush()
				}
			}
		}
	}
	flush()
	out := make([]passage, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, passage{heading: clean, body: c})
	}
	return out
}

// splitBlocks splits body into blank-line-separated blocks, keeping markdown
// tables (consecutive |-prefixed lines) intact as single blocks.
func splitBlocks(body string) []string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var blocks, table []string
	var para []string
	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, strings.Join(para, "\n"))
			para = nil
		}
	}
	flushTable := func() {
		if len(table) > 0 {
			blocks = append(blocks, strings.Join(table, "\n"))
			table = nil
		}
	}
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(strings.TrimSpace(ln), "|"):
			flushPara()
			table = append(table, ln)
		case strings.TrimSpace(ln) == "":
			flushPara()
			flushTable()
		default:
			flushTable()
			para = append(para, ln)
		}
	}
	flushPara()
	flushTable()
	return blocks
}

func isTable(block string) bool {
	return strings.HasPrefix(strings.TrimSpace(block), "|")
}

// splitTableGroups splits an oversized table into header-repeating row groups.
func splitTableGroups(block string, size int, heading string) []string {
	lines := strings.Split(block, "\n")
	if len(lines) < 3 { // header + separator + at least one row
		return hardSplit(block, size)
	}
	header := lines[0] + "\n" + lines[1]
	var groups []string
	cur := header
	for _, row := range lines[2:] {
		if runeLen(cur)+runeLen(row)+1 > size-runeLen(heading)-1 && cur != header {
			groups = append(groups, cur)
			cur = header
		}
		cur += "\n" + row
	}
	if cur != header {
		groups = append(groups, cur)
	}
	return groups
}

// hardSplit is the fixed-width guarantee for a single oversized block, with no
// overlap. A non-positive size is guarded against to avoid an infinite loop.
func hardSplit(s string, size int) []string {
	runes := []rune(s)
	if size <= 0 || len(runes) <= size {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(runes); i += size {
		end := min(i+size, len(runes))
		out = append(out, string(runes[i:end]))
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }
