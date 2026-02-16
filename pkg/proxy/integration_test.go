package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godex/pkg/backend"
)

// TestChatCompletionsRouting tests that requests are routed to the correct backend.
func TestChatCompletionsRouting(t *testing.T) {
	// Create mock backends
	anthropicMock := &backend.MockBackend{
		BackendName: "anthropic",
		Response:    "Hello from Anthropic!",
	}
	codexMock := &backend.MockBackend{
		BackendName: "codex",
		Response:    "Hello from Codex!",
	}

	// Create router
	router := backend.NewRouter(backend.RouterConfig{
		Default: "codex",
		Patterns: map[string][]string{
			"anthropic": {"claude-", "sonnet"},
			"codex":     {"gpt-", "codex-"},
		},
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5",
		},
	})
	router.Register("anthropic", anthropicMock)
	router.Register("codex", codexMock)

	// Create server with router
	srv := &Server{
		cfg: Config{
			AllowAnyKey: true,
		},
		cache:    NewCache(0),
		router:   router,
		models:   map[string]ModelEntry{},
		usage:    NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters: NewLimiterStore("60/m", 10),
		logger:   NewLogger(LogLevelInfo),
	}

	tests := []struct {
		name         string
		model        string
		wantBackend  string
		wantResponse string
	}{
		{
			name:         "claude model routes to anthropic",
			model:        "claude-sonnet-4-5",
			wantBackend:  "anthropic",
			wantResponse: "Hello from Anthropic!",
		},
		{
			name:         "sonnet alias routes to anthropic",
			model:        "sonnet",
			wantBackend:  "anthropic",
			wantResponse: "Hello from Anthropic!",
		},
		{
			name:         "gpt model routes to codex",
			model:        "gpt-4o",
			wantBackend:  "codex",
			wantResponse: "Hello from Codex!",
		},
		{
			name:         "unknown model routes to default (codex)",
			model:        "unknown-model",
			wantBackend:  "codex",
			wantResponse: "Hello from Codex!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := OpenAIChatRequest{
				Model: tt.model,
				Messages: []OpenAIChatMessage{
					{Role: "user", Content: "Hello"},
				},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-key")

			w := httptest.NewRecorder()
			srv.handleChatCompletions(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
			}

			var chatResp OpenAIChatResponse
			if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if len(chatResp.Choices) == 0 {
				t.Fatal("no choices in response")
			}

			content := chatResp.Choices[0].Message.Content
			if content != tt.wantResponse {
				t.Errorf("got content %q, want %q", content, tt.wantResponse)
			}
		})
	}
}

// TestChatCompletionsStreaming tests streaming responses.
func TestChatCompletionsStreaming(t *testing.T) {
	mock := &backend.MockBackend{
		BackendName: "mock",
		Response:    "Streamed response",
	}

	router := backend.NewRouter(backend.RouterConfig{Default: "mock"})
	router.Register("mock", mock)

	srv := &Server{
		cfg: Config{
			AllowAnyKey: true,
		},
		cache:    NewCache(0),
		router:   router,
		models:   map[string]ModelEntry{},
		usage:    NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters: NewLimiterStore("60/m", 10),
		logger:   NewLogger(LogLevelInfo),
	}

	reqBody := OpenAIChatRequest{
		Model:  "any-model",
		Stream: true,
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Check content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// Check SSE format
	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "data:") {
		t.Error("response should contain SSE data lines")
	}
	if !strings.Contains(string(respBody), "[DONE]") {
		t.Error("response should end with [DONE]")
	}
}

// TestChatCompletionsToolCalls tests tool call responses.
func TestChatCompletionsToolCalls(t *testing.T) {
	mock := &backend.MockBackend{
		BackendName: "mock",
		ToolCalls: []backend.ToolCall{
			{
				CallID:    "call_123",
				Name:      "get_weather",
				Arguments: `{"location": "Tokyo"}`,
			},
		},
	}

	router := backend.NewRouter(backend.RouterConfig{Default: "mock"})
	router.Register("mock", mock)

	srv := &Server{
		cfg: Config{
			AllowAnyKey: true,
		},
		cache:    NewCache(0),
		router:   router,
		models:   map[string]ModelEntry{},
		usage:    NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters: NewLimiterStore("60/m", 10),
		logger:   NewLogger(LogLevelInfo),
	}

	reqBody := OpenAIChatRequest{
		Model: "any-model",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []OpenAIChatTool{
			{
				Type: "function",
				Function: &OpenAIFunction{
					Name:        "get_weather",
					Description: "Get weather",
					Parameters:  json.RawMessage(`{"type":"object"}`),
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp OpenAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(chatResp.Choices) == 0 {
		t.Fatal("no choices in response")
	}

	toolCalls := chatResp.Choices[0].Message.ToolCalls
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	if toolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", toolCalls[0].Function.Name)
	}

	if chatResp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason tool_calls, got %s", chatResp.Choices[0].FinishReason)
	}
}

// TestModelAliasExpansion tests that model aliases are expanded.
func TestModelAliasExpansion(t *testing.T) {
	mock := &backend.MockBackend{BackendName: "mock", Response: "OK"}

	router := backend.NewRouter(backend.RouterConfig{
		Default: "mock",
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
			"opus":   "claude-opus-4-5",
		},
	})
	router.Register("mock", mock)

	srv := &Server{
		cfg: Config{
			AllowAnyKey: true,
		},
		cache:    NewCache(0),
		router:   router,
		models:   map[string]ModelEntry{},
		usage:    NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters: NewLimiterStore("60/m", 10),
		logger:   NewLogger(LogLevelInfo),
	}

	tests := []struct {
		inputModel    string
		expectedModel string
	}{
		{"sonnet", "claude-sonnet-4-5-20250929"},
		{"opus", "claude-opus-4-5"},
		{"gpt-4o", "gpt-4o"}, // no alias
	}

	for _, tt := range tests {
		t.Run(tt.inputModel, func(t *testing.T) {
			reqBody := OpenAIChatRequest{
				Model: tt.inputModel,
				Messages: []OpenAIChatMessage{
					{Role: "user", Content: "Hi"},
				},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-key")

			w := httptest.NewRecorder()
			srv.handleChatCompletions(w, req)

			var chatResp OpenAIChatResponse
			json.NewDecoder(w.Result().Body).Decode(&chatResp)

			if chatResp.Model != tt.expectedModel {
				t.Errorf("expected model %s, got %s", tt.expectedModel, chatResp.Model)
			}
		})
	}
}
