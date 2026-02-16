// Package backend provides a unified interface for LLM backends.
package backend

import (
	"context"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// ToolCall represents a function call from the model.
type ToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

// StreamResult contains the collected output from a streaming response.
type StreamResult struct {
	Text      string
	ToolCalls []ToolCall
	Usage     *protocol.Usage
}

// Backend defines the interface that all LLM backends must implement.
type Backend interface {
	// Name returns the backend identifier (e.g., "codex", "anthropic").
	Name() string

	// StreamResponses sends a request and streams events back via the callback.
	StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error

	// StreamAndCollect streams a request and returns collected output.
	StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error)
}

// ToolHandler processes tool calls from the model.
type ToolHandler interface {
	Handle(ctx context.Context, call ToolCall) (string, error)
}

// ToolLoopOptions configures the tool execution loop.
type ToolLoopOptions struct {
	MaxSteps int
}

// Streamer is a function type for streaming responses.
type Streamer func(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error
