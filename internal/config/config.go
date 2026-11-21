// Package config owns kbcli user settings persisted under ~/.kbcli.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DirName is the settings directory inside the user home directory.
	DirName = ".kbcli"
	// FileName is the settings file name for the global configuration.
	FileName = "config.yaml"
	// WorkspaceFileName stores the workspace-to-settings relation.
	WorkspaceFileName = "settings.json"
	// SessionsDirName is the directory holding per-workspace session memory.
	SessionsDirName = "sessions"

	LangEn = "en"
	LangZh = "zh"

	// DefaultIndexFileName is the vector file created in an indexed directory.
	// It must stay equal to index.DefaultFileName.
	DefaultIndexFileName = ".kbvec"

	ProviderLocal  = "local"
	ProviderRembed = "rembed"
	AgentReact     = "react"
	AgentDeep      = "deep"

	ChatProviderOpenAI    = "openai"
	ChatProviderAnthropic = "anthropic"

	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
	DefaultAnthropicBaseURL = "https://api.anthropic.com/v1"

	RetrievalVector = "vector"
	RetrievalBM25   = "bm25"
	RetrievalHybrid = "hybrid"

	RerankCrossEncoder = "cross_encoder"
	RerankLLM          = "llm"

	TransformNone          = "none"
	TransformHyDE          = "hyde"
	TransformMultiQuery    = "multi_query"
	TransformDecomposition = "decomposition"
	TransformStepBack      = "step_back"
)

// Config holds every user-tunable knob. Zero values are replaced by defaults
// on load, so a partially written file is always usable.
//
// Fields are grouped by the pipeline stage they affect. Fields marked
// "(index)" change the bytes that get embedded and therefore require a
// rebuild when modified. All other fields only affect query time.
type Config struct {
	// Foundation settings.
	APIKey         string `yaml:"api_key" json:"api_key"`
	BaseURL        string `yaml:"base_url" json:"base_url"`
	Model          string `yaml:"model" json:"model"`
	ChatProvider   string `yaml:"chat_provider" json:"chat_provider"`
	Embedder       string `yaml:"embedder" json:"embedder"`       // (index)
	EmbedModel     string `yaml:"embed_model" json:"embed_model"` // (index)
	Agent          string `yaml:"agent" json:"agent"`
	IndexFileName  string `yaml:"index_file" json:"index_file"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxIterations  int    `yaml:"max_iterations" json:"max_iterations"`

	// Ingestion settings (index).
	ChunkSize    int `yaml:"chunk_size" json:"chunk_size"`
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap"`

	// Retrieval settings. Ported from Porto's rag namespace.
	TopK                 int     `yaml:"top_k" json:"top_k"`
	RetrievalMethod      string  `yaml:"retrieval_method" json:"retrieval_method"`
	BM25TopK             int     `yaml:"bm25_top_k" json:"bm25_top_k"`
	HybridVectorWeight   float64 `yaml:"hybrid_vector_weight" json:"hybrid_vector_weight"`
	RRFK                 int     `yaml:"rrf_k" json:"rrf_k"`
	BM25K1               float64 `yaml:"bm25_k1" json:"bm25_k1"`
	BM25B                float64 `yaml:"bm25_b" json:"bm25_b"`
	MinScore             float64 `yaml:"min_score" json:"min_score"`
	ContextExpand        int     `yaml:"context_expand" json:"context_expand"`
	ContextCharBudget    int     `yaml:"context_char_budget" json:"context_char_budget"`
	ContextMaxChunkChars int     `yaml:"context_max_chunk_chars" json:"context_max_chunk_chars"`

	// Rerank settings. Ported from Porto's rerankers.py.
	RerankEnabled bool   `yaml:"rerank_enabled" json:"rerank_enabled"`
	RerankType    string `yaml:"rerank_type" json:"rerank_type"`
	RerankTopN    int    `yaml:"rerank_top_n" json:"rerank_top_n"`
	RerankBaseURL string `yaml:"rerank_base_url" json:"rerank_base_url"`
	RerankModel   string `yaml:"rerank_model" json:"rerank_model"`
	RerankAPIKey  string `yaml:"rerank_api_key" json:"rerank_api_key"`

	// Query transform settings. Ported from Porto's query_transform.py.
	QueryTransform  string `yaml:"query_transform" json:"query_transform"`
	MultiQueryCount int    `yaml:"multi_query_count" json:"multi_query_count"`
	CondenseEnabled bool   `yaml:"condense_enabled" json:"condense_enabled"`

	// Internationalization and session memory. Ported from Porto's
	// memory/{facts,compaction,session_store}.py and orchestrator.py.
	Language                 string  `yaml:"language" json:"language"`
	SessionEnabled           bool    `yaml:"session_enabled" json:"session_enabled"`
	FactsEnabled             bool    `yaml:"facts_enabled" json:"facts_enabled"`
	FactsMaxPerCategory      int     `yaml:"facts_max_per_category" json:"facts_max_per_category"`
	FactsSimilarityThreshold float64 `yaml:"facts_similarity_threshold" json:"facts_similarity_threshold"`
	FactsRecentContextTurns  int     `yaml:"facts_recent_context_turns" json:"facts_recent_context_turns"`
	CompactAfterTurns        int     `yaml:"compact_after_turns" json:"compact_after_turns"`
	SessionMemoryTopK        int     `yaml:"session_memory_top_k" json:"session_memory_top_k"`
}

// Default returns the out-of-the-box configuration. All new optimization
// switches default to the pre-existing behaviour (pure vector retrieval, no
// rerank, no query transform), so an upgraded install changes nothing.
func Default() *Config {
	return &Config{
		ChatProvider:             ChatProviderOpenAI,
		BaseURL:                  DefaultOpenAIBaseURL,
		Model:                    "gpt-4o-mini",
		Embedder:                 ProviderLocal,
		Agent:                    AgentReact,
		IndexFileName:            DefaultIndexFileName,
		TimeoutSeconds:           120,
		MaxIterations:            8,
		ChunkSize:                600,
		ChunkOverlap:             0,
		TopK:                     6,
		RetrievalMethod:          RetrievalVector,
		BM25TopK:                 20,
		HybridVectorWeight:       0.5,
		RRFK:                     60,
		BM25K1:                   1.5,
		BM25B:                    0.75,
		MinScore:                 0.0,
		ContextExpand:            0,
		ContextCharBudget:        16000,
		ContextMaxChunkChars:     800,
		RerankEnabled:            false,
		RerankType:               RerankCrossEncoder,
		RerankTopN:               5,
		QueryTransform:           TransformNone,
		MultiQueryCount:          4,
		CondenseEnabled:          true,
		Language:                 LangEn,
		SessionEnabled:           true,
		FactsEnabled:             true,
		FactsMaxPerCategory:      20,
		FactsSimilarityThreshold: 0.5,
		FactsRecentContextTurns:  6,
		CompactAfterTurns:        20,
		SessionMemoryTopK:        5,
	}
}

// Clone returns an independent copy. Workspace overlays must never mutate the
// global configuration in place.
func (c *Config) Clone() *Config {
	if c == nil {
		return Default()
	}
	out := *c
	return &out
}

// IndexSignature is the Porto SPLITTER_VERSION equivalent: a stable, readable
// encoding of every setting that changes the bytes fed to the embedder. It is
// stored in the vector file header so a stale index can be detected on load,
// and so the UI can name the exact fields that drifted.
func (c *Config) IndexSignature() string {
	return strings.Join([]string{
		"chunk_size=" + strconv.Itoa(c.ChunkSize),
		"chunk_overlap=" + strconv.Itoa(c.ChunkOverlap),
		"embedder=" + c.Embedder,
		"embed_model=" + c.EmbedModel,
	}, "|")
}

// IndexSignatureChanges compares a stored signature with the current config
// and returns the field names that drifted. A legacy empty signature reports
// no changes so old index files stay usable.
func IndexSignatureChanges(stored string, current *Config) []string {
	if stored == "" || current == nil || stored == current.IndexSignature() {
		return nil
	}
	old := parseSignature(stored)
	now := parseSignature(current.IndexSignature())
	var out []string
	for _, key := range IndexAffectingKeys() {
		if old[key] != now[key] {
			out = append(out, key)
		}
	}
	return out
}

func parseSignature(sig string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(sig, "|") {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

// IndexAffectingChanges lists fields whose change invalidates an existing
// vector index. An empty slice means no rebuild is needed.
func IndexAffectingChanges(before, after *Config) []string {
	if before == nil || after == nil {
		return nil
	}
	var out []string
	if before.ChunkSize != after.ChunkSize {
		out = append(out, "chunk_size")
	}
	if before.ChunkOverlap != after.ChunkOverlap {
		out = append(out, "chunk_overlap")
	}
	if before.Embedder != after.Embedder {
		out = append(out, "embedder")
	}
	if before.EmbedModel != after.EmbedModel {
		out = append(out, "embed_model")
	}
	return out
}

// Dir returns the absolute settings directory path.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, DirName), nil
}

// Path returns the absolute settings file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// WorkspacePath returns the absolute path of the workspace relation file.
func WorkspacePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, WorkspaceFileName), nil
}

// SessionsDir returns the directory holding per-workspace session memory.
func SessionsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SessionsDirName), nil
}

// Load reads settings from disk. A missing file yields defaults without
// creating anything; callers decide when to persist.
func Load() (*Config, error) {
	c := Default()
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes settings with owner-only permissions because the file holds an
// API key.
func (c *Config) Save() error {
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: encode settings: %w", err)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// Validate rejects unsupported selectors and nonsensical numeric bounds.
// Zero values of the new numeric knobs are legal; Default supplies the normal
// values when a field is absent from disk.
func (c *Config) Validate() error {
	switch c.ChatProvider {
	case ChatProviderOpenAI, ChatProviderAnthropic:
	default:
		return fmt.Errorf("config: unsupported chat_provider %q (want %q or %q)", c.ChatProvider, ChatProviderOpenAI, ChatProviderAnthropic)
	}
	switch c.Embedder {
	case ProviderLocal, ProviderRembed:
	default:
		return fmt.Errorf("config: unsupported embedder %q (want %q or %q)", c.Embedder, ProviderLocal, ProviderRembed)
	}
	switch c.Agent {
	case AgentReact, AgentDeep:
	default:
		return fmt.Errorf("config: unsupported agent %q (want %q or %q)", c.Agent, AgentReact, AgentDeep)
	}
	switch c.RetrievalMethod {
	case RetrievalVector, RetrievalBM25, RetrievalHybrid:
	default:
		return fmt.Errorf("config: unsupported retrieval_method %q (want vector, bm25 or hybrid)", c.RetrievalMethod)
	}
	switch c.RerankType {
	case RerankCrossEncoder, RerankLLM:
	default:
		return fmt.Errorf("config: unsupported rerank_type %q (want cross_encoder or llm)", c.RerankType)
	}
	switch c.QueryTransform {
	case TransformNone, TransformHyDE, TransformMultiQuery, TransformDecomposition, TransformStepBack:
	default:
		return fmt.Errorf("config: unsupported query_transform %q", c.QueryTransform)
	}
	switch c.Language {
	case LangEn, LangZh:
	default:
		return fmt.Errorf("config: unsupported language %q (want %q or %q)", c.Language, LangEn, LangZh)
	}
	if c.FactsMaxPerCategory < 1 || c.FactsMaxPerCategory > 100 {
		return fmt.Errorf("config: facts_max_per_category must be 1..100, got %d", c.FactsMaxPerCategory)
	}
	if c.FactsSimilarityThreshold < 0 || c.FactsSimilarityThreshold > 1 {
		return fmt.Errorf("config: facts_similarity_threshold must be 0..1, got %g", c.FactsSimilarityThreshold)
	}
	if c.FactsRecentContextTurns < 1 || c.FactsRecentContextTurns > 20 {
		return fmt.Errorf("config: facts_recent_context_turns must be 1..20, got %d", c.FactsRecentContextTurns)
	}
	if c.CompactAfterTurns < 1 || c.CompactAfterTurns > 1000 {
		return fmt.Errorf("config: compact_after_turns must be 1..1000, got %d", c.CompactAfterTurns)
	}
	if c.SessionMemoryTopK < 0 || c.SessionMemoryTopK > 50 {
		return fmt.Errorf("config: session_memory_top_k must be 0..50, got %d", c.SessionMemoryTopK)
	}
	if c.TopK <= 0 || c.TopK > 50 {
		return fmt.Errorf("config: top_k must be 1..50, got %d", c.TopK)
	}
	if c.ChunkSize <= 0 {
		return fmt.Errorf("config: chunk_size must be > 0, got %d", c.ChunkSize)
	}
	if c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkSize {
		return fmt.Errorf("config: chunk_overlap must be 0..chunk_size-1, got %d", c.ChunkOverlap)
	}
	if c.MaxIterations <= 0 {
		return fmt.Errorf("config: max_iterations must be > 0, got %d", c.MaxIterations)
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("config: timeout_seconds must be > 0, got %d", c.TimeoutSeconds)
	}
	if c.BM25TopK < 1 || c.BM25TopK > 200 {
		return fmt.Errorf("config: bm25_top_k must be 1..200, got %d", c.BM25TopK)
	}
	if c.HybridVectorWeight < 0 || c.HybridVectorWeight > 1 {
		return fmt.Errorf("config: hybrid_vector_weight must be 0..1, got %g", c.HybridVectorWeight)
	}
	if c.RRFK < 1 {
		return fmt.Errorf("config: rrf_k must be >= 1, got %d", c.RRFK)
	}
	if c.BM25K1 < 0 || c.BM25K1 > 10 {
		return fmt.Errorf("config: bm25_k1 must be 0..10, got %g", c.BM25K1)
	}
	if c.BM25B < 0 || c.BM25B > 1 {
		return fmt.Errorf("config: bm25_b must be 0..1, got %g", c.BM25B)
	}
	if c.MinScore < 0 || c.MinScore > 1 {
		return fmt.Errorf("config: min_score must be 0..1, got %g", c.MinScore)
	}
	if c.ContextExpand < 0 || c.ContextExpand > 10 {
		return fmt.Errorf("config: context_expand must be 0..10, got %d", c.ContextExpand)
	}
	if c.ContextCharBudget < 1000 {
		return fmt.Errorf("config: context_char_budget must be >= 1000, got %d", c.ContextCharBudget)
	}
	if c.ContextMaxChunkChars < 1 {
		return fmt.Errorf("config: context_max_chunk_chars must be >= 1, got %d", c.ContextMaxChunkChars)
	}
	if c.RerankTopN < 1 || c.RerankTopN > 50 {
		return fmt.Errorf("config: rerank_top_n must be 1..50, got %d", c.RerankTopN)
	}
	if c.MultiQueryCount < 2 || c.MultiQueryCount > 8 {
		return fmt.Errorf("config: multi_query_count must be 2..8, got %d", c.MultiQueryCount)
	}
	return nil
}

// Normalize fills zero-value optimization knobs from Default without
// overwriting deliberate zero values of the float knobs, which are legal.
func (c *Config) Normalize() {
	d := Default()
	if c.BM25TopK <= 0 {
		c.BM25TopK = d.BM25TopK
	}
	if c.RRFK <= 0 {
		c.RRFK = d.RRFK
	}
	if c.ContextCharBudget <= 0 {
		c.ContextCharBudget = d.ContextCharBudget
	}
	if c.ContextMaxChunkChars <= 0 {
		c.ContextMaxChunkChars = d.ContextMaxChunkChars
	}
	if c.RerankTopN <= 0 {
		c.RerankTopN = d.RerankTopN
	}
	if c.MultiQueryCount <= 0 {
		c.MultiQueryCount = d.MultiQueryCount
	}
	if c.FactsMaxPerCategory <= 0 {
		c.FactsMaxPerCategory = d.FactsMaxPerCategory
	}
	if c.FactsRecentContextTurns <= 0 {
		c.FactsRecentContextTurns = d.FactsRecentContextTurns
	}
	if c.CompactAfterTurns <= 0 {
		c.CompactAfterTurns = d.CompactAfterTurns
	}
	if c.Language == "" {
		c.Language = d.Language
	}
	if c.ChatProvider == "" {
		c.ChatProvider = d.ChatProvider
	}
	if c.RetrievalMethod == "" {
		c.RetrievalMethod = d.RetrievalMethod
	}
	if c.RerankType == "" {
		c.RerankType = d.RerankType
	}
	if c.QueryTransform == "" {
		c.QueryTransform = d.QueryTransform
	}
	c.applyDefaults()
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.BaseURL == "" {
		c.BaseURL = d.BaseURL
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.ChatProvider == "" {
		c.ChatProvider = d.ChatProvider
	}
	if c.Embedder == "" {
		c.Embedder = d.Embedder
	}
	if c.Agent == "" {
		c.Agent = d.Agent
	}
	if c.IndexFileName == "" {
		c.IndexFileName = d.IndexFileName
	}
	if c.TopK <= 0 {
		c.TopK = d.TopK
	}
	if c.ChunkSize <= 0 {
		c.ChunkSize = d.ChunkSize
	}
	if c.ChunkOverlap < 0 {
		c.ChunkOverlap = d.ChunkOverlap
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = d.MaxIterations
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.Language == "" {
		c.Language = d.Language
	}
	if c.FactsMaxPerCategory <= 0 {
		c.FactsMaxPerCategory = d.FactsMaxPerCategory
	}
	if c.FactsRecentContextTurns <= 0 {
		c.FactsRecentContextTurns = d.FactsRecentContextTurns
	}
	if c.CompactAfterTurns <= 0 {
		c.CompactAfterTurns = d.CompactAfterTurns
	}
}
