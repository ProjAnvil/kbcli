package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	coretools "github.com/projanvil/langchain-golang/core/tools"
)

// Bounds for the read-only workspace tools. They keep a single tool call from
// flooding the prompt or walking an unbounded tree.
const (
	fsMaxEntries     = 200
	fsMaxReadBytes   = 64 << 10
	fsMaxGrepMatches = 100
	fsMaxWalkFiles   = 20000
	fsMaxLineRunes   = 240
)

// fsTools builds read-only tools scoped to root. Every path is resolved inside
// root, including through symlinks, so the agent cannot read outside the
// indexed workspace. A root that does not exist or is not a directory yields
// no tools; the kb_search tool still works and the runner stays usable.
func fsTools(root string) []coretools.Tool {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return nil
	}

	var tools []coretools.Tool
	add := func(name, desc string, fn func(context.Context, string) (coretools.Result, error)) {
		t, err := coretools.NewSimple(name, desc, fn)
		if err == nil {
			tools = append(tools, t)
		}
	}

	add("ls",
		"List one directory of the workspace. Input is a relative directory path; an empty input lists the workspace root. Read only.",
		func(ctx context.Context, input string) (coretools.Result, error) {
			dir, err := fsResolve(resolved, input)
			if err != nil {
				return coretools.Result{}, err
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return coretools.Result{}, fmt.Errorf("ls %q: %w", input, err)
			}
			var b strings.Builder
			n := 0
			for _, e := range entries {
				if fsSkipFile(e.Name()) {
					continue
				}
				if n >= fsMaxEntries {
					b.WriteString("... (truncated)\n")
					break
				}
				kind := "file"
				size := ""
				if info, err := e.Info(); err == nil {
					if e.IsDir() {
						kind = "dir"
					} else {
						size = fmt.Sprintf("\t%d", info.Size())
					}
				}
				fmt.Fprintf(&b, "%s\t%s%s\n", e.Name(), kind, size)
				n++
			}
			if n == 0 {
				return coretools.Result{Content: "(empty directory)"}, nil
			}
			return coretools.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
		})

	add("read_file",
		"Read one workspace file. Input is a relative file path. Returns up to 64 KiB of text; binary files are rejected. Read only.",
		func(ctx context.Context, input string) (coretools.Result, error) {
			p, err := fsResolve(resolved, input)
			if err != nil {
				return coretools.Result{}, err
			}
			info, err := os.Stat(p)
			if err != nil {
				return coretools.Result{}, fmt.Errorf("read_file %q: %w", input, err)
			}
			if info.IsDir() {
				return coretools.Result{}, fmt.Errorf("read_file %q: is a directory", input)
			}
			f, err := os.Open(p)
			if err != nil {
				return coretools.Result{}, fmt.Errorf("read_file %q: %w", input, err)
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, fsMaxReadBytes+1))
			if err != nil {
				return coretools.Result{}, fmt.Errorf("read_file %q: %w", input, err)
			}
			if fsIsBinary(data) {
				return coretools.Result{}, fmt.Errorf("read_file %q: binary file", input)
			}
			truncated := len(data) > fsMaxReadBytes
			if truncated {
				data = data[:fsMaxReadBytes]
			}
			text := string(data)
			if truncated {
				text += "\n\n... (truncated at 64 KiB)"
			}
			return coretools.Result{Content: text}, nil
		})

	add("glob",
		"Find workspace files by name pattern. Input is a relative glob such as **/*.md or docs/*.txt. Read only.",
		func(ctx context.Context, input string) (coretools.Result, error) {
			pattern := strings.TrimSpace(input)
			if pattern == "" {
				return coretools.Result{}, fmt.Errorf("glob: pattern is empty")
			}
			if filepath.IsAbs(pattern) {
				return coretools.Result{}, fmt.Errorf("glob: absolute patterns are not allowed")
			}
			re, err := fsGlobRegexp(pattern)
			if err != nil {
				return coretools.Result{}, err
			}
			var out []string
			seen := 0
			_ = filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				rel, err := filepath.Rel(resolved, p)
				if err != nil || rel == "." {
					return nil
				}
				if d.IsDir() {
					if fsSkipDir(rel) {
						return filepath.SkipDir
					}
					return nil
				}
				seen++
				if seen > fsMaxWalkFiles || len(out) >= fsMaxEntries {
					return filepath.SkipAll
				}
				relSlash := filepath.ToSlash(rel)
				if fsSkipFile(filepath.Base(relSlash)) {
					return nil
				}
				if re.MatchString(relSlash) {
					out = append(out, relSlash)
				}
				return nil
			})
			if len(out) == 0 {
				return coretools.Result{Content: "(no matching files)"}, nil
			}
			return coretools.Result{Content: strings.Join(out, "\n")}, nil
		})

	add("grep",
		"Search workspace file contents with a regular expression. Input is the pattern, optionally followed by a tab and a relative path to scope the search. Read only.",
		func(ctx context.Context, input string) (coretools.Result, error) {
			parts := strings.SplitN(input, "\t", 2)
			pat := strings.TrimSpace(parts[0])
			scope := "."
			if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
				scope = strings.TrimSpace(parts[1])
			}
			if pat == "" {
				return coretools.Result{}, fmt.Errorf("grep: pattern is empty")
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				return coretools.Result{}, fmt.Errorf("grep pattern: %w", err)
			}
			base, err := fsResolve(resolved, scope)
			if err != nil {
				return coretools.Result{}, err
			}
			var b strings.Builder
			matches := 0
			seen := 0
			_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					rel, err := filepath.Rel(resolved, p)
					if err == nil && fsSkipDir(rel) {
						return filepath.SkipDir
					}
					return nil
				}
				seen++
				if seen > fsMaxWalkFiles || matches >= fsMaxGrepMatches {
					return filepath.SkipAll
				}
				rel, err := filepath.Rel(resolved, p)
				if err != nil || fsSkipFile(filepath.Base(rel)) {
					return nil
				}
				data, err := os.ReadFile(p)
				if err != nil || fsIsBinary(data) {
					return nil
				}
				for i, line := range strings.Split(string(data), "\n") {
					if !re.MatchString(line) {
						continue
					}
					if matches >= fsMaxGrepMatches {
						return filepath.SkipAll
					}
					fmt.Fprintf(&b, "%s:%d: %s\n", filepath.ToSlash(rel), i+1, truncateRunes(line, fsMaxLineRunes))
					matches++
				}
				return nil
			})
			if matches == 0 {
				return coretools.Result{Content: "(no matches)"}, nil
			}
			return coretools.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
		})

	return tools
}

// fsResolve maps a user-supplied relative path to an absolute path inside
// root. It rejects absolute paths, .. escapes and symlinks that leave root.
func fsResolve(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", rel)
	}
	clean := filepath.Clean(rel)
	sep := string(filepath.Separator)
	if clean == ".." || strings.HasPrefix(clean, ".."+sep) {
		return "", fmt.Errorf("path escapes the workspace: %q", rel)
	}
	p := filepath.Join(root, clean)
	if target, err := filepath.EvalSymlinks(p); err == nil {
		if r, err := filepath.Rel(root, target); err != nil || r == ".." || strings.HasPrefix(r, ".."+sep) {
			return "", fmt.Errorf("path escapes the workspace: %q", rel)
		}
		return target, nil
	}
	if r, err := filepath.Rel(root, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+sep) {
		return "", fmt.Errorf("path escapes the workspace: %q", rel)
	}
	return p, nil
}

// fsGlobRegexp converts a glob with * and ** into an anchored regexp matched
// against slash-separated relative paths.
func fsGlobRegexp(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(pattern)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			r, size := utf8.DecodeRuneInString(pattern[i:])
			b.WriteString(regexp.QuoteMeta(string(r)))
			i += size
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func fsIsBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

func fsSkipDir(rel string) bool {
	switch filepath.Base(rel) {
	case ".git", "node_modules", ".kbcli":
		return true
	}
	return false
}

func fsSkipFile(name string) bool {
	return name == ".kbvec" || name == ".kbcli.kbvec" || strings.HasPrefix(name, ".kbvec")
}
