package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/retrieval"
	"github.com/projanvil/langchain-golang/core/language"
	"github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/langchain-golang/core/modelconfig"
	"github.com/projanvil/langchain-golang/partners/anthropic"
	"github.com/projanvil/langchain-golang/partners/openai"
)

// chatBaseURL picks the endpoint for the selected chat provider. For Anthropic
// an unset or OpenAI default URL must not silently point the Messages API at
// OpenAI; it falls back to the public Anthropic endpoint instead. Custom URLs
// are preserved so Anthropic-compatible gateways keep working.
func chatBaseURL(cfg *config.Config) string {
	if cfg.ChatProvider == config.ChatProviderAnthropic {
		if cfg.BaseURL == "" || cfg.BaseURL == config.DefaultOpenAIBaseURL {
			return config.DefaultAnthropicBaseURL
		}
	}
	return cfg.BaseURL
}

// buildModel wires a chat model from user settings. Anthropic uses the
// Messages API model; everything else (empty, OpenAI, or an unknown value
// already rejected by config validation) uses the OpenAI-compatible Chat
// Completions endpoint so DeepSeek, vLLM, Ollama and other gateways work too.
// An empty API key is fine: local endpoints often need no key. The key is
// never logged.
func buildModel(cfg *config.Config) language.ChatModel {
	opts := []modelconfig.Option{
		modelconfig.WithModel(cfg.Model),
		modelconfig.WithBaseURL(chatBaseURL(cfg)),
		modelconfig.WithAPIKey(cfg.APIKey),
		modelconfig.WithTimeout(time.Duration(cfg.TimeoutSeconds) * time.Second),
	}
	if cfg.ChatProvider == config.ChatProviderAnthropic {
		return anthropic.NewChatModel(opts...)
	}
	return openai.NewChatModel(opts...).WithChatCompletions()
}

// NewCompletion exposes the same adapter to other layers. The CLI search
// command uses it so retrieval_method, min_score and the other query-time
// knobs apply outside the chat TUI too.
func NewCompletion(cfg *config.Config) retrieval.CompleteFunc {
	return newComplete(cfg)
}

// newComplete adapts the same OpenAI-compatible chat model into the single
// completion function the retrieval pipeline uses for query transforms and
// LLM reranking. It never logs the API key. An empty model reply is treated as
// an error so the retrieval pipeline can fail open to plain retrieval.
func newComplete(cfg *config.Config) retrieval.CompleteFunc {
	model := buildModel(cfg)
	return func(ctx context.Context, system, user string) (string, error) {
		msgs := []messages.Message{messages.System(system), messages.Human(user)}
		resp, err := model.Invoke(ctx, msgs)
		if err != nil {
			return "", fmt.Errorf("agent: completion: %w", err)
		}
		if resp.Content == "" {
			return "", errors.New("agent: completion returned empty content")
		}
		return resp.Content, nil
	}
}
