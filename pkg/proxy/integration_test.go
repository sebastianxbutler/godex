package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godex/pkg/harness"
	"godex/pkg/router"
)

// TestChatCompletionsRouting tests that requests are routed to the correct harness.
func TestChatCompletionsRouting(t *testing.T) {
	anthropicMock := harness.NewMock(harness.MockConfig{
		HarnessName: "claude",
		Responses: [][]harness.Event{{
			harness.NewTextEvent("Hello from Anthropic!"),
			harness.NewUsageEvent(10, 5),
		}},
	})
	codexMock := harness.NewMock(harness.MockConfig{
		HarnessName: "codex",
		Responses: [][]harness.Event{{
			harness.NewTextEvent("Hello from Codex!"),
			harness.NewUsageEvent(10, 5),
		}},
	})

	r := router.New(router.Config{
		Default: "codex",
		Patterns: map[string][]string{
			"claude": {"claude-", "sonnet"},
			"codex":  {"gpt-", "codex-"},
		},
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5",
		},
	})
	r.Register("claude", anthropicMock)
	r.Register("codex", codexMock)

	srv := &Server{
		cfg: Config{
			AllowAnyKey: true,
		},
		cache:         NewCache(0),
		harnessRouter: r,
		models:        map[string]ModelEntry{},
		usage:         NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters:      NewLimiterStore("60/m", 10),
		logger:        NewLogger(LogLevelInfo),
	}

	tests := []struct {
		name         string
		model        string
		wantResponse string
	}{
		{
			name:         "claude model routes to claude harness",
			model:        "claude-sonnet-4-5",
			wantResponse: "Hello from Anthropic!",
		},
		{
			name:         "gpt model routes to codex harness",
			model:        "gpt-4o",
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
	mock := harness.NewMock(harness.MockConfig{
		HarnessName: "mock",
		Responses: [][]harness.Event{{
			harness.NewTextEvent("Streamed response"),
			harness.NewUsageEvent(10, 5),
		}},
	})

	r := router.New(router.Config{Default: "mock"})
	r.Register("mock", mock)

	srv := &Server{
		cfg:           Config{AllowAnyKey: true},
		cache:         NewCache(0),
		harnessRouter: r,
		models:        map[string]ModelEntry{},
		usage:         NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters:      NewLimiterStore("60/m", 10),
		logger:        NewLogger(LogLevelInfo),
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

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

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
	mock := harness.NewMock(harness.MockConfig{
		HarnessName: "mock",
		Responses: [][]harness.Event{{
			harness.NewToolCallEvent("call_123", "get_weather", `{"location":"Tokyo"}`),
		}},
	})

	r := router.New(router.Config{Default: "mock"})
	r.Register("mock", mock)

	srv := &Server{
		cfg:           Config{AllowAnyKey: true},
		cache:         NewCache(0),
		harnessRouter: r,
		models:        map[string]ModelEntry{},
		usage:         NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters:      NewLimiterStore("60/m", 10),
		logger:        NewLogger(LogLevelInfo),
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
	mock := harness.NewMock(harness.MockConfig{
		HarnessName: "mock",
		Responses: [][]harness.Event{
			{harness.NewTextEvent("OK"), harness.NewUsageEvent(10, 5)},
			{harness.NewTextEvent("OK"), harness.NewUsageEvent(10, 5)},
			{harness.NewTextEvent("OK"), harness.NewUsageEvent(10, 5)},
		},
	})

	r := router.New(router.Config{
		Default: "mock",
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
			"opus":   "claude-opus-4-5",
		},
	})
	r.Register("mock", mock)

	srv := &Server{
		cfg:           Config{AllowAnyKey: true},
		cache:         NewCache(0),
		harnessRouter: r,
		models:        map[string]ModelEntry{},
		usage:         NewUsageStore("", "", 0, 0, 0, "", 0, 0),
		limiters:      NewLimiterStore("60/m", 10),
		logger:        NewLogger(LogLevelInfo),
	}

	tests := []struct {
		inputModel    string
		expectedModel string
	}{
		{"sonnet", "claude-sonnet-4-5-20250929"},
		{"opus", "claude-opus-4-5"},
		{"gpt-4o", "gpt-4o"},
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
