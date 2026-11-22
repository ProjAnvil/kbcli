package config

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldKind is the value type of a configuration field.
type FieldKind int

const (
	// FieldInt is an integer knob.
	FieldInt FieldKind = iota
	// FieldFloat is a decimal knob.
	FieldFloat
	// FieldBool is an on/off switch.
	FieldBool
	// FieldEnum is one of a fixed set of strings.
	FieldEnum
	// FieldString is free-form text.
	FieldString
)

// Field describes one editable configuration knob. The settings screen, the
// workspace overrides and the CLI config command all share this catalog so a
// key can never drift between them.
type Field struct {
	Key         string
	Label       string
	Group       string
	Kind        FieldKind
	Min         float64
	Max         float64
	Step        float64
	Options     []string
	IndexAffect bool
	Secret      bool
	Help        string
}

const (
	groupFoundation = "Foundation"
	groupIngestion  = "Ingestion"
	groupRetrieval  = "Retrieval"
	groupRerank     = "Rerank"
	groupTransform  = "Query Transform"
	groupIntent     = "Intent"
	groupSession    = "Session"
)

var fieldCatalog = []Field{
	{Key: "api_key", Label: "API key", Group: groupFoundation, Kind: FieldString, Secret: true, Help: "LLM API key. Never printed in full."},
	{Key: "base_url", Label: "Base URL", Group: groupFoundation, Kind: FieldString, Help: "Chat Completions or Anthropic Messages endpoint."},
	{Key: "model", Label: "Chat model", Group: groupFoundation, Kind: FieldString, Help: "Model name sent to the endpoint."},
	{Key: "chat_provider", Label: "Chat provider", Group: groupFoundation, Kind: FieldEnum, Options: []string{ChatProviderOpenAI, ChatProviderAnthropic}, Help: "Chat API protocol. OpenAI-compatible or Anthropic Messages."},
	{Key: "embedder", Label: "Embedder", Group: groupFoundation, Kind: FieldEnum, Options: []string{ProviderLocal, ProviderRembed}, IndexAffect: true, Help: "Changing this requires a re-index."},
	{Key: "embed_model", Label: "Embed model", Group: groupFoundation, Kind: FieldString, IndexAffect: true, Help: "Neural model reference. Changing this requires a re-index."},
	{Key: "agent", Label: "Agent", Group: groupFoundation, Kind: FieldEnum, Options: []string{AgentReact, AgentDeep}},
	{Key: "index_file", Label: "Index file", Group: groupFoundation, Kind: FieldString, Help: "Vector file name inside a workspace."},
	{Key: "timeout_seconds", Label: "Timeout (s)", Group: groupFoundation, Kind: FieldInt, Min: 1, Max: 600, Step: 10},
	{Key: "max_iterations", Label: "Max iterations", Group: groupFoundation, Kind: FieldInt, Min: 1, Max: 50, Step: 1},

	{Key: "chunk_size", Label: "Chunk size", Group: groupIngestion, Kind: FieldInt, Min: 100, Max: 8000, Step: 100, IndexAffect: true, Help: "Rune cap for plain-text chunks only (default 4000). Structured formats use section_size. Changing this requires a re-index."},
	{Key: "chunk_overlap", Label: "Chunk overlap", Group: groupIngestion, Kind: FieldInt, Min: 0, Max: 2000, Step: 20, IndexAffect: true, Help: "Rune overlap between chunks (default 200). Changing this requires a re-index."},
	{Key: "section_size", Label: "Section size", Group: groupIngestion, Kind: FieldInt, Min: 100, Max: 8000, Step: 100, IndexAffect: true, Help: "Rune cap for one structured section: Markdown heading, CSV row group, HTML heading, PDF page, DOCX section (default 1000). Changing this requires a re-index."},
	{Key: "chunk_strategy", Label: "Chunk strategy", Group: groupIngestion, Kind: FieldEnum, Options: []string{ChunkStrategyAuto, ChunkStrategyFixed}, IndexAffect: true, Help: "auto picks a structure-aware chunker per file extension; fixed forces the legacy single-size splitter for every format. Changing this requires a re-index."},

	{Key: "top_k", Label: "Top K", Group: groupRetrieval, Kind: FieldInt, Min: 1, Max: 50, Step: 1, Help: "Final number of evidence passages."},
	{Key: "candidate_k", Label: "Candidate K", Group: groupRetrieval, Kind: FieldInt, Min: 1, Max: 200, Step: 5, Help: "Broad candidate pool fetched before reranking (default 40). Query-time only; no re-index needed."},
	{Key: "retrieval_method", Label: "Retrieval", Group: groupRetrieval, Kind: FieldEnum, Options: []string{RetrievalVector, RetrievalBM25, RetrievalHybrid}, Help: "Default: hybrid. Fuses dense vectors with BM25 by weighted RRF. vector and bm25 select a single channel."},
	{Key: "bm25_top_k", Label: "BM25 candidate K", Group: groupRetrieval, Kind: FieldInt, Min: 1, Max: 200, Step: 5, Help: "Porto: sparse candidate pool amplification before RRF fusion."},
	{Key: "hybrid_vector_weight", Label: "Vector weight", Group: groupRetrieval, Kind: FieldFloat, Min: 0, Max: 1, Step: 0.05, Help: "Porto: RRF weight for the vector list; BM25 gets 1-w."},
	{Key: "rrf_k", Label: "RRF k", Group: groupRetrieval, Kind: FieldInt, Min: 1, Max: 200, Step: 5, Help: "Porto: reciprocal rank smoothing constant, default 60."},
	{Key: "bm25_k1", Label: "BM25 k1", Group: groupRetrieval, Kind: FieldFloat, Min: 0, Max: 10, Step: 0.1, Help: "Porto: term frequency saturation."},
	{Key: "bm25_b", Label: "BM25 b", Group: groupRetrieval, Kind: FieldFloat, Min: 0, Max: 1, Step: 0.05, Help: "Porto: length normalization."},
	{Key: "min_score", Label: "Min score", Group: groupRetrieval, Kind: FieldFloat, Min: 0, Max: 1, Step: 0.01, Help: "kbcli extension: drop hits below this score."},
	{Key: "context_expand", Label: "Adjacent chunks", Group: groupRetrieval, Kind: FieldInt, Min: 0, Max: 10, Step: 1, Help: "kbcli extension: expand each hit with neighbouring chunks."},
	{Key: "context_char_budget", Label: "Context budget", Group: groupRetrieval, Kind: FieldInt, Min: 1000, Max: 64000, Step: 1000, Help: "Porto: character budget for the assembled prompt."},
	{Key: "context_max_chunk_chars", Label: "Per-chunk cap", Group: groupRetrieval, Kind: FieldInt, Min: 100, Max: 4000, Step: 100, Help: "Porto: per-citation character cap."},

	{Key: "rerank_enabled", Label: "Rerank", Group: groupRerank, Kind: FieldBool, Help: "Porto: enable post-retrieval reranking."},
	{Key: "rerank_type", Label: "Rerank backend", Group: groupRerank, Kind: FieldEnum, Options: []string{RerankCrossEncoder, RerankLLM, RerankAikit}, Help: "Default: local aikit cross-encoder."},
	{Key: "rerank_top_n", Label: "Rerank top N", Group: groupRerank, Kind: FieldInt, Min: 1, Max: 50, Step: 1},
	{Key: "rerank_base_url", Label: "Rerank URL", Group: groupRerank, Kind: FieldString, Help: "Cross-encoder /rerank endpoint base URL."},
	{Key: "rerank_model", Label: "Rerank model", Group: groupRerank, Kind: FieldString, Help: "Model name: HF leaf name or external HTTP model."},
	{Key: "rerank_model_path", Label: "Rerank model path", Group: groupRerank, Kind: FieldString, Help: "Empty uses ~/.kbcli/model/<rerank_model>."},
	{Key: "rerank_api_key", Label: "Rerank key", Group: groupRerank, Kind: FieldString, Secret: true},

	{Key: "query_transform", Label: "Query transform", Group: groupTransform, Kind: FieldEnum, Options: []string{TransformNone, TransformHyDE, TransformMultiQuery, TransformDecomposition, TransformStepBack}, Help: "Porto: HyDE / multi-query / decomposition / step-back. Needs an LLM."},
	{Key: "multi_query_count", Label: "Query variants", Group: groupTransform, Kind: FieldInt, Min: 2, Max: 8, Step: 1},
	{Key: "condense_enabled", Label: "Condense query", Group: groupTransform, Kind: FieldBool, Help: "Porto: resolve pronouns and ellipsis from recent history before retrieval."},

	{Key: "intent_mode", Label: "Intent mode", Group: groupIntent, Kind: FieldEnum, Options: []string{IntentOff, IntentBinary, IntentAdaptive}, Help: "Porto: off / binary (direct|rag) / adaptive (direct|quick_rag|deep_rag)."},
	{Key: "intent_provider", Label: "Intent provider", Group: groupIntent, Kind: FieldEnum, Options: []string{IntentProviderInherit, ChatProviderOpenAI, ChatProviderAnthropic}, Help: "inherit uses the global chat provider, endpoint, model and key."},
	{Key: "intent_api_key", Label: "Intent key", Group: groupIntent, Kind: FieldString, Secret: true, Help: "Empty inherits the global API key."},
	{Key: "intent_base_url", Label: "Intent URL", Group: groupIntent, Kind: FieldString, Help: "Empty inherits the global base URL, with the provider's default fallback."},
	{Key: "intent_model", Label: "Intent model", Group: groupIntent, Kind: FieldString, Help: "Empty inherits the global chat model."},

	{Key: "language", Label: "Language", Group: groupSession, Kind: FieldEnum, Options: []string{LangEn, LangZh}, Help: "TUI and prompt language. Query-time only."},
	{Key: "session_enabled", Label: "Session memory", Group: groupSession, Kind: FieldBool, Help: "Porto: persist per-workspace turns, summary and facts across TUI runs."},
	{Key: "facts_enabled", Label: "Fact extraction", Group: groupSession, Kind: FieldBool, Help: "Porto: extract durable decisions, preferences, context and open questions."},
	{Key: "facts_max_per_category", Label: "Facts cap", Group: groupSession, Kind: FieldInt, Min: 1, Max: 100, Step: 1, Help: "Porto: oldest active fact is evicted past this cap."},
	{Key: "facts_similarity_threshold", Label: "Fact Jaccard", Group: groupSession, Kind: FieldFloat, Min: 0, Max: 1, Step: 0.05, Help: "Porto: token Jaccard threshold for upsert deduplication."},
	{Key: "facts_recent_context_turns", Label: "Fact context turns", Group: groupSession, Kind: FieldInt, Min: 1, Max: 20, Step: 1, Help: "Porto: recent turns passed to fact extraction."},
	{Key: "compact_after_turns", Label: "Compact after", Group: groupSession, Kind: FieldInt, Min: 1, Max: 1000, Step: 5, Help: "Porto: summarize older turns once history exceeds this count."},
	{Key: "session_memory_top_k", Label: "Memory top K", Group: groupSession, Kind: FieldInt, Min: 0, Max: 50, Step: 1, Help: "Porto: session-isolated memory passages injected each round. 0 disables."},
}

// Fields returns the ordered field catalog. Callers must not mutate it.
func Fields() []Field {
	out := make([]Field, len(fieldCatalog))
	copy(out, fieldCatalog)
	return out
}

// FieldByKey looks up one field.
func FieldByKey(key string) (Field, bool) {
	for _, f := range fieldCatalog {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// FieldKeys returns every key in catalog order.
func FieldKeys() []string {
	out := make([]string, 0, len(fieldCatalog))
	for _, f := range fieldCatalog {
		out = append(out, f.Key)
	}
	return out
}

// IndexAffectingKeys lists fields that invalidate an existing vector index.
func IndexAffectingKeys() []string {
	var out []string
	for _, f := range fieldCatalog {
		if f.IndexAffect {
			out = append(out, f.Key)
		}
	}
	return out
}

// GetField returns the current value of key with its concrete Go type.
func (c *Config) GetField(key string) (any, error) {
	if c == nil {
		return nil, fmt.Errorf("config: nil receiver")
	}
	switch key {
	case "api_key":
		return c.APIKey, nil
	case "base_url":
		return c.BaseURL, nil
	case "model":
		return c.Model, nil
	case "chat_provider":
		return c.ChatProvider, nil
	case "embedder":
		return c.Embedder, nil
	case "embed_model":
		return c.EmbedModel, nil
	case "agent":
		return c.Agent, nil
	case "index_file":
		return c.IndexFileName, nil
	case "timeout_seconds":
		return c.TimeoutSeconds, nil
	case "max_iterations":
		return c.MaxIterations, nil
	case "chunk_size":
		return c.ChunkSize, nil
	case "chunk_overlap":
		return c.ChunkOverlap, nil
	case "section_size":
		return c.SectionSize, nil
	case "chunk_strategy":
		return c.ChunkStrategy, nil
	case "top_k":
		return c.TopK, nil
	case "candidate_k":
		return c.CandidateK, nil
	case "retrieval_method":
		return c.RetrievalMethod, nil
	case "bm25_top_k":
		return c.BM25TopK, nil
	case "hybrid_vector_weight":
		return c.HybridVectorWeight, nil
	case "rrf_k":
		return c.RRFK, nil
	case "bm25_k1":
		return c.BM25K1, nil
	case "bm25_b":
		return c.BM25B, nil
	case "min_score":
		return c.MinScore, nil
	case "context_expand":
		return c.ContextExpand, nil
	case "context_char_budget":
		return c.ContextCharBudget, nil
	case "context_max_chunk_chars":
		return c.ContextMaxChunkChars, nil
	case "rerank_enabled":
		return c.RerankEnabled, nil
	case "rerank_type":
		return c.RerankType, nil
	case "rerank_top_n":
		return c.RerankTopN, nil
	case "rerank_base_url":
		return c.RerankBaseURL, nil
	case "rerank_model":
		return c.RerankModel, nil
	case "rerank_model_path":
		return c.RerankModelPath, nil
	case "rerank_api_key":
		return c.RerankAPIKey, nil
	case "query_transform":
		return c.QueryTransform, nil
	case "multi_query_count":
		return c.MultiQueryCount, nil
	case "condense_enabled":
		return c.CondenseEnabled, nil
	case "intent_mode":
		return c.IntentMode, nil
	case "intent_provider":
		return c.IntentProvider, nil
	case "intent_api_key":
		return c.IntentAPIKey, nil
	case "intent_base_url":
		return c.IntentBaseURL, nil
	case "intent_model":
		return c.IntentModel, nil
	case "language":
		return c.Language, nil
	case "session_enabled":
		return c.SessionEnabled, nil
	case "facts_enabled":
		return c.FactsEnabled, nil
	case "facts_max_per_category":
		return c.FactsMaxPerCategory, nil
	case "facts_similarity_threshold":
		return c.FactsSimilarityThreshold, nil
	case "facts_recent_context_turns":
		return c.FactsRecentContextTurns, nil
	case "compact_after_turns":
		return c.CompactAfterTurns, nil
	case "session_memory_top_k":
		return c.SessionMemoryTopK, nil
	default:
		return nil, fmt.Errorf("config: unknown key %q", key)
	}
}

// SetField assigns key from its string representation. It validates ranges,
// enum membership and numeric syntax before touching the receiver.
func (c *Config) SetField(key, value string) error {
	f, ok := FieldByKey(key)
	if !ok {
		return fmt.Errorf("config: unknown key %q", key)
	}
	switch f.Kind {
	case FieldString:
		return c.setString(key, value)
	case FieldBool:
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("config: %s %w", key, err)
		}
		return c.setBool(key, v)
	case FieldEnum:
		for _, opt := range f.Options {
			if value == opt {
				return c.setString(key, value)
			}
		}
		return fmt.Errorf("config: %s must be one of %s, got %q", key, strings.Join(f.Options, ", "), value)
	case FieldInt:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("config: %s expects an integer, got %q", key, value)
		}
		if float64(n) < f.Min || float64(n) > f.Max {
			return fmt.Errorf("config: %s must be %g..%g, got %d", key, f.Min, f.Max, n)
		}
		return c.setInt(key, n)
	case FieldFloat:
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return fmt.Errorf("config: %s expects a number, got %q", key, value)
		}
		if v < f.Min || v > f.Max {
			return fmt.Errorf("config: %s must be %g..%g, got %g", key, f.Min, f.Max, v)
		}
		return c.setFloat(key, v)
	default:
		return fmt.Errorf("config: unsupported field kind for %q", key)
	}
}

// ValueString renders a field for display. Secret values are masked.
func (c *Config) ValueString(key string) string {
	v, err := c.GetField(key)
	if err != nil {
		return ""
	}
	if f, ok := FieldByKey(key); ok && f.Secret {
		s, _ := v.(string)
		if s == "" {
			return "<empty>"
		}
		if len(s) <= 4 {
			return "****"
		}
		return "****" + s[len(s)-4:]
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return "<empty>"
		}
		return x
	case int:
		return strconv.Itoa(x)
	case bool:
		if x {
			return "on"
		}
		return "off"
	case float64:
		return strconv.FormatFloat(x, 'f', 3, 64)
	default:
		return fmt.Sprint(v)
	}
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expects on/off, got %q", s)
	}
}

func (c *Config) setString(key, value string) error {
	switch key {
	case "api_key":
		c.APIKey = value
	case "base_url":
		c.BaseURL = value
	case "model":
		c.Model = value
	case "chat_provider":
		c.ChatProvider = value
	case "embedder":
		c.Embedder = value
	case "embed_model":
		c.EmbedModel = value
	case "agent":
		c.Agent = value
	case "index_file":
		c.IndexFileName = value
	case "retrieval_method":
		c.RetrievalMethod = value
	case "rerank_type":
		c.RerankType = value
	case "rerank_base_url":
		c.RerankBaseURL = value
	case "rerank_model":
		c.RerankModel = value
	case "rerank_model_path":
		c.RerankModelPath = value
	case "rerank_api_key":
		c.RerankAPIKey = value
	case "query_transform":
		c.QueryTransform = value
	case "chunk_strategy":
		c.ChunkStrategy = value
	case "intent_mode":
		c.IntentMode = value
	case "intent_provider":
		c.IntentProvider = value
	case "intent_api_key":
		c.IntentAPIKey = value
	case "intent_base_url":
		c.IntentBaseURL = value
	case "intent_model":
		c.IntentModel = value
	case "language":
		c.Language = value
	default:
		return fmt.Errorf("config: %q is not a string field", key)
	}
	return nil
}

func (c *Config) setBool(key string, value bool) error {
	switch key {
	case "rerank_enabled":
		c.RerankEnabled = value
	case "condense_enabled":
		c.CondenseEnabled = value
	case "session_enabled":
		c.SessionEnabled = value
	case "facts_enabled":
		c.FactsEnabled = value
	default:
		return fmt.Errorf("config: %q is not a boolean field", key)
	}
	return nil
}

func (c *Config) setInt(key string, value int) error {
	switch key {
	case "timeout_seconds":
		c.TimeoutSeconds = value
	case "max_iterations":
		c.MaxIterations = value
	case "chunk_size":
		c.ChunkSize = value
	case "chunk_overlap":
		c.ChunkOverlap = value
	case "section_size":
		c.SectionSize = value
	case "top_k":
		c.TopK = value
	case "candidate_k":
		c.CandidateK = value
	case "bm25_top_k":
		c.BM25TopK = value
	case "rrf_k":
		c.RRFK = value
	case "context_expand":
		c.ContextExpand = value
	case "context_char_budget":
		c.ContextCharBudget = value
	case "context_max_chunk_chars":
		c.ContextMaxChunkChars = value
	case "rerank_top_n":
		c.RerankTopN = value
	case "multi_query_count":
		c.MultiQueryCount = value
	case "facts_max_per_category":
		c.FactsMaxPerCategory = value
	case "facts_recent_context_turns":
		c.FactsRecentContextTurns = value
	case "compact_after_turns":
		c.CompactAfterTurns = value
	case "session_memory_top_k":
		c.SessionMemoryTopK = value
	default:
		return fmt.Errorf("config: %q is not an integer field", key)
	}
	return nil
}

func (c *Config) setFloat(key string, value float64) error {
	switch key {
	case "hybrid_vector_weight":
		c.HybridVectorWeight = value
	case "bm25_k1":
		c.BM25K1 = value
	case "bm25_b":
		c.BM25B = value
	case "min_score":
		c.MinScore = value
	case "facts_similarity_threshold":
		c.FactsSimilarityThreshold = value
	default:
		return fmt.Errorf("config: %q is not a numeric field", key)
	}
	return nil
}
