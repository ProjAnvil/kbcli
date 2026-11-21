package ingest

import (
	"fmt"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/embed"
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
func splitFallbackChunks(text string, size int, meta chunkMeta) ([]index.Chunk, error) {
	return buildChunks(splitFallback(text, size), meta, 0)
}

// capPassagesByTokens re-splits passages whose embedding text would exceed the
// model's real token limit. It runs before ordinals and IDs are assigned so
// every emitted chunk is within the cap. A nil counter is the local hashing
// embedder path: passages are returned untouched and rune-based chunking stays
// exactly as before. An embedding prefix that alone exceeds the budget is a
// hard error rather than an over-cap chunk.
func capPassagesByTokens(passages []passage, meta chunkMeta, counter embed.TokenCounter) ([]passage, error) {
	if counter == nil {
		return passages, nil
	}
	maxTokens := meta.MaxTokens
	if maxTokens <= 0 {
		maxTokens = counter.MaxTokens()
	}
	budget := maxTokens - 16
	if budget <= 0 {
		budget = 1
	}
	out := make([]passage, 0, len(passages))
	for _, p := range passages {
		// makeChunk builds Title+"\n"+Heading+"\n"+Body, and the provider's
		// CountForEmbedding adds its internal prefix, so this is the exact
		// text whose token count must stay within budget.
		prefix := meta.Title + "\n" + p.heading + "\n"
		if counter.CountForEmbedding(prefix) > budget {
			return nil, fmt.Errorf("ingest: embedding prefix exceeds model token budget for source %q", meta.Source)
		}
		if counter.CountForEmbedding(prefix+p.body) <= budget {
			out = append(out, p)
			continue
		}
		var cur string
		flush := func() {
			if cur != "" {
				out = append(out, passage{heading: p.heading, body: cur})
				cur = ""
			}
		}
		for _, block := range splitBlocks(p.body) {
			if block == "" {
				continue
			}
			if counter.CountForEmbedding(prefix+block) > budget {
				flush()
				for _, piece := range hardSplitByTokens(prefix, block, budget, counter) {
					out = append(out, passage{heading: p.heading, body: piece})
				}
				continue
			}
			if cur == "" {
				cur = block
			} else if counter.CountForEmbedding(prefix+cur+"\n\n"+block) <= budget {
				cur += "\n\n" + block
			} else {
				flush()
				cur = block
			}
		}
		flush()
	}
	return out, nil
}

// hardSplitByTokens splits s into the longest rune pieces whose complete
// embedding text (prefix+piece) fits the budget. The binary search always
// accepts at least one rune, so an unfitably large prefix cannot loop forever.
func hardSplitByTokens(prefix, s string, budget int, counter embed.TokenCounter) []string {
	runes := []rune(s)
	var out []string
	for start := 0; start < len(runes); {
		lo, hi, best := start+1, len(runes), start+1
		for lo <= hi {
			mid := lo + (hi-lo)/2
			if counter.CountForEmbedding(prefix+string(runes[start:mid])) <= budget {
				best = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		piece := string(runes[start:best])
		if piece != "" {
			out = append(out, piece)
		}
		start = best
	}
	return out
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
