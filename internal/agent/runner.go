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
	"github.com/projanvil/langchain-golang/core/runnables"
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

// streamInvoker is the *agents.Agent slice needed for SSE-style streaming. It
// is separate from invoker so tests can stub Invoke without a stream.
type streamInvoker interface {
	StreamEvents(ctx context.Context, msgs []messages.Message) (runnables.Stream[agents.StreamEvent], error)
}

// runner orchestrates retrieval, the agent, session memory, and evidence
// publication.
type runner struct {
	cfg        *config.Config
	store      index.Store
	retriever  retrieval.Retriever
	sink       Sink
	agent      invoker
	streamer   streamInvoker
	sess       *session.Store
	classifier *intent.Classifier
	complete   retrieval.CompleteFunc

	// Citations are shared by the grounded prompt and the kb_search tool so a
	// follow-up lookup cannot renumber evidence the model already cited.
	ledger     *citationLedger
	ledgerOnce sync.Once

	metaMu sync.RWMutex
	meta   AnswerMeta
}

// citations lazily creates the ledger so runners built by hand in unit tests
// (a struct literal without newRunner) still work without a nil panic.
func (r *runner) citations() *citationLedger {
	r.ledgerOnce.Do(func() {
		if r.ledger == nil {
			r.ledger = newCitationLedger()
		}
	})
	return r.ledger
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
	ledger := newCitationLedger()
	tool, err := newSearchToolWithLedger(cfg, ret, sink, ledger)
	if err != nil {
		return nil, err
	}

	var ag *agents.Agent
	switch cfg.Agent {
	case config.AgentReact:
		tools := []coretools.Tool{tool}
		if store != nil {
			tools = append(tools, fsTools(store.Root())...)
		}
		ag, err = prebuilt.CreateReactAgent(model, tools,
			agents.WithAgentSystemPrompt(systemPromptFor(cfg)),
			agents.WithAgentRecursionLimit(cfg.MaxIterations))
	case config.AgentDeep:
		ag, err = deepagents.CreateDeepAgent(model,
			deepagents.WithSystemPrompt(systemPromptFor(cfg)),
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
		streamer:   ag,
		sess:       sess,
		ledger:     ledger,
		classifier: intent.New(cfg, intent.CompleteFunc(complete)),
		complete:   complete,
	}, nil
}

// answerFunc runs the configured agent on the assembled messages and returns
// the final answer. It is the only difference between Ask and AskStream.
type answerFunc func(context.Context, []messages.Message) (string, error)

// Ask classifies the message, retrieves evidence through the selected route,
// runs the agent, and returns the ranked hits. When the classifier, retrieval,
// or the LLM fails it falls open to the original full-RAG path or to a
// deterministic answer, so the TUI stays useful offline. Errors are never
// surfaced.
func (r *runner) Ask(ctx context.Context, history []Turn, question string) (string, []index.Hit, error) {
	return r.ask(ctx, history, question, r.invokeAnswer)
}

// AskStream mirrors Ask but delivers each model text delta to onDelta as it
// arrives. onDelta runs on the caller's goroutine and is never called
// concurrently. A broken stream fails open to the Invoke path, except when
// the caller cancelled: re-running the LLM on a cancelled context wastes a
// request and can resurrect an answer the user already discarded.
func (r *runner) AskStream(ctx context.Context, history []Turn, question string, onDelta func(string)) (string, []index.Hit, error) {
	return r.AskStreamHooked(ctx, history, question, StreamHooks{OnDelta: onDelta})
}

// AskStreamHooked is the full streaming entry point. OnReset lets the UI drop
// preamble text when a tool round starts, so only the final answer round
// reaches the transcript.
func (r *runner) AskStreamHooked(ctx context.Context, history []Turn, question string, hooks StreamHooks) (string, []index.Hit, error) {
	return r.ask(ctx, history, question, func(ctx context.Context, msgs []messages.Message) (string, error) {
		answer, err := r.streamAnswer(ctx, msgs, hooks)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return r.invokeAnswer(ctx, msgs)
		}
		return answer, nil
	})
}

// invokeAnswer runs the agent through Invoke and extracts the last AI message.
func (r *runner) invokeAnswer(ctx context.Context, msgs []messages.Message) (string, error) {
	out, err := r.agent.Invoke(ctx, msgs)
	if err != nil {
		return "", err
	}
	answer := lastAIAnswer(out)
	if answer == "" {
		return "", errors.New("no AI message in agent output")
	}
	return answer, nil
}

// streamAnswer drains the agent event stream and returns the text of the last
// model round that produced no tool calls. A ReAct agent may run several model
// rounds before answering: each round that ends in a tool call is a preamble
// ("let me look that up") and must not leak into the answer. Deltas are still
// pushed live through hooks.OnDelta, so hooks.OnReset tells the consumer to
// clear a preamble as soon as a tool round is detected.
//
// The upstream graph sometimes terminates a tool-using run with a wrapped
// context.Canceled that is not a caller cancellation. In that case the final
// answer text we already collected wins instead of restarting the agent via
// Invoke. A real caller cancellation (ctx.Err() != nil) is always propagated.
func (r *runner) streamAnswer(ctx context.Context, msgs []messages.Message, hooks StreamHooks) (string, error) {
	if r.streamer == nil {
		return "", errors.New("agent: streaming unavailable")
	}
	stream, err := r.streamer.StreamEvents(ctx, msgs)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var round strings.Builder // text of the model round in progress
	var final strings.Builder // text of the last round without tool calls
	delivered := false        // round text has been pushed through OnDelta

	discardRound := func() {
		if delivered && hooks.OnReset != nil {
			hooks.OnReset()
		}
		round.Reset()
		delivered = false
	}

	for {
		ev, ok, err := stream.Next(ctx)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		switch ev.Type {
		case agents.StreamNodeStart:
			if ev.Node == agents.ModelNodeName {
				round.Reset()
				delivered = false
			}
		case agents.StreamModelDelta:
			if ev.Text == "" {
				continue // tool-call or reasoning delta, nothing to render
			}
			round.WriteString(ev.Text)
			if hooks.OnDelta != nil {
				hooks.OnDelta(ev.Text)
				delivered = true
			}
		case agents.StreamModelEnd:
			if modelHasToolCalls(ev.Message) {
				discardRound()
			} else {
				final.Reset()
				final.WriteString(round.String())
			}
		case agents.StreamToolStart:
			// Belt and suspenders: some providers omit tool calls from the
			// model_end message but still dispatch tools.
			discardRound()
		case agents.StreamEnd:
			if ev.Err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				if errors.Is(ev.Err, context.Canceled) || errors.Is(ev.Err, context.DeadlineExceeded) {
					if answer := final.String(); strings.TrimSpace(answer) != "" {
						return answer, nil
					}
				}
				return "", ev.Err
			}
			// Some models emit only a terminal message without deltas or a
			// model_end event.
			if strings.TrimSpace(final.String()) == "" && ev.Message != nil {
				final.Reset()
				final.WriteString(ev.Message.Content)
			}
		}
	}
	answer := final.String()
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("agent: empty answer stream")
	}
	return answer, nil
}

// modelHasToolCalls reports whether an assembled model message requested one
// or more tool calls. Those rounds are preambles, not the final answer.
func modelHasToolCalls(m *messages.Message) bool {
	return m != nil && (len(m.ToolCalls) > 0 || len(m.InvalidToolCalls) > 0)
}

// ask holds the retrieval, routing, session and evidence logic shared by Ask
// and AskStream. answerFn is the only branch that differs.
func (r *runner) ask(ctx context.Context, history []Turn, question string, answerFn answerFunc) (string, []index.Hit, error) {
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
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	meta.Intent = decision.Intent
	meta.IntentReason = decision.Reason
	meta.IntentEngine = decision.Engine
	r.trackUserTurn(question, &meta)

	// Each turn builds a fresh prompt, so citation numbers start at [1] again.
	r.citations().Reset()

	// Porto's direct route never touches the vector store. Answer with one
	// short LLM call and publish empty evidence so a previous result clears.
	if decision.Intent == intent.Direct {
		hits := []index.Hit{}
		r.sink.Set(hits)
		r.setMeta(meta)
		answer := r.answerDirect(ctx, question, lang)
		if err := ctx.Err(); err != nil {
			return "", hits, err
		}
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
	numbers, allHits := r.citations().Register(hits)
	r.sink.Set(allHits)

	// Surface Porto's condense gate result to the TUI.
	if reporter, ok := r.retriever.(retrieval.CondenseReporter); ok {
		cm := reporter.LastCondense()
		meta.Condensed = cm.Applied
		meta.Original = cm.Original
		meta.Rewritten = cm.Rewritten
		meta.Method = cm.Method
	}

	// A cancelled run must not fabricate a fallback answer or write a bogus
	// assistant turn into session memory.
	if err := ctx.Err(); err != nil {
		return "", hits, err
	}

	if err != nil {
		answer := fallbackAnswer(hits, fmt.Errorf("agent: retrieve evidence: %w", err), lang)
		meta.Degraded, meta.Reason = true, "retrieval_failed"
		r.setMeta(meta)
		r.recordAnswer(answer)
		return answer, hits, nil
	}
	// Production RAG refuses when retrieval found nothing. Sending an empty
	// evidence block to an LLM invites hallucination and wastes a call; the
	// grounded system prompt already defines the refusal wording.
	if len(hits) == 0 {
		key := "answer.no_evidence_en"
		if lang == i18n.LangZh {
			key = "answer.no_evidence_zh"
		}
		answer := lang.T(key)
		meta.Degraded, meta.Reason = true, "no_evidence"
		r.setMeta(meta)
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
	// Keep every citation as its own part. TrimToBudget then drops whole
	// trailing snippets instead of cutting a numbered passage in half.
	parts = append(parts, lang.Tf("prompt.evidence", ""))
	parts = append(parts, retrieval.FormatCitationParts(hits, numbers, r.cfg.ContextMaxChunkChars)...)
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

	answer, invokeErr := answerFn(ctx, msgs)
	if err := ctx.Err(); err != nil {
		r.setMeta(meta)
		return "", hits, err
	}
	if answer == "" {
		if invokeErr == nil {
			invokeErr = errors.New("no AI message in agent output")
		}
		answer = fallbackAnswer(hits, invokeErr, lang)
		meta.Degraded, meta.Reason = true, "llm_failed"
	}
	r.setMeta(meta)
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
