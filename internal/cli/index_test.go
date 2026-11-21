package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
)

func TestIndexAndSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")

	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "alpha.md"), []byte("# Retry\n\nRetry uses exponential backoff with jitter.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "beta.md"), []byte("# Deploy\n\nDeploy runs on Fridays.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexFile := filepath.Join(docs, ".kbvec")
	cfg := config.Default()

	out, err := runCmd(t, cfg, "index", docs, "--out", indexFile)
	if err != nil {
		t.Fatalf("index: %v\n%s", err, out)
	}
	if !strings.Contains(out, "chunks indexed:") {
		t.Fatalf("index summary is missing: %s", out)
	}
	if !strings.Contains(out, "index: "+indexFile) {
		t.Fatalf("index path is missing from summary: %s", out)
	}
	if _, err := os.Stat(indexFile); err != nil {
		t.Fatalf("vector file was not created: %v", err)
	}

	out, err = runCmd(t, cfg, "search", "retry", "--index", indexFile, "--top-k", "3")
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	if !strings.Contains(out, "#1") || !strings.Contains(out, "score=") {
		t.Fatalf("no ranked hit printed: %s", out)
	}
}

func TestCommandsWithoutIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	empty := t.TempDir()

	cases := [][]string{
		{"reindex", "--dir", empty},
		{"update", "--dir", empty},
		{"search", "hello", "--dir", empty},
	}
	for _, args := range cases {
		out, err := runCmd(t, config.Default(), args...)
		if err == nil {
			t.Fatalf("%v: expected an error, got output %q", args, out)
		}
		if !strings.Contains(err.Error(), "no kbcli index found") {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
	}
}

func TestReindexOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")

	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "alpha.md"), []byte("# Retry\n\nRetry uses exponential backoff.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexFile := filepath.Join(docs, ".kbvec")
	outFile := filepath.Join(t.TempDir(), "copy.kbvec")
	cfg := config.Default()

	if out, err := runCmd(t, cfg, "index", docs, "--out", indexFile); err != nil {
		t.Fatalf("index: %v\n%s", err, out)
	}
	out, err := runCmd(t, cfg, "reindex", "--index", indexFile, "--out", outFile)
	if err != nil {
		t.Fatalf("reindex --out: %v\n%s", err, out)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("reindex --out did not write the new path: %v", err)
	}
	if !strings.Contains(out, "index: "+outFile) {
		t.Fatalf("reindex summary is missing the out path: %s", out)
	}
}

func TestIndexDryRunReportsWithoutWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A\n\nalpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, config.Default(), "index", docs, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "preflight:") || !strings.Contains(out, "risk ok") {
		t.Fatalf("preflight report missing: %s", out)
	}
	if !strings.Contains(out, "dry-run: no index written") {
		t.Fatalf("dry-run notice missing: %s", out)
	}
	if _, err := os.Stat(filepath.Join(docs, ".kbvec")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote a vector file")
	}
}

func TestIndexRefusesHomeDirectoryWithoutYes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KBCLI_INDEX", "")
	if err := os.WriteFile(filepath.Join(home, "note.md"), []byte("# Note\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, config.Default(), "index", home)
	if err == nil || !strings.Contains(err.Error(), "refusing to auto-index") {
		t.Fatalf("home directory was not blocked: err=%v out=%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".kbvec")); !os.IsNotExist(err) {
		t.Fatal("a blocked index was still written")
	}
}

func TestIndexYesOverridesCriticalRisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KBCLI_INDEX", "")
	if err := os.WriteFile(filepath.Join(home, "note.md"), []byte("# Note\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, config.Default(), "index", home, "--yes")
	if err != nil {
		t.Fatalf("--yes did not override the guard: %v\n%s", err, out)
	}
	if !strings.Contains(out, "risk critical") {
		t.Fatalf("critical warning missing from the report: %s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".kbvec")); err != nil {
		t.Fatalf("vector file not written: %v", err)
	}
}
