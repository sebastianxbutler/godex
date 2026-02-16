package anthropic

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"godex/pkg/backend"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// Config holds configuration for the Anthropic client.
type Config struct {
	// CredentialsPath is the path to the credentials file.
	// Defaults to ~/.claude/.credentials.json
	CredentialsPath string

	// DefaultMaxTokens is used when MaxTokens is not specified in the request.
	DefaultMaxTokens int
}

// Client implements the Backend interface for Anthropic.
type Client struct {
	tokens *TokenStore
	cfg    Config
}

// Ensure Client implements Backend interface.
var _ backend.Backend = (*Client)(nil)

// New creates a new Anthropic client.
func New(cfg Config) (*Client, error) {
	tokens := NewTokenStore(cfg.CredentialsPath)
	if err := tokens.Load(); err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}

	if cfg.DefaultMaxTokens <= 0 {
		cfg.DefaultMaxTokens = 4096
	}

	return &Client{
		tokens: tokens,
		cfg:    cfg,
	}, nil
}

// Name returns the backend identifier.
func (c *Client) Name() string {
	return "anthropic"
}

// StreamResponses sends a request and streams events back via the callback.
func (c *Client) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	token, err := c.tokens.AccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	client := anthropic.NewClient(option.WithAuthToken(token))

	// Translate request
	anthropicReq, err := translateRequest(req, c.cfg.DefaultMaxTokens)
	if err != nil {
		return fmt.Errorf("translate request: %w", err)
	}

	// Start streaming
	stream := client.Messages.NewStreaming(ctx, anthropicReq)

	// Track state for translation
	var currentItemID string
	var currentToolID string

	for stream.Next() {
		event := stream.Current()

		// Translate and emit events
		sseEvents := translateStreamEvent(event, &currentItemID, &currentToolID)
		for _, ev := range sseEvents {
			if err := onEvent(ev); err != nil {
				return err
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	return nil
}

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	var result backend.StreamResult
	calls := make(map[string]*backend.ToolCall)
	var usage *protocol.Usage

	err := c.StreamResponses(ctx, req, func(ev sse.Event) error {
		switch ev.Value.Type {
		case "response.output_text.delta":
			result.Text += ev.Value.Delta

		case "response.output_item.added":
			if ev.Value.Item != nil && ev.Value.Item.Type == "function_call" {
				calls[ev.Value.Item.CallID] = &backend.ToolCall{
					CallID: ev.Value.Item.CallID,
					Name:   ev.Value.Item.Name,
				}
			}

		case "response.function_call_arguments.delta":
			if ev.Value.Item != nil {
				if call, ok := calls[ev.Value.Item.CallID]; ok {
					call.Arguments += ev.Value.Delta
				}
			}

		case "response.done":
			if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
				usage = ev.Value.Response.Usage
			}
		}
		return nil
	})

	if err != nil {
		return backend.StreamResult{}, err
	}

	for _, call := range calls {
		result.ToolCalls = append(result.ToolCalls, *call)
	}
	result.Usage = usage

	return result, nil
}
