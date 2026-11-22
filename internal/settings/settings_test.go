package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
)

// prepareHome points $HOME at a fresh temp dir and returns the expected
// settings.json path for that home.
func prepareHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := config.WorkspacePath()
	if err != nil {
		t.Fatalf("config.WorkspacePath: %v", err)
	}
	return path
}

func writeRawSettings(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func mustOpen(t *testing.T) Store {
	t.Helper()
	s, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpenMissingFileDefaults(t *testing.T) {
	path := prepareHome(t)
	s := mustOpen(t)
	if s.Path() != path {
		t.Fatalf("Path() = %q, want %q", s.Path(), path)
	}
	f := s.File()
	if f.Version != Version {
		t.Fatalf("Version = %d, want %d", f.Version, Version)
	}
	if f.Workspaces == nil {
		t.Fatal("Workspaces must be initialized")
	}
	if len(f.Workspaces) != 0 {
		t.Fatalf("Workspaces should be empty, got %d", len(f.Workspaces))
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open must not create the file, stat err = %v", err)
	}
	ws := s.Get("/some/missing/root")
	if ws.Mode != ModeGlobal {
		t.Fatalf("missing root mode = %q, want %q", ws.Mode, ModeGlobal)
	}
	if ws.Overrides == nil || len(ws.Overrides) != 0 {
		t.Fatalf("missing root overrides = %#v, want empty non-nil map", ws.Overrides)
	}
	if len(s.File().Workspaces) != 0 {
		t.Fatal("Get on a missing root must not insert a record")
	}
}

func TestSetGetRoundtrip(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/workspaces/demo"
	overrides := map[string]any{"top_k": float64(9), "rerank_enabled": true}
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: overrides, IndexPath: "/idx/demo.kbvec"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := s.Get(root)
	if got.Mode != ModeWorkspace {
		t.Fatalf("mode = %q, want %q", got.Mode, ModeWorkspace)
	}
	if got.IndexPath != "/idx/demo.kbvec" {
		t.Fatalf("index path = %q", got.IndexPath)
	}
	if got.UpdatedAt == "" {
		t.Fatal("UpdatedAt must be filled")
	}
	if _, err := time.Parse(time.RFC3339, got.UpdatedAt); err != nil {
		t.Fatalf("UpdatedAt %q is not RFC3339: %v", got.UpdatedAt, err)
	}
	if v, ok := got.Overrides["top_k"].(float64); !ok || v != 9 {
		t.Fatalf("stored top_k override = %#v", got.Overrides["top_k"])
	}

	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("persisted file is not valid JSON: %v", err)
	}
	if f.Version != Version {
		t.Fatalf("persisted version = %d, want %d", f.Version, Version)
	}
	if _, ok := f.Workspaces[normalizeRoot(root)]; !ok {
		t.Fatalf("persisted workspaces missing normalized key: %#v", f.Workspaces)
	}
	leftovers, _ := filepath.Glob(s.Path() + ".tmp-*")
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestSetDefaultMode(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/default-mode"
	if err := s.Set(root, Workspace{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get(root); got.Mode != ModeGlobal {
		t.Fatalf("empty mode must default to %q, got %q", ModeGlobal, got.Mode)
	}
	if err := s.Set(root, Workspace{Mode: "sideways"}); err == nil {
		t.Fatal("Set with an invalid mode must fail")
	}
}

func TestOverridableKeys(t *testing.T) {
	got := OverridableKeys()
	want := config.FieldKeys()
	if len(got) != len(want) {
		t.Fatalf("OverridableKeys len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OverridableKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveGlobalModeIgnoresOverrides(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/global"
	if err := s.Set(root, Workspace{Mode: ModeGlobal, Overrides: map[string]any{"top_k": float64(42)}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	global := config.Default()
	resolved, err := s.Resolve(global, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.TopK != global.TopK {
		t.Fatalf("global mode must ignore stored overrides, top_k = %d, want %d", resolved.TopK, global.TopK)
	}
	if resolved == global {
		t.Fatal("Resolve must not return the global pointer")
	}
	if global.TopK != 6 {
		t.Fatal("Resolve mutated the caller's global config")
	}
}

func TestResolveWorkspaceModeAppliesSparseOverrides(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/sparse"
	overrides := map[string]any{
		"top_k":            float64(9),
		"rerank_enabled":   true,
		"retrieval_method": "bm25",
	}
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: overrides}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	global := config.Default()
	beforeChunkSize := global.ChunkSize
	resolved, err := s.Resolve(global, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.TopK != 9 || !resolved.RerankEnabled || resolved.RetrievalMethod != "bm25" {
		t.Fatalf("overrides not applied: top_k=%d rerank=%v retrieval=%q", resolved.TopK, resolved.RerankEnabled, resolved.RetrievalMethod)
	}
	if resolved.ChunkSize != beforeChunkSize {
		t.Fatalf("sparse override changed an unrelated field: chunk_size %d -> %d", beforeChunkSize, resolved.ChunkSize)
	}
	if global.TopK != 6 || !global.RerankEnabled || global.RetrievalMethod != "vector" {
		t.Fatal("Resolve mutated the caller's global config")
	}
}

func TestResolveAppliesZeroValueOverride(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/zero"
	// hybrid_vector_weight = 0 is a legal deliberate value; the global default
	// is 0.5, so a zero override must not be replaced by a default afterwards.
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"hybrid_vector_weight": float64(0)}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	resolved, err := s.Resolve(config.Default(), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HybridVectorWeight != 0 {
		t.Fatalf("deliberate zero override was not applied, hybrid_vector_weight = %g", resolved.HybridVectorWeight)
	}
}

func TestApplyUnknownKeyError(t *testing.T) {
	err := Apply(config.Default(), map[string]any{"no_such_key": "x"})
	if err == nil || !strings.Contains(err.Error(), "no_such_key") {
		t.Fatalf("expected unknown key error, got %v", err)
	}
}

func TestApplyWrongTypeError(t *testing.T) {
	err := Apply(config.Default(), map[string]any{"top_k": []any{1, 2}})
	if err == nil {
		t.Fatal("expected wrong-type error")
	}
	if !strings.Contains(err.Error(), "top_k") || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected error naming key and type, got %v", err)
	}
}

func TestApplyInvalidEnumError(t *testing.T) {
	err := Apply(config.Default(), map[string]any{"retrieval_method": "telepathy"})
	if err == nil || !strings.Contains(err.Error(), "retrieval_method") {
		t.Fatalf("expected invalid enum error, got %v", err)
	}
}

func TestApplyBoolAndJSONNumber(t *testing.T) {
	cfg := config.Default()
	overrides := map[string]any{
		"rerank_enabled":       true,
		"hybrid_vector_weight": json.Number("0.25"),
	}
	if err := Apply(cfg, overrides); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !cfg.RerankEnabled || cfg.HybridVectorWeight != 0.25 {
		t.Fatalf("bool/json.Number overrides not applied: rerank=%v weight=%g", cfg.RerankEnabled, cfg.HybridVectorWeight)
	}
}

func TestResolveReturnsOverrideErrors(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/bad-override"
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"top_k": map[string]any{"bad": true}}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := s.Resolve(config.Default(), root); err == nil || !strings.Contains(err.Error(), "top_k") {
		t.Fatalf("expected descriptive override error from Resolve, got %v", err)
	}
}

func TestResolveLoadedFromDisk(t *testing.T) {
	path := prepareHome(t)
	root := "/kb/ws/on-disk"
	// Numbers written as JSON decode to float64 inside the Overrides map.
	raw := fmt.Sprintf(`{"version":1,"workspaces":{%q:{"mode":"workspace","overrides":{"top_k":9,"hybrid_vector_weight":0.25}}}}`, root)
	writeRawSettings(t, path, []byte(raw))
	s := mustOpen(t)
	resolved, err := s.Resolve(config.Default(), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.TopK != 9 || resolved.HybridVectorWeight != 0.25 {
		t.Fatalf("overrides from disk not applied: top_k=%d weight=%g", resolved.TopK, resolved.HybridVectorWeight)
	}
}

func TestDelete(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/delete"
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"top_k": float64(3)}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete(root); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := s.Get(root); got.Mode != ModeGlobal {
		t.Fatalf("after Delete, mode = %q, want %q", got.Mode, ModeGlobal)
	}
	if len(s.File().Workspaces) != 0 {
		t.Fatal("Delete left a record behind")
	}
	if err := s.Delete(root); err != nil {
		t.Fatalf("deleting an absent root must be a no-op: %v", err)
	}
}

func TestOverrideKeys(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/keys"
	overrides := map[string]any{"top_k": float64(5), "model": "gpt-4o"}
	// Keys are reported even in global mode where they are inactive.
	if err := s.Set(root, Workspace{Mode: ModeGlobal, Overrides: overrides}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	keys := s.OverrideKeys(root)
	if !keys["top_k"] || !keys["model"] || len(keys) != 2 {
		t.Fatalf("OverrideKeys = %#v", keys)
	}
	if len(s.OverrideKeys("/kb/ws/missing")) != 0 {
		t.Fatal("a missing root must report no override keys")
	}
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: overrides}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if keys = s.OverrideKeys(root); !keys["top_k"] {
		t.Fatalf("OverrideKeys in workspace mode = %#v", keys)
	}
}

func TestGetNormalizesRoot(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	if err := s.Set("/kb/ws/norm/../norm", Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"model": "x"}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("/kb/ws/norm"); got.Mode != ModeWorkspace {
		t.Fatalf("equivalent path did not resolve to the same record, mode = %q", got.Mode)
	}
	if len(s.File().Workspaces) != 1 {
		t.Fatalf("normalization created duplicate keys: %#v", s.File().Workspaces)
	}
}

func TestDefensiveCopies(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/copy"
	nested := map[string]any{"inner": float64(1)}
	if err := s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"top_k": float64(7), "obj": nested}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The caller's input map must not be shared with the store.
	nested["inner"] = float64(999)
	storedNested := s.Get(root).Overrides["obj"].(map[string]any)
	if v := storedNested["inner"]; v != float64(1) {
		t.Fatal("Set must copy the input overrides (nested map was shared)")
	}

	// Get returns an independent copy, including nested containers.
	g := s.Get(root)
	g.Overrides["top_k"] = float64(123)
	g.Overrides["new"] = "x"
	if v := s.Get(root).Overrides["top_k"]; v != float64(7) {
		t.Fatal("mutating a Get result's Overrides changed the store")
	}
	if _, ok := s.Get(root).Overrides["new"]; ok {
		t.Fatal("adding a key to a Get result leaked into the store")
	}
	copiedNested := s.Get(root).Overrides["obj"].(map[string]any)
	copiedNested["inner"] = float64(555)
	storedNested = s.Get(root).Overrides["obj"].(map[string]any)
	if v := storedNested["inner"]; v != float64(1) {
		t.Fatal("mutating a nested value through a Get result changed the store")
	}

	// File returns an independent copy.
	f := s.File()
	fws := f.Workspaces[root]
	fws.Overrides["top_k"] = float64(321)
	if v := s.Get(root).Overrides["top_k"]; v != float64(7) {
		t.Fatal("mutating a File result's Overrides changed the store")
	}
	delete(f.Workspaces, root)
	if _, ok := s.File().Workspaces[root]; !ok {
		t.Fatal("deleting a key from a File result changed the store")
	}

	// List returns an independent copy.
	l := s.List()
	lws := l[root]
	lws.Overrides["top_k"] = float64(456)
	if v := s.Get(root).Overrides["top_k"]; v != float64(7) {
		t.Fatal("mutating a List result's Overrides changed the store")
	}
	delete(l, root)
	if _, ok := s.File().Workspaces[root]; !ok {
		t.Fatal("deleting a key from a List result changed the store")
	}
}

func TestConcurrentSetResolveGet(t *testing.T) {
	prepareHome(t)
	s := mustOpen(t)
	root := "/kb/ws/concurrent"
	global := config.Default()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Set(root, Workspace{Mode: ModeWorkspace, Overrides: map[string]any{"top_k": float64(i%20 + 1)}})
			_ = s.Get(root)
			_, _ = s.Resolve(global, root)
			_ = s.File()
			_ = s.List()
			_ = s.OverrideKeys(root)
		}(i)
	}
	wg.Wait()
	if _, ok := s.File().Workspaces[normalizeRoot(root)]; !ok {
		t.Fatal("concurrent Set lost the record")
	}
}

func TestCorruptJSONError(t *testing.T) {
	path := prepareHome(t)
	writeRawSettings(t, path, []byte("{this is not valid json"))
	if _, err := Open(); err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
}

func TestVersionMismatchError(t *testing.T) {
	path := prepareHome(t)
	writeRawSettings(t, path, []byte(`{"version":999,"workspaces":{}}`))
	_, err := Open()
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected a version mismatch error, got %v", err)
	}
}
