// Package codex implements the Codex/ChatGPT backend.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"godex/pkg/auth"
	"godex/pkg/backend"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

const defaultBaseURL = "https://chatgpt.com/backend-api/codex"

// Config holds configuration for the Codex client.
type Config struct {
	BaseURL      string
	Originator   string
	UserAgent    string
	SessionID    string
	AllowRefresh bool
	RetryMax     int
	RetryDelay   time.Duration
}

// Client implements the Backend interface for Codex.
type Client struct {
	httpClient *http.Client
	auth       *auth.Store
	cfg        Config
}

// Ensure Client implements Backend interface.
var _ backend.Backend = (*Client)(nil)

// New creates a new Codex client.
func New(httpClient *http.Client, authStore *auth.Store, cfg Config) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.Originator == "" {
		cfg.Originator = "codex_cli_rs"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "codex_cli_rs/0.0"
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = 1
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 300 * time.Millisecond
	}
	return &Client{httpClient: httpClient, auth: authStore, cfg: cfg}
}

// Name returns the backend identifier.
func (c *Client) Name() string {
	return "codex"
}

// StreamResponses sends a request and streams events back via the callback.
func (c *Client) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if onEvent == nil {
		return fmt.Errorf("onEvent callback is required")
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	refreshed := false
	retried := 0
	for {
		resp, err := c.doRequest(ctx, payload)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && !refreshed {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if c.auth != nil && c.cfg.AllowRefresh {
				if err := c.auth.Refresh(ctx, auth.RefreshOptions{AllowNetwork: true, HTTPClient: c.httpClient}); err == nil {
					refreshed = true
					continue
				}
			}
			return fmt.Errorf("request failed with status 401")
		}
		if isRetryable(resp.StatusCode) && retried < c.cfg.RetryMax {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			delay := c.retryDelay(retried + 1)
			if delay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
			retried++
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		defer resp.Body.Close()
		return sse.ParseStream(resp.Body, onEvent)
	}
}

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	collector := sse.NewCollector()
	calls := map[string]backend.ToolCall{}
	var usage *protocol.Usage

	err := c.StreamResponses(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
			usage = ev.Value.Response.Usage
		}
		if ev.Value.Type == "response.output_item.added" && ev.Value.Item != nil {
			item := ev.Value.Item
			if item.Type == "function_call" && item.CallID != "" {
				calls[item.CallID] = backend.ToolCall{CallID: item.CallID, Name: item.Name}
			}
		}
		return nil
	})
	if err != nil {
		return backend.StreamResult{}, err
	}

	out := backend.StreamResult{Text: collector.OutputText(), Usage: usage}
	for callID, tc := range calls {
		tc.Arguments = collector.FunctionArgs(callID)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}

// WithBaseURL returns a new client with a different base URL.
func (c *Client) WithBaseURL(baseURL string) *Client {
	newCfg := c.cfg
	newCfg.BaseURL = baseURL
	return &Client{
		httpClient: c.httpClient,
		auth:       c.auth,
		cfg:        newCfg,
	}
}

func (c *Client) doRequest(ctx context.Context, payload []byte) (*http.Response, error) {
	if c.auth == nil {
		return nil, fmt.Errorf("auth store is required")
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/responses"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	token, err := c.auth.AuthorizationToken()
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+token)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("originator", c.cfg.Originator)
	hreq.Header.Set("User-Agent", c.cfg.UserAgent)
	if c.cfg.SessionID != "" {
		hreq.Header.Set("session_id", c.cfg.SessionID)
	}
	if c.auth.IsChatGPT() {
		if accountID := c.auth.AccountID(); accountID != "" {
			hreq.Header.Set("chatgpt-account-id", accountID)
		}
	}
	resp, err := c.httpClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func (c *Client) retryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return time.Duration(attempt) * c.cfg.RetryDelay
}

// knownCodexModels lists the models available via the Codex/ChatGPT backend.
// This is a static list since there's no discovery API.
var knownCodexModels = []backend.ModelInfo{
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"},
	{ID: "gpt-5.2-codex", DisplayName: "GPT-5.2 Codex"},
	{ID: "o3", DisplayName: "o3"},
	{ID: "o3-mini", DisplayName: "o3 Mini"},
	{ID: "o1-pro", DisplayName: "o1 Pro"},
	{ID: "o1", DisplayName: "o1"},
	{ID: "o1-mini", DisplayName: "o1 Mini"},
}

// ListModels returns the known models for the Codex backend.
func (c *Client) ListModels(ctx context.Context) ([]backend.ModelInfo, error) {
	return knownCodexModels, nil
}
