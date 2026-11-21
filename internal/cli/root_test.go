package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
)

// runCmd builds a fresh root command for each invocation so no cobra state
// leaks between commands. cfg is shared so settings mutations are visible.
func runCmd(t *testing.T, cfg *config.Config, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := NewRootCmd(cfg, "test")
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestRootCommandWiring(t *testing.T) {
	root := NewRootCmd(config.Default(), "1.2.3")

	if root.Use != "kbcli" {
		t.Fatalf("Use = %q, want kbcli", root.Use)
	}
	if root.RunE == nil {
		t.Fatal("root command is not wired with RunE")
	}
	if !root.SilenceUsage {
		t.Fatal("root.SilenceUsage should be true")
	}
	if root.SilenceErrors {
		t.Fatal("root.SilenceErrors should be false")
	}
	if root.PersistentFlags().Lookup("index") == nil {
		t.Fatal("missing --index persistent flag")
	}
	if root.PersistentFlags().Lookup("dir") == nil {
		t.Fatal("missing --dir persistent flag")
	}

	want := map[string]bool{
		"index": false, "reindex": false, "update": false,
		"config": false, "settings": false, "search": false, "version": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}

	for _, c := range root.Commands() {
		switch c.Name() {
		case "config":
			cwant := map[string]bool{"set": false, "get": false, "list": false, "path": false, "keys": false}
			for _, sub := range c.Commands() {
				if _, ok := cwant[sub.Name()]; ok {
					cwant[sub.Name()] = true
				}
			}
			for name, found := range cwant {
				if !found {
					t.Errorf("missing config subcommand %q", name)
				}
			}
		case "settings":
			swant := map[string]bool{
				"path": false, "list": false, "show": false,
				"use-global": false, "use-workspace": false,
				"set": false, "unset": false, "reset": false,
			}
			for _, sub := range c.Commands() {
				if _, ok := swant[sub.Name()]; ok {
					swant[sub.Name()] = true
				}
			}
			for name, found := range swant {
				if !found {
					t.Errorf("missing settings subcommand %q", name)
				}
			}
		}
	}
}

func TestSettingsCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KBCLI_INDEX", "")
	root := t.TempDir()
	cfg := config.Default()

	out, err := runCmd(t, cfg, "settings", "path")
	if err != nil {
		t.Fatalf("settings path: %v\n%s", err, out)
	}
	if !strings.Contains(out, "settings.json") {
		t.Fatalf("settings path = %q", out)
	}

	out, err = runCmd(t, cfg, "settings", "list")
	if err != nil {
		t.Fatalf("settings list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no workspace overrides") {
		t.Fatalf("empty list = %q", out)
	}

	out, err = runCmd(t, cfg, "settings", "set", "top_k", "9", "--dir", root)
	if err != nil {
		t.Fatalf("settings set: %v\n%s", err, out)
	}

	out, err = runCmd(t, cfg, "settings", "list")
	if err != nil {
		t.Fatalf("settings list after set: %v\n%s", err, out)
	}
	if !strings.Contains(out, "top_k") || !strings.Contains(out, root) {
		t.Fatalf("list after set = %q", out)
	}

	out, err = runCmd(t, cfg, "settings", "show", "--dir", root)
	if err != nil {
		t.Fatalf("settings show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "workspace:") || !strings.Contains(out, "top_k") {
		t.Fatalf("show = %q", out)
	}

	if _, err := runCmd(t, cfg, "settings", "use-global", "--dir", root); err != nil {
		t.Fatalf("use-global: %v", err)
	}
	if _, err := runCmd(t, cfg, "settings", "unset", "top_k", "--dir", root); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if _, err := runCmd(t, cfg, "settings", "reset", "--dir", root); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func TestVersionOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var buf bytes.Buffer
	root := NewRootCmd(config.Default(), "1.2.3")
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "kbcli 1.2.3" {
		t.Fatalf("version output = %q", buf.String())
	}
}
