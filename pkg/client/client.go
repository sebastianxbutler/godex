// Package client provides backward-compatible access to the Codex backend.
// New code should use godex/pkg/backend/codex directly.
package client

import (
	"context"
	"net/http"
	"time"

	"godex/pkg/auth"
	"godex/pkg/backend"
	"godex/pkg/backend/codex"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// Re-export types for backward compatibility.
type (
	ToolCall     = backend.ToolCall
	StreamResult = backend.StreamResult
	Streamer     = backend.Streamer
	ToolHandler  = backend.ToolHandler
)

// ToolLoopOptions configures the tool execution loop.
type ToolLoopOptions = backend.ToolLoopOptions

// Config holds configuration for the client.
type Config = codex.Config

// Client wraps the Codex backend for backward compatibility.
type Client struct {
	*codex.Client
}

// New creates a new client.
func New(httpClient *http.Client, authStore *auth.Store, cfg Config) *Client {
	return &Client{Client: codex.New(httpClient, authStore, cfg)}
}

// WithBaseURL returns a new client with a different base URL.
func (c *Client) WithBaseURL(baseURL string) *Client {
	return &Client{Client: c.Client.WithBaseURL(baseURL)}
}

// StreamAndCollect streams a request and returns collected output.
// Returns the client-package StreamResult type.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	return c.Client.StreamAndCollect(ctx, req)
}

// RunToolLoop executes a tool loop with the given handler.
func (c *Client) RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions) (StreamResult, error) {
	return c.Client.RunToolLoop(ctx, req, handler, opts)
}

// BuildToolFollowupInputs builds follow-up input items.
func BuildToolFollowupInputs(calls []ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	return codex.BuildToolFollowupInputs(calls, outputs)
}

// StreamAndCollectWith streams using the provided function and returns collected output.
// Deprecated: Use backend.StreamAndCollect or the client's StreamAndCollect method.
func StreamAndCollectWith(ctx context.Context, req protocol.ResponsesRequest, stream Streamer) (StreamResult, error) {
	collector := sse.NewCollector()
	calls := map[string]ToolCall{}

	err := stream(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Type == "response.output_item.added" && ev.Value.Item != nil {
			item := ev.Value.Item
			if item.Type == "function_call" && item.CallID != "" {
				calls[item.CallID] = ToolCall{CallID: item.CallID, Name: item.Name}
			}
		}
		return nil
	})
	if err != nil {
		return StreamResult{}, err
	}

	out := StreamResult{Text: collector.OutputText()}
	for callID, tc := range calls {
		tc.Arguments = collector.FunctionArgs(callID)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}

// RunToolLoopWith executes a tool loop with a custom streamer function.
// Deprecated: Use the client's RunToolLoop method.
func RunToolLoopWith(ctx context.Context, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions, stream Streamer) (StreamResult, error) {
	max := opts.MaxSteps
	if max <= 0 {
		max = 4
	}
	current := req

	for step := 0; step < max; step++ {
		result, err := StreamAndCollectWith(ctx, current, stream)
		if err != nil {
			return StreamResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			return result, nil
		}

		outputs := map[string]string{}
		for _, call := range result.ToolCalls {
			out, herr := handler.Handle(ctx, call)
			if herr != nil {
				out = "err: " + herr.Error()
			}
			outputs[call.CallID] = out
		}

		current = followupRequest(req, BuildToolFollowupInputs(result.ToolCalls, outputs))
	}
	return StreamResult{}, nil
}

func followupRequest(base protocol.ResponsesRequest, input []protocol.ResponseInputItem) protocol.ResponsesRequest {
	return protocol.ResponsesRequest{
		Model:             base.Model,
		Instructions:      base.Instructions,
		Input:             input,
		Tools:             base.Tools,
		ToolChoice:        "auto",
		ParallelToolCalls: base.ParallelToolCalls,
		Reasoning:         base.Reasoning,
		Store:             base.Store,
		Stream:            true,
		Include:           base.Include,
		PromptCacheKey:    base.PromptCacheKey,
		Text:              base.Text,
	}
}

// Legacy default URL for reference.
const defaultBaseURL = "https://chatgpt.com/backend-api/codex"

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		BaseURL:    defaultBaseURL,
		Originator: "codex_cli_rs",
		UserAgent:  "codex_cli_rs/0.0",
		RetryMax:   1,
		RetryDelay: 300 * time.Millisecond,
	}
}
