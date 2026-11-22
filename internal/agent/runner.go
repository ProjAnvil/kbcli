package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/intent"
	"github.com/ProjAnvil/kbcli/internal/retrieval"
	"github.com/ProjAnvil/kbcli/internal/session"
	"github.com/projanvil/langchain-golang/core/messages"
	coretools "github.com/projanvil/langchain-golang/core/tools"
	"github.com/projanvil/langchain-golang/deepagents"
	"github.com/projanvil/langchain-golang/deepagents/backends"
	"github.com/projanvil/langchain-golang/langchain/agents"
	"github.com/projanvil/langchain-golang/langgraph/prebuilt"
)

// maxHistoryTurns caps prior turns fed back to the model to bound context.
const maxHistoryTurns = 20

// invoker is the narrow slice of *agents.Agent the runner needs. Keeping it a
// small private interface lets tests stub the agent without a live LLM.
type invoker interface {
	Invoke(ctx context.Context, msgs []messages.Message) ([]messages.Message, error)
}

// runner orchestrates retrieval, the agent, session memory, and evidence
// publication.
type runner struct {
	cfg        *config.Config
	store      index.Store
	retriever  retrieval.Retriever
	sink       Sink
	agent      invoker
	sess       *session.Store
	classifier *intent.Classifier
	complete   retrieval.CompleteFunc

	metaMu sync.RWMutex
	meta   AnswerMeta
}

// LastMeta exposes the diagnostics of the most recent Ask call.
func (r *runner) LastMeta() AnswerMeta {
	r.metaMu.RLock()
	defer r.metaMu.RUnlock()
	return r.meta
}

func (r *runner) setMeta(m AnswerMeta) {
	r.metaMu.Lock()
	r.meta = m
	r.metaMu.Unlock()
}

// newRunner builds the model, the optimized retriever, the kb_search tool, and
// the configured agent. sess may be nil when session memory is disabled.
func newRunner(cfg *config.Config, store index.Store, sink Sink, sess *session.Store) (Answerer, error) {
	model := buildModel(cfg)
	ret, err := retrieval.New(retrieval.Options{
		Config:   cfg,
		Store:    store,
		Complete: newComplete(cfg),
	})
	if err != nil {
		return nil, fmt.Errorf("agent: build retriever: %w", err)
	}
	tool, err := newSearchTool(cfg, ret, sink)
	if err != nil {
		return nil, err
	}

	var ag *agents.Agent
	switch cfg.Agent {
	case config.AgentReact:
		ag, err = prebuilt.CreateReactAgent(model, []coretools.Tool{tool},
			agents.WithAgentSystemPrompt(SystemPrompt),
			agents.WithAgentRecursionLimit(cfg.MaxIterations))
	case config.AgentDeep:
		ag, err = deepagents.CreateDeepAgent(model,
			deepagents.WithSystemPrompt(SystemPrompt),
			deepagents.WithTools(tool),
			deepagents.WithBackend(backends.NewStateBackend()))
	default:
		return nil, fmt.Errorf("agent: unsupported agent type %q", cfg.Agent)
	}
	if err != nil {
		return nil, fmt.Errorf("agent: create %s agent: %w", cfg.Agent, err)
	}
	iCfg := intentConfig(cfg)
	complete := newComplete(iCfg)
	return &runner{
		cfg:        cfg,
		store:      store,
		retriever:  ret,
		sink:       sink,
		agent:      ag,
		sess:       sess,
		classifier: intent.New(cfg, intent.CompleteFunc(complete)),
		complete:   complete,
	}, nil
}

// Ask classifies the message, retrieves evidence through the selected route,
// runs the agent, and returns the ranked hits. When the classifier, retrieval,
// or the LLM fails it falls open to the original full-RAG path or to a
// deterministic answer, so the TUI stays useful offline. Errors are never
// surfaced.
func (r *runner) Ask(ctx context.Context, history []Turn, question string) (string, []index.Hit, error) {
	turns := make([]retrieval.Turn, 0, len(history))
	for _, t := range history {
		turns = append(turns, retrieval.Turn{Role: t.Role, Content: t.Content})
	}

	lang := i18n.Parse(r.cfg.Language)
	meta := AnswerMeta{}
	decision := intent.Decision{Intent: intent.RAG, Engine: intent.EngineOff, Reason: "disabled"}
	if r.classifier != nil {
		decision = r.classifier.Classify(ctx, question)
	}
	meta.Intent = decision.Intent
	meta.IntentReason = decision.Reason
	meta.IntentEngine = decision.Engine
	r.trackUserTurn(question, &meta)

	// Porto's direct route never touches the vector store. Answer with one
	// short LLM call and publish empty evidence so a previous result clears.
	if decision.Intent == intent.Direct {
		hits := []index.Hit{}
		r.sink.Set(hits)
		r.setMeta(meta)
		answer := r.answerDirect(ctx, question, lang)
		r.recordAnswer(answer)
		return answer, hits, nil
	}

	var raw []index.Hit
	var err error
	if decision.Intent == intent.QuickRAG {
		if quick, ok := r.retriever.(retrieval.QuickRetriever); ok {
			raw, err = quick.SearchQuick(ctx, question, r.cfg.TopK)
		} else {
			raw, err = r.retriever.Search(ctx, question, r.cfg.TopK)
		}
	} else {
		raw, err = r.retriever.SearchWithHistory(ctx, question, r.cfg.TopK, turns)
	}
	hits := append([]index.Hit{}, raw...)
	r.sink.Set(hits)

	// Surface Porto's condense gate result to the TUI.
	if reporter, ok := r.retriever.(retrieval.CondenseReporter); ok {
		cm := reporter.LastCondense()
		meta.Condensed = cm.Applied
		meta.Original = cm.Original
		meta.Rewritten = cm.Rewritten
		meta.Method = cm.Method
	}
	r.setMeta(meta)

	if err != nil {
		answer := fallbackAnswer(hits, fmt.Errorf("agent: retrieve evidence: %w", err), lang)
		r.recordAnswer(answer)
		return answer, hits, nil
	}
	// Assemble the grounded prompt under Porto's character budget. Session
	// facts and summary are injected every round, exactly like Porto's
	// orchestrator.py prompt_parts. The question is placed before the evidence
	// so TrimToBudget always preserves it.
	sys := lang.T("prompt.system_en")
	if lang == i18n.LangZh {
		sys = lang.T("prompt.system_zh")
	}
	parts := []string{sys, lang.Tf("prompt.question", question)}
	if r.sess != nil && r.cfg.SessionEnabled {
		if block := r.sess.ContextBlock(); block != "" {
			parts = append(parts, lang.Tf("prompt.facts", block))
		}
	}
	parts = append(parts, lang.Tf("prompt.evidence", retrieval.FormatSources(hits, r.cfg.ContextMaxChunkChars)))
	parts = retrieval.TrimToBudget(parts, r.cfg.ContextCharBudget)

	// Turn the trimmed parts into system + human messages: the first part is the
	// grounding system prompt, the rest (question, then evidence) become one
	// human message. Then keep buildMessages for the chat history and the final
	// Human(question); drop its leading system prompt so evidence reaches the
	// model before the history.
	msgs := make([]messages.Message, 0, len(parts)+len(history)+2)
	systemPart := sys
	if len(parts) > 0 {
		systemPart = parts[0]
	}
	msgs = append(msgs, messages.System(systemPart))
	if len(parts) > 1 {
		msgs = append(msgs, messages.Human(strings.Join(parts[1:], "\n\n")))
	}
	base := buildMessagesLang(history, question, sys)
	msgs = append(msgs, base[1:]...)

	out, invokeErr := r.agent.Invoke(ctx, msgs)
	var answer string
	if invokeErr == nil {
		answer = lastAIAnswer(out)
	}
	if answer == "" {
		if invokeErr == nil {
			invokeErr = errors.New("no AI message in agent output")
		}
		answer = fallbackAnswer(hits, invokeErr, lang)
	}
	r.recordAnswer(answer)
	return answer, hits, nil
}

// trackUserTurn records the user turn and captures session memory diagnostics
// before the background extraction starts.
func (r *runner) trackUserTurn(question string, meta *AnswerMeta) {
	if r.sess == nil || !r.cfg.SessionEnabled {
		return
	}
	r.sess.AddTurn("user", question)
	meta.FactsCount = len(r.sess.ActiveFacts())
	meta.HasSummary = r.sess.Summary() != ""
	go r.backgroundSession(question)
}

// answerDirect answers a greeting or smalltalk message without retrieval. The
// intent classifier's own provider serves this call; a failed call degrades to
// a fixed greeting so the TUI never shows an error for a "hello".
func (r *runner) answerDirect(ctx context.Context, question string, lang i18n.Lang) string {
	key := "intent.direct_en"
	fallback := "intent.fallback_en"
	if lang == i18n.LangZh {
		key, fallback = "intent.direct_zh", "intent.fallback_zh"
	}
	if r.complete == nil {
		return lang.T(fallback)
	}
	answer, err := r.complete(ctx, lang.T(key), question)
	if err != nil || strings.TrimSpace(answer) == "" {
		return lang.T(fallback)
	}
	return strings.TrimSpace(answer)
}

// backgroundSession mirrors Porto's fire-and-forget fact extraction and
// compaction. Every LLM failure fails open inside the session package.
func (r *runner) backgroundSession(question string) {
	timeout := time.Duration(r.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	bgCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if r.cfg.FactsEnabled {
		r.sess.ExtractFacts(bgCtx, question)
	}
	r.sess.Compact(bgCtx)
	_ = r.sess.Save()
}

// recordAnswer appends the assistant turn and persists immediately so the
// session survives an abrupt TUI exit.
func (r *runner) recordAnswer(answer string) {
	if r.sess == nil || !r.cfg.SessionEnabled {
		return
	}
	r.sess.AddTurn("assistant", answer)
	_ = r.sess.Save()
}

// buildMessages assembles the system prompt, the last maxHistoryTurns turns,
// and the current question. Unknown roles are ignored.
func buildMessages(history []Turn, question string) []messages.Message {
	return buildMessagesLang(history, question, SystemPrompt)
}

func buildMessagesLang(history []Turn, question, sys string) []messages.Message {
	msgs := make([]messages.Message, 0, len(history)+2)
	msgs = append(msgs, messages.System(sys))
	start := 0
	if len(history) > maxHistoryTurns {
		start = len(history) - maxHistoryTurns
	}
	for _, t := range history[start:] {
		switch t.Role {
		case "user":
			msgs = append(msgs, messages.Human(t.Content))
		case "assistant":
			msgs = append(msgs, messages.AI(t.Content))
		}
	}
	return append(msgs, messages.Human(question))
}

// lastAIAnswer returns the content of the last AI message with non-empty
// content, or "" when there is none.
func lastAIAnswer(out []messages.Message) string {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == messages.RoleAI && out[i].Content != "" {
			return out[i].Content
		}
	}
	return ""
}

// fallbackAnswer builds a deterministic extractive answer from the top three
// hits. cause is the failure that triggered the fallback and is surfaced in a
// short note so users know the LLM did not produce the answer.
func fallbackAnswer(hits []index.Hit, cause error, lang i18n.Lang) string {
	var b strings.Builder
	b.WriteString(lang.Tf("fallback.llm_unavailable", cause))
	b.WriteString("\n\n")
	b.WriteString(formatEvidence(hits, 3))
	return b.String()
}
