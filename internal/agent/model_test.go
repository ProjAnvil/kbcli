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

func TestIntentConfigInheritsGlobal(t *testing.T) {
	cfg := config.Default()
	cfg.APIKey = "global-key"
	cfg.BaseURL = "https://gateway.example/v1"
	cfg.Model = "global-model"
	got := intentConfig(cfg)
	if got.ChatProvider != cfg.ChatProvider || got.APIKey != cfg.APIKey ||
		got.BaseURL != cfg.BaseURL || got.Model != cfg.Model {
		t.Fatalf("inherited intent config = %+v", got)
	}
	cfg.Model = "changed-after-clone"
	if got.Model != "global-model" {
		t.Fatal("intentConfig must return an independent copy")
	}
}

func TestIntentConfigOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.APIKey = "global-key"
	cfg.Model = "global-model"
	cfg.IntentProvider = config.ChatProviderAnthropic
	cfg.IntentAPIKey = "intent-key"
	cfg.IntentBaseURL = "https://intent.example/v1"
	cfg.IntentModel = "intent-model"
	got := intentConfig(cfg)
	if got.ChatProvider != config.ChatProviderAnthropic || got.APIKey != "intent-key" ||
		got.BaseURL != "https://intent.example/v1" || got.Model != "intent-model" {
		t.Fatalf("overridden intent config = %+v", got)
	}
	if cfg.Model != "global-model" || cfg.ChatProvider != config.ChatProviderOpenAI {
		t.Fatal("intentConfig must not mutate the global config")
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
