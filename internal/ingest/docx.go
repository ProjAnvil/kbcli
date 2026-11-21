package ingest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/projanvil/kbcli/internal/index"
)

const maxDOCXUncompressed = 100 << 20 // 100MB decompression budget

// chunkDOCX converts a dependency-free DOCX (zip + word/document.xml) into
// Markdown, then feeds it through the structure-aware fallback splitter.
func chunkDOCX(data []byte, meta chunkMeta, opts Options) ([]index.Chunk, error) {
	md, err := docxToMarkdown(data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(md) == "" {
		return []index.Chunk{}, nil
	}
	return splitFallbackChunks(md, chunkSize(opts), meta), nil
}

// docxToMarkdown walks w:p / w:r / w:t, maps pStyle Heading1..4 to "# ".."#### ",
// numbered paragraphs to "- ", and table cells to pipe rows. It emits Markdown
// in reading order. Elements are matched by local name only (OOXML namespace
// prefixes vary by producer). Strict encoding/xml decoding keeps XXE/DTD out.
func docxToMarkdown(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a valid docx zip: %w", err)
	}
	// Validate every entry BEFORE any consumer opens the archive: reject path
	// traversal, per-entry and cumulative decompression bombs.
	var total uint64
	for _, f := range zr.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") {
			return "", fmt.Errorf("docx entry %q has an unsafe path", f.Name)
		}
		if f.UncompressedSize64 > maxDOCXUncompressed {
			return "", fmt.Errorf("docx entry %q decompresses to %d bytes (limit %d)", f.Name, f.UncompressedSize64, uint64(maxDOCXUncompressed))
		}
		total += f.UncompressedSize64
		if total > maxDOCXUncompressed {
			return "", fmt.Errorf("docx decompressed size exceeds %d bytes (zip bomb?)", uint64(maxDOCXUncompressed))
		}
	}

	doc := findZipEntry(zr, "word/document.xml")
	if doc == nil {
		return "", fmt.Errorf("docx is missing word/document.xml")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("open word/document.xml: %w", err)
	}
	// A hard read cap independent of the declared header catches a forged small
	// UncompressedSize64 that would otherwise stream gigabytes through.
	xmlBytes, err := io.ReadAll(io.LimitReader(rc, maxDOCXUncompressed+1))
	rc.Close()
	if err != nil {
		return "", fmt.Errorf("read word/document.xml: %w", err)
	}
	if len(xmlBytes) > maxDOCXUncompressed {
		return "", fmt.Errorf("word/document.xml exceeds the %d byte limit", uint64(maxDOCXUncompressed))
	}
	return parseDocumentXML(xmlBytes)
}

func findZipEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func parseDocumentXML(xmlBytes []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	dec.Strict = true
	var sb strings.Builder
	var para strings.Builder
	var curCell strings.Builder
	var curRow []string
	var tblRows [][]string
	var paraStyle string
	var paraNum, inPara, inCell bool

	flushPara := func() {
		text := strings.TrimSpace(para.String())
		para.Reset()
		if text == "" {
			paraStyle, paraNum = "", false
			return
		}
		switch {
		case headingLevel(paraStyle) > 0:
			sb.WriteString(strings.Repeat("#", headingLevel(paraStyle)) + " " + text + "\n\n")
		case paraNum:
			sb.WriteString("- " + text + "\n")
		case inCell:
			if curCell.Len() > 0 {
				curCell.WriteString(" ")
			}
			curCell.WriteString(text)
		default:
			sb.WriteString(text + "\n\n")
		}
		paraStyle, paraNum = "", false
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse word/document.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inPara, paraNum, paraStyle = true, false, ""
				para.Reset()
			case "pStyle":
				for _, a := range t.Attr {
					if a.Name.Local == "val" {
						paraStyle = a.Value
					}
				}
			case "numPr":
				paraNum = true
			case "t":
				if inPara {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						para.WriteString(s)
					}
				}
			case "tr":
				curRow = nil
			case "tc":
				inCell, curCell = true, strings.Builder{}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				flushPara()
				inPara = false
			case "tc":
				inCell = false
				curRow = append(curRow, cellText(curCell.String()))
			case "tr":
				if curRow != nil {
					tblRows = append(tblRows, curRow)
				}
				curRow = nil
			case "tbl":
				if len(tblRows) > 0 {
					writeMarkdownTable(&sb, tblRows)
				}
				tblRows = nil
			}
		}
	}
	flushPara()
	return strings.TrimSpace(sb.String()), nil
}

// headingLevel maps OOXML style ids (Heading1, heading 2, ...) to markdown depth.
func headingLevel(style string) int {
	s := strings.TrimPrefix(strings.ToLower(style), "heading")
	var lvl int
	if _, err := fmt.Sscanf(s, "%d", &lvl); err != nil {
		return 0
	}
	if lvl < 1 {
		return 0
	}
	return min(lvl, 4)
}

// cellText flattens a table cell for a pipe row: whitespace collapsed, pipes escaped.
func cellText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

// writeMarkdownTable renders rows as a pipe table with the first row as header.
func writeMarkdownTable(sb *strings.Builder, rows [][]string) {
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	if width == 0 {
		return
	}
	pad := func(r []string) []string {
		out := make([]string, width)
		copy(out, r)
		return out
	}
	sb.WriteString("| " + strings.Join(pad(rows[0]), " | ") + " |\n")
	sb.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
	for _, r := range rows[1:] {
		sb.WriteString("| " + strings.Join(pad(r), " | ") + " |\n")
	}
	sb.WriteString("\n")
}
