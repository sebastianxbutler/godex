package codex

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

	"godex/pkg/auth"
	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

const defaultBaseURL = "https://chatgpt.com/backend-api/codex"

// ClientConfig holds configuration for the Codex client.
type ClientConfig struct {
	BaseURL      string
	Originator   string
	UserAgent    string
	SessionID    string
	AllowRefresh bool
	RetryMax     int
	RetryDelay   time.Duration
}

// Client implements the Codex/ChatGPT API client directly.
type Client struct {
	httpClient *http.Client
	auth       *auth.Store
	cfg        ClientConfig
}

// NewClient creates a new Codex API client.
func NewClient(httpClient *http.Client, authStore *auth.Store, cfg ClientConfig) *Client {
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

// StreamAndCollect streams a request and returns collected output.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	collector := sse.NewCollector()
	calls := map[string]ToolCall{}
	var usage *protocol.Usage

	err := c.StreamResponses(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
			usage = ev.Value.Response.Usage
		}
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

	out := StreamResult{Text: collector.OutputText(), Usage: usage}
	for callID, tc := range calls {
		tc.Arguments = collector.FunctionArgs(callID)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}

// knownCodexModels lists the models available via the Codex/ChatGPT backend.
var knownCodexModels = []harness.ModelInfo{
	{ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex", Provider: "codex"},
	{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", Provider: "codex"},
	{ID: "o3", Name: "o3", Provider: "codex"},
	{ID: "o3-mini", Name: "o3 Mini", Provider: "codex"},
	{ID: "o1-pro", Name: "o1 Pro", Provider: "codex"},
	{ID: "o1", Name: "o1", Provider: "codex"},
	{ID: "o1-mini", Name: "o1 Mini", Provider: "codex"},
}

// ListModels returns models for the Codex/GPT backend.
func (c *Client) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	discovered, err := c.discoverModels(ctx)
	if err != nil || len(discovered) == 0 {
		return knownCodexModels, nil
	}
	seen := map[string]bool{}
	var merged []harness.ModelInfo
	for _, m := range discovered {
		seen[m.ID] = true
		merged = append(merged, m)
	}
	for _, m := range knownCodexModels {
		if !seen[m.ID] {
			merged = append(merged, m)
		}
	}
	return merged, nil
}

func (c *Client) discoverModels(ctx context.Context) ([]harness.ModelInfo, error) {
	key := ""
	if k, ok := harness.ProviderKey(ctx); ok {
		key = k
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if key == "" {
		return nil, fmt.Errorf("no OpenAI API key")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI models API: %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var models []harness.ModelInfo
	for _, m := range body.Data {
		models = append(models, harness.ModelInfo{ID: m.ID, Provider: "codex"})
	}
	return models, nil
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

// RunToolLoop executes a tool loop using the Codex Responses API wire format.
func (c *Client) RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler ToolLoopHandler, opts ToolLoopOptions) (StreamResult, error) {
	if handler == nil {
		return StreamResult{}, fmt.Errorf("tool handler is required")
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = 4
	}
	current := req

	for step := 0; step < max; step++ {
		result, err := c.StreamAndCollect(ctx, current)
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
	return StreamResult{}, fmt.Errorf("tool loop exceeded max steps")
}

// ToolLoopHandler processes tool calls from the model at the wire level.
type ToolLoopHandler interface {
	Handle(ctx context.Context, call ToolCall) (string, error)
}

// ToolLoopOptions configures the tool execution loop.
type ToolLoopOptions struct {
	MaxSteps int
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

// BuildToolFollowupInputs builds follow-up input items containing the tool call
// and tool output pairs. Outputs map is keyed by call_id.
func BuildToolFollowupInputs(calls []ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	items := make([]protocol.ResponseInputItem, 0, len(calls)*2)
	for _, call := range calls {
		items = append(items, protocol.FunctionCallInput(call.Name, call.CallID, call.Arguments))
		output := outputs[call.CallID]
		items = append(items, protocol.FunctionCallOutputInput(call.CallID, output))
	}
	return items
}
