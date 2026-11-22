package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/index"
)

const (
	defaultChunkSize    = 600
	defaultMaxFileBytes = 20 << 20
	defaultMaxPDFPages  = 500
)

// supportedExtList returns every extension the package can ingest. A fresh slice
// avoids exposing shared mutable state to callers.
func supportedExtList() []string {
	return []string{
		".md", ".markdown", ".mdx", ".txt", ".text",
		".csv", ".html", ".htm", ".pdf", ".docx",
	}
}

// defaultExcludeDirs returns the generated/build directories skipped by default.
func defaultExcludeDirs() []string {
	return []string{
		".git", "node_modules", "vendor", "dist", ".idea",
		".vscode", "__pycache__", "target", "build",
	}
}

func defaultOptions() Options {
	// SectionSize is intentionally left at zero: sectionSize(opts) falls back
	// to chunkSize(opts), so DefaultOptions() keeps the legacy split sizes while
	// still opting into the auto per-extension strategy registry.
	return Options{
		ChunkSize:     defaultChunkSize,
		ChunkOverlap:  0,
		IncludeExt:    supportedExtList(),
		ExcludeDir:    defaultExcludeDirs(),
		MaxFileBytes:  defaultMaxFileBytes,
		MaxPDFPages:   defaultMaxPDFPages,
		ChunkStrategy: StrategyAuto,
	}
}

// chunkSize returns the effective chunk size, falling back to the default when
// Options carries no positive value (e.g. a zero-value Options).
func chunkSize(opts Options) int {
	if opts.ChunkSize > 0 {
		return opts.ChunkSize
	}
	return defaultChunkSize
}

// maxPDFPages returns the effective page cap with a sane default.
func maxPDFPages(opts Options) int {
	if opts.MaxPDFPages > 0 {
		return opts.MaxPDFPages
	}
	return defaultMaxPDFPages
}

// walk lists supported files under root as sorted absolute paths. It always
// skips dot-prefixed directories, the configured exclusion directories, the
// vector index file itself, and files larger than MaxFileBytes.
func walk(root string, opts Options) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}

	include := opts.IncludeExt
	if len(include) == 0 {
		include = supportedExtList()
	}
	includeSet := make(map[string]struct{}, len(include))
	for _, e := range include {
		includeSet[strings.ToLower(e)] = struct{}{}
	}

	exclude := opts.ExcludeDir
	if len(exclude) == 0 {
		exclude = defaultExcludeDirs()
	}
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, d := range exclude {
		excludeSet[d] = struct{}{}
	}

	var paths []string
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == absRoot {
				return nil // never skip the walk root itself
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, bad := excludeSet[name]; bad {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == index.DefaultFileName {
			return nil // never index the vector file itself
		}
		if _, ok := includeSet[strings.ToLower(filepath.Ext(p))]; !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", p, err)
		}
		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %q: %w", root, walkErr)
	}
	sort.Strings(paths)
	return paths, nil
}

// relPath returns path relative to root with slash separators, tolerating
// either absolute or relative inputs.
func relPath(root, path string) string {
	r, err := filepath.Abs(root)
	if err != nil {
		r = root
	}
	p, err := filepath.Abs(path)
	if err != nil {
		p = path
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		rel = filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}

// stateFromData builds a FileState from already-read content, so chunkFile can
// hash once and reuse the bytes for parsing instead of reading/hashing twice.
func stateFromData(root, path string, data []byte, info os.FileInfo) index.FileState {
	sum := sha256.Sum256(data)
	return index.FileState{
		Path:    relPath(root, path),
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
		SHA256:  hex.EncodeToString(sum[:]),
	}
}

func fileStateFor(root, path string) (index.FileState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return index.FileState{}, fmt.Errorf("stat %q: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return index.FileState{}, fmt.Errorf("read %q: %w", path, err)
	}
	return stateFromData(root, path, data, info), nil
}
