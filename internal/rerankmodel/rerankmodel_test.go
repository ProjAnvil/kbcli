package rerankmodel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func setResolveBase(s string) func() {
	old := resolveBase
	resolveBase = s
	return func() { resolveBase = old }
}

func setLocalSourceRoot(f func() string) func() {
	old := localSourceRoot
	localSourceRoot = f
	return func() { localSourceRoot = old }
}

func disableLocalSource(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(setLocalSourceRoot(func() string { return root }))
}

func writeSparse(t *testing.T, dir, name string, size int64) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func makeCompleteDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range modelFiles {
		writeSparse(t, dir, f.name, f.size)
	}
	return dir
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".kbcli-dl-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

// zeroReader streams arbitrary byte counts without allocating a 90 MB buffer.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func fileSizeFor(name string) int64 {
	for _, f := range modelFiles {
		if f.name == name {
			return f.size
		}
	}
	return 0
}

func TestIsComplete(t *testing.T) {
	dir := makeCompleteDir(t)
	if !IsComplete(dir) {
		t.Fatal("complete dir reported incomplete")
	}
	writeSparse(t, dir, "config.json", 795)
	if IsComplete(dir) {
		t.Fatal("wrong-sized file reported complete")
	}
	if IsComplete(t.TempDir()) {
		t.Fatal("empty dir reported complete")
	}
}

func TestEnsureCompleteDestSkipsNetwork(t *testing.T) {
	dest := makeCompleteDir(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		t.Error("network must not be used for a complete dest")
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()
	disableLocalSource(t)

	var got []Progress
	changed, err := Ensure(context.Background(), DefaultModel, dest, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
	if calls != 0 {
		t.Fatalf("network calls = %d, want 0", calls)
	}
	assertNoTemps(t, dest)
	if len(got) == 0 {
		t.Fatal("no progress emitted")
	}
	last := got[len(got)-1]
	if last.Phase != "done" || last.Downloaded != totalBytes || last.Total != totalBytes || last.FilesDone != len(modelFiles) {
		t.Fatalf("done progress = %+v", last)
	}
}

func TestEnsureLocalSourceCopy(t *testing.T) {
	root := t.TempDir()
	defer setLocalSourceRoot(func() string { return root })()
	src := filepath.Join(root, "model", DefaultModel)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range modelFiles {
		writeSparse(t, src, f.name, f.size)
	}
	if got := LocalSourceDir(DefaultModel); got != src {
		t.Fatalf("LocalSourceDir = %q, want %q", got, src)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("network must not be used for a bundled source")
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	dest := filepath.Join(t.TempDir(), "dest")
	var got []Progress
	changed, err := Ensure(context.Background(), DefaultModel, dest, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if !IsComplete(dest) {
		t.Fatal("dest is not complete after local copy")
	}
	assertNoTemps(t, dest)
	sawCopy := false
	for _, p := range got {
		if p.Phase == "copying" {
			sawCopy = true
		}
	}
	if !sawCopy {
		t.Fatalf("no copying phase in %+v", got)
	}
	last := got[len(got)-1]
	if last.Phase != "done" || last.Downloaded != totalBytes {
		t.Fatalf("last progress = %+v", last)
	}
}

func TestEnsureDownloadsWithCumulativeProgress(t *testing.T) {
	disableLocalSource(t)
	prefix := "/cross-encoder/" + DefaultModel + "/resolve/main/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		size := fileSizeFor(name)
		if size == 0 {
			t.Errorf("unknown file %q", name)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		_, _ = io.CopyN(w, zeroReader{}, size)
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	dest := filepath.Join(t.TempDir(), "dest")
	var got []Progress
	changed, err := Ensure(context.Background(), DefaultModel, dest, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !IsComplete(dest) {
		t.Fatalf("download failed: changed=%v", changed)
	}
	assertNoTemps(t, dest)
	var prev int64 = -1
	sawDownload := false
	for _, p := range got {
		if p.Downloaded < prev {
			t.Fatalf("progress went backwards: %+v after %d", p, prev)
		}
		prev = p.Downloaded
		if p.Phase == "downloading" {
			sawDownload = true
			if p.File == "" {
				t.Error("downloading progress has no file")
			}
		}
	}
	if !sawDownload {
		t.Fatal("no downloading phase")
	}
	last := got[len(got)-1]
	if last.Phase != "done" || last.Downloaded != totalBytes || last.Total != totalBytes || last.FilesDone != len(modelFiles) {
		t.Fatalf("last progress = %+v", last)
	}
}

func TestEnsureInterruptedDownloadCleansUpAndRetries(t *testing.T) {
	disableLocalSource(t)
	var mu sync.Mutex
	attempts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/cross-encoder/" + DefaultModel + "/resolve/main/"
		name := strings.TrimPrefix(r.URL.Path, prefix)
		size := fileSizeFor(name)
		if size == 0 {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		attempts[name]++
		n := attempts[name]
		mu.Unlock()
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if name == "model.safetensors" && n == 1 {
			_, _ = io.CopyN(w, zeroReader{}, 1024)
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		_, _ = io.CopyN(w, zeroReader{}, size)
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	dest := filepath.Join(t.TempDir(), "dest")
	ctx := context.Background()
	if _, err := Ensure(ctx, DefaultModel, dest, nil); err == nil {
		t.Fatal("want error on interrupted download")
	}
	assertNoTemps(t, dest)
	if fileComplete(filepath.Join(dest, "model.safetensors"), 90870598) {
		t.Fatal("partial file was committed")
	}

	var got []Progress
	changed, err := Ensure(ctx, DefaultModel, dest, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if !changed || !IsComplete(dest) {
		t.Fatalf("retry incomplete: changed=%v", changed)
	}
	assertNoTemps(t, dest)
	last := got[len(got)-1]
	if last.Downloaded != totalBytes {
		t.Fatalf("retry progress = %+v", last)
	}
}

func TestEnsureContextCancel(t *testing.T) {
	disableLocalSource(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("network must not be used with a canceled context")
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(t.TempDir(), "dest")
	if _, err := Ensure(ctx, DefaultModel, dest, nil); err == nil {
		t.Fatal("want error for canceled context")
	}
	assertNoTemps(t, dest)
}

func TestEnsureRejectsPathTraversal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("network must not be used for an unsafe name")
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	for _, name := range []string{"", "..", "../evil", "a/../../b", "/abs", `a\..\b`, "a//b", "a:b", "cross-encoder/../../evil"} {
		if _, err := Ensure(context.Background(), name, filepath.Join(t.TempDir(), "dest"), nil); err == nil {
			t.Errorf("Ensure(%q) accepted an unsafe name", name)
		}
		if got := LocalSourceDir(name); got != "" {
			t.Errorf("LocalSourceDir(%q) = %q, want empty", name, got)
		}
	}
}

func TestEnsureRejectsWrongLinkedSize(t *testing.T) {
	disableLocalSource(t)
	resolvePath := "/cross-encoder/" + DefaultModel + "/resolve/main/config.json"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == resolvePath {
			w.Header().Set("X-Linked-Size", "123")
			w.Header().Set("Location", "/cdn/config.json")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Length", "794")
		_, _ = io.CopyN(w, zeroReader{}, 794)
	}))
	defer srv.Close()
	defer setResolveBase(srv.URL)()

	dest := filepath.Join(t.TempDir(), "dest")
	if _, err := Ensure(context.Background(), DefaultModel, dest, nil); err == nil {
		t.Fatal("want error for mismatched X-Linked-Size")
	}
	assertNoTemps(t, dest)
}
