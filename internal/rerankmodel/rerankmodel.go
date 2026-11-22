// Package rerankmodel ensures the aikit cross-encoder model files are present
// locally. Files come from the Hugging Face Hub or a bundled offline source,
// are streamed with byte-level progress, and are committed atomically.
package rerankmodel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultModel is the bundled cross-encoder leaf name.
const DefaultModel = "ms-marco-MiniLM-L-6-v2"

// Progress reports Ensure's state. Phase is one of checking, copying,
// downloading or done. Downloaded and Total are cumulative across all files.
type Progress struct {
	Phase      string
	File       string
	Downloaded int64
	Total      int64
	FilesDone  int
	FilesTotal int
}

type modelFile struct {
	name string
	size int64
}

// modelFiles is the exact set aikit loads, with the byte sizes of the pinned
// cross-encoder/ms-marco-MiniLM-L-6-v2 revision. Sizes drive progress totals
// and integrity checks.
var modelFiles = []modelFile{
	{"config.json", 794},
	{"model.safetensors", 90870598},
	{"tokenizer.json", 711396},
	{"tokenizer_config.json", 1330},
	{"vocab.txt", 231508},
}

const totalBytes int64 = 794 + 90870598 + 711396 + 1330 + 231508

// resolveBase is the Hub root. Replaceable in tests.
var resolveBase = "https://huggingface.co"

// localSourceRoot is the directory holding bundled "model/<name>" trees.
// Replaceable in tests.
var localSourceRoot = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// repoFor maps a leaf name to a Hub repo; a name containing "/" is used as-is.
// It rejects absolute paths, "."/".." segments, empty segments and NUL/colons
// so neither a URL nor a filesystem path can escape its root.
func repoFor(modelName string) (string, error) {
	name := strings.TrimSpace(modelName)
	if name == "" {
		return "", errors.New("rerankmodel: empty model name")
	}
	if strings.ContainsAny(name, "\x00:") || filepath.IsAbs(name) || strings.HasPrefix(name, `\`) {
		return "", fmt.Errorf("rerankmodel: unsafe model name %q", modelName)
	}
	for _, seg := range strings.Split(strings.ReplaceAll(name, `\`, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("rerankmodel: unsafe model name %q", modelName)
		}
	}
	if strings.Contains(name, "/") {
		return name, nil
	}
	return "cross-encoder/" + name, nil
}

// LocalSourceDir returns the bundled offline directory for modelName, or "" if
// the name is unsafe or the executable directory cannot be resolved.
func LocalSourceDir(modelName string) string {
	if _, err := repoFor(modelName); err != nil {
		return ""
	}
	root := localSourceRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "model", filepath.FromSlash(strings.TrimSpace(modelName)))
}

func fileComplete(path string, size int64) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() == size
}

// IsComplete reports whether dir has all five files at their exact sizes.
func IsComplete(dir string) bool {
	for _, f := range modelFiles {
		if !fileComplete(filepath.Join(dir, f.name), f.size) {
			return false
		}
	}
	return true
}

type progressState struct {
	downloaded int64
	filesDone  int
	emit       func(Progress)
}

func (s *progressState) step(phase, file string, n int64) {
	s.downloaded += n
	if s.emit != nil {
		s.emit(Progress{
			Phase: phase, File: file, Downloaded: s.downloaded, Total: totalBytes,
			FilesDone: s.filesDone, FilesTotal: len(modelFiles),
		})
	}
}

func (s *progressState) skip(size int64) {
	s.downloaded += size
	s.filesDone++
}

func (s *progressState) done() {
	if s.emit != nil {
		s.emit(Progress{
			Phase: "done", Downloaded: s.downloaded, Total: totalBytes,
			FilesDone: len(modelFiles), FilesTotal: len(modelFiles),
		})
	}
}

// Ensure makes modelName available in destDir. It reports whether it wrote
// anything (false means destDir was already complete). onProgress is invoked
// synchronously from this goroutine and may be nil.
func Ensure(ctx context.Context, modelName, destDir string, onProgress func(Progress)) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repo, err := repoFor(modelName)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("rerankmodel: canceled: %w", err)
	}
	s := &progressState{emit: onProgress}
	if s.emit != nil {
		s.emit(Progress{Phase: "checking", Total: totalBytes, FilesTotal: len(modelFiles)})
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false, fmt.Errorf("rerankmodel: create %s: %w", destDir, err)
	}
	if IsComplete(destDir) {
		s.downloaded, s.filesDone = totalBytes, len(modelFiles)
		s.done()
		return false, nil
	}
	if src := LocalSourceDir(modelName); src != "" && IsComplete(src) {
		if err := copyLocal(ctx, src, destDir, s); err != nil {
			return false, err
		}
		s.done()
		return true, nil
	}
	if err := downloadAll(ctx, repo, destDir, s); err != nil {
		return false, err
	}
	s.done()
	return true, nil
}

func copyLocal(ctx context.Context, src, destDir string, s *progressState) error {
	for _, f := range modelFiles {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rerankmodel: canceled: %w", err)
		}
		dst := filepath.Join(destDir, f.name)
		if fileComplete(dst, f.size) {
			s.skip(f.size)
			continue
		}
		in, err := os.Open(filepath.Join(src, f.name))
		if err != nil {
			return fmt.Errorf("rerankmodel: open bundled %s: %w", f.name, err)
		}
		err = commitStream(ctx, destDir, dst, in, f.size, "copying", f.name, s)
		closeErr := in.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("rerankmodel: close bundled %s: %w", f.name, closeErr)
		}
		s.filesDone++
	}
	return nil
}

func downloadAll(ctx context.Context, repo, destDir string, s *progressState) error {
	for _, f := range modelFiles {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rerankmodel: canceled: %w", err)
		}
		dst := filepath.Join(destDir, f.name)
		if fileComplete(dst, f.size) {
			s.skip(f.size)
			continue
		}
		if err := downloadOne(ctx, repo, destDir, f, s); err != nil {
			return err
		}
		s.filesDone++
	}
	return nil
}

func downloadOne(ctx context.Context, repo, destDir string, f modelFile, s *progressState) error {
	u := fmt.Sprintf("%s/%s/resolve/main/%s", strings.TrimRight(resolveBase, "/"), repo, f.name)
	// LFS size headers live on the first redirect hop, as in rembed's hub.
	var linkedSize int64 = -1
	client := &http.Client{
		Timeout: 15 * time.Minute,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			// A custom CheckRedirect disables net/http's default 10-hop cap.
			if len(via) >= 10 {
				return fmt.Errorf("rerankmodel: too many redirects for %s", f.name)
			}
			if len(via) > 0 && via[len(via)-1].Response != nil {
				if v := strings.TrimSpace(via[len(via)-1].Response.Header.Get("X-Linked-Size")); v != "" {
					if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
						linkedSize = n
					}
				}
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("rerankmodel: build request for %s: %w", f.name, err)
	}
	req.Header.Set("User-Agent", "kbcli-rerankmodel/1")
	if tok := os.Getenv("HF_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("rerankmodel: fetch %s: %w", f.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rerankmodel: fetch %s: HTTP %s", f.name, resp.Status)
	}
	switch {
	case linkedSize > 0 && linkedSize != f.size:
		return fmt.Errorf("rerankmodel: %s reports %d bytes, want %d", f.name, linkedSize, f.size)
	case linkedSize <= 0 && resp.ContentLength > 0 && resp.ContentLength != f.size:
		return fmt.Errorf("rerankmodel: %s reports %d bytes, want %d", f.name, resp.ContentLength, f.size)
	}
	return commitStream(ctx, destDir, filepath.Join(destDir, f.name), resp.Body, f.size, "downloading", f.name, s)
}

// commitStream writes r to a temp file in destDir, reporting each chunk, then
// syncs, closes and atomically renames it into place. Any failure removes the
// temp file so a partial download is never committed.
func commitStream(ctx context.Context, destDir, dst string, r io.Reader, expected int64, phase, file string, s *progressState) error {
	tmp, err := os.CreateTemp(destDir, ".kbcli-dl-*")
	if err != nil {
		return fmt.Errorf("rerankmodel: create temp for %s: %w", file, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	buf := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rerankmodel: canceled: %w", err)
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				return fmt.Errorf("rerankmodel: write temp for %s: %w", file, err)
			}
			written += int64(n)
			s.step(phase, file, int64(n))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("rerankmodel: read %s: %w", file, readErr)
		}
	}
	if written != expected {
		return fmt.Errorf("rerankmodel: %s truncated: %d of %d bytes", file, written, expected)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("rerankmodel: sync %s: %w", file, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("rerankmodel: close temp for %s: %w", file, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rerankmodel: commit %s: %w", file, err)
	}
	committed = true
	return nil
}
