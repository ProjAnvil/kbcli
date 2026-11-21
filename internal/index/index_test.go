package index

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeEmbedder is a deterministic in-test Embedder. An optional vectors map
// pins exact vectors for chosen texts; every other text gets a stable,
// dim-length fallback vector. It is safe for concurrent use.
type fakeEmbedder struct {
	mu      sync.Mutex
	dim     int
	version string
	vectors map[string][]float32
}

func newFakeEmbedder(dim int, version string, vectors map[string][]float32) *fakeEmbedder {
	if vectors == nil {
		vectors = map[string][]float32{}
	}
	return &fakeEmbedder{dim: dim, version: version, vectors: vectors}
}

func (f *fakeEmbedder) Dim() int        { return f.dim }
func (f *fakeEmbedder) Version() string { return f.version }
func (f *fakeEmbedder) Close() error    { return nil }

func (f *fakeEmbedder) Embed(s string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vectorFor(s), nil
}

func (f *fakeEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vectorFor(t)
	}
	return out, nil
}

// vectorFor requires f.mu to be held.
func (f *fakeEmbedder) vectorFor(s string) []float32 {
	if v, ok := f.vectors[s]; ok {
		return append([]float32(nil), v...)
	}
	v := make([]float32, f.dim)
	for i := range v {
		v[i] = float32((len(s)+i)%7) / 7.0
	}
	return v
}

func equalVec(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "index.kbvec") // exercises MkdirAll
	root := "/some/root"

	emb := newFakeEmbedder(4, "test-v1", map[string][]float32{
		"alpha": {1, 0, 0, 0},
		"beta":  {0, 1, 0, 0},
	})
	s := New(emb, root).(*store)

	chunks := []Chunk{
		{ID: "c1", Text: "alpha", Body: "body one", Source: "a.md", Title: "A", Heading: "H1", Page: 2, Ordinal: 0, Metadata: map[string]string{"k": "v"}},
		{ID: "c2", Text: "beta", Body: "body two", Source: "b.md", Title: "B", Page: 5, Ordinal: 1},
	}
	if err := s.Add(chunks); err != nil {
		t.Fatal(err)
	}
	s.SetSource(FileState{Path: "a.md", Size: 10, ModTime: 123, SHA256: "aaa", Chunks: 1})
	s.SetSource(FileState{Path: "b.md", Size: 20, ModTime: 456, SHA256: "bbb", Chunks: 1})

	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	// A fresh embedder with matching dim/version must load the file.
	loaded, err := Load(path, newFakeEmbedder(4, "test-v1", nil))
	if err != nil {
		t.Fatal(err)
	}
	ls := loaded.(*store)

	if ls.Root() != root {
		t.Errorf("root: want %q, got %q", root, ls.Root())
	}
	if ls.EmbedderVersion() != "test-v1" {
		t.Errorf("version: want test-v1, got %q", ls.EmbedderVersion())
	}
	if ls.Len() != 2 {
		t.Fatalf("len: want 2, got %d", ls.Len())
	}

	want := []Chunk{
		{ID: "c1", Text: "alpha", Body: "body one", Source: "a.md", Title: "A", Heading: "H1", Page: 2, Ordinal: 0, Metadata: map[string]string{"k": "v"}, Vector: []float32{1, 0, 0, 0}},
		{ID: "c2", Text: "beta", Body: "body two", Source: "b.md", Title: "B", Page: 5, Ordinal: 1, Vector: []float32{0, 1, 0, 0}},
	}
	if !reflect.DeepEqual(want, ls.chunks) {
		t.Errorf("chunks mismatch:\n want %+v\n got  %+v", want, ls.chunks)
	}
	for i := range want {
		if !equalVec(want[i].Vector, ls.chunks[i].Vector) {
			t.Errorf("chunk %d vector: want %v, got %v", i, want[i].Vector, ls.chunks[i].Vector)
		}
	}

	srcs := loaded.Sources()
	if len(srcs) != 2 {
		t.Fatalf("sources: want 2, got %d", len(srcs))
	}
	wantA := FileState{Path: "a.md", Size: 10, ModTime: 123, SHA256: "aaa", Chunks: 1}
	if srcs["a.md"] != wantA {
		t.Errorf("a.md state: want %+v, got %+v", wantA, srcs["a.md"])
	}
	// byID must be rebuilt on load so later replacement works.
	if pos, ok := ls.byID["c2"]; !ok || pos != 1 {
		t.Errorf("byID inconsistent: pos=%d ok=%v", pos, ok)
	}
}

func TestStoreSaveLoadEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.kbvec")
	emb := newFakeEmbedder(3, "v1", nil)
	if err := New(emb, "/r").Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, newFakeEmbedder(3, "v1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 0 {
		t.Errorf("empty store len: want 0, got %d", loaded.Len())
	}
	if len(loaded.Sources()) != 0 {
		t.Error("empty store should have no sources")
	}
}

func TestLoadRejectsMismatchedEmbedders(t *testing.T) {
	cases := []struct {
		name        string
		buildEmb    *fakeEmbedder
		loadEmb     *fakeEmbedder
		wantSubstrs []string
	}{
		{
			name:        "embedder version mismatch",
			buildEmb:    newFakeEmbedder(3, "old-v1", nil),
			loadEmb:     newFakeEmbedder(3, "new-v2", nil),
			wantSubstrs: []string{"old-v1", "new-v2", "run 'kbcli reindex'"},
		},
		{
			name:        "dim mismatch",
			buildEmb:    newFakeEmbedder(3, "same", nil),
			loadEmb:     newFakeEmbedder(4, "same", nil),
			wantSubstrs: []string{"3-dimensional", "4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := saveValidIndex(t, tc.buildEmb)
			_, err := Load(path, tc.loadEmb)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, sub := range tc.wantSubstrs {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain %q", err.Error(), sub)
				}
			}
		})
	}
}

func TestLoadRejectsCorruptFiles(t *testing.T) {
	emb := newFakeEmbedder(3, "v1", nil)

	t.Run("bad magic", func(t *testing.T) {
		path := saveValidIndex(t, emb)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data[0] = 'X'
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, emb); err == nil || !strings.Contains(err.Error(), "magic") {
			t.Fatalf("want bad magic error, got %v", err)
		}
	})

	t.Run("file version mismatch", func(t *testing.T) {
		path := saveValidIndex(t, emb)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint32(data[6:10], uint32(FileVersion+1))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, emb); err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("want version error, got %v", err)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		full, err := os.ReadFile(saveValidIndex(t, emb))
		if err != nil {
			t.Fatal(err)
		}
		lengths := []int{0, 3, 6, 8, 14, 20, len(full) - 1}
		for _, n := range lengths {
			dir := t.TempDir()
			p := filepath.Join(dir, "trunc.kbvec")
			if err := os.WriteFile(p, full[:n], 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p, emb); err == nil {
				t.Errorf("truncated to %d bytes: expected error, got nil", n)
			}
		}
	})
}

// saveValidIndex writes a small, loadable index and returns its path.
func saveValidIndex(t *testing.T, emb *fakeEmbedder) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v.kbvec")
	s := New(emb, "/root")
	if err := s.Add([]Chunk{
		{ID: "a", Text: "alpha", Body: "A"},
		{ID: "b", Text: "beta", Body: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetSource(FileState{Path: "a.md", Size: 1})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStoreAddDeduplicatesAndReplaces(t *testing.T) {
	s := New(newFakeEmbedder(3, "v1", nil), "/r").(*store)

	// nil and empty adds are no-ops.
	if err := s.Add(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Add([]Chunk{}); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Fatalf("empty add changed len: %d", s.Len())
	}

	if err := s.Add([]Chunk{
		{ID: "a", Text: "alpha", Body: "A1"},
		{ID: "b", Text: "beta", Body: "B1"},
	}); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 2 {
		t.Fatalf("want 2, got %d", s.Len())
	}

	// Replace b, add c, and include a duplicate id in the same batch (last wins).
	if err := s.Add([]Chunk{
		{ID: "b", Text: "beta", Body: "B2"},
		{ID: "c", Text: "gamma", Body: "C1"},
		{ID: "c", Text: "gamma", Body: "C2"},
	}); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 3 {
		t.Fatalf("want 3 after replace+add, got %d", s.Len())
	}

	byBody := map[string]string{}
	for i, c := range s.chunks {
		byBody[c.ID] = c.Body
		if pos, ok := s.byID[c.ID]; !ok || pos != i || s.chunks[pos].ID != c.ID {
			t.Fatalf("byID inconsistent for %q (pos=%d ok=%v)", c.ID, pos, ok)
		}
	}
	if byBody["a"] != "A1" || byBody["b"] != "B2" || byBody["c"] != "C2" {
		t.Fatalf("unexpected bodies: %v", byBody)
	}
}

func TestStoreDropSource(t *testing.T) {
	s := New(newFakeEmbedder(3, "v1", nil), "/r").(*store)
	if err := s.Add([]Chunk{
		{ID: "x1", Text: "t1", Source: "x.md"},
		{ID: "y1", Text: "t2", Source: "y.md"},
		{ID: "x2", Text: "t3", Source: "x.md"},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetSource(FileState{Path: "x.md", Chunks: 2})
	s.SetSource(FileState{Path: "y.md", Chunks: 1})

	s.DropSource("x.md")

	if s.Len() != 1 || s.chunks[0].ID != "y1" {
		t.Fatalf("after drop: want only y1, got %+v", s.chunks)
	}
	srcs := s.Sources()
	if _, ok := srcs["x.md"]; ok {
		t.Error("x.md state should be removed")
	}
	if _, ok := srcs["y.md"]; !ok {
		t.Error("y.md state should remain")
	}
	// Sources() must be a defensive copy.
	delete(srcs, "y.md")
	if _, ok := s.Sources()["y.md"]; !ok {
		t.Error("Sources() leaked the internal map")
	}

	// byID was rebuilt: replacing the surviving chunk must still work.
	if err := s.Add([]Chunk{{ID: "y1", Text: "t2b", Body: "replaced", Source: "y.md"}}); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 || s.chunks[0].Body != "replaced" {
		t.Fatalf("replacement after DropSource failed: %+v", s.chunks)
	}
	if pos, ok := s.byID["y1"]; !ok || pos != 0 {
		t.Error("byID wrong after rebuild + replace")
	}
}

func TestStoreSearchRanking(t *testing.T) {
	emb := newFakeEmbedder(3, "v1", map[string][]float32{
		"q": {0, 0, 0},
		"a": {0, 0, 0}, // dist 0
		"b": {1, 0, 0}, // dist 1
		"d": {0, 2, 0}, // dist 2
		"c": {3, 4, 0}, // dist 5
	})
	s := New(emb, "/r").(*store)
	if err := s.Add([]Chunk{
		{ID: "d", Text: "d", Body: "D"},
		{ID: "a", Text: "a", Body: "A"},
		{ID: "c", Text: "c", Body: "C"},
		{ID: "b", Text: "b", Body: "B"},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.Search("q", 0) // k <= 0 -> default 6, but only 4 chunks
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"a", "b", "d", "c"}
	if len(hits) != len(wantOrder) {
		t.Fatalf("want %d hits, got %d", len(wantOrder), len(hits))
	}
	for i, h := range hits {
		if h.Chunk.ID != wantOrder[i] {
			t.Errorf("rank %d: want %s, got %s", i+1, wantOrder[i], h.Chunk.ID)
		}
		if h.Rank != i+1 {
			t.Errorf("rank field: want %d, got %d", i+1, h.Rank)
		}
		if h.Score != 1.0/(1.0+h.Dist) {
			t.Errorf("score != 1/(1+dist): score=%f dist=%f", h.Score, h.Dist)
		}
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Error("score is not monotonically non-increasing")
		}
		if hits[i].Dist < hits[i-1].Dist {
			t.Error("dist is not monotonically non-decreasing")
		}
	}

	top2, err := s.Search("q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(top2) != 2 || top2[0].Chunk.ID != "a" || top2[1].Chunk.ID != "b" {
		t.Errorf("top-2 truncation wrong: %+v", top2)
	}

	// Hit.Chunk.Vector must be a defensive copy (not aliased with the store).
	var stored *Chunk
	s.mu.RLock()
	for i := range s.chunks {
		if s.chunks[i].ID == "a" {
			stored = &s.chunks[i]
		}
	}
	s.mu.RUnlock()
	if len(hits[0].Chunk.Vector) > 0 && len(stored.Vector) > 0 &&
		&hits[0].Chunk.Vector[0] == &stored.Vector[0] {
		t.Error("Hit.Chunk.Vector aliases the stored vector (not defensive)")
	}
}

func TestSearchSkipsMismatchedVectorDim(t *testing.T) {
	emb := newFakeEmbedder(3, "v1", map[string][]float32{
		"q": {0, 0, 0},
		"a": {0, 0, 0},
		"b": {1, 0, 0},
	})
	s := New(emb, "/r").(*store)
	if err := s.Add([]Chunk{{ID: "a", Text: "a"}, {ID: "b", Text: "b"}}); err != nil {
		t.Fatal(err)
	}
	// Inject a chunk whose vector has the wrong width; Search must skip it.
	s.mu.Lock()
	s.chunks[0].Vector = []float32{1, 2}
	s.mu.Unlock()

	hits, err := s.Search("q", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "b" {
		t.Fatalf("expected only chunk b, got %+v", hits)
	}
}

func TestSearchEmptyIndexAndEmptyQuery(t *testing.T) {
	s := New(newFakeEmbedder(3, "v1", nil), "/r")

	hits, err := s.Search("anything", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty index: want 0 hits, got %d", len(hits))
	}

	if err := s.Add([]Chunk{{ID: "a", Text: "a"}}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.Search("", 5) // nil/empty query must not panic or error
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("empty query: want 1 hit, got %d", len(hits))
	}
}

func TestStoreConcurrentSearchAndAdd(t *testing.T) {
	s := New(newFakeEmbedder(8, "v1", nil), "/r")
	seed := make([]Chunk, 10)
	for i := range seed {
		seed[i] = Chunk{ID: fmt.Sprintf("seed-%d", i), Text: fmt.Sprintf("seed text %d", i)}
	}
	if err := s.Add(seed); err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	const iters = 50
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iters)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if g%2 == 0 {
					c := Chunk{ID: fmt.Sprintf("g%d-%d", g, i), Text: fmt.Sprintf("text %d %d", g, i)}
					if err := s.Add([]Chunk{c}); err != nil {
						errCh <- err
						return
					}
				} else {
					if _, err := s.Search("query", 5); err != nil {
						errCh <- err
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}
}

// writeRawIndexFile writes a header and gob payload directly, bypassing Save.
// It lets tests construct legacy (version-1) or unsupported-version files that
// Save itself would never produce.
func writeRawIndexFile(t *testing.T, path string, h fileHeader, p diskPayload) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHeader(f, h); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := encodePayload(f, p); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadBuildSignatureRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sig.kbvec")
	root := "/sig/root"
	sig := "chunk_size=600|chunk_overlap=0|embedder=local|embed_model="

	emb := newFakeEmbedder(4, "test-v1", map[string][]float32{
		"alpha": {1, 0, 0, 0},
		"beta":  {0, 1, 0, 0},
	})
	s := New(emb, root).(*store)
	if err := s.Add([]Chunk{
		{ID: "c1", Text: "alpha", Body: "body one", Source: "a.md", Metadata: map[string]string{"k": "v"}},
		{ID: "c2", Text: "beta", Body: "body two", Source: "b.md"},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetBuildSignature(sig)
	s.SetSource(FileState{Path: "a.md", Size: 1})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path, newFakeEmbedder(4, "test-v1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.BuildSignature(); got != sig {
		t.Errorf("BuildSignature: want %q, got %q", sig, got)
	}
	if got := loaded.Root(); got != root {
		t.Errorf("Root: want %q, got %q", root, got)
	}
	if loaded.Len() != 2 {
		t.Fatalf("Len: want 2, got %d", loaded.Len())
	}
	want := []Chunk{
		{ID: "c1", Text: "alpha", Body: "body one", Source: "a.md", Metadata: map[string]string{"k": "v"}, Vector: []float32{1, 0, 0, 0}},
		{ID: "c2", Text: "beta", Body: "body two", Source: "b.md", Vector: []float32{0, 1, 0, 0}},
	}
	ls := loaded.(*store)
	if !reflect.DeepEqual(want, ls.chunks) {
		t.Errorf("chunks did not survive the round-trip:\n want %+v\n got  %+v", want, ls.chunks)
	}
}

func TestLegacyVersion1FileLoadsWithEmptySignature(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.kbvec")
	emb := newFakeEmbedder(3, "v1", nil)

	// Build a normal store so the payload carries real embedded vectors.
	s := New(emb, "/legacy/root").(*store)
	if err := s.Add([]Chunk{
		{ID: "c1", Text: "alpha", Body: "A", Source: "a.md", Metadata: map[string]string{"k": "v"}},
		{ID: "c2", Text: "beta", Body: "B", Source: "b.md"},
	}); err != nil {
		t.Fatal(err)
	}

	// Manually write a legacy version-1 header (no build signature) followed
	// by the gob payload. writeHeader must omit the signature field for v1 so
	// the payload immediately follows the root bytes.
	h := fileHeader{
		Version:         LegacyFileVersion,
		Dim:             uint32(emb.Dim()),
		EmbedderVersion: emb.Version(),
		Root:            "/legacy/root",
	}
	p := diskPayload{
		Chunks:  s.Chunks(),
		Sources: []FileState{{Path: "a.md", Size: 1}},
	}
	writeRawIndexFile(t, path, h, p)

	loaded, err := Load(path, newFakeEmbedder(3, "v1", nil))
	if err != nil {
		t.Fatalf("load legacy version-1 file: %v", err)
	}
	if got := loaded.BuildSignature(); got != "" {
		t.Errorf("legacy BuildSignature: want empty, got %q", got)
	}
	if got := loaded.Root(); got != "/legacy/root" {
		t.Errorf("legacy Root: want /legacy/root, got %q", got)
	}
	if loaded.Len() != 2 {
		t.Fatalf("legacy Len: want 2, got %d", loaded.Len())
	}

	// The critical assertion: readHeader must NOT have consumed the first
	// four bytes of the gob payload as a signature length. If it had, the
	// decode would fail or the chunks would be corrupted. Vectors and
	// metadata must therefore still be intact.
	got := loaded.Chunks()
	if got[0].Metadata["k"] != "v" {
		t.Errorf("legacy metadata lost: %+v", got[0].Metadata)
	}
	if len(got[0].Vector) != 3 || len(got[1].Vector) != 3 {
		t.Errorf("legacy vectors lost: %+v", got)
	}
	if len(loaded.Sources()) != 1 {
		t.Errorf("legacy sources lost: %+v", loaded.Sources())
	}
}

func TestPeekBuildSignature(t *testing.T) {
	emb := newFakeEmbedder(3, "v1", nil)

	t.Run("version 2", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "v2.kbvec")
		sig := "chunk_size=600|chunk_overlap=0|embedder=local|embed_model="
		s := New(emb, "/r")
		if err := s.Add([]Chunk{{ID: "a", Text: "a", Body: "A"}}); err != nil {
			t.Fatal(err)
		}
		s.SetBuildSignature(sig)
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		got, err := PeekBuildSignature(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != sig {
			t.Errorf("PeekBuildSignature: want %q, got %q", sig, got)
		}
	})

	t.Run("version 1 empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "v1.kbvec")
		h := fileHeader{
			Version:         LegacyFileVersion,
			Dim:             uint32(emb.Dim()),
			EmbedderVersion: emb.Version(),
			Root:            "/r",
		}
		p := diskPayload{Chunks: []Chunk{{ID: "a", Text: "a", Body: "A", Vector: []float32{1, 2, 3}}}}
		writeRawIndexFile(t, path, h, p)
		got, err := PeekBuildSignature(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("PeekBuildSignature v1: want empty, got %q", got)
		}
	})
}

func TestChunksDefensiveCopy(t *testing.T) {
	s := New(newFakeEmbedder(3, "v1", nil), "/r").(*store)
	if err := s.Add([]Chunk{
		{ID: "a", Text: "alpha", Body: "A", Metadata: map[string]string{"k": "v"}},
		{ID: "b", Text: "beta", Body: "B"},
	}); err != nil {
		t.Fatal(err)
	}

	pristine := s.Chunks()
	// Capture original values in independent memory so an aliasing bug cannot
	// make the "before" and "after" snapshots change together unnoticed.
	origVec0 := append([]float32(nil), pristine[0].Vector...)
	origMeta0 := map[string]string{}
	for k, v := range pristine[0].Metadata {
		origMeta0[k] = v
	}
	origID0 := pristine[0].ID
	origBody1 := pristine[1].Body

	mutated := s.Chunks()
	mutated[0].ID = "mutated"
	mutated[0].Vector[0] = 999
	mutated[0].Metadata["k"] = "changed"
	mutated[0].Metadata["extra"] = "added"
	mutated[1].Body = "mutated"
	mutated = append(mutated, Chunk{ID: "c"})

	after := s.Chunks()
	if len(after) != 2 {
		t.Fatalf("store slice length changed: want 2, got %d", len(after))
	}
	if after[0].ID != origID0 {
		t.Errorf("store ID changed through returned slice: want %q, got %q", origID0, after[0].ID)
	}
	if !equalVec(after[0].Vector, origVec0) {
		t.Errorf("store Vector changed through returned slice: want %v, got %v", origVec0, after[0].Vector)
	}
	if !reflect.DeepEqual(after[0].Metadata, origMeta0) {
		t.Errorf("store Metadata changed through returned slice: want %v, got %v", origMeta0, after[0].Metadata)
	}
	if after[1].Body != origBody1 {
		t.Errorf("store Body changed through returned slice: want %q, got %q", origBody1, after[1].Body)
	}
}

func TestRevisionIncrements(t *testing.T) {
	s := New(newFakeEmbedder(3, "v1", nil), "/r").(*store)

	// Sources() is a read and SetSource only touches file states; neither
	// changes the chunk set, so the revision must stay put.
	base := s.Revision()
	_ = s.Sources()
	if got := s.Revision(); got != base {
		t.Errorf("Sources() bumped revision: %d -> %d", base, got)
	}
	s.SetSource(FileState{Path: "x.md", Chunks: 1})
	if got := s.Revision(); got != base {
		t.Errorf("SetSource bumped revision: %d -> %d", base, got)
	}

	// Adding new chunks bumps.
	if err := s.Add([]Chunk{
		{ID: "x1", Text: "t1", Source: "x.md"},
		{ID: "y1", Text: "t2", Source: "y.md"},
	}); err != nil {
		t.Fatal(err)
	}
	afterAdd := s.Revision()
	if afterAdd <= base {
		t.Errorf("Add did not bump revision: %d -> %d", base, afterAdd)
	}

	// Dropping a source that owns chunks bumps.
	s.DropSource("x.md")
	afterDrop := s.Revision()
	if afterDrop <= afterAdd {
		t.Errorf("DropSource did not bump revision: %d -> %d", afterAdd, afterDrop)
	}

	// Re-adding an existing ID (replacement) bumps.
	if err := s.Add([]Chunk{{ID: "y1", Text: "t2b", Body: "replaced", Source: "y.md"}}); err != nil {
		t.Fatal(err)
	}
	afterReplace := s.Revision()
	if afterReplace <= afterDrop {
		t.Errorf("replacement Add did not bump revision: %d -> %d", afterDrop, afterReplace)
	}

	// A no-op drop (path owns no chunks) does not bump.
	s.DropSource("nonexistent.md")
	if got := s.Revision(); got != afterReplace {
		t.Errorf("no-op DropSource bumped revision: %d -> %d", afterReplace, got)
	}

	// SetBuildSignature is metadata, not a chunk change: no bump.
	s.SetBuildSignature("chunk_size=600|chunk_overlap=0|embedder=local|embed_model=")
	if got := s.Revision(); got != afterReplace {
		t.Errorf("SetBuildSignature bumped revision: %d -> %d", afterReplace, got)
	}
}

func TestUnsupportedFileVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.kbvec")
	emb := newFakeEmbedder(3, "v1", nil)

	// A future, unsupported version. writeHeader emits the signature field for
	// any version >= 2, but readHeader must reject version 3 before decoding
	// anything, with a clear error.
	h := fileHeader{
		Version:         3,
		Dim:             uint32(emb.Dim()),
		EmbedderVersion: emb.Version(),
		Root:            "/r",
		BuildSignature:  "whatever",
	}
	p := diskPayload{Chunks: []Chunk{{ID: "a", Text: "a", Body: "A", Vector: []float32{1, 2, 3}}}}
	writeRawIndexFile(t, path, h, p)

	if _, err := Load(path, emb); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Load of version-3 file: want a version error, got %v", err)
	}
	if _, err := PeekBuildSignature(path); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("PeekBuildSignature of version-3 file: want a version error, got %v", err)
	}
}
