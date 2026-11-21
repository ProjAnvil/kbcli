package index

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/projanvil/kbcli/internal/embed"
)

// tmpSeq makes each atomic-write temp file unique across concurrent saves.
var tmpSeq atomic.Uint64

// addBatchSize is the number of texts passed to EmbedBatch at a time.
const addBatchSize = 32

// store is the in-memory vector index. All mutable state is guarded by mu.
// The embedder is never called while mu is held: Add embeds before locking and
// Search snapshots before embedding (see search.go).
type store struct {
	mu              sync.RWMutex
	chunks          []Chunk
	byID            map[string]int
	sources         map[string]FileState
	root            string
	embedderVersion string
	buildSignature  string
	revision        uint64
	emb             embed.Embedder
}

func newStore(emb embed.Embedder, root string) Store {
	return &store{
		byID:            make(map[string]int),
		sources:         make(map[string]FileState),
		root:            root,
		embedderVersion: emb.Version(),
		emb:             emb,
	}
}

func loadStore(path string, emb embed.Embedder) (Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("index: open %s: %w", path, err)
	}
	defer f.Close()

	// A single buffered reader is shared by the header parser and the gob
	// decoder so both observe the exact same byte stream position.
	br := bufio.NewReader(f)
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("index: read header of %s: %w", path, err)
	}

	// Sentia retrieval-domain isolation: vectors from a different embedder
	// version or width are meaningless under L2 distance and must be rebuilt.
	// A differing build signature is intentionally NOT rejected here: whether
	// a stale index needs rebuilding is decided at the app layer.
	if h.Dim != uint32(emb.Dim()) {
		return nil, fmt.Errorf("index: %s stores %d-dimensional vectors but embedder uses %d; run 'kbcli reindex'", path, h.Dim, emb.Dim())
	}
	if h.EmbedderVersion != emb.Version() {
		return nil, fmt.Errorf("index was built with embedder %q but config uses %q; run 'kbcli reindex'", h.EmbedderVersion, emb.Version())
	}

	payload, err := decodePayload(br)
	if err != nil {
		return nil, fmt.Errorf("index: decode %s: %w", path, err)
	}

	// A loaded store already holds a populated snapshot. Starting the revision
	// at 1 (rather than the zero value) ensures consumers whose cached revision
	// starts at 0 rebuild their initial snapshot from these chunks.
	s := &store{
		chunks:          payload.Chunks,
		byID:            make(map[string]int, len(payload.Chunks)),
		sources:         make(map[string]FileState, len(payload.Sources)),
		root:            h.Root,
		embedderVersion: h.EmbedderVersion,
		buildSignature:  h.BuildSignature,
		revision:        1,
		emb:             emb,
	}
	if s.chunks == nil {
		s.chunks = []Chunk{}
	}
	for i := range s.chunks {
		s.byID[s.chunks[i].ID] = i
	}
	for _, st := range payload.Sources {
		s.sources[st.Path] = st
	}
	return s, nil
}

// Add embeds chunk.Text in batches and replaces chunks that share an ID. The
// embedder is called before the lock is taken so slow embedding never blocks
// concurrent readers.
func (s *store) Add(chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].Text
	}
	dim := s.emb.Dim()

	vectors := make([][]float32, len(chunks))
	for start := 0; start < len(texts); start += addBatchSize {
		end := start + addBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := s.emb.EmbedBatch(texts[start:end])
		if err != nil {
			return fmt.Errorf("index: embed batch [%d:%d]: %w", start, end, err)
		}
		if len(batch) != end-start {
			return fmt.Errorf("index: embed batch [%d:%d]: got %d vectors, want %d", start, end, len(batch), end-start)
		}
		for i, v := range batch {
			if len(v) != dim {
				return fmt.Errorf("index: embed batch [%d:%d]: vector %d has dim %d, want %d", start, end, i, len(v), dim)
			}
			// Own a private copy: some embedders reuse an internal buffer.
			vectors[start+i] = append([]float32(nil), v...)
		}
	}

	// Build the replacement records (with defensive metadata copies) before
	// taking the lock.
	prepared := make([]Chunk, len(chunks))
	for i := range chunks {
		prepared[i] = cloneChunk(chunks[i])
		prepared[i].Vector = vectors[i]
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range prepared {
		if pos, ok := s.byID[c.ID]; ok {
			s.chunks[pos] = c // replace in place, ID map stays valid
		} else {
			s.byID[c.ID] = len(s.chunks)
			s.chunks = append(s.chunks, c)
		}
	}
	// len(prepared) > 0 (empty adds returned earlier), so every call adds or
	// replaces at least one chunk: the chunk set changed.
	s.revision++
	return nil
}

// Save atomically writes the whole index to path. It writes to a temp file in
// the same directory, syncs and closes it, then renames over path. On any
// error the temp file is removed.
func (s *store) Save(path string) error {
	dim := s.emb.Dim()
	s.mu.RLock()
	// Vector backing arrays are immutable once stored, so a shallow copy of
	// each Chunk is enough for a consistent snapshot without holding the lock
	// during disk I/O.
	chunks := make([]Chunk, len(s.chunks))
	copy(chunks, s.chunks)
	sources := make([]FileState, 0, len(s.sources))
	for _, st := range s.sources {
		sources = append(sources, st)
	}
	root := s.root
	ev := s.embedderVersion
	sig := s.buildSignature
	s.mu.RUnlock()

	// Map iteration is randomized; sort for byte-stable output across saves.
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("index: create directory %s: %w", dir, err)
	}

	h := fileHeader{
		Version:         FileVersion,
		Dim:             uint32(dim),
		EmbedderVersion: ev,
		Root:            root,
		BuildSignature:  sig,
	}
	payload := diskPayload{Chunks: chunks, Sources: sources}

	tmp := fmt.Sprintf("%s.tmp-%d-%d", path, os.Getpid(), tmpSeq.Add(1))
	if err := writeTemp(tmp, h, payload); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("index: rename %s to %s: %w", tmp, path, err)
	}
	return nil
}

// writeTemp creates a fresh 0600 file, writes header + payload, syncs and
// closes it. It fully owns the file lifecycle so Save only handles rename.
func writeTemp(tmp string, h fileHeader, p diskPayload) error {
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("index: create temp file %s: %w", tmp, err)
	}
	if err := writeVectorFile(f, h, p); err != nil {
		f.Close()
		return fmt.Errorf("index: write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("index: sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("index: close %s: %w", tmp, err)
	}
	return nil
}

func (s *store) Sources() map[string]FileState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]FileState, len(s.sources))
	for k, v := range s.sources {
		out[k] = v
	}
	return out
}

func (s *store) SetSource(state FileState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[state.Path] = state // FileState is a value: stored as a copy
}

// DropSource removes every chunk belonging to path and its state. The
// revision is bumped only when at least one chunk was actually removed; a
// no-op drop (path had no chunks) leaves the chunk set unchanged.
func (s *store) DropSource(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sources, path)
	kept := make([]Chunk, 0, len(s.chunks))
	changed := false
	for _, c := range s.chunks {
		if c.Source == path {
			changed = true
			continue
		}
		kept = append(kept, c)
	}
	if !changed {
		return
	}
	s.chunks = kept
	// Positions shifted, so rebuild the id -> position map from scratch.
	s.byID = make(map[string]int, len(s.chunks))
	for i := range s.chunks {
		s.byID[s.chunks[i].ID] = i
	}
	s.revision++
}

func (s *store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks)
}

func (s *store) EmbedderVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embedderVersion
}

func (s *store) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root
}

// Chunks returns a defensive deep copy of every indexed chunk. Sparse
// retrieval builds its BM25 index from this snapshot. Cloning each Vector
// slice and Metadata map under the read lock means callers can freely mutate
// the result without racing with or corrupting the store.
func (s *store) Chunks() []Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Chunk, len(s.chunks))
	for i := range s.chunks {
		out[i] = cloneChunk(s.chunks[i])
	}
	return out
}

// Revision reports how many times the chunk set has changed, allowing
// snapshot consumers (e.g. a BM25 index) to detect staleness cheaply.
func (s *store) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// BuildSignature returns the config.IndexSignature recorded for this index.
// Legacy version-1 files yield an empty string.
func (s *store) BuildSignature() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildSignature
}

// SetBuildSignature records the signature written by Save. It deliberately
// does NOT bump the revision: the signature is build metadata, not a change
// to the chunk set, so snapshot consumers need not rebuild because of it.
func (s *store) SetBuildSignature(sig string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildSignature = sig
}

// cloneChunk returns a defensive deep copy of c, including its metadata map
// and vector slice, so callers cannot mutate the store through a value.
func cloneChunk(c Chunk) Chunk {
	out := c
	if c.Metadata != nil {
		m := make(map[string]string, len(c.Metadata))
		for k, v := range c.Metadata {
			m[k] = v
		}
		out.Metadata = m
	}
	if c.Vector != nil {
		out.Vector = append([]float32(nil), c.Vector...)
	}
	return out
}
