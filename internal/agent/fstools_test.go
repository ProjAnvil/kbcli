package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	coretools "github.com/projanvil/langchain-golang/core/tools"
)

func fsInvoke(t *testing.T, tools []coretools.Tool, name, input string) (string, error) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() != name {
			continue
		}
		out, err := tool.Invoke(context.Background(), map[string]any{"tool_input": input})
		return out.Content, err
	}
	t.Fatalf("tool %q not found", name)
	return "", nil
}

func fsTestRoot(t *testing.T) (string, []coretools.Tool) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.md":       "# Alpha\nretry uses backoff\n",
		"docs/b.md":  "# Beta\nretry limit is 5\n",
		"docs/c.txt": "plain text\n",
		".kbvec":     "binary-index-placeholder",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tools := fsTools(root)
	if len(tools) == 0 {
		t.Fatal("fsTools returned no tools for a valid root")
	}
	return root, tools
}

func TestFSToolsAreReadOnly(t *testing.T) {
	_, tools := fsTestRoot(t)
	want := map[string]bool{"ls": true, "read_file": true, "glob": true, "grep": true}
	if len(tools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		if !want[tool.Name()] {
			t.Fatalf("unexpected tool %q; write tools must not be exposed", tool.Name())
		}
		if tool.Description() == "" {
			t.Fatalf("tool %q has no description", tool.Name())
		}
	}
}

func TestFSToolsRejectPathEscapes(t *testing.T) {
	root, tools := fsTestRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, tc := range []struct{ name, tool, input string }{
		{"parent traversal", "ls", "../"},
		{"absolute path", "read_file", "/etc/passwd"},
		{"symlink out of root", "ls", "escape"},
		{"glob absolute", "glob", "/etc/*.conf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := fsInvoke(t, tools, tc.tool, tc.input); err == nil {
				t.Fatalf("%s %q must be rejected", tc.tool, tc.input)
			}
		})
	}
}

func TestFSToolsListAndRead(t *testing.T) {
	_, tools := fsTestRoot(t)
	got, err := fsInvoke(t, tools, "ls", "")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(got, "a.md") || !strings.Contains(got, "docs\tdir") {
		t.Fatalf("ls output missing entries: %q", got)
	}
	if strings.Contains(got, ".kbvec") {
		t.Fatalf("ls must hide the index file: %q", got)
	}

	got, err = fsInvoke(t, tools, "read_file", "docs/b.md")
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(got, "retry limit is 5") {
		t.Fatalf("read_file content = %q", got)
	}

	if _, err := fsInvoke(t, tools, "read_file", "docs"); err == nil {
		t.Fatal("read_file must reject a directory")
	}
}

func TestFSToolsGlobAndGrep(t *testing.T) {
	_, tools := fsTestRoot(t)
	got, err := fsInvoke(t, tools, "glob", "**/*.md")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(got, "a.md") || !strings.Contains(got, "docs/b.md") {
		t.Fatalf("glob output = %q", got)
	}

	got, err = fsInvoke(t, tools, "grep", "retry")
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(got, "a.md:2") || !strings.Contains(got, "docs/b.md:2") {
		t.Fatalf("grep output = %q", got)
	}

	got, err = fsInvoke(t, tools, "grep", "retry\tdocs")
	if err != nil {
		t.Fatalf("scoped grep: %v", err)
	}
	if strings.Contains(got, "a.md:2") {
		t.Fatalf("scoped grep leaked root files: %q", got)
	}
}

func TestFSToolsRejectBinaryAndInvalidRoot(t *testing.T) {
	root, tools := fsTestRoot(t)
	bin := []byte{'o', 'k', 0, 'n', 'u', 'l'}
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fsInvoke(t, tools, "read_file", "blob.bin"); err == nil {
		t.Fatal("read_file must reject binary files")
	}
	if got := fsTools(filepath.Join(root, "does-not-exist")); len(got) != 0 {
		t.Fatalf("invalid root must yield no tools, got %d", len(got))
	}
}

func TestSystemPromptForLanguage(t *testing.T) {
	en := &config.Config{Language: config.LangEn}
	zh := &config.Config{Language: config.LangZh}
	if systemPromptFor(en) != SystemPrompt {
		t.Fatal("English config must select SystemPrompt")
	}
	if systemPromptFor(zh) != SystemPromptZh {
		t.Fatal("Chinese config must select SystemPromptZh")
	}
	if strings.Contains(SystemPrompt, "Answer only from") {
		t.Fatal("system prompt must not restrict the model to reciting evidence")
	}
}
