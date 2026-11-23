package config

import (
	"fmt"
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

func TestPerExtensionChunkingDefaults(t *testing.T) {
	d := Default()
	if d.ChunkSize != 4000 || d.ChunkOverlap != 200 {
		t.Fatalf("plain-text defaults = %d/%d, want 4000/200", d.ChunkSize, d.ChunkOverlap)
	}
	if d.SectionSize != 1000 {
		t.Fatalf("SectionSize = %d, want 1000", d.SectionSize)
	}
	if d.ChunkStrategy != ChunkStrategyAuto {
		t.Fatalf("ChunkStrategy = %q, want %q", d.ChunkStrategy, ChunkStrategyAuto)
	}
	if d.CandidateK != 40 {
		t.Fatalf("CandidateK = %d, want 40", d.CandidateK)
	}
	if d.SetupDone {
		t.Fatal("SetupDone must default to false so the first-run wizard appears")
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}

	keys := IndexAffectingKeys()
	for _, want := range []string{"section_size", "chunk_strategy"} {
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
	for _, mustNotAffect := range []string{"candidate_k", "setup_done"} {
		for _, k := range keys {
			if k == mustNotAffect {
				t.Errorf("%q must not be index affecting", mustNotAffect)
			}
		}
	}
}

func TestChunkerVersionInvalidatesIndex(t *testing.T) {
	before, after := Default(), Default()
	after.SectionSize = 700
	if got := IndexAffectingChanges(before, after); len(got) != 1 || got[0] != "section_size" {
		t.Fatalf("section_size drift = %v, want [section_size]", got)
	}
	after = Default()
	after.ChunkStrategy = ChunkStrategyFixed
	if got := IndexAffectingChanges(before, after); len(got) != 1 || got[0] != "chunk_strategy" {
		t.Fatalf("chunk_strategy drift = %v, want [chunk_strategy]", got)
	}
	sig := before.IndexSignature()
	for _, want := range []string{"section_size=1000", "chunk_strategy=auto", "chunker=" + ChunkerVersion} {
		if !strings.Contains(sig, want) {
			t.Errorf("IndexSignature %q missing %q", sig, want)
		}
	}
	// CandidateK is query-time only: changing it must not report drift.
	after = Default()
	after.CandidateK = 80
	if got := IndexAffectingChanges(before, after); len(got) != 0 {
		t.Fatalf("candidate_k must not invalidate the index, got %v", got)
	}
	if got := IndexSignatureChanges(sig, after); len(got) != 0 {
		t.Fatalf("candidate_k must not change the signature drift, got %v", got)
	}
}

func TestNewFieldsValidateAndRoundTrip(t *testing.T) {
	c := Default()
	for _, tc := range []struct {
		key, value string
	}{
		{"section_size", "1400"},
		{"candidate_k", "80"},
		{"chunk_strategy", ChunkStrategyFixed},
	} {
		if err := c.SetField(tc.key, tc.value); err != nil {
			t.Fatalf("SetField(%q, %q): %v", tc.key, tc.value, err)
		}
		if got, err := c.GetField(tc.key); err != nil || fmt.Sprint(got) != tc.value {
			t.Fatalf("GetField(%q) = %v, %v; want %q", tc.key, got, err, tc.value)
		}
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("edited config invalid: %v", err)
	}
	bad := Default()
	for _, tc := range []struct {
		key, value string
	}{
		{"chunk_strategy", "magic"},
		{"section_size", "0"},
		{"candidate_k", "0"},
	} {
		bad = Default()
		if err := bad.SetField(tc.key, tc.value); err == nil {
			t.Errorf("SetField(%q, %q) must fail", tc.key, tc.value)
		}
	}
}

func TestPartialFileAppliesNewDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("api_key: sk-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "sk-existing" {
		t.Fatalf("api_key = %q", cfg.APIKey)
	}
	if cfg.SectionSize != 1000 || cfg.CandidateK != 40 || cfg.ChunkStrategy != ChunkStrategyAuto {
		t.Fatalf("new defaults not applied to a partial file: %+v", cfg)
	}
	// A user with an existing key must not be nagged by the wizard.
	if cfg.SetupDone {
		t.Fatal("setup_done must not be forced true for an existing config")
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
	// lite is the default tier and downloads no model, so the aikit reranker
	// is dormant. The aikit fields stay populated so switching to custom has
	// a usable starting point.
	if d.RerankEnabled {
		t.Fatal("lite default must not enable the model-dependent aikit reranker")
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
