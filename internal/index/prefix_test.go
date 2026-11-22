package index

import (
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/embed"
)

// recordingEmbedder captures every text handed to the encoder. It implements
// QueryPrefixer so the store's query and document paths can be distinguished.
type recordingEmbedder struct {
	seen []string
}

func (e *recordingEmbedder) Dim() int               { return embed.Dim }
func (e *recordingEmbedder) Version() string        { return "recording-v1" }
func (e *recordingEmbedder) QueryPrefix() string    { return "query: " }
func (e *recordingEmbedder) DocumentPrefix() string { return "passage: " }
func (e *recordingEmbedder) Embed(text string) ([]float32, error) {
	e.seen = append(e.seen, text)
	v := make([]float32, embed.Dim)
	v[0] = 1
	return v, nil
}
func (e *recordingEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		v, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (e *recordingEmbedder) Close() error { return nil }

func TestStoreAppliesAsymmetricPrefixes(t *testing.T) {
	e := &recordingEmbedder{}
	s := newStore(e, t.TempDir())
	chunks := []Chunk{
		{ID: "a", Source: "a.md", Text: "alpha body", Body: "alpha body"},
		{ID: "b", Source: "b.md", Text: "beta body", Body: "beta body"},
	}
	if err := s.Add(chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Search("alpha", 2); err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []string{"passage: alpha body", "passage: beta body", "query: alpha"}
	if got := strings.Join(e.seen, "|"); got != strings.Join(want, "|") {
		t.Fatalf("encoded texts = %v, want %v", e.seen, want)
	}
}
