// Package agent wires a ReAct (or Deep) agent to the local vector index.
package agent

import (
	"context"

	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/index"
	"github.com/projanvil/kbcli/internal/session"
)

// Turn is one chat message. Role is "user" or "assistant".
type Turn struct {
	Role    string
	Content string
}

// Sink carries the latest retrieval evidence from the agent to the TUI.
// Implementations must be safe for concurrent use.
type Sink interface {
	Set(hits []index.Hit)
	Get() []index.Hit
}

// Answerer produces grounded answers from the indexed knowledge base.
type Answerer interface {
	// Ask retrieves evidence for question, publishes it to the sink, then
	// runs the agent. It returns the answer and the ranked hits.
	Ask(ctx context.Context, history []Turn, question string) (string, []index.Hit, error)
}

// AnswerMeta is diagnostic context for one answer. The TUI shows it as a
// status line so users can see whether anaphora resolution and session memory
// actually ran.
type AnswerMeta struct {
	Condensed    bool
	Original     string
	Rewritten    string
	Method       string
	FactsCount   int
	SessionTitle string
	HasSummary   bool
}

// MetaAnswerer is an optional Answerer extension.
type MetaAnswerer interface {
	Answerer
	LastMeta() AnswerMeta
}

// NewSink creates an empty evidence sink. Implemented in evidence.go.
func NewSink() Sink {
	return newSink()
}

// New builds the chat model and agent. Implemented in runner.go.
func New(cfg *config.Config, store index.Store, sink Sink) (Answerer, error) {
	return newRunner(cfg, store, sink, nil)
}

// NewWithSession attaches per-workspace session memory. Porto injects the
// session's facts, summary and recent turns into every round's prompt.
func NewWithSession(cfg *config.Config, store index.Store, sink Sink, sess *session.Store) (Answerer, error) {
	return newRunner(cfg, store, sink, sess)
}
