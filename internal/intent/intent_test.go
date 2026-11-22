package intent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
)

func completeWith(s string) CompleteFunc {
	return func(ctx context.Context, system, user string) (string, error) { return s, nil }
}

func classify(mode, message string, complete CompleteFunc) Decision {
	cfg := &config.Config{IntentMode: mode, Language: config.LangEn}
	return New(cfg, complete).Classify(context.Background(), message)
}

func TestRuleRouteGreeting(t *testing.T) {
	for _, msg := range []string{"hello", "你好", "Hi!", "早上好"} {
		got := classify(config.IntentAdaptive, msg, nil)
		if got.Intent != Direct || got.Reason != "greeting" {
			t.Fatalf("%q: got %+v, want direct/greeting", msg, got)
		}
	}
}

func TestRuleRouteDeepAndQuick(t *testing.T) {
	deep := classify(config.IntentAdaptive, "请分析支付系统的架构与权衡", nil)
	if deep.Intent != DeepRAG {
		t.Fatalf("deep question = %+v, want deep_rag", deep)
	}
	quick := classify(config.IntentAdaptive, "chunk_size 的默认值是多少", nil)
	if quick.Intent != QuickRAG {
		t.Fatalf("simple question = %+v, want quick_rag", quick)
	}
}

// Short English and Chinese questions must still reach the knowledge base.
// A previous length-based shortcut classified them as direct smalltalk and
// silently skipped retrieval and grounding.
func TestShortQuestionsReachRAG(t *testing.T) {
	for _, msg := range []string{"why?", "how?", "what?", "when?", "它呢？", "为什么？"} {
		got := classify(config.IntentAdaptive, msg, nil)
		if got.Intent == Direct {
			t.Fatalf("%q was routed direct: %+v", msg, got)
		}
	}
}

func TestRuleRouteBinary(t *testing.T) {
	got := classify(config.IntentBinary, "chunk_size 的默认值是多少", nil)
	if got.Intent != RAG {
		t.Fatalf("binary mode = %+v, want rag", got)
	}
	got = classify(config.IntentBinary, "谢谢", nil)
	if got.Intent != Direct {
		t.Fatalf("binary greeting = %+v, want direct", got)
	}
}

func TestDisabledMode(t *testing.T) {
	if got := classify("", "hello", completeWith(`{"intent":"direct"}`)); got.Intent != RAG || got.Engine != EngineOff {
		t.Fatalf("disabled classifier = %+v, want rag/off", got)
	}
}

func TestLLMRouteUsesAllowedCategories(t *testing.T) {
	out := `{"intent":"deep_rag","reason":"complex design"}`
	got := classify(config.IntentAdaptive, "design a new pipeline", completeWith(out))
	if got.Intent != DeepRAG || got.Engine != EngineLLM || got.Reason != "complex design" {
		t.Fatalf("llm decision = %+v", got)
	}

	binary := classify(config.IntentBinary, "design a new pipeline", completeWith(out))
	if binary.Intent != RAG || binary.Engine != EngineRule {
		t.Fatalf("adaptive intent leaked into binary mode: %+v", binary)
	}
}

func TestLLMFailureFallsBackToRule(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return "", errors.New("offline")
	}
	got := classify(config.IntentAdaptive, "hello", complete)
	if got.Intent != Direct || got.Engine != EngineRule || !strings.HasPrefix(got.Reason, "llm_unavailable:") {
		t.Fatalf("fallback decision = %+v", got)
	}
}

func TestLLMMessageIsTruncated(t *testing.T) {
	long := strings.Repeat("问", 800)
	var seen string
	complete := func(ctx context.Context, system, user string) (string, error) {
		seen = user
		return `{"intent":"quick_rag","reason":"fact"}`, nil
	}
	classify(config.IntentAdaptive, long, complete)
	if n := strings.Count(seen, "问"); n != 500 {
		t.Fatalf("LLM prompt contains %d of 800 message runes", n)
	}
}
