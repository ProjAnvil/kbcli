package ingest

import (
	"sort"
	"strings"
	"sync"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// Strategy values for Options.ChunkStrategy.
const (
	StrategyAuto  = "auto"
	StrategyFixed = "fixed"
)

// Source is everything a strategy needs to chunk one file. Text is the decoded
// UTF-8 content (empty for binary PDF/DOCX sources); Data is the raw bytes.
type Source struct {
	Root string
	Path string
	Ext  string
	Text string
	Data []byte
	Meta chunkMeta
	Opts Options
}

// Strategy chunks one source according to a format-specific rule.
type Strategy interface {
	Name() string
	Extensions() []string
	Chunk(Source) ([]index.Chunk, error)
}

// Registry maps file extensions (lower-cased) to strategies and falls back to a
// plain-text strategy for unknown extensions.
type Registry struct {
	byExt    map[string]Strategy
	fallback Strategy
}

// NewRegistry returns an empty registry whose unknown-extension fallback is the
// plain-text strategy.
func NewRegistry() *Registry {
	return &Registry{
		byExt:    make(map[string]Strategy),
		fallback: plainTextStrategy{},
	}
}

// Register binds a strategy to every extension it declares.
func (r *Registry) Register(s Strategy) {
	for _, ext := range s.Extensions() {
		r.byExt[strings.ToLower(ext)] = s
	}
}

// For resolves a strategy by extension, case-insensitively. Unknown
// extensions resolve to the plain-text fallback.
func (r *Registry) For(ext string) Strategy {
	if s, ok := r.byExt[strings.ToLower(ext)]; ok {
		return s
	}
	return r.fallback
}

// Strategies returns the distinct registered strategies sorted by name.
func (r *Registry) Strategies() []Strategy {
	seen := make(map[string]Strategy)
	for _, s := range r.byExt {
		seen[s.Name()] = s
	}
	out := make([]Strategy, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

var (
	autoOnce  sync.Once
	autoReg   *Registry
	fixedOnce sync.Once
	fixedReg  *Registry
)

// autoRegistry is the per-extension strategy mapping used by StrategyAuto.
// Strategies are stateless, so a single registry is built once and only read.
func autoRegistry() *Registry {
	autoOnce.Do(func() {
		r := NewRegistry()
		r.Register(markdownStrategy{})
		r.Register(plainTextStrategy{})
		r.Register(csvStrategy{})
		r.Register(htmlStrategy{})
		r.Register(pdfStrategy{})
		r.Register(docxStrategy{})
		autoReg = r
	})
	return autoReg
}

// fixedRegistry reproduces the legacy single-size dispatch for every supported
// extension. fixedStrategy declares all supported extensions itself.
func fixedRegistry() *Registry {
	fixedOnce.Do(func() {
		r := NewRegistry()
		r.Register(fixedStrategy{})
		fixedReg = r
	})
	return fixedReg
}

// sectionSize returns the rune cap for one structured section, falling back to
// ChunkSize when no positive SectionSize is configured.
func sectionSize(opts Options) int {
	if opts.SectionSize > 0 {
		return opts.SectionSize
	}
	return chunkSize(opts)
}

// withSize returns a copy of opts whose ChunkSize is set to size. Strategies
// that reuse legacy splitters (which read ChunkSize internally) call this to
// apply the section cap without mutating the caller's options.
func withSize(opts Options, size int) Options {
	opts.ChunkSize = size
	return opts
}

// StrategyTable describes the StrategyAuto mapping for docs and tests.
// SizeField names the option that caps a section for that strategy.
func StrategyTable() []struct {
	Extensions []string
	Strategy   string
	SizeField  string
} {
	type row = struct {
		Extensions []string
		Strategy   string
		SizeField  string
	}
	return []row{
		{markdownStrategy{}.Extensions(), markdownStrategy{}.Name(), "section_size"},
		{plainTextStrategy{}.Extensions(), plainTextStrategy{}.Name(), "chunk_size"},
		{csvStrategy{}.Extensions(), csvStrategy{}.Name(), "section_size"},
		{htmlStrategy{}.Extensions(), htmlStrategy{}.Name(), "section_size"},
		{pdfStrategy{}.Extensions(), pdfStrategy{}.Name(), "section_size"},
		{docxStrategy{}.Extensions(), docxStrategy{}.Name(), "section_size"},
	}
}
