// Package index defines the on-disk vector index and its retrieval contract.
package index

import "github.com/projanvil/kbcli/internal/embed"

const (
	// FileMagic guards the first bytes of a kbcli vector file.
	FileMagic = "KBCLI\x00"
	// FileVersion is the current serialization version. Version 1 has no
	// build signature; version 2 adds it. Readers accept both.
	FileVersion = 2
	// LegacyFileVersion is the original header layout without a signature.
	LegacyFileVersion = 1
	// DefaultFileName is the vector file created in an indexed directory.
	DefaultFileName = ".kbvec"
	// LegacyFileName is the original double-dot name. Discover still finds
	// these files so existing indexes keep working; new builds use
	// DefaultFileName.
	LegacyFileName = ".kbcli.kbvec"
)

// Chunk is one indexed passage. Text is the exact string passed to the
// embedder; Body is what the TUI shows as evidence.
type Chunk struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Body     string            `json:"body"`
	Source   string            `json:"source"`
	Title    string            `json:"title"`
	Heading  string            `json:"heading"`
	Page     int               `json:"page"`
	Ordinal  int               `json:"ordinal"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Vector   []float32         `json:"vector"`
}

// FileState records one source file so update can skip unchanged files.
type FileState struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	SHA256  string `json:"sha256"`
	Chunks  int    `json:"chunks"`
}

// Hit is a ranked retrieval result. Score is 1/(1+dist): higher is better.
type Hit struct {
	Rank  int     `json:"rank"`
	Score float64 `json:"score"`
	Dist  float64 `json:"dist"`
	Chunk Chunk   `json:"chunk"`
}

// Store is the vector index contract. Implementations must be safe for
// concurrent Search calls because the TUI reads while indexing may run.
type Store interface {
	// Add embeds and appends chunks. Chunks with duplicate IDs are replaced.
	Add(chunks []Chunk) error
	// Search returns at most k hits ordered by descending score.
	Search(query string, k int) ([]Hit, error)
	// Save writes the whole index to path atomically.
	Save(path string) error
	// Sources returns a copy of the tracked file states.
	Sources() map[string]FileState
	// SetSource records a file state.
	SetSource(state FileState)
	// DropSource removes every chunk belonging to path and its state.
	DropSource(path string)
	// Len returns the number of indexed chunks.
	Len() int
	// EmbedderVersion reports the retrieval domain of stored vectors.
	EmbedderVersion() string
	// Root returns the directory paths inside this index are relative to.
	Root() string
	// Chunks returns a defensive copy of every indexed chunk. Sparse
	// retrieval builds its BM25 index from this snapshot.
	Chunks() []Chunk
	// Revision increments whenever the chunk set changes.
	Revision() uint64
	// BuildSignature is the config.IndexSignature recorded when the index was
	// built. Legacy version-1 files return an empty string.
	BuildSignature() string
	// SetBuildSignature records the signature to be written by Save.
	SetBuildSignature(sig string)
}

// New creates an empty index rooted at root. Implemented in store.go.
func New(emb embed.Embedder, root string) Store {
	return newStore(emb, root)
}

// Load reads a kbcli vector file. Implemented in store.go.
func Load(path string, emb embed.Embedder) (Store, error) {
	return loadStore(path, emb)
}
