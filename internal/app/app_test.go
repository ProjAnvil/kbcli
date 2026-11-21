package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/settings"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.APIKey = ""
	cfg.BaseURL = "http://127.0.0.1:1/v1" // unreachable on purpose; no network in tests
	cfg.TimeoutSeconds = 1
	return cfg
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDiscoverFindsIndexInCurrentDirOnly(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	indexPath := filepath.Join(root, cfg.IndexFileName)
	writeFile(t, indexPath, "not-a-real-index")

	if got := Discover(root, cfg); got != indexPath {
		t.Fatalf("Discover = %q, want %q", got, indexPath)
	}
	// A parent index must never be picked up from a subdirectory: that used
	// to silently attach a huge parent corpus to a small working directory.
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Discover(child, cfg); got != "" {
		t.Fatalf("Discover walked up to a parent: %q", got)
	}
	if got := Discover(t.TempDir(), cfg); got != "" {
		t.Fatalf("Discover on empty dir = %q, want empty", got)
	}
}

func TestDiscoverFindsLegacyFileName(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	legacy := filepath.Join(root, index.LegacyFileName)
	writeFile(t, legacy, "legacy-index")
	if got := Discover(root, cfg); got != legacy {
		t.Fatalf("Discover did not find the legacy name: %q, want %q", got, legacy)
	}
}

func TestConfigDefaultFileNameMatchesIndexPackage(t *testing.T) {
	if config.DefaultIndexFileName != index.DefaultFileName {
		t.Fatalf("config default %q != index default %q", config.DefaultIndexFileName, index.DefaultFileName)
	}
}

func TestResolveIndexPath(t *testing.T) {
	cfg := config.Default()
	explicit := filepath.Join(t.TempDir(), "custom.kbvec")
	if got := ResolveIndexPath("/tmp/root", explicit, cfg); got != explicit {
		t.Fatalf("explicit path = %q, want %q", got, explicit)
	}
	if got := ResolveIndexPath("/tmp/root", "", cfg); got != filepath.Join("/tmp/root", index.DefaultFileName) {
		t.Fatalf("default path = %q", got)
	}
}

func TestBuildThenSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "retry.md"), "# Retry\n\nThe client retries failed requests three times with exponential backoff.\n")
	writeFile(t, filepath.Join(root, "auth.md"), "# Auth\n\nTokens expire after one hour and must be refreshed.\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if a.Ready() {
		t.Fatal("Ready before Build")
	}
	n, err := a.Build(context.Background(), root, "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n < 2 {
		t.Fatalf("indexed %d chunks, want >= 2", n)
	}
	if !a.Ready() {
		t.Fatal("Ready after Build")
	}
	wantPath := filepath.Join(root, cfg.IndexFileName)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("vector file missing: %v", err)
	}

	hits, err := a.Search(context.Background(), "retry backoff", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	if hits[0].Rank != 1 {
		t.Fatalf("first hit rank = %d, want 1", hits[0].Rank)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("first hit score = %f, want > 0", hits[0].Score)
	}

	// A fresh session must auto-discover and load the same index.
	b, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open after Build: %v", err)
	}
	if !b.Ready() {
		t.Fatal("second session not ready")
	}
	if b.storeLen() != n {
		t.Fatalf("reloaded chunks = %d, want %d", b.storeLen(), n)
	}
}

func TestBuildWithProgressPhaseOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nalpha beta gamma\n")

	a, err := Open(testConfig(t), root, "")
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	if _, err := a.BuildWithProgress(context.Background(), root, "", func(p Progress) {
		phases = append(phases, p.Phase)
	}); err != nil {
		t.Fatalf("BuildWithProgress: %v", err)
	}
	if len(phases) == 0 {
		t.Fatal("no progress phases emitted")
	}
	if phases[0] != ProgressLoadingModel {
		t.Fatalf("first phase = %q, want %q (phases: %v)", phases[0], ProgressLoadingModel, phases)
	}
	if phases[len(phases)-1] != ProgressDone {
		t.Fatalf("last phase = %q, want %q (phases: %v)", phases[len(phases)-1], ProgressDone, phases)
	}
}

func TestSaveSettingsQueryOnlyDoesNotRequireReindex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nalpha beta gamma\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}
	draft := a.cfg.Clone()
	draft.TopK = 9
	res, err := a.SaveSettings(draft, settings.ModeGlobal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ReindexRequired || len(res.ChangedFields) != 0 {
		t.Fatalf("query-only top_k change must not require reindex: %+v", res)
	}
}

func TestUpdateIncremental(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	doc := filepath.Join(root, "doc.md")
	writeFile(t, doc, "# One\n\nalpha beta gamma\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Unchanged file: no work.
	changed, err := a.Update(context.Background(), root)
	if err != nil {
		t.Fatalf("Update unchanged: %v", err)
	}
	if changed != 0 {
		t.Fatalf("unchanged update reported %d changes, want 0", changed)
	}

	// Modified file plus a new file.
	writeFile(t, doc, "# One\n\nalpha beta gamma delta epsilon\n")
	writeFile(t, filepath.Join(root, "new.md"), "# Two\n\nzeta eta theta\n")
	changed, err = a.Update(context.Background(), root)
	if err != nil {
		t.Fatalf("Update changed: %v", err)
	}
	if changed < 2 {
		t.Fatalf("changed = %d, want >= 2", changed)
	}
	hits, err := a.Search(context.Background(), "zeta eta", 3)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if strings.Contains(h.Chunk.Body, "zeta") {
			found = true
		}
	}
	if !found {
		t.Fatal("new file content not found after update")
	}

	// Deleted file is dropped.
	if err := os.Remove(filepath.Join(root, "new.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Update(context.Background(), root); err != nil {
		t.Fatalf("Update after delete: %v", err)
	}
	for _, src := range a.Sources() {
		if strings.Contains(src, "new.md") {
			t.Fatal("deleted source still indexed")
		}
	}
}

func TestHandleCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\ncontent about caching\n")
	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}

	out, err := a.HandleCommand(":help")
	if err != nil || !strings.Contains(out, "reindex") {
		t.Fatalf("help = %q, %v", out, err)
	}
	out, err = a.HandleCommand("reindex")
	if err != nil || !strings.Contains(out, "reindex done") {
		t.Fatalf("reindex = %q, %v", out, err)
	}
	out, err = a.HandleCommand(":update " + filepath.Join(root, "a.md"))
	if err != nil || !strings.Contains(out, "update done") {
		t.Fatalf("update = %q, %v", out, err)
	}
	if _, err := a.HandleCommand(":nope"); err == nil {
		t.Fatal("unknown command must fail")
	}
}

func TestSearchWithoutIndexIsClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	cfg := testConfig(t)
	a, err := Open(cfg, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Search(context.Background(), "x", 1); err == nil || !strings.Contains(err.Error(), "kbcli index") {
		t.Fatalf("expected a clear error, got %v", err)
	}
}

func TestIndexSignatureStaleDetection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nalpha beta gamma caching retries\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// A freshly loaded index was built with the current signature.
	b, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open after Build: %v", err)
	}
	if stale, _ := b.IndexStale(); stale {
		t.Fatal("index should not be stale right after build")
	}

	// Changing an index-affecting field makes it stale and names the field.
	b.mu.Lock()
	b.cfg.ChunkSize = 300
	b.mu.Unlock()
	stale, fields := b.IndexStale()
	if !stale {
		t.Fatal("index should be stale after changing chunk_size")
	}
	if !slices.Contains(fields, "chunk_size") {
		t.Fatalf("stale fields = %v, want chunk_size", fields)
	}
}

// fakeCounterEmbedder implements both Embedder and TokenCounter so a test can
// prove build() wires the counter into ingest.Options without a model download.
type fakeCounterEmbedder struct{}

func (fakeCounterEmbedder) Dim() int        { return embed.Dim }
func (fakeCounterEmbedder) Version() string { return "fake-counter-v1" }
func (fakeCounterEmbedder) Embed(string) ([]float32, error) {
	return make([]float32, embed.Dim), nil
}
func (fakeCounterEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, embed.Dim)
	}
	return out, nil
}
func (fakeCounterEmbedder) Close() error                      { return nil }
func (fakeCounterEmbedder) CountForEmbedding(text string) int { return len([]rune(text)) }
func (fakeCounterEmbedder) MaxTokens() int                    { return 64 }

func TestBuildWithLocalEmbedderKeepsRuneChunking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "long.md"), "# A\n\n"+strings.Repeat("a", 1500)+"\n")

	cfg := testConfig(t)
	cfg.ChunkSize = 600
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := embed.TokenCounterOf(a.emb); ok {
		t.Fatal("local embedder must not implement TokenCounter")
	}
	chunks := a.store.Chunks()
	if len(chunks) < 2 {
		t.Fatalf("rune chunking produced %d chunks, want >= 2", len(chunks))
	}
	for _, c := range chunks {
		if n := len([]rune(c.Body)); n > 600 {
			t.Errorf("local chunk body has %d runes, want <= 600", n)
		}
	}
}

func TestBuildUsesInjectedTokenCounter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "long.md"), "# A\n\n"+strings.Repeat("a", 200)+"\n")

	cfg := testConfig(t)
	cfg.ChunkSize = 200
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	a.emb = fakeCounterEmbedder{} // injected; no model download
	n, err := a.Build(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if n <= 1 {
		t.Fatalf("injected counter did not cap chunks: %d chunks (rune chunking would give 1)", n)
	}
	for _, c := range a.store.Chunks() {
		text := c.Title + "\n" + c.Heading + "\n" + c.Body
		if got := len([]rune(text)); got > 48 { // MaxTokens(64) - 16
			t.Errorf("chunk text has %d runes, budget is 48: %q", got, c.Body)
		}
	}
}

func TestWorkspaceOverrideAppliesOnOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	key := filepath.Clean(root)

	settingsPath, err := config.WorkspacePath()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, settingsPath, fmt.Sprintf(
		`{"version":1,"workspaces":{%q:{"mode":"workspace","overrides":{"top_k":9}}}}`, key))

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if a.cfg.TopK != 9 {
		t.Fatalf("effective top_k = %d, want 9 (workspace override)", a.cfg.TopK)
	}
	if a.global.TopK == 9 {
		t.Fatal("global config was mutated by the workspace override")
	}
	if cfg.TopK == 9 {
		t.Fatal("passed-in global config was mutated")
	}
}

func TestSaveSettingsGlobal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nalpha beta gamma\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}

	draft := a.cfg.Clone()
	draft.ChunkSize = 300
	res, err := a.SaveSettings(draft, settings.ModeGlobal, nil)
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if !res.ReindexRequired {
		t.Fatal("expected ReindexRequired after chunk_size change")
	}
	if !slices.Contains(res.ChangedFields, "chunk_size") {
		t.Fatalf("changed fields = %v, want chunk_size", res.ChangedFields)
	}
	if !strings.Contains(res.Message, "run :reindex") {
		t.Fatalf("message missing reindex hint: %q", res.Message)
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.ChunkSize != 300 {
		t.Fatalf("config.yaml chunk_size = %d, want 300", onDisk.ChunkSize)
	}
}

func TestSaveSettingsWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nalpha beta gamma\n")

	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}

	draft := a.cfg.Clone()
	draft.TopK = 9
	draft.ChunkSize = 300
	marks := map[string]bool{"top_k": true, "chunk_size": false}
	if _, err := a.SaveSettings(draft, settings.ModeWorkspace, marks); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	st, err := settings.Open()
	if err != nil {
		t.Fatal(err)
	}
	ws := st.Get(root)
	if ws.Mode != settings.ModeWorkspace {
		t.Fatalf("mode = %s, want workspace", ws.Mode)
	}
	v, ok := ws.Overrides["top_k"]
	if !ok {
		t.Fatal("marked top_k override was not persisted")
	}
	if fmt.Sprint(v) != "9" {
		t.Fatalf("top_k override = %v, want 9", v)
	}
	if _, ok := ws.Overrides["chunk_size"]; ok {
		t.Fatal("unmarked chunk_size was persisted")
	}

	// Workspace mode must not touch the global config.yaml.
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("config.yaml must not be written in workspace mode")
	}
	if cfg.TopK == 9 {
		t.Fatal("global config was mutated")
	}
}
