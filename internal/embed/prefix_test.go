package embed

import (
	"strings"
	"testing"
)

// plainStub records the exact text it receives and has no QueryPrefixer.
type plainStub struct {
	seen []string
}

func (s *plainStub) Dim() int        { return Dim }
func (s *plainStub) Version() string { return "stub-v1" }
func (s *plainStub) Embed(text string) ([]float32, error) {
	s.seen = append(s.seen, text)
	v := make([]float32, Dim)
	v[0] = 1
	return v, nil
}
func (s *plainStub) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		v, err := s.Embed(text)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *plainStub) Close() error { return nil }

// asymmetricStub is a plainStub that additionally implements QueryPrefixer.
type asymmetricStub struct {
	plainStub
}

func (s *asymmetricStub) QueryPrefix() string    { return "query: " }
func (s *asymmetricStub) DocumentPrefix() string { return "passage: " }

func TestPlainEmbedderUsesNoPrefix(t *testing.T) {
	e := &plainStub{}
	if _, err := EmbedQuery(e, "alpha"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if _, err := EmbedDocuments(e, []string{"beta", "gamma"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if strings.Join(e.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen = %v, want %v", e.seen, want)
	}
	if got := DocumentText(e, "body"); got != "body" {
		t.Fatalf("DocumentText = %q, want %q", got, "body")
	}
}

func TestAsymmetricEmbedderUsesDistinctPrefixes(t *testing.T) {
	e := &asymmetricStub{}
	if _, err := EmbedQuery(e, "alpha"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if _, err := EmbedDocuments(e, []string{"beta", "gamma"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	want := []string{"query: alpha", "passage: beta", "passage: gamma"}
	if strings.Join(e.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen = %v, want %v", e.seen, want)
	}
	if got := DocumentText(e, "body"); got != "passage: body" {
		t.Fatalf("DocumentText = %q, want %q", got, "passage: body")
	}
}

func TestRembedPrefixesAreAsymmetric(t *testing.T) {
	e := &rembedEmbedder{}
	if e.QueryPrefix() == e.DocumentPrefix() {
		t.Fatalf("query and document prefixes must differ, both are %q", e.QueryPrefix())
	}
	if e.QueryPrefix() != "query: " || e.DocumentPrefix() != "passage: " {
		t.Fatalf("prefixes = %q / %q, want query: / passage:", e.QueryPrefix(), e.DocumentPrefix())
	}
	if _, ok := interface{}(e).(QueryPrefixer); !ok {
		t.Fatal("rembedEmbedder must implement QueryPrefixer")
	}
}

func TestRembedVersionCarriesDomain(t *testing.T) {
	got := rembedVersion("org/name")
	if !strings.Contains(got, "#"+RembedDomainVersion) {
		t.Fatalf("rembedVersion = %q, missing domain %q", got, RembedDomainVersion)
	}
}
