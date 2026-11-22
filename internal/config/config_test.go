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
	if cfg.TopK != 6 || cfg.ChunkSize != 4000 || cfg.ChunkOverlap != 200 {
		t.Fatalf("numeric defaults = %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(mustDir(t), FileName)); !os.IsNotExist(err) {
		t.Fatal("Load must not create a settings file")
	}
}

func TestChunkingBaselineDefaults(t *testing.T) {
	d := Default()
	if d.ChunkSize != 4000 {
		t.Fatalf("ChunkSize = %d, want 4000", d.ChunkSize)
	}
	if d.ChunkOverlap != 200 {
		t.Fatalf("ChunkOverlap = %d, want 200", d.ChunkOverlap)
	}
	keys := IndexAffectingKeys()
	for _, want := range []string{"chunk_size", "chunk_overlap"} {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q must be index affecting, got %v", want, keys)
		}
	}
}

func TestChunkingLangChainDefaults(t *testing.T) {
	d := Default()
	if d.ChunkSize != 4000 {
		t.Fatalf("ChunkSize = %d, want 4000", d.ChunkSize)
	}
	if d.ChunkOverlap != 200 {
		t.Fatalf("ChunkOverlap = %d, want 200", d.ChunkOverlap)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	keys := IndexAffectingKeys()
	for _, want := range []string{"chunk_size", "chunk_overlap"} {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q must be index affecting, got %v", want, keys)
		}
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

func TestAikitRerankConfig(t *testing.T) {
	d := Default()
	if !d.RerankEnabled {
		t.Fatal("default rerank_enabled must be true")
	}
	if d.RerankType != RerankAikit {
		t.Fatalf("default rerank_type = %q, want %q", d.RerankType, RerankAikit)
	}
	if d.RerankModel != DefaultRerankModel {
		t.Fatalf("default rerank_model = %q, want %q", d.RerankModel, DefaultRerankModel)
	}
	if d.RerankModelPath != "" {
		t.Fatalf("default rerank_model_path = %q, want empty", d.RerankModelPath)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}

	// An empty path means a managed model directory and is valid for aikit.
	emptyPath := Default()
	emptyPath.RerankModelPath = ""
	if err := emptyPath.Validate(); err != nil {
		t.Fatalf("aikit with empty path rejected: %v", err)
	}
	custom := Default()
	custom.RerankModelPath = "/models/bge-reranker"
	if err := custom.Validate(); err != nil {
		t.Fatalf("aikit with custom path rejected: %v", err)
	}
}

func TestRerankModelNormalization(t *testing.T) {
	c := &Config{}
	c.Normalize()
	if c.RerankModel != DefaultRerankModel {
		t.Fatalf("Normalize rerank_model = %q, want %q", c.RerankModel, DefaultRerankModel)
	}
	c = &Config{}
	c.applyDefaults()
	if c.RerankModel != DefaultRerankModel {
		t.Fatalf("applyDefaults rerank_model = %q, want %q", c.RerankModel, DefaultRerankModel)
	}
	c = Default()
	c.RerankModel = "custom-reranker"
	c.Normalize()
	if c.RerankModel != "custom-reranker" {
		t.Fatalf("Normalize overwrote explicit rerank_model = %q", c.RerankModel)
	}
}

func TestModelDirSanitization(t *testing.T) {
	root := filepath.Join(mustDir(t), "model")
	for _, tc := range []struct{ name, want string }{
		{"", root},
		{"   ", root},
		{".", root},
		{DefaultRerankModel, filepath.Join(root, DefaultRerankModel)},
		{"  /models/nested/bge-reranker  ", filepath.Join(root, "bge-reranker")},
	} {
		if got := ModelDir(tc.name); got != tc.want {
			t.Fatalf("ModelDir(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRerankModelPathNotIndexAffecting(t *testing.T) {
	for _, k := range IndexAffectingKeys() {
		if k == "rerank_model_path" {
			t.Fatal("rerank_model_path must not be index affecting")
		}
	}
}

func TestRerankModelPathFieldRoundTrip(t *testing.T) {
	c := Default()
	if err := c.SetField("rerank_model_path", "/models/bge-reranker"); err != nil {
		t.Fatalf("SetField: %v", err)
	}
	if got, err := c.GetField("rerank_model_path"); err != nil {
		t.Fatalf("GetField: %v", err)
	} else if got != "/models/bge-reranker" {
		t.Fatalf("round trip = %v, want path", got)
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
