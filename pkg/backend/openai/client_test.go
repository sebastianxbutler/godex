package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"godex/pkg/backend"
	"godex/pkg/config"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     Config{Name: "test", BaseURL: "http://localhost:8080/v1"},
			wantErr: false,
		},
		{
			name:    "missing base_url",
			cfg:     Config{Name: "test"},
			wantErr: true,
		},
		{
			name: "with api key",
			cfg: Config{
				Name:    "test",
				BaseURL: "http://localhost:8080/v1",
				Auth:    config.BackendAuthConfig{Type: "api_key", Key: "test-key"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClientName(t *testing.T) {
	c, _ := New(Config{Name: "ollama", BaseURL: "http://localhost:11434/v1"})
	if c.Name() != "ollama" {
		t.Errorf("Name() = %s, want ollama", c.Name())
	}
}

func TestClientListModelsHardcoded(t *testing.T) {
	c, _ := New(Config{
		Name:      "test",
		BaseURL:   "http://localhost:8080/v1",
		Discovery: false,
		Models: []config.BackendModelDef{
			{ID: "model-a", DisplayName: "Model A"},
			{ID: "model-b", DisplayName: "Model B"},
		},
	})

	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "model-a" {
		t.Errorf("expected model-a, got %s", models[0].ID)
	}
}

func TestClientListModelsDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "discovered-model-1"},
				{"id": "discovered-model-2"},
			},
		})
	}))
	defer server.Close()

	c, _ := New(Config{
		Name:      "test",
		BaseURL:   server.URL + "/v1",
		Discovery: true,
	})

	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestBuildChatRequest(t *testing.T) {
	c, _ := New(Config{Name: "test", BaseURL: "http://localhost/v1"})

	req := protocol.ResponsesRequest{
		Model:        "test-model",
		Instructions: "You are helpful",
		Input: []protocol.ResponseInputItem{
			{Type: "message", Role: "user", Content: []protocol.InputContentPart{{Type: "input_text", Text: "Hello"}}},
		},
	}

	cr := c.buildChatRequest(req)

	if cr.Model != "test-model" {
		t.Errorf("model = %s, want test-model", cr.Model)
	}
	if !cr.Stream {
		t.Error("stream should be true")
	}
	if len(cr.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(cr.Messages))
	}
	if cr.Messages[0].Role != "system" {
		t.Errorf("first message role = %s, want system", cr.Messages[0].Role)
	}
	if cr.Messages[1].Role != "user" {
		t.Errorf("second message role = %s, want user", cr.Messages[1].Role)
	}
	if cr.Messages[1].Content != "Hello" {
		t.Errorf("user content = %s, want Hello", cr.Messages[1].Content)
	}
}

func TestBuildChatRequestWithTools(t *testing.T) {
	c, _ := New(Config{Name: "test", BaseURL: "http://localhost/v1"})

	req := protocol.ResponsesRequest{
		Model: "test-model",
		Input: []protocol.ResponseInputItem{
			{Type: "message", Role: "user", Content: []protocol.InputContentPart{{Type: "input_text", Text: "Run ls"}}},
		},
		Tools: []protocol.ToolSpec{
			{Type: "function", Name: "exec", Description: "Run command", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}

	cr := c.buildChatRequest(req)

	if len(cr.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cr.Tools))
	}
	if cr.Tools[0].Function.Name != "exec" {
		t.Errorf("tool name = %s, want exec", cr.Tools[0].Function.Name)
	}
}

func TestBuildChatRequestWithToolHistory(t *testing.T) {
	c, _ := New(Config{Name: "test", BaseURL: "http://localhost/v1"})

	req := protocol.ResponsesRequest{
		Model: "test-model",
		Input: []protocol.ResponseInputItem{
			{Type: "message", Role: "user", Content: []protocol.InputContentPart{{Type: "input_text", Text: "Run ls"}}},
			{Type: "function_call", Name: "exec", CallID: "call_123", Arguments: `{"command":"ls"}`},
			{Type: "function_call_output", CallID: "call_123", Output: "file1\nfile2"},
		},
	}

	cr := c.buildChatRequest(req)

	if len(cr.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(cr.Messages))
	}
	// User message
	if cr.Messages[0].Role != "user" {
		t.Errorf("msg[0] role = %s, want user", cr.Messages[0].Role)
	}
	// Assistant tool call
	if cr.Messages[1].Role != "assistant" {
		t.Errorf("msg[1] role = %s, want assistant", cr.Messages[1].Role)
	}
	if len(cr.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(cr.Messages[1].ToolCalls))
	}
	if cr.Messages[1].ToolCalls[0].ID != "call_123" {
		t.Errorf("tool call id = %s, want call_123", cr.Messages[1].ToolCalls[0].ID)
	}
	// Tool result
	if cr.Messages[2].Role != "tool" {
		t.Errorf("msg[2] role = %s, want tool", cr.Messages[2].Role)
	}
	if cr.Messages[2].ToolCallID != "call_123" {
		t.Errorf("tool_call_id = %s, want call_123", cr.Messages[2].ToolCallID)
	}
	if cr.Messages[2].Content != "file1\nfile2" {
		t.Errorf("tool content = %s, want file1\\nfile2", cr.Messages[2].Content)
	}
}

func TestStreamAndCollectText(t *testing.T) {
	// Mock server that returns a streaming text response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	c, _ := New(Config{Name: "test", BaseURL: server.URL + "/v1"})
	result, err := c.StreamAndCollect(context.Background(), protocol.ResponsesRequest{
		Model: "test-model",
		Input: []protocol.ResponseInputItem{
			protocol.UserMessage("Hi"),
		},
	})

	if err != nil {
		t.Fatalf("StreamAndCollect: %v", err)
	}
	if result.Text != "Hello world" {
		t.Errorf("text = %q, want %q", result.Text, "Hello world")
	}
}

func TestStreamAndCollectToolCalls(t *testing.T) {
	// Mock server that returns a streaming tool call response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"exec","arguments":""}}]}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":"}}]}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls /tmp\"}"}}]}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	c, _ := New(Config{Name: "test", BaseURL: server.URL + "/v1"})
	result, err := c.StreamAndCollect(context.Background(), protocol.ResponsesRequest{
		Model: "test-model",
		Input: []protocol.ResponseInputItem{
			protocol.UserMessage("List files"),
		},
		Tools: []protocol.ToolSpec{
			{Type: "function", Name: "exec", Description: "Run command"},
		},
	})

	if err != nil {
		t.Fatalf("StreamAndCollect: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Name != "exec" {
		t.Errorf("tool name = %s, want exec", tc.Name)
	}
	if tc.CallID != "call_abc" {
		t.Errorf("call id = %s, want call_abc", tc.CallID)
	}
	if tc.Arguments != `{"command":"ls /tmp"}` {
		t.Errorf("arguments = %s, want {\"command\":\"ls /tmp\"}", tc.Arguments)
	}
}

func TestStreamResponsesEmitsCodexEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"}}]}`,
			`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	c, _ := New(Config{Name: "test", BaseURL: server.URL + "/v1"})

	var events []string
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{
		Model: "test-model",
		Input: []protocol.ResponseInputItem{protocol.UserMessage("Hi")},
	}, func(ev sse.Event) error {
		events = append(events, ev.Value.Type)
		return nil
	})

	if err != nil {
		t.Fatalf("StreamResponses: %v", err)
	}

	// Should get Codex-format events
	want := []string{
		"response.content_part.added",
		"response.output_text.delta",
		"response.completed",
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events %v, want %d %v", len(events), events, len(want), want)
	}
	for i, w := range want {
		if events[i] != w {
			t.Errorf("event[%d] = %s, want %s", i, events[i], w)
		}
	}
}

func TestClientAuthHeaders(t *testing.T) {
	tests := []struct {
		name     string
		auth     config.BackendAuthConfig
		wantAuth string
	}{
		{
			name:     "api_key",
			auth:     config.BackendAuthConfig{Type: "api_key", Key: "test-key"},
			wantAuth: "Bearer test-key",
		},
		{
			name:     "bearer",
			auth:     config.BackendAuthConfig{Type: "bearer", Key: "bearer-token"},
			wantAuth: "Bearer bearer-token",
		},
		{
			name:     "none",
			auth:     config.BackendAuthConfig{Type: "none"},
			wantAuth: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(Config{Name: "test", BaseURL: "http://localhost/v1", Auth: tt.auth})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			req, _ := http.NewRequest("GET", "http://localhost", nil)
			c.applyAuth(req)
			got := req.Header.Get("Authorization")
			if got != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tt.wantAuth)
			}
		})
	}
}

// Ensure Client implements Backend interface at compile time.
func TestBackendInterface(t *testing.T) {
	var _ backend.Backend = (*Client)(nil)
}
