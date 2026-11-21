package agent

import (
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/projanvil/langchain-golang/partners/anthropic"
	"github.com/projanvil/langchain-golang/partners/openai"
)

// TestChatBaseURL verifies endpoint selection without any network call.
func TestChatBaseURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "openai keeps default openai url",
			cfg:  &config.Config{ChatProvider: config.ChatProviderOpenAI, BaseURL: config.DefaultOpenAIBaseURL},
			want: config.DefaultOpenAIBaseURL,
		},
		{
			name: "anthropic with empty url falls back",
			cfg:  &config.Config{ChatProvider: config.ChatProviderAnthropic, BaseURL: ""},
			want: config.DefaultAnthropicBaseURL,
		},
		{
			name: "anthropic with openai url falls back",
			cfg:  &config.Config{ChatProvider: config.ChatProviderAnthropic, BaseURL: config.DefaultOpenAIBaseURL},
			want: config.DefaultAnthropicBaseURL,
		},
		{
			name: "anthropic keeps custom gateway",
			cfg:  &config.Config{ChatProvider: config.ChatProviderAnthropic, BaseURL: "https://gateway.example/v1"},
			want: "https://gateway.example/v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatBaseURL(tt.cfg); got != tt.want {
				t.Fatalf("chatBaseURL = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildModelNonNilAndType builds both providers offline: construction
// never dials the endpoint, so no network call is made. Concrete types differ,
// so we assert the provider-specific type rather than only non-nil.
func TestBuildModelNonNilAndType(t *testing.T) {
	openAIModel := buildModel(config.Default())
	if openAIModel == nil {
		t.Fatal("buildModel(openai) returned nil")
	}
	if _, ok := openAIModel.(openai.ChatModel); !ok {
		t.Fatalf("buildModel(openai) type = %T, want openai.ChatModel", openAIModel)
	}

	anthropicCfg := config.Default()
	anthropicCfg.ChatProvider = config.ChatProviderAnthropic
	anthropicCfg.BaseURL = config.DefaultOpenAIBaseURL
	anthropicModel := buildModel(anthropicCfg)
	if anthropicModel == nil {
		t.Fatal("buildModel(anthropic) returned nil")
	}
	if _, ok := anthropicModel.(anthropic.ChatModel); !ok {
		t.Fatalf("buildModel(anthropic) type = %T, want anthropic.ChatModel", anthropicModel)
	}
	if openAIModel == anthropicModel {
		t.Fatal("openai and anthropic models must be distinct types")
	}
}
