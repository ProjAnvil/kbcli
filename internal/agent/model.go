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
	"github.com/projanvil/langchain-golang/partners/openai"
)

// buildModel wires an OpenAI-compatible chat model from user settings. The
// Chat Completions endpoint is selected so DeepSeek, vLLM, Ollama and other
// compatible gateways work, not just the OpenAI Responses API. An empty API
// key is fine: local endpoints often need no key. The key is never logged.
func buildModel(cfg *config.Config) language.ChatModel {
	return openai.NewChatModel(
		modelconfig.WithModel(cfg.Model),
		modelconfig.WithBaseURL(cfg.BaseURL),
		modelconfig.WithAPIKey(cfg.APIKey),
		modelconfig.WithTimeout(time.Duration(cfg.TimeoutSeconds)*time.Second),
	).WithChatCompletions()
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
