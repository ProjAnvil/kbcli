package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanCountsFilesDepthAndExtensions(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "# A\n\nalpha\n")
	write(t, filepath.Join(root, "sub", "b.md"), "# B\n\nbeta\n")
	write(t, filepath.Join(root, "sub", "deep", "c.pdf"), "%PDF-1.4 fake")
	write(t, filepath.Join(root, "ignored.txt.bak"), "not supported")

	r, err := Scan(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 3 {
		t.Fatalf("files = %d, want 3", r.Files)
	}
	if r.MaxDepth < 3 {
		t.Fatalf("max depth = %d, want >= 3", r.MaxDepth)
	}
	if r.Dirs != 2 {
		t.Fatalf("dirs = %d, want 2", r.Dirs)
	}
	if r.Risk != RiskOK {
		t.Fatalf("risk = %s, want ok", r.RiskLabel())
	}
	if len(r.Extensions) < 2 {
		t.Fatalf("extensions = %+v", r.Extensions)
	}
	if r.Extensions[0].Ext != ".md" || r.Extensions[0].Count != 2 {
		t.Fatalf("top extension = %+v", r.Extensions[0])
	}
}

func TestScanSkipsGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "real.md"), "real")
	write(t, filepath.Join(root, "node_modules", "huge.md"), strings.Repeat("x", 1000))
	write(t, filepath.Join(root, ".hidden", "secret.md"), "secret")
	write(t, filepath.Join(root, ".git", "config.md"), "git")

	r, err := Scan(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 1 {
		t.Fatalf("files = %d, want 1 (generated/hidden dirs skipped)", r.Files)
	}
}

func TestScanTruncatesAtLimits(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		write(t, filepath.Join(root, "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "file"+string(rune('a'+i))+".md"), "x")
	}
	limits := DefaultLimits()
	limits.MaxFiles = 5
	limits.MaxDepth = 4
	r, err := Scan(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated {
		t.Fatal("bounded scan must report truncation")
	}
	if r.Files > 5 {
		t.Fatalf("files = %d, exceeded MaxFiles", r.Files)
	}
	if r.Risk != RiskCritical {
		t.Fatalf("truncated scan risk = %s, want critical", r.RiskLabel())
	}
	if r.CanAutoIndex() {
		t.Fatal("truncated scan must not be auto-indexable")
	}
}

func TestScanRefusesProtectedRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	// Stat only; a real home walk would be slow. Scan is bounded, but we still
	// avoid touching the user's real files in a unit test by using a temp dir
	// that is not protected.
	root := t.TempDir()
	r, err := Scan(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if r.Risk == RiskCritical {
		t.Fatal("a temp directory must not be protected")
	}
	_ = home

	// Verify the protected-root table itself.
	protected := protectedRoots(home)
	if _, ok := protected[filepath.Clean(string(filepath.Separator))]; !ok {
		t.Fatal("filesystem root missing from protected table")
	}
	if _, ok := protected[filepath.Clean(home)]; !ok {
		t.Fatal("home directory missing from protected table")
	}
	if _, ok := protected[filepath.Join(root, "etc")]; ok {
		t.Fatal("protected roots must match exact paths only")
	}
}

func TestScanRejectsFileAndMissingPath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.md")
	write(t, file, "x")
	if _, err := Scan(file, DefaultLimits()); err == nil {
		t.Fatal("Scan on a file must fail")
	}
	if _, err := Scan(filepath.Join(root, "missing"), DefaultLimits()); err == nil {
		t.Fatal("Scan on a missing path must fail")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KB"},
		{5 << 20, "5.0 MB"},
	}
	for _, tc := range cases {
		if got := HumanBytes(tc.n); got != tc.want {
			t.Fatalf("HumanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestLargeCorpusIsHighRisk(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		write(t, filepath.Join(root, "file"+string(rune('a'+i))+".md"), strings.Repeat("x", 100))
	}
	limits := DefaultLimits()
	limits.WarnFiles = 3
	r, err := Scan(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	if r.Risk != RiskHigh {
		t.Fatalf("risk = %s, want high", r.RiskLabel())
	}
	if !r.NeedsSecondConfirm() {
		t.Fatal("high risk must require a second confirmation")
	}
	if !r.CanAutoIndex() {
		t.Fatal("high risk is still auto-indexable after confirmation")
	}
}

func TestProtectedRootsUseStdlibPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	protected := protectedRoots(home)
	sep := string(filepath.Separator)
	if _, ok := protected[filepath.Clean(sep)]; !ok {
		t.Fatalf("filesystem root %q missing from protected table", sep)
	}
	if _, ok := protected[filepath.Dir(filepath.Clean(home))]; !ok {
		t.Fatal("home parent (users directory) missing from protected table")
	}
	if runtime.GOOS == "windows" {
		if windir := os.Getenv("SystemRoot"); windir != "" {
			if _, ok := protected[filepath.Clean(windir)]; !ok {
				t.Fatal("SystemRoot missing from protected table")
			}
		}
	}
}

func TestSamePathFollowsPlatformCaseRules(t *testing.T) {
	a := filepath.Join("C:"+string(filepath.Separator), "Users", "Me")
	b := filepath.Join("c:"+string(filepath.Separator), "users", "me")
	if runtime.GOOS == "windows" {
		if !samePath(a, b) {
			t.Fatal("Windows paths must compare case-insensitively")
		}
	}
	if !samePath(filepath.Clean("/kb/docs"), filepath.Clean("/kb/./docs")) {
		t.Fatal("Clean should normalize identical paths")
	}
}

func TestIsVolumeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		root := "C:" + string(filepath.Separator)
		if !isVolumeRoot(root) {
			t.Fatalf("%q should be a volume root", root)
		}
		if isVolumeRoot(filepath.Join(root, "Users")) {
			t.Fatal("a subdirectory must not be a volume root")
		}
		return
	}
	if isVolumeRoot(string(filepath.Separator)) {
		t.Fatal("Unix filesystem root has no volume name")
	}
}
