// Package openai implements a generic OpenAI-compatible backend.
package openai

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
	apiKey     string // resolved from config
}

// Ensure Client implements Backend interface.
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

	// Resolve API key from config
	if err := c.resolveAuth(); err != nil {
		return nil, err
	}

	return c, nil
}

// resolveAuth resolves authentication from config.
func (c *Client) resolveAuth() error {
	switch c.cfg.Auth.Type {
	case "api_key", "bearer":
		if c.cfg.Auth.KeyEnv != "" {
			c.apiKey = os.Getenv(c.cfg.Auth.KeyEnv)
			if c.apiKey == "" {
				return fmt.Errorf("environment variable %s not set", c.cfg.Auth.KeyEnv)
			}
		} else if c.cfg.Auth.Key != "" {
			c.apiKey = c.resolveEnvVars(c.cfg.Auth.Key)
		}
	case "header", "none", "":
		// No API key needed
	default:
		return fmt.Errorf("unknown auth type: %s", c.cfg.Auth.Type)
	}
	return nil
}

// resolveEnvVars replaces ${VAR} patterns with environment values.
func (c *Client) resolveEnvVars(s string) string {
	return os.Expand(s, os.Getenv)
}

// Name returns the backend identifier.
func (c *Client) Name() string {
	return c.cfg.Name
}

// StreamResponses sends a request and streams events back via the callback.
func (c *Client) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if onEvent == nil {
		return fmt.Errorf("onEvent callback is required")
	}

	// Convert to OpenAI chat completions format
	chatReq := c.toOpenAIRequest(req)
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	return c.parseSSEStream(resp.Body, onEvent)
}

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	var result backend.StreamResult
	var textParts []string

	err := c.StreamResponses(ctx, req, func(ev sse.Event) error {
		// Extract text delta from OpenAI format
		var data struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *protocol.Usage `json:"usage"`
		}
		if err := json.Unmarshal(ev.Raw, &data); err == nil {
			if len(data.Choices) > 0 && data.Choices[0].Delta.Content != "" {
				textParts = append(textParts, data.Choices[0].Delta.Content)
			}
			if data.Usage != nil {
				result.Usage = data.Usage
			}
		}
		return nil
	})

	result.Text = strings.Join(textParts, "")
	return result, err
}

// ListModels returns the models available from this backend.
func (c *Client) ListModels(ctx context.Context) ([]backend.ModelInfo, error) {
	// If hard-coded models are specified, return those
	if len(c.cfg.Models) > 0 {
		models := make([]backend.ModelInfo, len(c.cfg.Models))
		for i, m := range c.cfg.Models {
			models[i] = backend.ModelInfo{
				ID:          m.ID,
				DisplayName: m.DisplayName,
			}
		}
		return models, nil
	}

	// If discovery is disabled, return nil
	if !c.cfg.Discovery {
		return nil, nil
	}

	// Try to fetch from /v1/models
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
		models[i] = backend.ModelInfo{
			ID: m.ID,
		}
	}
	return models, nil
}

// toOpenAIRequest converts a ResponsesRequest to OpenAI chat completions format.
func (c *Client) toOpenAIRequest(req protocol.ResponsesRequest) map[string]any {
	result := map[string]any{
		"model":  req.Model,
		"stream": true,
	}

	// Build messages from input items
	var messages []map[string]any
	for _, item := range req.Input {
		switch item.Type {
		case "message":
			// Extract text content from parts
			var content string
			for _, part := range item.Content {
				if part.Type == "input_text" || part.Type == "text" {
					content += part.Text
				}
			}
			msg := map[string]any{
				"role":    item.Role,
				"content": content,
			}
			messages = append(messages, msg)
		}
	}

	// Add system message if instructions present
	if req.Instructions != "" {
		messages = append([]map[string]any{
			{"role": "system", "content": req.Instructions},
		}, messages...)
	}

	result["messages"] = messages

	// Add tools if present
	if len(req.Tools) > 0 {
		result["tools"] = req.Tools
	}

	return result
}

// doRequest sends an HTTP request to the backend.
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

	// Set headers
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "text/event-stream")

	// Apply authentication
	c.applyAuth(req)

	return c.httpClient.Do(req)
}

// applyAuth adds authentication headers to the request.
func (c *Client) applyAuth(req *http.Request) {
	switch c.cfg.Auth.Type {
	case "api_key", "bearer":
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	case "header":
		for k, v := range c.cfg.Auth.Headers {
			req.Header.Set(k, c.resolveEnvVars(v))
		}
	}
}

// parseSSEStream reads SSE events from the response body.
func (c *Client) parseSSEStream(body io.Reader, onEvent func(sse.Event) error) error {
	return sse.ParseStream(body, onEvent)
}
