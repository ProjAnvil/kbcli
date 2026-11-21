package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReturnsDefaultsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder != ProviderLocal || cfg.Agent != AgentReact {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.TopK != 6 || cfg.ChunkSize != 600 {
		t.Fatalf("numeric defaults = %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(mustDir(t), FileName)); !os.IsNotExist(err) {
		t.Fatal("Load must not create a settings file")
	}
}

func TestSaveLoadRoundTripAndPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Default()
	cfg.APIKey = "sk-test-secret"
	cfg.Model = "gpt-4o"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(home, DirName, FileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.APIKey != "sk-test-secret" || loaded.Model != "gpt-4o" {
		t.Fatalf("round trip = %+v", loaded)
	}
}

func TestValidateRejectsBadSelectors(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"embedder", func(c *Config) { c.Embedder = "unknown" }},
		{"agent", func(c *Config) { c.Agent = "unknown" }},
		{"top_k", func(c *Config) { c.TopK = 0 }},
		{"chunk_size", func(c *Config) { c.ChunkSize = -1 }},
		{"overlap", func(c *Config) { c.ChunkOverlap = c.ChunkSize }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mut(c)
			if err := c.Validate(); err == nil {
				t.Fatalf("Validate accepted %+v", c)
			}
		})
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestChatProviderSelector(t *testing.T) {
	d := Default()
	if d.ChatProvider != ChatProviderOpenAI {
		t.Fatalf("default chat_provider = %q, want %q", d.ChatProvider, ChatProviderOpenAI)
	}
	if d.BaseURL != DefaultOpenAIBaseURL {
		t.Fatalf("default base_url = %q, want %q", d.BaseURL, DefaultOpenAIBaseURL)
	}
	for _, p := range []string{ChatProviderOpenAI, ChatProviderAnthropic} {
		c := Default()
		c.ChatProvider = p
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate(%q): %v", p, err)
		}
	}
	bad := Default()
	bad.ChatProvider = "gemini"
	err := bad.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unknown chat_provider")
	}
	if !strings.Contains(err.Error(), ChatProviderOpenAI) || !strings.Contains(err.Error(), ChatProviderAnthropic) {
		t.Fatalf("error must name both providers: %v", err)
	}

	empty := &Config{}
	empty.applyDefaults()
	if empty.ChatProvider != ChatProviderOpenAI {
		t.Fatalf("applyDefaults chat_provider = %q", empty.ChatProvider)
	}
	empty = &Config{}
	empty.Normalize()
	if empty.ChatProvider != ChatProviderOpenAI {
		t.Fatalf("Normalize chat_provider = %q", empty.ChatProvider)
	}

	before, after := Default(), Default()
	after.ChatProvider = ChatProviderAnthropic
	keys := append(append([]string{}, IndexAffectingKeys()...), IndexAffectingChanges(before, after)...)
	for _, k := range keys {
		if k == "chat_provider" {
			t.Fatal("chat_provider must not be index affecting")
		}
	}
}

func TestPartialFileAppliesDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("model: custom-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "custom-model" || cfg.TopK != 6 || cfg.Embedder != ProviderLocal {
		t.Fatalf("partial load = %+v", cfg)
	}
}

func mustDir(t *testing.T) string {
	t.Helper()
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
