package retrieval

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// truncationSuffix marks a clipped part. Porto's context builder trims from
// the end of the parts slice so the question always survives the budget.
const truncationSuffix = " ... (truncated)"

// truncateRunes clips s to at most n runes.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func totalRunes(parts []string) int {
	n := 0
	for i, p := range parts {
		if i > 0 {
			n += 2 // "\n\n"
		}
		n += utf8.RuneCountInString(p)
	}
	return n
}

// trimToBudget trims evidence from the end until the joined text fits budget
// runes. The first part (the question) is kept even when it alone exceeds the
// budget. It never returns nil.
func trimToBudget(parts []string, budget int) []string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 || budget <= 0 {
		return []string{}
	}

	for totalRunes(kept) > budget && len(kept) > 1 {
		excess := totalRunes(kept) - budget
		last := kept[len(kept)-1]
		avail := utf8.RuneCountInString(last) - excess - utf8.RuneCountInString(truncationSuffix)
		if avail <= 0 {
			kept = kept[:len(kept)-1]
			continue
		}
		kept[len(kept)-1] = truncateRunes(last, avail) + truncationSuffix
		break
	}
	return kept
}

// formatCitationParts returns one independent part per citation. Keeping
// citations as separate prompt parts lets TrimToBudget drop whole trailing
// snippets instead of slicing a numbered passage mid-way, which used to leave
// dangling [n] markers in the evidence block.
func formatCitationParts(hits []index.Hit, numbers []int, perChunk int) []string {
	if len(hits) == 0 {
		return nil
	}
	if perChunk <= 0 {
		perChunk = 800
	}
	parts := make([]string, 0, len(hits))
	for i, h := range hits {
		n := i + 1
		if i < len(numbers) && numbers[i] > 0 {
			n = numbers[i]
		}
		var b strings.Builder
		fmt.Fprintf(&b, "[%d] %s score=%.3f\n", n, h.Chunk.Source, h.Score)
		body := h.Chunk.Body
		if body == "" {
			body = h.Chunk.Text
		}
		b.WriteString(truncateRunes(body, perChunk))
		parts = append(parts, b.String())
	}
	return parts
}

// formatCitations joins per-citation parts into one evidence block. numbers
// carries stable 1-based citation IDs from the agent's citation ledger; a nil
// or short slice falls back to positional numbering.
func formatCitations(hits []index.Hit, numbers []int, perChunk int) string {
	parts := formatCitationParts(hits, numbers, perChunk)
	if len(parts) == 0 {
		return "No knowledge base passages available."
	}
	return strings.Join(parts, "\n\n")
}

// formatSources renders ranked evidence as numbered citations for the agent
// prompt. Porto caps each citation separately before the global budget runs.
func formatSources(hits []index.Hit, perChunk int) string {
	return formatCitations(hits, nil, perChunk)
}
