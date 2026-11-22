// Package app wires ingestion, indexing, retrieval, the agent and the TUI
// into one application object. It owns no UI concerns.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/i18n"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/ingest"
	"github.com/ProjAnvil/kbcli/internal/rerankmodel"
	"github.com/ProjAnvil/kbcli/internal/retrieval"
	"github.com/ProjAnvil/kbcli/internal/scan"
	"github.com/ProjAnvil/kbcli/internal/session"
	"github.com/ProjAnvil/kbcli/internal/settings"
	"github.com/ProjAnvil/kbcli/internal/tui"
)

// ensureRerankModel is the download/copy hook. It is a package variable so
// tests can inject a fake without touching the network.
var ensureRerankModel = rerankmodel.Ensure

// App holds the state of one kbcli session.
//
// global is the config.yaml configuration; cfg is the effective configuration
// for the current workspace (global plus any sparse workspace overrides). The
// settings store keeps the workspace-to-configuration relation on disk.
type App struct {
	global    *config.Config
	cfg       *config.Config
	settings  settings.Store
	root      string
	indexPath string
	version   string

	mu       sync.RWMutex
	emb      embed.Embedder
	store    index.Store
	runner   agent.Answerer
	searcher retrieval.Retriever
	sess     *session.Store
	sink     agent.Sink
}

// Open prepares a session. A missing index is not an error: the TUI can still
// start and tell the user to run `kbcli index`.
func Open(cfg *config.Config, root, indexPath string) (*App, error) {
	a, err := OpenFresh(cfg, root)
	if err != nil {
		return nil, err
	}
	if indexPath == "" {
		indexPath = Discover(a.root, cfg)
	}
	a.indexPath = indexPath
	// No index here. The TUI shows a command hint; it never scans or builds
	// on startup. Indexing starts only after the user runs :index <dir>.
	if indexPath == "" {
		return a, nil
	}
	if _, err := os.Stat(indexPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a, nil
		}
		return nil, fmt.Errorf("app: stat index %s: %w", indexPath, err)
	}
	if err := a.load(); err != nil {
		return nil, err
	}
	return a, nil
}

// OpenFresh prepares a session without reading any existing vector file. Use
// it for full rebuilds: loading an old index whose embedder domain differs
// from the current config would be rejected, even though rebuilding is exactly
// what the user asked for.
func OpenFresh(cfg *config.Config, root string) (*App, error) {
	if cfg == nil {
		return nil, errors.New("app: nil config")
	}
	absRoot, err := ResolveRoot(root)
	if err != nil {
		return nil, err
	}
	st, err := settings.Open()
	if err != nil {
		return nil, fmt.Errorf("app: open settings: %w", err)
	}
	effective, err := st.Resolve(cfg, absRoot)
	if err != nil {
		return nil, fmt.Errorf("app: resolve workspace settings: %w", err)
	}
	return &App{
		global:   cfg,
		cfg:      effective,
		settings: st,
		sess:     openSession(effective, absRoot),
		root:     absRoot,
		version:  buildVersion(),
		sink:     agent.NewSink(),
	}, nil
}

// NewWindow builds a fresh runner and sink for TUI window id. The session
// file key derives from the workspace plus the id, so each window keeps its
// own turns and facts. Failures fall back to a runner without session memory.
func (a *App) NewWindow(id int) (agent.Answerer, agent.Sink) {
	a.mu.RLock()
	store, cfg, root, base := a.store, a.cfg, a.root, a.runner
	a.mu.RUnlock()
	// No index means there is no runner to clone. Stay silent: writing to
	// stderr here leaks into the alt-screen TUI and corrupts a rendered row.
	if store == nil || base == nil {
		return base, nil
	}
	sink := agent.NewSink()
	sess := openSessionKeyed(cfg, root, fmt.Sprintf("window-%d", id))
	runner, err := agent.NewWithSession(cfg, store, sink, sess)
	if err != nil {
		runner, err = agent.New(cfg, store, sink)
		if err != nil {
			return base, nil
		}
	}
	return runner, sink
}

// openSession attaches Porto-style per-workspace session memory. A broken
// session file must never prevent the app from starting, so failures degrade
// to in-memory-only chat with a stderr note.
func openSession(cfg *config.Config, root string) *session.Store {
	return openSessionKeyed(cfg, root, "")
}

// openSessionKeyed derives the session file key from root and key. An empty
// key keeps the original per-workspace session identity.
func openSessionKeyed(cfg *config.Config, root, key string) *session.Store {
	if !cfg.SessionEnabled {
		return nil
	}
	dir, err := config.SessionsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kbcli: session directory: %v\n", err)
		return nil
	}
	sessionKey := root
	if key != "" {
		sessionKey = root + "#" + key
	}
	st, err := session.Open(session.Options{
		Dir:                 dir,
		Workspace:           sessionKey,
		Lang:                i18n.Parse(cfg.Language),
		Enabled:             true,
		FactsEnabled:        cfg.FactsEnabled,
		MaxFactsPerCategory: cfg.FactsMaxPerCategory,
		SimilarityThreshold: cfg.FactsSimilarityThreshold,
		RecentContextTurns:  cfg.FactsRecentContextTurns,
		MemoryTopK:          cfg.SessionMemoryTopK,
		CompactAfterTurns:   cfg.CompactAfterTurns,
		Complete:            agent.NewCompletion(cfg),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kbcli: session memory unavailable: %v\n", err)
		return nil
	}
	return st
}

// Session returns the active session store, or nil when disabled.
func (a *App) Session() *session.Store {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sess
}

// managedRerankModel reports the model name and directory when the managed
// cross-encoder is in use, or two empty strings when it is not.
func (a *App) managedRerankModel() (string, string) {
	cfg := a.cfg
	if cfg == nil || !cfg.RerankEnabled || cfg.RerankType != config.RerankAikit || strings.TrimSpace(cfg.RerankModelPath) != "" {
		return "", ""
	}
	return cfg.RerankModel, config.ModelDir(cfg.RerankModel)
}

// NeedsRerankModel reports whether the managed cross-encoder is missing files.
// The TUI uses it to decide whether to open on the boot screen.
func (a *App) NeedsRerankModel() bool {
	name, dir := a.managedRerankModel()
	if name == "" {
		return false
	}
	return !rerankmodel.IsComplete(dir)
}

// EnsureRerankModel provisions the managed cross-encoder model when aikit
// reranking is enabled without an explicit model path. It reports whether any
// work was done.
func (a *App) EnsureRerankModel(ctx context.Context, onProgress func(rerankmodel.Progress)) (bool, error) {
	name, dir := a.managedRerankModel()
	if name == "" {
		return false, nil
	}
	return ensureRerankModel(ctx, name, dir, onProgress)
}

// IndexRoot reads the source directory recorded in a vector file header
// without loading its vectors.
func IndexRoot(path string) (string, error) {
	return index.PeekRoot(path)
}

// Discover locates the kbcli vector file. KBCLI_INDEX wins, then only the
// current directory is searched. Parent directories are deliberately not
// searched, to avoid attaching a large parent index to a small working
// directory. The legacy ".kbcli.kbvec" name is still accepted.
func Discover(root string, cfg *config.Config) string {
	if env := strings.TrimSpace(os.Getenv("KBCLI_INDEX")); env != "" {
		if p, err := filepath.Abs(env); err == nil {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	name := index.DefaultFileName
	if cfg != nil && cfg.IndexFileName != "" {
		name = cfg.IndexFileName
	}
	// Accept the original double-dot name so existing indexes keep working.
	// New builds write only DefaultFileName.
	names := []string{name}
	if name == index.DefaultFileName {
		names = append(names, index.LegacyFileName)
	}
	dir, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	// Only the current directory is searched. Walking up to parents used to
	// make subdirectories convenient, but it silently attached a huge parent
	// index (for example a home directory) to a small working directory.
	for _, candidateName := range names {
		candidate := filepath.Join(dir, candidateName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

// ResolveRoot turns an optional directory argument into an absolute path.
func ResolveRoot(arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("app: resolve working directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("app: resolve %q: %w", arg, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("app: stat %s: %w", abs, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("app: %s is not a directory", abs)
	}
	return abs, nil
}

// ResolveIndexPath returns the explicit output path or the default vector
// file inside root.
func ResolveIndexPath(root, out string, cfg *config.Config) string {
	if strings.TrimSpace(out) != "" {
		if abs, err := filepath.Abs(out); err == nil {
			return abs
		}
		return out
	}
	name := index.DefaultFileName
	if cfg != nil && cfg.IndexFileName != "" {
		name = cfg.IndexFileName
	}
	return filepath.Join(root, name)
}

// Ready reports whether an index and an agent are available.
func (a *App) Ready() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.store != nil && a.runner != nil
}

// Root returns the directory the index is relative to.
func (a *App) Root() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store != nil {
		return a.store.Root()
	}
	return a.root
}

// IndexPath returns the active vector file path.
func (a *App) IndexPath() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.indexPath
}

// Config returns the active settings.
func (a *App) Config() *config.Config {
	return a.cfg
}

// Progress phase values. LoadingModel is emitted before the embedder is
// created; with rembed this is where model weights are downloaded.
const (
	ProgressLoadingModel = "loading_model"
	ProgressScanning     = "scanning"
	ProgressIndexing     = "indexing"
	ProgressSaving       = "saving"
	ProgressDone         = "done"
)

// Progress is one indexing checkpoint. The TUI renders it under a spinner.
type Progress struct {
	Phase      string // ProgressLoadingModel | ProgressScanning | ProgressIndexing | ProgressSaving | ProgressDone
	FilesDone  int
	FilesTotal int
	Chunks     int
	Current    string
}

// Preflight runs a bounded, read-only scan before any indexing. It never reads
// file contents and never writes anything.
func Preflight(root string) (scan.Report, error) {
	abs, err := ResolveRoot(root)
	if err != nil {
		return scan.Report{}, err
	}
	return scan.Scan(abs, scan.DefaultLimits())
}

// Build performs a full index of root and writes the vector file to out.
func (a *App) Build(ctx context.Context, root, out string) (int, error) {
	return a.build(ctx, root, out, nil)
}

// BuildWithProgress is Build with a progress callback for the confirmation
// screen. A nil callback is allowed.
func (a *App) BuildWithProgress(ctx context.Context, root, out string, on func(Progress)) (int, error) {
	return a.build(ctx, root, out, on)
}

func (a *App) build(ctx context.Context, root, out string, on func(Progress)) (int, error) {
	report := func(p Progress) {
		if on != nil {
			on(p)
		}
	}
	absRoot, err := ResolveRoot(root)
	if err != nil {
		return 0, err
	}
	outPath := ResolveIndexPath(absRoot, out, a.cfg)
	// Loading may block on a model download (rembed). Report the phase before
	// it starts so the CLI/TUI can show a message; local hashing does not
	// download anything and behaves exactly as before.
	report(Progress{Phase: ProgressLoadingModel})
	emb, err := a.ensureEmbedder()
	if err != nil {
		return 0, err
	}
	opts := ingest.DefaultOptions()
	opts.ChunkSize = a.cfg.ChunkSize
	opts.ChunkOverlap = a.cfg.ChunkOverlap
	if tc, ok := embed.TokenCounterOf(emb); ok {
		opts.TokenCounter = tc
		fmt.Fprintf(os.Stderr, "kbcli: %s caps chunks at %d tokens; chunk_size=%d is bounded at split time\n", a.cfg.Embedder, tc.MaxTokens(), a.cfg.ChunkSize)
	}
	// Never index our own output file.
	opts.ExcludeDir = append(opts.ExcludeDir, filepath.Base(outPath))

	paths, err := ingest.Walk(absRoot, opts)
	if err != nil {
		return 0, fmt.Errorf("app: walk %s: %w", absRoot, err)
	}
	report(Progress{Phase: ProgressScanning, FilesTotal: len(paths)})
	store := index.New(emb, absRoot)
	for i, p := range paths {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		res, err := ingest.ChunkFile(absRoot, p, opts)
		if err != nil {
			return 0, fmt.Errorf("app: index %s: %w", p, err)
		}
		if err := store.Add(res.Chunks); err != nil {
			return 0, fmt.Errorf("app: embed %s: %w", p, err)
		}
		store.SetSource(res.State)
		report(Progress{
			Phase:      ProgressIndexing,
			FilesDone:  i + 1,
			FilesTotal: len(paths),
			Chunks:     store.Len(),
			Current:    filepath.Base(p),
		})
	}
	// Record the effective index-affecting settings in the file header so a
	// later session can detect a stale index.
	store.SetBuildSignature(a.cfg.IndexSignature())
	report(Progress{Phase: ProgressSaving, FilesDone: len(paths), FilesTotal: len(paths), Chunks: store.Len()})
	if err := store.Save(outPath); err != nil {
		return 0, fmt.Errorf("app: save index: %w", err)
	}
	// Remember which vector file belongs to this workspace. This is metadata
	// only: the index has already been written, so a settings write failure
	// must not misreport an otherwise successful build. Log it and carry on.
	ws := a.settings.Get(absRoot)
	ws.IndexPath = outPath
	if ws.Mode == "" {
		ws.Mode = settings.ModeGlobal
	}
	if err := a.settings.Set(absRoot, ws); err != nil {
		fmt.Fprintf(os.Stderr, "kbcli: could not record workspace settings: %v\n", err)
	}
	if err := a.install(absRoot, outPath, emb, store); err != nil {
		return 0, err
	}
	report(Progress{Phase: ProgressDone, FilesDone: len(paths), FilesTotal: len(paths), Chunks: store.Len()})
	return store.Len(), nil
}

// Reindex rebuilds the whole index from its recorded root.
func (a *App) Reindex(ctx context.Context) (int, error) {
	return a.ReindexWithProgress(ctx, nil)
}

// ReindexWithProgress is Reindex with the same progress stream as a first
// build. A nil callback is allowed.
func (a *App) ReindexWithProgress(ctx context.Context, on func(Progress)) (int, error) {
	root := a.Root()
	out := a.IndexPath()
	if out == "" {
		out = ResolveIndexPath(root, "", a.cfg)
	}
	return a.BuildWithProgress(ctx, root, out, on)
}

// Update incrementally re-indexes target, which may be a directory or one
// file. Unchanged files are skipped by SHA-256; deleted files are dropped.
func (a *App) Update(ctx context.Context, target string) (int, error) {
	a.mu.RLock()
	store := a.store
	a.mu.RUnlock()
	if store == nil {
		return 0, errors.New("no kbcli index found; run 'kbcli index <dir>' first")
	}
	root := store.Root()
	if dir, err := ResolveRoot(root); err == nil {
		root = dir
	}
	if strings.TrimSpace(target) == "" {
		target = root
	} else if !filepath.IsAbs(target) {
		// Source paths in the index are relative to the index root, so a
		// relative update target must be resolved against that same root,
		// not against whatever directory the process happens to run in.
		target = filepath.Join(root, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return 0, fmt.Errorf("app: resolve %q: %w", target, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return 0, fmt.Errorf("app: stat %s: %w", abs, err)
	}

	emb, err := a.ensureEmbedder()
	if err != nil {
		return 0, err
	}
	opts := ingest.DefaultOptions()
	opts.ChunkSize = a.cfg.ChunkSize
	opts.ChunkOverlap = a.cfg.ChunkOverlap
	if tc, ok := embed.TokenCounterOf(emb); ok {
		opts.TokenCounter = tc
	}

	var paths []string
	if st.IsDir() {
		paths, err = ingest.Walk(abs, opts)
		if err != nil {
			return 0, fmt.Errorf("app: walk %s: %w", abs, err)
		}
	} else {
		paths = []string{abs}
	}

	known := store.Sources()
	changed := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		state, err := ingest.FileStateFor(root, p)
		if err != nil {
			return changed, fmt.Errorf("app: stat source %s: %w", p, err)
		}
		if prev, ok := known[state.Path]; ok && prev.SHA256 == state.SHA256 && prev.Size == state.Size {
			delete(known, state.Path)
			continue
		}
		delete(known, state.Path)
		res, err := ingest.ChunkFile(root, p, opts)
		if err != nil {
			return changed, fmt.Errorf("app: index %s: %w", p, err)
		}
		store.DropSource(state.Path)
		if err := store.Add(res.Chunks); err != nil {
			return changed, fmt.Errorf("app: embed %s: %w", p, err)
		}
		store.SetSource(res.State)
		changed++
	}
	// Remaining known entries no longer exist on disk.
	for rel := range known {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return changed, fmt.Errorf("app: stat %s: %w", full, err)
		}
		store.DropSource(rel)
		changed++
	}
	out := a.IndexPath()
	if out == "" {
		out = ResolveIndexPath(root, "", a.cfg)
	}
	if err := store.Save(out); err != nil {
		return changed, fmt.Errorf("app: save index: %w", err)
	}
	return changed, nil
}

// Ask answers one question through the agent.
func (a *App) Ask(ctx context.Context, history []agent.Turn, question string) (string, []index.Hit, error) {
	a.mu.RLock()
	runner := a.runner
	a.mu.RUnlock()
	if runner == nil {
		return "", nil, errors.New("no kbcli index found; run 'kbcli index <dir>' first")
	}
	return runner.Ask(ctx, history, question)
}

// Search returns ranked evidence without invoking the LLM. It runs the same
// optimized retrieval pipeline as the chat TUI, so every query-time knob
// applies to `kbcli search` as well.
func (a *App) Search(ctx context.Context, query string, k int) ([]index.Hit, error) {
	a.mu.RLock()
	store := a.store
	searcher := a.searcher
	a.mu.RUnlock()
	if store == nil {
		return nil, errors.New("no kbcli index found; run 'kbcli index <dir>' first")
	}
	if searcher != nil {
		return searcher.Search(ctx, query, k)
	}
	return store.Search(query, k)
}

// IndexStale reports whether the loaded index was built with different
// index-affecting settings than the effective configuration. A legacy
// version-1 vector file (empty build signature) is never reported stale.
func (a *App) IndexStale() (bool, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil {
		return false, nil
	}
	changed := config.IndexSignatureChanges(a.store.BuildSignature(), a.cfg)
	return len(changed) > 0, changed
}

// LoadSettings returns an editable draft of the effective configuration plus
// the metadata the settings screen needs.
func (a *App) LoadSettings() (*config.Config, tui.SettingsInfo, error) {
	root := a.Root()
	stale, staleFields := a.IndexStale()
	a.mu.RLock()
	draft := a.cfg.Clone()
	global := a.global.Clone()
	a.mu.RUnlock()
	info := tui.SettingsInfo{
		Root:        root,
		Mode:        a.settings.Get(root).Mode,
		Overrides:   a.settings.OverrideKeys(root),
		Global:      global,
		FilePath:    a.settings.Path(),
		IndexStale:  stale,
		StaleFields: staleFields,
	}
	return draft, info, nil
}

// SaveSettings persists an edited configuration draft. In global mode the
// draft becomes the new config.yaml; in workspace mode only the marked fields
// are stored as sparse overrides. It returns whether a re-index is needed.
func (a *App) SaveSettings(draft *config.Config, mode string, overrides map[string]bool) (tui.ApplyResult, error) {
	var result tui.ApplyResult

	draft.Normalize()
	if err := draft.Validate(); err != nil {
		return result, fmt.Errorf("app: validate settings: %w", err)
	}

	before := a.cfg.Clone()
	changed := config.IndexAffectingChanges(before, draft)
	result.ReindexRequired = len(changed) > 0
	result.ChangedFields = changed

	if mode != settings.ModeGlobal && mode != settings.ModeWorkspace {
		return result, fmt.Errorf("app: invalid settings mode %q", mode)
	}

	root := a.Root()
	ws := a.settings.Get(root)
	ws.Mode = mode

	if mode == settings.ModeGlobal {
		if err := draft.Save(); err != nil {
			return result, fmt.Errorf("app: save global config: %w", err)
		}
		a.mu.Lock()
		a.global = draft.Clone()
		a.mu.Unlock()
		// Keep ws.Overrides: they are inactive in global mode but retained so
		// switching back to workspace mode restores the user's choices.
	} else {
		sparse := make(map[string]any)
		for key, marked := range overrides {
			if !marked {
				continue
			}
			v, err := draft.GetField(key)
			if err != nil {
				return result, fmt.Errorf("app: read override %s: %w", key, err)
			}
			sparse[key] = overrideValue(v)
		}
		ws.Overrides = sparse
	}

	if err := a.settings.Set(root, ws); err != nil {
		return result, fmt.Errorf("app: persist workspace settings: %w", err)
	}

	effective, err := a.settings.Resolve(a.global, root)
	if err != nil {
		return result, fmt.Errorf("app: resolve settings: %w", err)
	}

	// Install the effective config under the lock. ensureEmbedder and install
	// both take a.mu, so they must only run after the lock is released.
	a.mu.Lock()
	embedChanged := a.cfg.Embedder != effective.Embedder || a.cfg.EmbedModel != effective.EmbedModel
	sessionChanged := a.cfg.SessionEnabled != effective.SessionEnabled
	a.cfg = effective
	if embedChanged {
		a.emb = nil
	}
	lang := i18n.Parse(effective.Language)
	if sessionChanged {
		a.sess = openSession(effective, root)
	} else if a.sess != nil {
		a.sess.SetLang(lang)
	}
	store := a.store
	indexPath := a.indexPath
	a.mu.Unlock()

	// Never call ensureEmbedder or install while holding a.mu.
	if store != nil {
		emb, err := a.ensureEmbedder()
		if err != nil {
			return result, fmt.Errorf("app: rebuild embedder: %w", err)
		}
		if err := a.install(root, indexPath, emb, store); err != nil {
			return result, err
		}
	}

	if len(changed) > 0 {
		result.Message = fmt.Sprintf("settings saved; %s changed - run :reindex to rebuild the index", strings.Join(changed, ", "))
	} else {
		result.Message = "settings saved"
	}
	return result, nil
}

// overrideValue converts a typed config value into a type the settings store
// accepts. encoding/json yields string/float64/bool/json.Number, so a Go int
// must become float64: otherwise a Resolve on the same store instance would
// reject it as an unsupported type.
func overrideValue(v any) any {
	if n, ok := v.(int); ok {
		return float64(n)
	}
	return v
}

// TUI starts the two-pane chat interface.
func (a *App) TUI() error {
	a.mu.RLock()
	store, runner, sink, indexPath := a.store, a.runner, a.sink, a.indexPath
	a.mu.RUnlock()
	var ensureModel func(func(tui.ModelProgress)) error
	if a.NeedsRerankModel() {
		ensureModel = func(progress func(tui.ModelProgress)) error {
			_, err := a.EnsureRerankModel(context.Background(), func(p rerankmodel.Progress) {
				progress(tui.ModelProgress{
					Phase:      p.Phase,
					File:       p.File,
					Downloaded: p.Downloaded,
					Total:      p.Total,
					FilesDone:  p.FilesDone,
					FilesTotal: p.FilesTotal,
				})
			})
			return err
		}
	}
	return tui.Run(tui.Options{
		Config:      a.cfg,
		Store:       store,
		Runner:      runner,
		Sink:        sink,
		IndexPath:   indexPath,
		OnCommand:   a.HandleCommand,
		EnsureModel: ensureModel,
		NewWindow:   a.NewWindow,
		Refresh: func() (index.Store, agent.Answerer) {
			a.mu.RLock()
			defer a.mu.RUnlock()
			return a.store, a.runner
		},
		LoadSettings: a.LoadSettings,
		SaveSettings: a.SaveSettings,
		Preflight:    Preflight,
		RunIndex: func(root string, progress func(tui.IndexProgress)) (int, error) {
			out := ResolveIndexPath(root, "", a.cfg)
			return a.BuildWithProgress(context.Background(), root, out, func(p Progress) {
				progress(tui.IndexProgress{
					Phase:      p.Phase,
					FilesDone:  p.FilesDone,
					FilesTotal: p.FilesTotal,
					Chunks:     p.Chunks,
					Current:    p.Current,
				})
			})
		},
		ReindexWithProgress: func(progress func(tui.IndexProgress)) (int, error) {
			return a.ReindexWithProgress(context.Background(), func(p Progress) {
				progress(tui.IndexProgress{
					Phase:      p.Phase,
					FilesDone:  p.FilesDone,
					FilesTotal: p.FilesTotal,
					Chunks:     p.Chunks,
					Current:    p.Current,
				})
			})
		},
		WorkspaceRoot: a.root,
		Version:       a.version,
	})
}

// HandleCommand implements the in-TUI command language.
func (a *App) HandleCommand(line string) (string, error) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":"))
	if line == "" {
		return "", nil
	}
	fields := strings.Fields(line)
	switch strings.ToLower(fields[0]) {
	case "help":
		return "commands: :reindex | :update [path] | :settings | :stale | :clear | :q | :help", nil
	case "clear":
		return "", nil
	case "settings", "config", "index":
		// The TUI opens these screens locally; these aliases only reach this
		// handler as a fallback, so return a hint instead of an error.
		return "settings are managed in the :settings screen", nil
	case "stale":
		stale, changed := a.IndexStale()
		if stale {
			return fmt.Sprintf("index is STALE - %s changed; run :reindex", strings.Join(changed, ", ")), nil
		}
		return "index is up to date", nil
	case "reindex":
		start := time.Now()
		n, err := a.Reindex(context.Background())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("reindex done: %d chunks in %s -> %s", n, time.Since(start).Round(time.Millisecond), a.IndexPath()), nil
	case "update":
		target := ""
		if len(fields) > 1 {
			target = strings.Join(fields[1:], " ")
		}
		start := time.Now()
		n, err := a.Update(context.Background(), target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("update done: %d files changed in %s (index: %d chunks)", n, time.Since(start).Round(time.Millisecond), a.storeLen()), nil
	default:
		return "", fmt.Errorf("unknown command %q (try :help)", fields[0])
	}
}

func (a *App) storeLen() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil {
		return 0
	}
	return a.store.Len()
}

func (a *App) load() error {
	emb, err := a.ensureEmbedder()
	if err != nil {
		return err
	}
	store, err := index.Load(a.indexPath, emb)
	if err != nil {
		return fmt.Errorf("app: load index %s: %w", a.indexPath, err)
	}
	return a.install(store.Root(), a.indexPath, emb, store)
}

func (a *App) ensureEmbedder() (embed.Embedder, error) {
	a.mu.RLock()
	emb := a.emb
	a.mu.RUnlock()
	if emb != nil {
		return emb, nil
	}
	built, err := embed.New(a.cfg.Embedder, a.cfg.EmbedModel)
	if err != nil {
		return nil, fmt.Errorf("app: create embedder: %w", err)
	}
	a.mu.Lock()
	if a.emb == nil {
		a.emb = built
	} else {
		_ = built.Close()
	}
	emb = a.emb
	a.mu.Unlock()
	return emb, nil
}

func (a *App) install(root, indexPath string, emb embed.Embedder, store index.Store) error {
	runner, err := agent.NewWithSession(a.cfg, store, a.sink, a.sess)
	if err != nil {
		return fmt.Errorf("app: create agent: %w", err)
	}
	searcher, err := retrieval.New(retrieval.Options{
		Config:   a.cfg,
		Store:    store,
		Complete: agent.NewCompletion(a.cfg),
	})
	if err != nil {
		return fmt.Errorf("app: create retriever: %w", err)
	}
	a.mu.Lock()
	a.root = root
	a.indexPath = indexPath
	a.emb = emb
	a.store = store
	a.runner = runner
	a.searcher = searcher
	a.mu.Unlock()
	return nil
}

// Sources returns sorted relative source paths, useful for diagnostics.
func (a *App) Sources() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil {
		return nil
	}
	m := a.store.Sources()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}
