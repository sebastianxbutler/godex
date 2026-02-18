// Package openapi implements a generic OpenAI-compatible backend.
// It translates between Codex Responses API format (used internally by godex)
// and OpenAI Chat Completions format (used by most providers: Gemini, Groq, etc).
package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"godex/pkg/backend"
	"godex/pkg/config"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

const defaultTimeout = 120 * time.Second

// Config holds configuration for a generic OpenAI-compatible backend.
type Config struct {
	Name      string
	BaseURL   string
	Auth      config.BackendAuthConfig
	Timeout   time.Duration
	Discovery bool
	Models    []config.BackendModelDef
}

// Client implements the Backend interface for OpenAI-compatible APIs.
type Client struct {
	httpClient *http.Client
	cfg        Config
	apiKey     string
}

var _ backend.Backend = (*Client)(nil)

// New creates a new OpenAI-compatible client.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	c := &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		cfg:        cfg,
	}
	if err := c.resolveAuth(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) resolveAuth() error {
	switch c.cfg.Auth.Type {
	case "api_key", "bearer":
		if c.cfg.Auth.KeyEnv != "" {
			c.apiKey = os.Getenv(c.cfg.Auth.KeyEnv)
			if c.apiKey == "" {
				return fmt.Errorf("environment variable %s not set", c.cfg.Auth.KeyEnv)
			}
		} else if c.cfg.Auth.Key != "" {
			c.apiKey = os.Expand(c.cfg.Auth.Key, os.Getenv)
		}
	case "header", "none", "":
		// No API key needed
	default:
		return fmt.Errorf("unknown auth type: %s", c.cfg.Auth.Type)
	}
	return nil
}

func (c *Client) Name() string { return c.cfg.Name }

// ---------------------------------------------------------------------------
// Chat Completions types (OpenAI wire format)
// ---------------------------------------------------------------------------

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// chatChunk is the SSE delta from a streaming chat completion.
type chatChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string         `json:"role,omitempty"`
			Content   string         `json:"content,omitempty"`
			ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// ---------------------------------------------------------------------------
// Request translation: Codex Responses → OpenAI Chat Completions
// ---------------------------------------------------------------------------

func (c *Client) buildChatRequest(req protocol.ResponsesRequest) chatRequest {
	cr := chatRequest{
		Model:  req.Model,
		Stream: true,
	}

	// System instruction
	if req.Instructions != "" {
		cr.Messages = append(cr.Messages, chatMessage{
			Role:    "system",
			Content: req.Instructions,
		})
	}

	// Convert input items to messages
	for _, item := range req.Input {
		switch item.Type {
		case "message":
			var content string
			for _, part := range item.Content {
				content += part.Text
			}
			contentType := "output_text"
			if item.Role != "assistant" {
				contentType = "input_text"
			}
			_ = contentType // only used in Codex format; for chat completions we just use text
			cr.Messages = append(cr.Messages, chatMessage{
				Role:    item.Role,
				Content: content,
			})

		case "function_call":
			cr.Messages = append(cr.Messages, chatMessage{
				Role: "assistant",
				ToolCalls: []chatToolCall{{
					ID:   item.CallID,
					Type: "function",
					Function: chatFunctionCall{
						Name:      item.Name,
						Arguments: item.Arguments,
					},
				}},
			})

		case "function_call_output":
			cr.Messages = append(cr.Messages, chatMessage{
				Role:       "tool",
				ToolCallID: item.CallID,
				Content:    item.Output,
			})
		}
	}

	// Convert tools
	for _, tool := range req.Tools {
		if tool.Type == "function" {
			cr.Tools = append(cr.Tools, chatTool{
				Type: "function",
				Function: chatFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			})
		}
	}

	return cr
}

// ---------------------------------------------------------------------------
// Response translation: OpenAI Chat SSE → Codex Responses SSE
// ---------------------------------------------------------------------------

// StreamResponses translates OpenAI Chat Completions SSE into Codex Responses SSE events.
func (c *Client) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if onEvent == nil {
		return fmt.Errorf("onEvent callback is required")
	}

	chatReq := c.buildChatRequest(req)
	payload, err := json.Marshal(chatReq)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	resp, err := c.doRequest(ctx, "/chat/completions", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Track tool call state for reconstruction
	type toolState struct {
		id   string
		name string
		args strings.Builder
	}
	calls := map[int]*toolState{}
	textStarted := false

	return sse.ParseStream(resp.Body, func(ev sse.Event) error {
		// Try to parse as chat chunk
		var chunk chatChunk
		if err := json.Unmarshal(ev.Raw, &chunk); err != nil {
			return nil // skip unparseable events
		}
		if len(chunk.Choices) == 0 {
			// Usage-only event at end
			if chunk.Usage != nil {
				return onEvent(codexEvent("response.completed", &protocol.StreamEvent{
					Type: "response.completed",
					Response: &protocol.ResponseRef{
						Usage: &protocol.Usage{
							InputTokens:  chunk.Usage.PromptTokens,
							OutputTokens: chunk.Usage.CompletionTokens,
						},
					},
				}))
			}
			return nil
		}

		choice := chunk.Choices[0]

		// Handle text content
		if choice.Delta.Content != "" {
			if !textStarted {
				textStarted = true
				// Emit content part added
				if err := onEvent(codexEvent("response.content_part.added", &protocol.StreamEvent{
					Type: "response.content_part.added",
					Part: &protocol.ContentPart{Type: "output_text"},
				})); err != nil {
					return err
				}
			}
			if err := onEvent(codexEvent("response.output_text.delta", &protocol.StreamEvent{
				Type:  "response.output_text.delta",
				Delta: choice.Delta.Content,
			})); err != nil {
				return err
			}
		}

		// Handle tool calls
		for _, tc := range choice.Delta.ToolCalls {
			state, ok := calls[tc.Index]
			if !ok {
				// New tool call
				state = &toolState{id: tc.ID, name: tc.Function.Name}
				calls[tc.Index] = state

				// Emit output_item.added for the function_call
				if err := onEvent(codexEvent("response.output_item.added", &protocol.StreamEvent{
					Type: "response.output_item.added",
					Item: &protocol.OutputItem{
						ID:     tc.ID,
						Type:   "function_call",
						CallID: tc.ID,
						Name:   tc.Function.Name,
					},
				})); err != nil {
					return err
				}
			}

			// Accumulate arguments
			if tc.Function.Arguments != "" {
				state.args.WriteString(tc.Function.Arguments)

				if err := onEvent(codexEvent("response.function_call_arguments.delta", &protocol.StreamEvent{
					Type:   "response.function_call_arguments.delta",
					Delta:  tc.Function.Arguments,
					ItemID: state.id,
				})); err != nil {
					return err
				}
			}
		}

		// Handle finish
		if choice.FinishReason != nil {
			// Emit response.completed
			var usage *protocol.Usage
			if chunk.Usage != nil {
				usage = &protocol.Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				}
			}
			return onEvent(codexEvent("response.completed", &protocol.StreamEvent{
				Type: "response.completed",
				Response: &protocol.ResponseRef{
					Usage: usage,
				},
			}))
		}

		return nil
	})
}

// codexEvent creates an sse.Event from a Codex StreamEvent.
func codexEvent(eventType string, se *protocol.StreamEvent) sse.Event {
	raw, _ := json.Marshal(se)
	return sse.Event{
		Raw:   raw,
		Value: *se,
	}
}

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	var result backend.StreamResult
	var textParts []string
	calls := map[string]*backend.ToolCall{}
	argsAccum := map[string]*strings.Builder{}

	err := c.StreamResponses(ctx, req, func(ev sse.Event) error {
		switch ev.Value.Type {
		case "response.output_text.delta":
			textParts = append(textParts, ev.Value.Delta)

		case "response.output_item.added":
			if ev.Value.Item != nil && ev.Value.Item.Type == "function_call" {
				tc := &backend.ToolCall{
					CallID: ev.Value.Item.CallID,
					Name:   ev.Value.Item.Name,
				}
				calls[ev.Value.Item.CallID] = tc
				argsAccum[ev.Value.Item.CallID] = &strings.Builder{}
			}

		case "response.function_call_arguments.delta":
			if b, ok := argsAccum[ev.Value.ItemID]; ok {
				b.WriteString(ev.Value.Delta)
			}

		case "response.completed":
			if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
				result.Usage = ev.Value.Response.Usage
			}
		}
		return nil
	})

	result.Text = strings.Join(textParts, "")
	for id, tc := range calls {
		if b, ok := argsAccum[id]; ok {
			tc.Arguments = b.String()
		}
		result.ToolCalls = append(result.ToolCalls, *tc)
	}
	return result, err
}

// ListModels returns the models available from this backend.
func (c *Client) ListModels(ctx context.Context) ([]backend.ModelInfo, error) {
	if len(c.cfg.Models) > 0 {
		models := make([]backend.ModelInfo, len(c.cfg.Models))
		for i, m := range c.cfg.Models {
			models[i] = backend.ModelInfo{ID: m.ID, DisplayName: m.DisplayName}
		}
		return models, nil
	}
	if !c.cfg.Discovery {
		return nil, nil
	}

	resp, err := c.doRequest(ctx, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models request failed with status %d", resp.StatusCode)
	}

	var modelsResp struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]backend.ModelInfo, len(modelsResp.Data))
	for i, m := range modelsResp.Data {
		models[i] = backend.ModelInfo{ID: m.ID}
	}
	return models, nil
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

func (c *Client) doRequest(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := strings.TrimSuffix(c.cfg.BaseURL, "/") + path

	var reqBody io.Reader
	method := http.MethodGet
	if body != nil {
		reqBody = bytes.NewReader(body)
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "text/event-stream")
	c.applyAuth(req)

	return c.httpClient.Do(req)
}

func (c *Client) applyAuth(req *http.Request) {
	switch c.cfg.Auth.Type {
	case "api_key", "bearer":
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	case "header":
		for k, v := range c.cfg.Auth.Headers {
			req.Header.Set(k, os.Expand(v, os.Getenv))
		}
	}
}
