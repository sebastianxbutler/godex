package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		UserPatterns: map[string][]string{
			"claude": {"claude-", "sonnet"},
			"codex":  {"gpt-", "codex-"},
		},
		UserAliases: map[string]string{
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

	r := router.New(router.Config{
		UserPatterns: map[string][]string{
			"mock": {"any-model", "gpt-", "claude-", "sonnet", "opus"},
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

	r := router.New(router.Config{
		UserPatterns: map[string][]string{
			"mock": {"any-model", "gpt-", "claude-", "sonnet", "opus"},
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
					Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
				},
			},
		},
		ToolChoice: "auto",
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

	if len(chatResp.Choices[0].Message.ToolCalls) == 0 {
		t.Fatal("expected tool call in response")
	}

	call := chatResp.Choices[0].Message.ToolCalls[0]
	if call.Function.Name != "get_weather" {
		t.Errorf("expected tool name get_weather, got %s", call.Function.Name)
	}
}

// TestModelAliasExpansion tests alias expansion.
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
		UserAliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
			"opus":   "claude-opus-4-5",
		},
		UserPatterns: map[string][]string{
			"mock": {"claude-", "gpt-"},
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

func TestResponsesStreamingToolCallContract(t *testing.T) {
	mock := harness.NewMock(harness.MockConfig{
		HarnessName: "mock",
		Responses: [][]harness.Event{{
			harness.NewToolCallEvent("call_exec_1", "exec", `{"command":"ls","workdir":"/tmp"}`),
			harness.NewUsageEvent(12, 7),
		}},
	})

	r := router.New(router.Config{
		UserPatterns: map[string][]string{
			"mock": {"any-model", "gpt-", "claude-", "sonnet", "opus"},
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

	stream := true
	reqBody := OpenAIResponsesRequest{
		Model:  "any-model",
		Stream: &stream,
		Input:  json.RawMessage(`"run ls"`),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	w := httptest.NewRecorder()
	srv.handleResponses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	type sseEvent struct {
		Type string `json:"type"`
		Item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"item"`
	}

	var sawAdded bool
	var sawAddedArgs bool
	var sawArgsDone bool
	var sawItemDone bool

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" || strings.TrimSpace(payload) == "" {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_item.added":
			if ev.Item.Type == "function_call" && ev.Item.CallID == "call_exec_1" && ev.Item.Name == "exec" {
				sawAdded = true
				if ev.Item.Arguments == `{"command":"ls","workdir":"/tmp"}` {
					sawAddedArgs = true
				}
			}
		case "response.function_call_arguments.done":
			if ev.Item.CallID == "call_exec_1" && ev.Item.Name == "exec" && ev.Item.Arguments == `{"command":"ls","workdir":"/tmp"}` {
				sawArgsDone = true
			}
		case "response.output_item.done":
			if ev.Item.CallID == "call_exec_1" && ev.Item.Name == "exec" && ev.Item.Arguments == `{"command":"ls","workdir":"/tmp"}` {
				sawItemDone = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan SSE: %v", err)
	}
	if !sawAdded {
		t.Fatal("missing response.output_item.added function_call event for exec")
	}
	if !sawAddedArgs {
		t.Fatal("missing expected exec arguments on response.output_item.added")
	}
	if !sawArgsDone {
		t.Fatal("missing response.function_call_arguments.done with expected exec arguments")
	}
	if !sawItemDone {
		t.Fatal("missing response.output_item.done with expected exec arguments")
	}
}

func TestResponsesReplayFixtureContract(t *testing.T) {
	tests := []struct {
		name            string
		fixtureFile     string
		priorFailedCall string
	}{
		{
			name:            "fixture_v1",
			fixtureFile:     "responses_replay_fixture.json",
			priorFailedCall: "call_prev_exec",
		},
		{
			name:            "fixture_v2",
			fixtureFile:     "responses_replay_fixture_v2.json",
			priorFailedCall: "call_prev_exec_2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", tt.fixtureFile)
			body, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			mock := harness.NewMock(harness.MockConfig{
				HarnessName: "mock",
				Record:      true,
				Responses: [][]harness.Event{{
					harness.NewToolCallEvent("call_exec_fixture", "exec", `{"command":"ls","workdir":"/home/cmd/clawd"}`),
					harness.NewUsageEvent(20, 10),
				}},
			})

			r := router.New(router.Config{
				UserPatterns: map[string][]string{
					"mock": {"any-model", "gpt-", "claude-", "sonnet", "opus"},
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

			req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-key")

			w := httptest.NewRecorder()
			srv.handleResponses(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
			}

			var sawExecDone bool
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				line := sc.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				payload := strings.TrimPrefix(line, "data: ")
				if payload == "[DONE]" || strings.TrimSpace(payload) == "" {
					continue
				}
				var ev struct {
					Type string `json:"type"`
					Item struct {
						Type      string `json:"type"`
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"item"`
				}
				if err := json.Unmarshal([]byte(payload), &ev); err != nil {
					continue
				}
				if ev.Type == "response.output_item.done" && ev.Item.Type == "function_call" && ev.Item.Name == "exec" {
					sawExecDone = true
					if ev.Item.Arguments == "" || ev.Item.Arguments == "{}" {
						t.Fatalf("unexpected empty exec arguments in output_item.done: %q", ev.Item.Arguments)
					}
				}
			}
			if err := sc.Err(); err != nil {
				t.Fatalf("scan SSE: %v", err)
			}
			if !sawExecDone {
				t.Fatal("expected exec output_item.done event")
			}

			recorded := mock.Recorded()
			if len(recorded) != 1 {
				t.Fatalf("expected 1 recorded turn, got %d", len(recorded))
			}
			turn := recorded[0]
			for _, msg := range turn.Messages {
				if msg.Role == "assistant" && msg.ToolID == tt.priorFailedCall && msg.Content == "{}" {
					t.Fatalf("unexpected retained failed exec tool-call history for %s", tt.priorFailedCall)
				}
			}
		})
	}
}
