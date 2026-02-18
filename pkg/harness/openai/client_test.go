package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godex/pkg/config"
	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestNewClient_MissingBaseURL(t *testing.T) {
	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestNewClient_Basic(t *testing.T) {
	c, err := NewClient(ClientConfig{
		Name:    "test",
		BaseURL: "http://localhost:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "test" {
		t.Errorf("expected 'test', got %q", c.Name())
	}
}

func TestNewClient_UnknownAuth(t *testing.T) {
	_, err := NewClient(ClientConfig{
		BaseURL: "http://localhost",
		Auth:    config.BackendAuthConfig{Type: "magic"},
	})
	if err == nil {
		t.Fatal("expected error for unknown auth type")
	}
}

func TestNewClient_AuthTypes(t *testing.T) {
	tests := []struct {
		authType string
	}{
		{"api_key"},
		{"bearer"},
		{"header"},
		{"none"},
		{""},
	}
	for _, tt := range tests {
		t.Run(tt.authType, func(t *testing.T) {
			_, err := NewClient(ClientConfig{
				BaseURL: "http://localhost",
				Auth:    config.BackendAuthConfig{Type: tt.authType},
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewClient_ApiKeyFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY_12345", "sk-test")
	c, err := NewClient(ClientConfig{
		BaseURL: "http://localhost",
		Auth: config.BackendAuthConfig{
			Type:   "api_key",
			KeyEnv: "TEST_API_KEY_12345",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "sk-test" {
		t.Errorf("expected 'sk-test', got %q", c.apiKey)
	}
}

func TestBuildChatRequest(t *testing.T) {
	c, _ := NewClient(ClientConfig{BaseURL: "http://localhost"})
	req := protocol.ResponsesRequest{
		Model:        "gpt-4o",
		Instructions: "Be helpful",
		Input: []protocol.ResponseInputItem{
			{Type: "message", Role: "user", Content: []protocol.InputContentPart{{Text: "Hello"}}},
			{Type: "function_call", CallID: "c1", Name: "shell", Arguments: `{"cmd":"ls"}`},
			{Type: "function_call_output", CallID: "c1", Output: "file.go"},
		},
		Tools: []protocol.ToolSpec{
			{Type: "function", Name: "shell", Description: "Run command", Parameters: json.RawMessage(`{}`)},
		},
	}

	cr := c.buildChatRequest(req)
	if cr.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", cr.Model)
	}
	// system + user + assistant(tool_call) + tool = 4 messages
	if len(cr.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(cr.Messages))
	}
	if cr.Messages[0].Role != "system" {
		t.Error("expected system message first")
	}
	if len(cr.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cr.Tools))
	}
}

func TestListModels_StaticModels(t *testing.T) {
	c, _ := NewClient(ClientConfig{
		BaseURL: "http://localhost",
		Name:    "test",
		Models: []config.BackendModelDef{
			{ID: "model-a", DisplayName: "Model A"},
			{ID: "model-b", DisplayName: "Model B"},
		},
	})
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "model-a" || models[0].Provider != "test" {
		t.Errorf("unexpected model: %+v", models[0])
	}
}

func TestListModels_NoDiscovery(t *testing.T) {
	c, _ := NewClient(ClientConfig{BaseURL: "http://localhost"})
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if models != nil {
		t.Error("expected nil models with no discovery and no static models")
	}
}

func TestListModels_Discovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4o", "created": 1000},
				},
			})
		}
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL, Name: "test", Discovery: true})
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4o" {
		t.Errorf("unexpected models: %v", models)
	}
}

func TestListModels_DiscoveryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL, Discovery: true})
	_, err := c.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamResponses_NilCallback(t *testing.T) {
	c, _ := NewClient(ClientConfig{BaseURL: "http://localhost"})
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for nil callback")
	}
}

func TestStreamResponses_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{Model: "test"}, func(ev sse.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error: %v", err)
	}
}

func sseChunk(data string) string {
	return fmt.Sprintf("data: %s\n\n", data)
}

func TestStreamResponses_TextStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk1 := chatChunk{ID: "1", Choices: []struct {
			Index int `json:"index"`
			Delta struct {
				Role      string         `json:"role,omitempty"`
				Content   string         `json:"content,omitempty"`
				ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason,omitempty"`
		}{{Delta: struct {
			Role      string         `json:"role,omitempty"`
			Content   string         `json:"content,omitempty"`
			ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
		}{Content: "Hello"}}}}
		d1, _ := json.Marshal(chunk1)
		w.Write([]byte(sseChunk(string(d1))))

		stop := "stop"
		chunk2 := chatChunk{ID: "1", Choices: []struct {
			Index int `json:"index"`
			Delta struct {
				Role      string         `json:"role,omitempty"`
				Content   string         `json:"content,omitempty"`
				ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason,omitempty"`
		}{{FinishReason: &stop}}, Usage: &struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{PromptTokens: 10, CompletionTokens: 5}}
		d2, _ := json.Marshal(chunk2)
		w.Write([]byte(sseChunk(string(d2))))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL})
	var events []sse.Event
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{Model: "test"}, func(ev sse.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should have: content_part.added + text.delta + completed
	hasText := false
	hasCompleted := false
	for _, ev := range events {
		if ev.Value.Type == "response.output_text.delta" {
			hasText = true
		}
		if ev.Value.Type == "response.completed" {
			hasCompleted = true
		}
	}
	if !hasText {
		t.Error("expected text delta event")
	}
	if !hasCompleted {
		t.Error("expected completed event")
	}
}

func TestStreamResponses_ToolCallStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Tool call start
		chunk1 := `{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":""}}]}}]}`
		w.Write([]byte(sseChunk(chunk1)))
		// Tool call args
		chunk2 := `{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":\"ls\"}"}}]}}]}`
		w.Write([]byte(sseChunk(chunk2)))
		// Finish
		chunk3 := `{"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
		w.Write([]byte(sseChunk(chunk3)))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL})
	var events []sse.Event
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{Model: "test"}, func(ev sse.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hasItemAdded := false
	hasArgsDelta := false
	for _, ev := range events {
		if ev.Value.Type == "response.output_item.added" {
			hasItemAdded = true
		}
		if ev.Value.Type == "response.function_call_arguments.delta" {
			hasArgsDelta = true
		}
	}
	if !hasItemAdded {
		t.Error("expected output_item.added")
	}
	if !hasArgsDelta {
		t.Error("expected function_call_arguments.delta")
	}
}

func TestClient_StreamAndCollect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `{"id":"1","choices":[{"index":0,"delta":{"content":"Hi"}}]}`
		w.Write([]byte(sseChunk(chunk)))
		stop := `{"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`
		w.Write([]byte(sseChunk(stop)))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.StreamAndCollect(context.Background(), protocol.ResponsesRequest{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hi" {
		t.Errorf("expected 'Hi', got %q", result.Text)
	}
}

func TestApplyAuth_ProviderKey(t *testing.T) {
	c, _ := NewClient(ClientConfig{BaseURL: "http://localhost"})
	ctx := harness.WithProviderKey(context.Background(), "override-key")
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost", nil)
	c.applyAuth(ctx, req)
	if got := req.Header.Get("Authorization"); got != "Bearer override-key" {
		t.Errorf("expected 'Bearer override-key', got %q", got)
	}
}

func TestApplyAuth_ApiKey(t *testing.T) {
	c, _ := NewClient(ClientConfig{
		BaseURL: "http://localhost",
		Auth:    config.BackendAuthConfig{Type: "api_key", Key: "sk-123"},
	})
	req, _ := http.NewRequest("GET", "http://localhost", nil)
	c.applyAuth(context.Background(), req)
	if got := req.Header.Get("Authorization"); got != "Bearer sk-123" {
		t.Errorf("expected 'Bearer sk-123', got %q", got)
	}
}

func TestApplyAuth_Header(t *testing.T) {
	c, _ := NewClient(ClientConfig{
		BaseURL: "http://localhost",
		Auth: config.BackendAuthConfig{
			Type:    "header",
			Headers: map[string]string{"X-Custom": "value"},
		},
	})
	req, _ := http.NewRequest("GET", "http://localhost", nil)
	c.applyAuth(context.Background(), req)
	if got := req.Header.Get("X-Custom"); got != "value" {
		t.Errorf("expected 'value', got %q", got)
	}
}

func TestStreamResponses_UsageOnlyChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Chunk with usage but no choices
		chunk := `{"id":"1","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":10}}`
		w.Write([]byte(sseChunk(chunk)))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL})
	var events []sse.Event
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{Model: "test"}, func(ev sse.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hasCompleted := false
	for _, ev := range events {
		if ev.Value.Type == "response.completed" {
			hasCompleted = true
		}
	}
	if !hasCompleted {
		t.Error("expected completed event from usage-only chunk")
	}
}

func TestCodexEvent(t *testing.T) {
	se := &protocol.StreamEvent{Type: "test.event", Delta: "hello"}
	ev := codexEvent("test.event", se)
	if ev.Value.Type != "test.event" {
		t.Errorf("expected 'test.event', got %q", ev.Value.Type)
	}
	if len(ev.Raw) == 0 {
		t.Error("expected non-empty raw")
	}
}
