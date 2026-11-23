package config

import "testing"

func TestApplyProfileLocalDisablesNeuralRerank(t *testing.T) {
	cfg := Default()
	cfg.Profile = ProfileLocal
	cfg.Embedder = ProviderRembed
	cfg.RerankEnabled = true
	cfg.RerankType = RerankLocalMaxSim

	cfg.ApplyProfile()

	if cfg.Embedder != ProviderLocal || cfg.RerankEnabled {
		t.Fatalf("local profile = %q, rerank %t", cfg.Embedder, cfg.RerankEnabled)
	}
	if len(cfg.PolicyNotes) < 2 {
		t.Fatalf("policy notes = %+v, want forced embedder and rerank", cfg.PolicyNotes)
	}
}

func TestApplyProfileMultilingualForcesLocalStack(t *testing.T) {
	cfg := Default()
	cfg.Profile = ProfileMultilingual
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
		t.Fatal("multilingual profile must explain forced changes")
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
