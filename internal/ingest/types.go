// Package ingest turns files in a document directory into indexable chunks.
//
// Markdown is split with the remote langchain-golang Markdown splitters. PDF
// and DOCX extraction follows the Sentia backend: row-ordered PDF text and a
// dependency-free ZIP/XML DOCX reader that emits Markdown.
package ingest

import (
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// Options controls directory walking and chunk sizes.
type Options struct {
	ChunkSize    int
	ChunkOverlap int
	IncludeExt   []string
	ExcludeDir   []string
	MaxFileBytes int64
	MaxPDFPages  int
	// TokenCounter, when non-nil, caps chunk bodies by the embedder's real
	// token limit. The local hashing embedder leaves it nil and keeps the
	// existing rune-based chunking.
	TokenCounter embed.TokenCounter
}

// Result is the chunking output for a single file.
type Result struct {
	State  index.FileState
	Chunks []index.Chunk
}

// DefaultOptions returns Sentia-compatible defaults. Implemented in walk.go.
func DefaultOptions() Options {
	return defaultOptions()
}

// Walk lists supported files under root, skipping generated directories.
// Paths are absolute. Implemented in walk.go.
func Walk(root string, opts Options) ([]string, error) {
	return walk(root, opts)
}

// ChunkFile parses one file and returns its chunks plus its file state.
// Implemented in dispatch.go.
func ChunkFile(root, path string, opts Options) (Result, error) {
	return chunkFile(root, path, opts)
}

// FileState computes the hash and metadata for one file without parsing it.
// Implemented in walk.go.
func FileStateFor(root, path string) (index.FileState, error) {
	return fileStateFor(root, path)
}

// Supports reports whether the extension is handled by this package.
// Implemented in dispatch.go.
func Supports(path string) bool {
	return supports(path)
}
