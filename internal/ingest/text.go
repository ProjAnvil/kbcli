package ingest

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/projanvil/kbcli/internal/index"
)

// decodeText validates UTF-8 and converts bytes to a string for text sources.
func decodeText(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file is not valid UTF-8 text")
	}
	return string(data), nil
}

// chunkText splits plain-text-like sources (txt/text/csv/mdx/html/htm). HTML
// sources have their tags and script/style contents stripped first.
func chunkText(text string, ext string, meta chunkMeta, opts Options) ([]index.Chunk, error) {
	if ext == ".html" || ext == ".htm" {
		text = stripHTML(text)
	}
	return splitFallbackChunks(text, chunkSize(opts), meta), nil
}

// stripHTML removes tags and the contents of script/style elements using a
// small stdlib scanner. Tags are replaced by newlines so adjacent text nodes do
// not silently merge; comments and script/style blocks are dropped entirely.
func stripHTML(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 { // unterminated tag: keep the rest verbatim
			b.WriteString(s[i:])
			break
		}
		tag := lower[i : i+end+1]
		after := i + end + 1
		switch {
		case strings.HasPrefix(tag, "<!--"):
			if k := strings.Index(lower[after:], "-->"); k >= 0 {
				after += k + 3
			}
			b.WriteByte('\n')
		case strings.HasPrefix(tag, "<script"), strings.HasPrefix(tag, "<style"):
			closeTag := "</script>"
			if strings.HasPrefix(tag, "<style") {
				closeTag = "</style>"
			}
			if k := strings.Index(lower[after:], closeTag); k >= 0 {
				after += k + len(closeTag)
			} else {
				after = len(s)
			}
			b.WriteByte('\n')
		default:
			b.WriteByte('\n')
		}
		i = after
	}
	return b.String()
}
