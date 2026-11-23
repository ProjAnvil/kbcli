package agent

import (
	"context"
	"testing"

	"github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/langchain-golang/langchain/agents/middleware"
)

func TestDedupeToolCallsMiddleware(t *testing.T) {
	resp := middleware.ModelResponse{Result: []messages.Message{
		{
			Role: messages.RoleAI,
			ToolCalls: []messages.ToolCall{
				{ID: "call_1", Name: "kb_search", Args: map[string]any{"a": 1}},
				{ID: "call_2", Name: "grep", Args: map[string]any{"b": 2}},
				{ID: "call_1", Name: "kb_search", Args: map[string]any{"a": 1}},
				{ID: "call_2", Name: "grep", Args: map[string]any{"b": 2}},
			},
		},
	}}
	next := func(context.Context, middleware.ModelRequest) (middleware.ModelResponse, error) {
		return resp, nil
	}

	got, err := dedupeToolCallsMiddleware{}.WrapModelCall(
		context.Background(), middleware.ModelRequest{}, next)
	if err != nil {
		t.Fatalf("WrapModelCall: %v", err)
	}
	calls := got.Result[0].ToolCalls
	if len(calls) != 2 || calls[0].ID != "call_1" || calls[1].ID != "call_2" {
		t.Fatalf("tool calls = %+v, want two unique calls", calls)
	}
}

func TestDedupeToolCallsMiddlewareKeepsUniqueCalls(t *testing.T) {
	resp := middleware.ModelResponse{Result: []messages.Message{
		{
			Role: messages.RoleAI,
			ToolCalls: []messages.ToolCall{
				{ID: "call_1", Name: "kb_search"},
				{ID: "call_2", Name: "grep"},
			},
		},
	}}
	next := func(context.Context, middleware.ModelRequest) (middleware.ModelResponse, error) {
		return resp, nil
	}

	got, err := dedupeToolCallsMiddleware{}.WrapModelCall(
		context.Background(), middleware.ModelRequest{}, next)
	if err != nil {
		t.Fatalf("WrapModelCall: %v", err)
	}
	if len(got.Result[0].ToolCalls) != 2 {
		t.Fatalf("tool calls = %+v, want two calls", got.Result[0].ToolCalls)
	}
}
