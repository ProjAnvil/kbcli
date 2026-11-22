package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// stubSetupProbe replaces the network probe for one test and restores it.
func stubSetupProbe(t *testing.T, fn func(string, time.Duration) error) {
	t.Helper()
	old := setupProbeEndpoint
	setupProbeEndpoint = fn
	t.Cleanup(func() { setupProbeEndpoint = old })
}

func TestNeedsSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !a.NeedsSetup() {
		t.Fatal("wizard must open when no API key is configured")
	}
	a.mu.Lock()
	a.cfg.APIKey = "sk-existing"
	a.mu.Unlock()
	if a.NeedsSetup() {
		t.Fatal("an existing API key must suppress the wizard")
	}
	a.mu.Lock()
	a.cfg.APIKey = ""
	a.cfg.SetupDone = true
	a.mu.Unlock()
	if a.NeedsSetup() {
		t.Fatal("a dismissed wizard must not reappear")
	}
}

func TestSetupApplyPersistsDraftAndRebuilds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, root+"/a.md", "# A\n\nalpha beta gamma\n")

	var probed []string
	stubSetupProbe(t, func(base string, _ time.Duration) error {
		probed = append(probed, base)
		return nil
	})
	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}

	draft := a.cfg.Clone()
	draft.APIKey = "sk-test-secret"
	draft.ChatProvider = config.ChatProviderOpenAI
	draft.BaseURL = "http://127.0.0.1:9999/v1"
	draft.Model = "gpt-4o"
	draft.SetupDone = true

	status, err := a.SetupApply(draft, false)
	if err != nil {
		t.Fatalf("SetupApply: %v", err)
	}
	if status == "" {
		t.Fatal("SetupApply must return a status line")
	}
	if len(probed) != 1 || probed[0] != draft.BaseURL {
		t.Fatalf("probe calls = %v, want one call with the new base URL", probed)
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if onDisk.APIKey != "sk-test-secret" || !onDisk.SetupDone {
		t.Fatalf("persisted config = %+v", onDisk)
	}
	if a.Config().APIKey != "sk-test-secret" || a.NeedsSetup() {
		t.Fatalf("effective config was not updated: %+v", a.Config())
	}
	a.mu.RLock()
	runner, searcher := a.runner, a.searcher
	a.mu.RUnlock()
	if runner == nil || searcher == nil {
		t.Fatal("agent and retriever must be rebuilt with the new API key")
	}
}

func TestSetupApplySkipBypassesProbe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	stubSetupProbe(t, func(string, time.Duration) error {
		t.Fatal("skip must not run the connectivity probe")
		return nil
	})
	a, err := Open(testConfig(t), root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	draft := a.cfg.Clone()
	draft.SetupDone = true
	status, err := a.SetupApply(draft, true)
	if err != nil {
		t.Fatalf("SetupApply: %v", err)
	}
	if status != i18n.LangEn.T("setup.skipped") {
		t.Fatalf("skip status = %q", status)
	}
	onDisk, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !onDisk.SetupDone || onDisk.APIKey != "" {
		t.Fatalf("skip changed more than setup_done: %+v", onDisk)
	}
	if a.NeedsSetup() {
		t.Fatal("skipping must dismiss the wizard")
	}
}

func TestSetupApplySavesEvenWhenProbeFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	stubSetupProbe(t, func(string, time.Duration) error { return errors.New("connection refused") })
	a, err := Open(testConfig(t), root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	draft := a.cfg.Clone()
	draft.APIKey = "sk-offline"
	draft.SetupDone = true
	status, err := a.SetupApply(draft, false)
	if err != nil {
		t.Fatalf("SetupApply must still succeed: %v", err)
	}
	if status != i18n.LangEn.T("setup.offline_warn") {
		t.Fatalf("status = %q, want the offline warning", status)
	}
	onDisk, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if onDisk.APIKey != "sk-offline" {
		t.Fatalf("config was not saved on probe failure: %+v", onDisk)
	}
}

func TestSetupApplyRejectsInvalidDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	a, err := Open(testConfig(t), root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	draft := a.cfg.Clone()
	draft.ChatProvider = "unsupported"
	draft.SetupDone = true
	if _, err := a.SetupApply(draft, false); err == nil {
		t.Fatal("SetupApply accepted an invalid provider")
	}
	if _, err := a.SetupApply(nil, true); err == nil {
		t.Fatal("SetupApply accepted a nil draft")
	}
}

// The new per-extension knobs must be recorded in the build signature so an
// index built before the upgrade is reported stale.
func TestPerExtensionKnobsInvalidateIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	writeFile(t, root+"/a.md", "# A\n\nalpha beta gamma\n")
	cfg := testConfig(t)
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.Build(context.Background(), root, ""); err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open after Build: %v", err)
	}
	if stale, _ := b.IndexStale(); stale {
		t.Fatal("fresh index must not be stale")
	}
	for _, tc := range []struct {
		name string
		mut  func(*config.Config)
		want string
	}{
		{"section_size", func(c *config.Config) { c.SectionSize = 700 }, "section_size"},
		{"chunk_strategy", func(c *config.Config) { c.ChunkStrategy = config.ChunkStrategyFixed }, "chunk_strategy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b.mu.Lock()
			tc.mut(b.cfg)
			b.mu.Unlock()
			stale, fields := b.IndexStale()
			if !stale {
				t.Fatal("index should be stale")
			}
			found := false
			for _, f := range fields {
				if f == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("stale fields = %v, want %s", fields, tc.want)
			}
			b.mu.Lock()
			b.cfg = config.Default()
			b.cfg.APIKey = ""
			b.cfg.BaseURL = "http://127.0.0.1:1/v1"
			b.mu.Unlock()
		})
	}
	b.mu.Lock()
	b.cfg.CandidateK = 80
	b.mu.Unlock()
	if stale, fields := b.IndexStale(); stale {
		t.Fatalf("candidate_k must not invalidate the index: %v", fields)
	}
}

func TestSetupApplyReportStatusIsLocalized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	stubSetupProbe(t, func(string, time.Duration) error { return nil })
	cfg := testConfig(t)
	cfg.Language = config.LangZh
	a, err := Open(cfg, root, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	draft := a.cfg.Clone()
	draft.APIKey = "sk-zh"
	draft.SetupDone = true
	status, err := a.SetupApply(draft, false)
	if err != nil {
		t.Fatalf("SetupApply: %v", err)
	}
	if status != i18n.LangZh.T("setup.saved_prefix") || strings.TrimSpace(status) == "" {
		t.Fatalf("status = %q, want the localized saved prefix", status)
	}
}
