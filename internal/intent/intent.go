// Package intent ports Porto's intent router. It classifies one user message
// before retrieval so greetings skip the knowledge base and deep questions get
// the full retrieval pipeline.
package intent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// Intent values mirror Porto's ChatIntent enum.
const (
	Direct   = "direct"
	RAG      = "rag"
	QuickRAG = "quick_rag"
	DeepRAG  = "deep_rag"
)

// Engine values identify which path produced a decision.
const (
	EngineOff  = "off"
	EngineLLM  = "llm"
	EngineRule = "rule"
)

// Decision is the routing result for one message.
type Decision struct {
	Intent string
	Reason string
	Engine string
}

// CompleteFunc is one LLM completion. A nil function selects rule routing.
type CompleteFunc func(ctx context.Context, system, user string) (string, error)

// Classifier routes messages with an LLM and falls back to rules. Every LLM
// failure fails open: the message still reaches the knowledge base.
type Classifier struct {
	mode     string
	lang     i18n.Lang
	complete CompleteFunc
}

// New builds a classifier. An empty mode or config disables routing, which
// keeps zero-value test configs on the original full-RAG path.
func New(cfg *config.Config, complete CompleteFunc) *Classifier {
	if cfg == nil || cfg.IntentMode == "" || cfg.IntentMode == config.IntentOff {
		return &Classifier{mode: config.IntentOff, lang: i18n.LangEn}
	}
	return &Classifier{
		mode:     cfg.IntentMode,
		lang:     i18n.Parse(cfg.Language),
		complete: complete,
	}
}

// Classify returns a routing decision for message. It never returns an error:
// an unusable LLM response falls back to the deterministic rule router.
func (c *Classifier) Classify(ctx context.Context, message string) Decision {
	if c == nil || c.mode == config.IntentOff {
		return Decision{Intent: RAG, Engine: EngineOff, Reason: "disabled"}
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return Decision{Intent: Direct, Engine: EngineRule, Reason: "empty"}
	}
	if c.complete == nil {
		return c.ruleRoute(msg)
	}

	// A slow classifier must not hold the chat answer hostage. Five seconds
	// is enough for one small JSON completion on a healthy endpoint.
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if d, ok := c.llmRoute(callCtx, msg); ok {
		return d
	}
	d := c.ruleRoute(msg)
	d.Reason = "llm_unavailable:" + d.Reason
	return d
}

func (c *Classifier) allowed() []string {
	if c.mode == config.IntentBinary {
		return []string{Direct, RAG}
	}
	return []string{Direct, QuickRAG, DeepRAG}
}

func (c *Classifier) llmRoute(ctx context.Context, msg string) (Decision, bool) {
	descKey := "intent.desc_adaptive"
	if c.mode == config.IntentBinary {
		descKey = "intent.desc_binary"
	}
	system := c.lang.T("intent.task") + "\n" + c.lang.T(descKey)
	user := c.lang.Tf("intent.user", truncateRunes(msg, 500))

	for attempt := 0; attempt < 2; attempt++ {
		out, err := c.complete(ctx, system, user)
		if err != nil {
			return Decision{}, false
		}
		choice, reason, ok := parseChoice(stripFence(out))
		if !ok || !contains(c.allowed(), choice) {
			continue
		}
		if reason == "" {
			reason = "llm"
		}
		return Decision{Intent: choice, Reason: reason, Engine: EngineLLM}, true
	}
	return Decision{}, false
}

// ruleRoute is Porto's keyword/regular-expression fallback, extended with
// English and Chinese greetings and deep-analysis hints.
func (c *Classifier) ruleRoute(msg string) Decision {
	lower := strings.ToLower(msg)
	if greetingRE.MatchString(lower) && !deepHintRE.MatchString(lower) {
		return Decision{Intent: Direct, Engine: EngineRule, Reason: "greeting"}
	}
	if smalltalkRE.MatchString(lower) && !domainHintRE.MatchString(lower) {
		return Decision{Intent: Direct, Engine: EngineRule, Reason: "smalltalk_or_help"}
	}
	if c.mode == config.IntentBinary {
		return Decision{Intent: RAG, Engine: EngineRule, Reason: "default"}
	}
	if deepHintRE.MatchString(lower) {
		return Decision{Intent: DeepRAG, Engine: EngineRule, Reason: "deep_hints"}
	}
	if []rune(msg)[0] < 128 && len([]rune(msg)) < 12 && !domainHintRE.MatchString(lower) {
		return Decision{Intent: Direct, Engine: EngineRule, Reason: "short_smalltalk"}
	}
	return Decision{Intent: QuickRAG, Engine: EngineRule, Reason: "default"}
}

var (
	greetingRE   = regexp.MustCompile(`^(hi|hello|hey|yo|good\s+(morning|afternoon|evening)|howdy|你好|您好|哈喽|嗨|早上好|下午好|晚上好|在吗)[\s!！。.,，?？~-]*$`)
	smalltalkRE  = regexp.MustCompile(`^(thank(s| you)?|thanks a lot|who are you|what can you do|help|你好呀|谢谢|感谢|多谢|你是谁|你能做什么|帮助|请问你能|早|好的|收到)[\s!！。.,，?？~-]*$`)
	domainHintRE = regexp.MustCompile(`知识|文档|文档库|架构|支付|风控|prd|需求|设计|接口|系统|配置|deploy|api|k8s|database|schema|error|bug|代码`)
	deepHintRE   = regexp.MustCompile(`分析|架构|设计|拆解|完整|详细|怎么实现|为什么|对比|权衡|方案|优化|原理|实现|architecture|design|analyze|compare|trade-?off|why|how do|how to`)
)

func parseChoice(raw string) (string, string, bool) {
	var payload struct {
		Intent string `json:"intent"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(payload.Intent), truncateRunes(strings.TrimSpace(payload.Reason), 60), true
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
