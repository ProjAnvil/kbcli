package agent

import (
	"context"

	"github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/langchain-golang/langchain/agents/middleware"
)

// dedupeToolCallsMiddleware removes the duplicate tool calls that the v0.14.3
// streaming projection emits for OpenAI-compatible tool-call chunks. Left in
// place, the tools node runs each call twice and appends two tool messages with
// the same tool_call_id; the next model request then fails with HTTP 400.
// Non-streaming responses pass through unchanged.
type dedupeToolCallsMiddleware struct{}

func (dedupeToolCallsMiddleware) WrapModelCall(
	ctx context.Context,
	req middleware.ModelRequest,
	next middleware.ModelHandler,
) (middleware.ModelResponse, error) {
	resp, err := next(ctx, req)
	if err != nil {
		return resp, err
	}
	for i := range resp.Result {
		msg := &resp.Result[i]
		if len(msg.ToolCalls) < 2 {
			continue
		}
		seen := make(map[string]bool, len(msg.ToolCalls))
		kept := make([]messages.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			if call.ID != "" && seen[call.ID] {
				continue
			}
			if call.ID != "" {
				seen[call.ID] = true
			}
			kept = append(kept, call)
		}
		msg.ToolCalls = kept
	}
	return resp, nil
}
