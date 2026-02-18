// Package client provides backward-compatible access to the Codex backend.
// New code should use godex/pkg/harness/codex directly.
package client

import (
	"context"
	"net/http"
	"time"

	"godex/pkg/auth"
	harnessCodex "godex/pkg/harness/codex"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// Re-export types for backward compatibility.
type (
	ToolCall     = harnessCodex.ToolCall
	StreamResult = harnessCodex.StreamResult
	ToolHandler  = harnessCodex.ToolLoopHandler
)

// ToolLoopOptions configures the tool execution loop.
type ToolLoopOptions = harnessCodex.ToolLoopOptions

// Config holds configuration for the client.
type Config = harnessCodex.ClientConfig

// Client wraps the Codex harness client for backward compatibility.
type Client struct {
	*harnessCodex.Client
}

// New creates a new client.
func New(httpClient *http.Client, authStore *auth.Store, cfg Config) *Client {
	return &Client{Client: harnessCodex.NewClient(httpClient, authStore, cfg)}
}

// WithBaseURL returns a new client with a different base URL.
func (c *Client) WithBaseURL(baseURL string) *Client {
	return &Client{Client: c.Client.WithBaseURL(baseURL)}
}

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	return c.Client.StreamAndCollect(ctx, req)
}

// RunToolLoop executes a tool loop with the given handler.
func (c *Client) RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions) (StreamResult, error) {
	return c.Client.RunToolLoop(ctx, req, handler, opts)
}

// BuildToolFollowupInputs builds follow-up input items.
func BuildToolFollowupInputs(calls []ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	return harnessCodex.BuildToolFollowupInputs(calls, outputs)
}

// Streamer is a function type for streaming responses.
type Streamer func(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error

// StreamAndCollectWith streams using the provided function and returns collected output.
// Deprecated: Use the client's StreamAndCollect method.
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
