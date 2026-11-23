package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyProfileLiteDisablesNeuralRerank(t *testing.T) {
	cfg := Default()
	cfg.Profile = ProfileLite
	cfg.Embedder = ProviderRembed
	cfg.RerankEnabled = true
	cfg.RerankType = RerankLocalMaxSim

	cfg.ApplyProfile()

	if cfg.Embedder != ProviderLocal || cfg.RerankEnabled {
		t.Fatalf("lite profile = %q, rerank %t", cfg.Embedder, cfg.RerankEnabled)
	}
	if len(cfg.PolicyNotes) < 2 {
		t.Fatalf("policy notes = %+v, want forced embedder and rerank", cfg.PolicyNotes)
	}
}

func TestApplyProfileProForcesLocalStack(t *testing.T) {
	cfg := Default()
	cfg.Profile = ProfilePro
	cfg.Embedder = ProviderLocal
	cfg.RetrievalMethod = RetrievalVector
	cfg.RerankType = RerankAikit
	cfg.RerankEnabled = false
	cfg.CandidateK = 10
	cfg.BM25TopK = 10

	cfg.ApplyProfile()

	if cfg.Embedder != ProviderRembed {
		t.Fatalf("embedder = %q, want rembed", cfg.Embedder)
	}
	if cfg.RerankType != RerankLocalMaxSim || !cfg.RerankEnabled {
		t.Fatalf("rerank = %q/%t, want local_maxsim/on", cfg.RerankType, cfg.RerankEnabled)
	}
	if cfg.RetrievalMethod != RetrievalHybrid || cfg.CandidateK < 50 || cfg.BM25TopK < 50 {
		t.Fatalf("candidate settings = %q/%d/%d", cfg.RetrievalMethod, cfg.CandidateK, cfg.BM25TopK)
	}
	if len(cfg.PolicyNotes) == 0 {
		t.Fatal("pro profile must explain forced changes")
	}
}

func TestApplyProfileCustomKeepsManualChoice(t *testing.T) {
	cfg := Default()
	cfg.Profile = ProfileCustom
	cfg.Embedder = ProviderRembed
	cfg.RerankType = RerankLocalMaxSim
	cfg.RerankEnabled = true

	cfg.ApplyProfile()

	if cfg.Embedder != ProviderRembed || cfg.RerankType != RerankLocalMaxSim {
		t.Fatalf("custom profile changed manual settings")
	}
}

func TestDefaultIsLite(t *testing.T) {
	cfg := Default()
	if cfg.Profile != ProfileLite {
		t.Fatalf("default profile = %q, want %q", cfg.Profile, ProfileLite)
	}
	if cfg.RerankEnabled {
		t.Fatal("lite default must not enable a model-dependent reranker")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestCanonicalProfileAliases(t *testing.T) {
	cases := map[string]string{
		"":             ProfileLite,
		"local":        ProfileLite,
		"multilingual": ProfilePro,
		"lite":         ProfileLite,
		"pro":          ProfilePro,
		"custom":       ProfileCustom,
		"  PRO ":       ProfilePro,
		"MULTILINGUAL": ProfilePro,
	}
	for in, want := range cases {
		if got := canonicalProfile(in); got != want {
			t.Errorf("canonicalProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetFieldAcceptsLegacyProfileNames(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"local", ProfileLite},
		{"multilingual", ProfilePro},
		{"pro", ProfilePro},
	} {
		cfg := Default()
		if err := cfg.SetField("profile", tc.in); err != nil {
			t.Fatalf("SetField(profile, %q): %v", tc.in, err)
		}
		if cfg.Profile != tc.want {
			t.Fatalf("SetField(profile, %q) = %q, want %q", tc.in, cfg.Profile, tc.want)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate after SetField(%q): %v", tc.in, err)
		}
	}
}

func TestLoadMigratesLegacyProfileNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, DirName), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want string }{
		{"local", ProfileLite},
		{"multilingual", ProfilePro},
		{"custom", ProfileCustom},
	} {
		data := "profile: " + tc.in + "\n"
		if err := os.WriteFile(filepath.Join(home, DirName, FileName), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(profile: %s): %v", tc.in, err)
		}
		if cfg.Profile != tc.want {
			t.Fatalf("Load(profile: %s) = %q, want %q", tc.in, cfg.Profile, tc.want)
		}
	}
}

func TestLoadWithoutProfileKeepsLegacyBehaviour(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, DirName), 0o700); err != nil {
		t.Fatal(err)
	}
	// A pre-profile config: no profile key, with the old aikit reranker on.
	data := "embedder: local\nrerank_enabled: true\nrerank_type: aikit\n"
	if err := os.WriteFile(filepath.Join(home, DirName, FileName), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Profile != ProfileCustom {
		t.Fatalf("legacy config profile = %q, want %q", cfg.Profile, ProfileCustom)
	}
	if !cfg.RerankEnabled || cfg.RerankType != RerankAikit {
		t.Fatalf("legacy config lost its reranker: enabled=%t type=%q", cfg.RerankEnabled, cfg.RerankType)
	}
}

func TestSaveWritesCanonicalProfileName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Default()
	cfg.Profile = "multilingual"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Profile != ProfilePro {
		t.Fatalf("saved profile = %q, want %q", loaded.Profile, ProfilePro)
	}
}
