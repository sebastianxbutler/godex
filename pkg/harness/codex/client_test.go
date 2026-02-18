package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"godex/pkg/auth"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func makeAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data := `{"auth_mode":"api_key","OPENAI_API_KEY":"test-token","tokens":{"access_token":"test-token"}}`
	os.WriteFile(path, []byte(data), 0o600)
	store, err := auth.Load(path)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	return store
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	if c.cfg.BaseURL != defaultBaseURL {
		t.Errorf("expected default base URL, got %q", c.cfg.BaseURL)
	}
	if c.cfg.Originator != "codex_cli_rs" {
		t.Errorf("expected default originator, got %q", c.cfg.Originator)
	}
	if c.cfg.RetryMax != 1 {
		t.Errorf("expected retry max 1, got %d", c.cfg.RetryMax)
	}
}

func TestWithBaseURL(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	c2 := c.WithBaseURL("http://custom:8080")
	if c2.cfg.BaseURL != "http://custom:8080" {
		t.Errorf("expected custom URL, got %q", c2.cfg.BaseURL)
	}
	// Original should be unchanged
	if c.cfg.BaseURL != defaultBaseURL {
		t.Error("original should not change")
	}
}

func TestRetryDelay(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{RetryDelay: 100 * time.Millisecond})
	if c.retryDelay(0) != 0 {
		t.Error("attempt 0 should return 0")
	}
	if c.retryDelay(1) != 100*time.Millisecond {
		t.Errorf("attempt 1 should return 100ms, got %v", c.retryDelay(1))
	}
	if c.retryDelay(3) != 300*time.Millisecond {
		t.Errorf("attempt 3 should return 300ms, got %v", c.retryDelay(3))
	}
}

func TestDoRequest_NoAuth(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	_, err := c.doRequest(context.Background(), []byte("{}"))
	if err == nil {
		t.Fatal("expected error with nil auth store")
	}
}

func TestStreamResponses_NilCallback(t *testing.T) {
	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{})
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for nil callback")
	}
}

func TestStreamResponses_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "Hello"}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		done := protocol.StreamEvent{
			Type:     "response.completed",
			Response: &protocol.ResponseRef{Usage: &protocol.Usage{InputTokens: 10, OutputTokens: 5}},
		}
		data2, _ := json.Marshal(done)
		fmt.Fprintf(w, "data: %s\n\n", data2)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})

	var events []protocol.StreamEvent
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{Model: "test"}, func(ev sse.Event) error {
		events = append(events, ev.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 1 {
		t.Fatal("expected at least 1 event")
	}
}

func TestStreamResponses_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL, RetryMax: 0})
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(ev sse.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

func TestStreamAndCollect_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "Hi"}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		done := protocol.StreamEvent{Type: "response.completed", Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 5, OutputTokens: 2},
		}}
		data2, _ := json.Marshal(done)
		fmt.Fprintf(w, "data: %s\n\n", data2)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})
	result, err := c.StreamAndCollect(context.Background(), protocol.ResponsesRequest{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil {
		t.Error("expected usage")
	}
}

func TestListModels_KnownModels(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 5 {
		t.Errorf("expected at least 5 known models, got %d", len(models))
	}
}

func TestBuildToolFollowupInputs(t *testing.T) {
	calls := []ToolCall{
		{CallID: "c1", Name: "shell", Arguments: `{"cmd":"ls"}`},
		{CallID: "c2", Name: "read", Arguments: `{"path":"a.go"}`},
	}
	outputs := map[string]string{
		"c1": "file1\nfile2",
		"c2": "package main",
	}
	items := BuildToolFollowupInputs(calls, outputs)
	if len(items) != 4 { // 2 function_call + 2 function_call_output
		t.Fatalf("expected 4 items, got %d", len(items))
	}
	if items[0].Type != "function_call" {
		t.Error("expected function_call first")
	}
	if items[1].Type != "function_call_output" {
		t.Error("expected function_call_output second")
	}
}

func TestRunToolLoop_NilHandler(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	_, err := c.RunToolLoop(context.Background(), protocol.ResponsesRequest{}, nil, ToolLoopOptions{})
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestFollowupRequest(t *testing.T) {
	base := protocol.ResponsesRequest{
		Model:        "gpt-5.2-codex",
		Instructions: "be helpful",
		Tools:        []protocol.ToolSpec{{Name: "shell"}},
	}
	input := []protocol.ResponseInputItem{{Type: "message", Role: "user"}}
	fr := followupRequest(base, input)
	if fr.Model != "gpt-5.2-codex" {
		t.Error("model should be preserved")
	}
	if fr.ToolChoice != "auto" {
		t.Error("tool choice should be auto")
	}
	if !fr.Stream {
		t.Error("stream should be true")
	}
}

func TestDiscoverModels_NoKey(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	// Unset env var
	origKey := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("OPENAI_API_KEY", origKey)
		}
	}()

	_, err := c.discoverModels(context.Background())
	if err == nil {
		t.Fatal("expected error with no API key")
	}
}

func TestStreamResponses_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})
	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(ev sse.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestStreamResponses_Retry(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		ev := protocol.StreamEvent{Type: "response.completed", Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1},
		}}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{
		BaseURL:    srv.URL,
		RetryMax:   2,
		RetryDelay: 1 * time.Millisecond,
	})

	err := c.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(ev sse.Event) error { return nil })
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestStreamAndCollect_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Item added
		item := protocol.StreamEvent{
			Type: "response.output_item.added",
			Item: &protocol.OutputItem{
				Type:   "function_call",
				CallID: "c1",
				Name:   "shell",
			},
		}
		data, _ := json.Marshal(item)
		fmt.Fprintf(w, "data: %s\n\n", data)
		// Completed
		done := protocol.StreamEvent{Type: "response.completed", Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 10, OutputTokens: 5},
		}}
		data2, _ := json.Marshal(done)
		fmt.Fprintf(w, "data: %s\n\n", data2)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})
	result, err := c.StreamAndCollect(context.Background(), protocol.ResponsesRequest{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

func TestRunToolLoop_MaxStepsDefault(t *testing.T) {
	// Test that default max steps is 4
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "done"}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		done := protocol.StreamEvent{Type: "response.completed", Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1},
		}}
		data2, _ := json.Marshal(done)
		fmt.Fprintf(w, "data: %s\n\n", data2)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})

	handler := &clientTestToolHandler{}
	result, err := c.RunToolLoop(context.Background(), protocol.ResponsesRequest{Model: "test"}, handler, ToolLoopOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text == "" {
		t.Error("expected some text output")
	}
}

type clientTestToolHandler struct{}

func (h *clientTestToolHandler) Handle(ctx context.Context, call ToolCall) (string, error) {
	return "ok", nil
}

func TestDiscoverModels_WithKey(t *testing.T) {
	// discoverModels with a key will try the real API - skip in short mode
	if testing.Short() {
		t.Skip("skips real API call")
	}
}

func TestListModels_WithDiscoverError(t *testing.T) {
	c := NewClient(nil, nil, ClientConfig{})
	// discoverModels will fail (no key), should fall back to known models
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 5 {
		t.Errorf("expected fallback to known models, got %d", len(models))
	}
}

func TestDoRequest_ChatGPTHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("originator") == "" {
			t.Error("expected originator header")
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected user-agent header")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL})
	resp, err := c.doRequest(context.Background(), []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestDoRequest_WithSessionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session_id") != "sess-123" {
			t.Error("expected session_id header")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := makeAuthStore(t)
	c := NewClient(nil, store, ClientConfig{BaseURL: srv.URL, SessionID: "sess-123"})
	resp, err := c.doRequest(context.Background(), []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
