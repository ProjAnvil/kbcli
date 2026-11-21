// Package scan performs a bounded, read-only preflight of a directory before
// indexing. It never reads file contents, so even a mistaken launch at a home
// directory stays cheap and the real build never starts without confirmation.
package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ProjAnvil/kbcli/internal/ingest"
)

// Risk is the preflight risk level.
type Risk int

const (
	// RiskOK is a small, ordinary document directory.
	RiskOK Risk = iota
	// RiskMedium means a deep tree that may contain generated content.
	RiskMedium
	// RiskHigh means a large corpus whose indexing cost is significant.
	RiskHigh
	// RiskCritical means the directory must not be indexed automatically.
	RiskCritical
)

// Limits bound the preflight walk so it can never itself become expensive.
type Limits struct {
	MaxFiles  int
	MaxDepth  int
	WarnFiles int
	WarnBytes int64
	WarnDepth int
}

// DefaultLimits matches the ingest walk's 20 MB file cap and adds a hard scan
// ceiling of 20000 files / 12 directory levels.
func DefaultLimits() Limits {
	return Limits{
		MaxFiles:  20000,
		MaxDepth:  12,
		WarnFiles: 5000,
		WarnBytes: 500 << 20,
		WarnDepth: 8,
	}
}

// ExtCount is one extension bucket in the report.
type ExtCount struct {
	Ext   string
	Count int
}

// Report is the preflight result shown on the confirmation screen.
type Report struct {
	Root       string
	Files      int
	Dirs       int
	Bytes      int64
	MaxDepth   int
	Extensions []ExtCount
	Truncated  bool
	Risk       Risk
	Warnings   []string
	Elapsed    time.Duration
}

// RiskLabel returns a stable machine-readable label for i18n lookup.
func (r Report) RiskLabel() string {
	switch r.Risk {
	case RiskCritical:
		return "critical"
	case RiskHigh:
		return "high"
	case RiskMedium:
		return "medium"
	default:
		return "ok"
	}
}

// HumanBytes renders a byte count for the confirmation screen.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, suffix)
		}
	}
	return fmt.Sprintf("%.1f PB", v/unit)
}

// protectedRoots builds the list of directories that must never be
// auto-indexed. No absolute path is hardcoded: the filesystem root comes from
// filepath.Separator, the users directory from the home parent, and Windows
// system directories from the process environment. Directories are matched by
// full path only, never by substring, so a project named "etc" stays valid.
func protectedRoots(home string) map[string]string {
	sep := string(filepath.Separator)
	out := make(map[string]string)
	add := func(p, reason string) {
		if p != "" {
			out[filepath.Clean(p)] = reason
		}
	}

	add(sep, "filesystem root")
	if home != "" {
		cleanHome := filepath.Clean(home)
		add(cleanHome, "home directory")
		if parent := filepath.Dir(cleanHome); parent != cleanHome {
			add(parent, "users directory")
		}
	}

	if runtime.GOOS == "windows" {
		for _, key := range []string{
			"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)",
			"ProgramData", "ALLUSERSPROFILE", "USERPROFILE",
		} {
			add(os.Getenv(key), "system directory ("+key+")")
		}
		return out
	}
	// Unix system directories are OS conventions, not stdlib constants. Build
	// them from the platform separator instead of hardcoded POSIX literals.
	for _, name := range []string{
		"bin", "sbin", "dev", "etc", "var", "usr", "opt", "private",
		"System", "Library", "Applications",
	} {
		add(filepath.Join(sep, name), "system directory")
	}
	return out
}

// isVolumeRoot reports a Windows drive root such as C:\ or a UNC share root.
// filepath.VolumeName returns "" on Unix, so this is a no-op there.
func isVolumeRoot(p string) bool {
	vol := filepath.VolumeName(p)
	if vol == "" {
		return false
	}
	rest := strings.TrimPrefix(p, vol)
	return rest == "" || rest == string(filepath.Separator) || rest == "/"
}

// samePath compares cleaned paths. Windows filesystems are case-insensitive.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Scan walks root with hard bounds. It returns a report even when the walk was
// truncated, so the caller can refuse to proceed on incomplete information.
func Scan(root string, limits Limits) (Report, error) {
	start := time.Now()
	abs, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("scan: resolve %q: %w", root, err)
	}
	abs = filepath.Clean(abs)
	st, err := os.Stat(abs)
	if err != nil {
		return Report{}, fmt.Errorf("scan: stat %s: %w", abs, err)
	}
	if !st.IsDir() {
		return Report{}, fmt.Errorf("scan: %s is not a directory", abs)
	}
	if limits.MaxFiles <= 0 || limits.MaxDepth <= 0 {
		limits = DefaultLimits()
	}

	opts := ingest.DefaultOptions()
	exclude := make(map[string]bool, len(opts.ExcludeDir))
	for _, d := range opts.ExcludeDir {
		exclude[d] = true
	}

	r := Report{Root: abs, Extensions: nil}
	byExt := make(map[string]int)
	stopped := false
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErrIn error) error {
		if walkErrIn != nil {
			// Unreadable subdirectories are common in home directories. Skip
			// them but keep the scan going.
			return nil
		}
		rel, relErr := filepath.Rel(abs, path)
		if relErr != nil {
			return nil
		}
		depth := 1
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if depth > r.MaxDepth {
			r.MaxDepth = depth
		}

		if d.IsDir() {
			if path == abs {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || exclude[name] {
				return filepath.SkipDir
			}
			if depth >= limits.MaxDepth {
				r.Truncated = true
				return filepath.SkipDir
			}
			r.Dirs++
			return nil
		}
		if !ingest.Supports(path) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || info.Size() > opts.MaxFileBytes {
			return nil
		}
		r.Files++
		r.Bytes += info.Size()
		ext := strings.ToLower(filepath.Ext(path))
		byExt[ext]++
		if r.Files >= limits.MaxFiles {
			r.Truncated = true
			stopped = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && err != fs.SkipAll && !stopped {
		return Report{}, fmt.Errorf("scan: walk %s: %w", abs, err)
	}

	for ext, n := range byExt {
		r.Extensions = append(r.Extensions, ExtCount{Ext: ext, Count: n})
	}
	sort.Slice(r.Extensions, func(i, j int) bool {
		if r.Extensions[i].Count == r.Extensions[j].Count {
			return r.Extensions[i].Ext < r.Extensions[j].Ext
		}
		return r.Extensions[i].Count > r.Extensions[j].Count
	})
	r.Elapsed = time.Since(start)
	r.assess(limits)
	return r, nil
}

func (r *Report) assess(limits Limits) {
	home, _ := os.UserHomeDir()
	protected := protectedRoots(home)
	clean := filepath.Clean(r.Root)
	for path, reason := range protected {
		if samePath(clean, path) {
			r.Risk = RiskCritical
			r.Warnings = append(r.Warnings, "protected:"+reason)
			break
		}
	}
	if r.Risk != RiskCritical && isVolumeRoot(clean) {
		r.Risk = RiskCritical
		r.Warnings = append(r.Warnings, "protected:volume root")
	}
	if r.Truncated {
		r.Risk = RiskCritical
		r.Warnings = append(r.Warnings, "truncated")
	}
	if r.Files > limits.WarnFiles {
		if r.Risk < RiskHigh {
			r.Risk = RiskHigh
		}
		r.Warnings = append(r.Warnings, "many_files")
	}
	if r.Bytes > limits.WarnBytes {
		if r.Risk < RiskHigh {
			r.Risk = RiskHigh
		}
		r.Warnings = append(r.Warnings, "large_bytes")
	}
	if r.MaxDepth > limits.WarnDepth && r.Risk < RiskMedium {
		r.Risk = RiskMedium
		r.Warnings = append(r.Warnings, "deep_tree")
	}
}

// CanAutoIndex reports whether a plain yes is enough. Critical directories
// require an explicit subdirectory instead.
func (r Report) CanAutoIndex() bool { return r.Risk != RiskCritical }

// NeedsSecondConfirm reports whether yes must be pressed twice.
func (r Report) NeedsSecondConfirm() bool { return r.Risk == RiskHigh }
