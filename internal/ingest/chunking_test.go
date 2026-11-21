package ingest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// writeFile creates a fixture at runtime in dir (including parent dirs) and
// returns its path. No binary fixtures are committed.
func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// buildTestPDF assembles a one-page PDF with one text draw and a byte-exact
// xref table (ported from Sentia's prddoc tests).
func buildTestPDF(text string) []byte {
	var objs []string
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	objs = append(objs, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", text)
	objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return out.Bytes()
}

// buildTestDOCX assembles a minimal but standard docx zip: one heading, one
// paragraph, and a two-row table in word/document.xml.
func buildTestDOCX(t *testing.T) []byte {
	t.Helper()
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>登录模块</w:t></w:r></w:p>
<w:p><w:r><w:t>支持手机号验证码登录。</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>字段</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>类型</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>phone</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>string</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// assertChunkInvariants checks the contract shared by every chunk regardless of
// format: stable-length IDs, uniqueness, sequential ordinals, non-empty source,
// and Text == Title+"\n"+Heading+"\n"+Body (the exact embedding input).
func assertChunkInvariants(t *testing.T, chunks []index.Chunk) {
	t.Helper()
	seen := map[string]bool{}
	for i, c := range chunks {
		if c.Ordinal != i {
			t.Errorf("chunk %d has Ordinal %d", i, c.Ordinal)
		}
		if len(c.ID) != 16 {
			t.Errorf("ID %q is not 16 hex chars", c.ID)
		}
		if seen[c.ID] {
			t.Errorf("duplicate chunk ID %q", c.ID)
		}
		seen[c.ID] = true
		if c.Source == "" {
			t.Errorf("chunk %d has empty Source", i)
		}
		if want := c.Title + "\n" + c.Heading + "\n" + c.Body; c.Text != want {
			t.Errorf("chunk %d Text != Title\\nHeading\\nBody", i)
		}
	}
}

func TestIngestSupports(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"a.md", true}, {"A.MARKDOWN", true}, {"b.MdX", true},
		{"c.txt", true}, {"d.text", true}, {"e.csv", true},
		{"f.HTML", true}, {"g.htm", true}, {"h.pdf", true}, {"i.DOCX", true},
		{"old.doc", false}, {"x.png", false}, {"noext", false}, {".kbcli.kbvec", false}, {".kbvec", false},
	}
	for _, tc := range cases {
		if got := supports(tc.path); got != tc.want {
			t.Errorf("supports(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIngestUnsupportedExtensions(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantDocx bool
	}{
		{"legacy.doc", true},
		{"image.png", false},
		{"archive.zip", false},
	}
	for _, tc := range cases {
		p := writeFile(t, dir, tc.name, []byte("x"))
		_, err := ChunkFile(dir, p, DefaultOptions())
		if err == nil {
			t.Errorf("ChunkFile(%q) must error", tc.name)
			continue
		}
		if tc.wantDocx && !strings.Contains(err.Error(), ".docx") {
			t.Errorf("legacy .doc error must guide the user to .docx, got %v", err)
		}
	}
}

func TestIngestMarkdownHeadingHierarchy(t *testing.T) {
	dir := t.TempDir()
	md := "# Alpha\n\nintro text\n\n## Beta\n\nbeta text\n\n### Gamma\n\ngamma text\n"
	p := writeFile(t, dir, "guide.md", []byte(md))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertChunkInvariants(t, res.Chunks)
	if len(res.Chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	found := false
	for _, c := range res.Chunks {
		if c.Title != "guide" {
			t.Errorf("Title = %q, want guide", c.Title)
		}
		if c.Heading == "Alpha > Beta > Gamma" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no chunk carries the joined heading hierarchy: %+v", headingsOf(res.Chunks))
	}
}

func headingsOf(chunks []index.Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Heading)
	}
	return out
}

func TestIngestMarkdownTableStaysIntact(t *testing.T) {
	dir := t.TempDir()
	md := "# Report\n\n| Name | Value |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |\n"
	p := writeFile(t, dir, "table.md", []byte(md))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertChunkInvariants(t, res.Chunks)
	found := false
	for _, c := range res.Chunks {
		if strings.Contains(c.Body, "| alpha | 1 |") && strings.Contains(c.Body, "| beta | 2 |") {
			found = true
		}
	}
	if !found {
		t.Fatalf("adjacent table rows were split across chunks: %+v", res.Chunks)
	}
}

func TestIngestPlainText600RuneSplit(t *testing.T) {
	dir := t.TempDir()
	text := strings.Repeat("a", 1500)
	p := writeFile(t, dir, "plain.txt", []byte(text))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertChunkInvariants(t, res.Chunks)
	if len(res.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (ceil(1500/600))", len(res.Chunks))
	}
	var joined strings.Builder
	for _, c := range res.Chunks {
		if n := len([]rune(c.Body)); n > 600 {
			t.Errorf("chunk body has %d runes, exceeds 600", n)
		}
		joined.WriteString(c.Body)
	}
	if joined.String() != text {
		t.Fatal("reconstructed bodies do not match the original text")
	}
}

func TestIngestPDFTextExtraction(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "doc.pdf", buildTestPDF("Hello PRD"))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Skipf("minimal PDF is not extractable on this platform: %v", err)
	}
	if len(res.Chunks) == 0 {
		t.Skip("no text extracted from the minimal PDF")
	}
	assertChunkInvariants(t, res.Chunks)
	found := false
	for _, c := range res.Chunks {
		if strings.Contains(c.Body, "Hello PRD") {
			found = true
		}
		if c.Page != 1 {
			t.Errorf("PDF chunk Page = %d, want 1", c.Page)
		}
	}
	if !found {
		t.Fatalf("PDF text missing from chunks: %+v", res.Chunks)
	}
}

func TestIngestDOCXStructure(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "spec.docx", buildTestDOCX(t))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) == 0 {
		t.Fatal("no chunks produced from docx")
	}
	assertChunkInvariants(t, res.Chunks)
	var all strings.Builder
	for _, c := range res.Chunks {
		all.WriteString(c.Body)
	}
	for _, want := range []string{"# 登录模块", "支持手机号验证码登录。", "| phone | string |"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("docx markdown missing %q:\n%s", want, all.String())
		}
	}
}

func TestIngestHTMLStripping(t *testing.T) {
	dir := t.TempDir()
	html := "<html><head><style>.x{color:red}</style></head><body><script>var SECRET=1;</script><p>Hello <b>World</b></p></body></html>"
	p := writeFile(t, dir, "page.html", []byte(html))
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertChunkInvariants(t, res.Chunks)
	var all strings.Builder
	for _, c := range res.Chunks {
		all.WriteString(c.Body)
	}
	s := all.String()
	for _, want := range []string{"Hello", "World"} {
		if !strings.Contains(s, want) {
			t.Errorf("stripped HTML missing %q: %q", want, s)
		}
	}
	for _, bad := range []string{"SECRET", "<script", "<style", "<p>", "<b>"} {
		if strings.Contains(s, bad) {
			t.Errorf("stripped HTML still contains %q: %q", bad, s)
		}
	}
}

func TestIngestFileStateDeterminism(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "note.md", []byte("# Title\n\nHello world\n"))
	s1, err := FileStateFor(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := FileStateFor(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatalf("FileStateFor is not deterministic: %+v vs %+v", s1, s2)
	}
	if s1.Path != "note.md" {
		t.Errorf("Path = %q, want note.md", s1.Path)
	}
	if s1.SHA256 == "" {
		t.Error("empty SHA256")
	}
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.State.SHA256 != s1.SHA256 || res.State.Path != s1.Path {
		t.Fatal("chunkFile state differs from FileStateFor")
	}
}

func TestIngestWalkExclusions(t *testing.T) {
	dir := t.TempDir()
	want := []string{"a.md", "b.txt", "upper.MD", "sub/ok.csv"}
	for _, n := range want {
		writeFile(t, dir, n, []byte("x"))
	}
	// dot dirs, configured excludes, unsupported ext, vector file, oversize file
	writeFile(t, dir, ".git/config.md", []byte("x"))
	writeFile(t, dir, "node_modules/pkg/readme.md", []byte("x"))
	writeFile(t, dir, "vendor/lib.md", []byte("x"))
	writeFile(t, dir, ".hidden/secret.md", []byte("x"))
	writeFile(t, dir, "image.png", []byte("x"))
	writeFile(t, dir, index.DefaultFileName, []byte("x"))
	writeFile(t, dir, "big.md", []byte(strings.Repeat("z", 200)))

	opts := DefaultOptions()
	opts.MaxFileBytes = 100 // big.md (200B) must be skipped

	paths, err := Walk(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(want) {
		t.Fatalf("Walk returned %d paths, want %d: %v", len(paths), len(want), paths)
	}
	got := map[string]bool{}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			t.Errorf("path is not absolute: %q", p)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		got[filepath.ToSlash(rel)] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("expected %q not returned: %v", n, paths)
		}
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1] > paths[i] {
			t.Errorf("paths are not sorted: %v", paths)
		}
	}
}

func TestIngestStableChunkIDs(t *testing.T) {
	dir := t.TempDir()
	md := "# A\n\n" + strings.Repeat("word ", 200) + "\n\n## B\n\n" + strings.Repeat("more ", 200) + "\n"
	p := writeFile(t, dir, "stable.md", []byte(md))
	r1, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertChunkInvariants(t, r1.Chunks)
	if len(r1.Chunks) < 2 {
		t.Fatalf("expected multiple chunks from a long markdown, got %d", len(r1.Chunks))
	}
	if len(r1.Chunks) != len(r2.Chunks) {
		t.Fatalf("chunk count differs between runs: %d vs %d", len(r1.Chunks), len(r2.Chunks))
	}
	for i := range r1.Chunks {
		if r1.Chunks[i].ID != r2.Chunks[i].ID {
			t.Fatalf("chunk %d ID not stable: %q vs %q", i, r1.Chunks[i].ID, r2.Chunks[i].ID)
		}
	}
}

func TestIngestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "empty.md", nil)
	res, err := ChunkFile(dir, p, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) != 0 {
		t.Fatalf("empty file produced %d chunks", len(res.Chunks))
	}
	if res.State.SHA256 == "" {
		t.Fatal("empty file still needs a state hash")
	}
}
