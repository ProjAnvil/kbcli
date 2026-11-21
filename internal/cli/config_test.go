package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
)

func TestConfigSetGetListPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	cfg := config.Default()

	if out, err := runCmd(t, cfg, "config", "set", "api_key", "sk-abcd1234"); err != nil {
		t.Fatalf("set: %v\n%s", err, out)
	}

	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %o, want 600", info.Mode().Perm())
	}

	out, err := runCmd(t, cfg, "config", "get", "api_key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "sk-abcd1234" {
		t.Fatalf("get api_key = %q", out)
	}

	out, err = runCmd(t, cfg, "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sk-abcd1234") {
		t.Fatalf("api_key leaked in list output: %s", out)
	}
	if !strings.Contains(out, "****1234") {
		t.Fatalf("api_key is not masked in list output: %s", out)
	}
	if !strings.Contains(out, "api_key=") {
		t.Fatalf("api_key line missing from list output: %s", out)
	}

	out, err = runCmd(t, cfg, "config", "path")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != path {
		t.Fatalf("path = %q, want %q", out, path)
	}

	if _, err := runCmd(t, cfg, "config", "set", "top_k", "abc"); err == nil {
		t.Fatal("expected an error for a non-integer top_k")
	}

	out, err = runCmd(t, cfg, "config", "set", "chunk_size", "800")
	if err != nil {
		t.Fatalf("set chunk_size: %v\n%s", err, out)
	}
	out, err = runCmd(t, cfg, "config", "get", "chunk_size")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "800" {
		t.Fatalf("get chunk_size = %q, want 800", out)
	}

	out, err = runCmd(t, cfg, "config", "keys")
	if err != nil {
		t.Fatalf("config keys: %v\n%s", err, out)
	}
	for _, key := range config.FieldKeys() {
		if !strings.Contains(out, key) {
			t.Fatalf("config keys output is missing %q", key)
		}
	}
}
