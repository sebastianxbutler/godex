package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"godex/pkg/auth"
	"godex/pkg/backend"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func createTestAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	content := `{
		"auth_mode": "chatgpt",
		"tokens": {
			"access_token": "test-token",
			"account_id": "test-account"
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestNew(t *testing.T) {
	store := createTestAuthStore(t)

	// Test with defaults
	client := New(nil, store, Config{})
	if client == nil {
		t.Fatal("New returned nil")
	}
	if client.cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want default", client.cfg.BaseURL)
	}
	if client.cfg.Originator != "codex_cli_rs" {
		t.Errorf("Originator = %q, want default", client.cfg.Originator)
	}
	if client.cfg.RetryMax != 1 {
		t.Errorf("RetryMax = %d, want 1", client.cfg.RetryMax)
	}

	// Test with custom config
	client = New(http.DefaultClient, store, Config{
		BaseURL:    "https://custom.api/v1",
		Originator: "custom",
		UserAgent:  "test/1.0",
		RetryMax:   3,
		RetryDelay: 500 * time.Millisecond,
	})
	if client.cfg.BaseURL != "https://custom.api/v1" {
		t.Errorf("BaseURL = %q", client.cfg.BaseURL)
	}
	if client.cfg.Originator != "custom" {
		t.Errorf("Originator = %q", client.cfg.Originator)
	}
}

func TestName(t *testing.T) {
	client := New(nil, nil, Config{})
	if got := client.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestStreamResponses(t *testing.T) {
	store := createTestAuthStore(t)

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("originator") != "codex_cli_rs" {
			t.Errorf("originator = %q", r.Header.Get("originator"))
		}

		// Stream SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		// Send text delta
		w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"Hello"}` + "\n\n"))
		flusher.Flush()

		// Send done
		w.Write([]byte(`data: {"type":"response.done","response":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL: server.URL,
	})

	var events []sse.Event
	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{
		Model: "gpt-5.2-codex",
	}, func(ev sse.Event) error {
		events = append(events, ev)
		return nil
	})

	if err != nil {
		t.Fatalf("StreamResponses error: %v", err)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
}

func TestStreamResponsesNilCallback(t *testing.T) {
	client := New(nil, nil, Config{})
	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, nil)
	if err == nil {
		t.Error("expected error for nil callback")
	}
}

func TestStreamResponsesNoAuth(t *testing.T) {
	client := New(nil, nil, Config{})
	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for nil auth")
	}
}

func TestStreamResponsesUnauthorized(t *testing.T) {
	store := createTestAuthStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL:      server.URL,
		AllowRefresh: false,
	})

	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})

	if err == nil {
		t.Error("expected error for 401")
	}
}

func TestStreamResponsesRetry(t *testing.T) {
	store := createTestAuthStore(t)
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"type":"response.done"}` + "\n\n"))
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL:    server.URL,
		RetryMax:   2,
		RetryDelay: 10 * time.Millisecond,
	})

	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})

	if err != nil {
		t.Fatalf("StreamResponses error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestStreamAndCollect(t *testing.T) {
	store := createTestAuthStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send text delta
		w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"Hello "}` + "\n\n"))
		flusher.Flush()
		w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"World"}` + "\n\n"))
		flusher.Flush()

		// Send function call
		w.Write([]byte(`data: {"type":"response.output_item.added","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"test_func"}}` + "\n\n"))
		flusher.Flush()

		// Send done
		w.Write([]byte(`data: {"type":"response.done","response":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL: server.URL,
	})

	result, err := client.StreamAndCollect(context.Background(), protocol.ResponsesRequest{})
	if err != nil {
		t.Fatalf("StreamAndCollect error: %v", err)
	}

	if result.Text != "Hello World" {
		t.Errorf("Text = %q, want %q", result.Text, "Hello World")
	}
	if result.Usage == nil {
		t.Error("expected Usage to be set")
	}
}

func TestWithBaseURL(t *testing.T) {
	store := createTestAuthStore(t)
	client := New(nil, store, Config{
		BaseURL: "https://original.api",
	})

	newClient := client.WithBaseURL("https://new.api")

	if newClient.cfg.BaseURL != "https://new.api" {
		t.Errorf("new BaseURL = %q", newClient.cfg.BaseURL)
	}
	if client.cfg.BaseURL != "https://original.api" {
		t.Error("original client should not be modified")
	}
}

func TestListModels(t *testing.T) {
	client := New(nil, nil, Config{})
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("expected at least one model")
	}

	// Check for gpt-5.3-codex
	found := false
	for _, m := range models {
		if m.ID == "gpt-5.3-codex" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gpt-5.3-codex in models")
	}
}

func TestKnownCodexModels(t *testing.T) {
	expectedModels := []string{
		"gpt-5.3-codex",
		"gpt-5.2-codex",
		"o3",
		"o3-mini",
		"o1-pro",
		"o1",
		"o1-mini",
	}

	for _, expected := range expectedModels {
		found := false
		for _, m := range knownCodexModels {
			if m.ID == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected model %q in knownCodexModels", expected)
		}
	}
}

// Note: isRetryable is tested in retry_test.go

func TestClientImplementsBackend(t *testing.T) {
	var _ backend.Backend = (*Client)(nil)
}

func TestStreamResponsesBadRequest(t *testing.T) {
	store := createTestAuthStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL: server.URL,
	})

	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})

	if err == nil {
		t.Error("expected error for 400")
	}
}

func TestStreamResponsesServerError(t *testing.T) {
	store := createTestAuthStore(t)
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL:    server.URL,
		RetryMax:   2,
		RetryDelay: 1 * time.Millisecond,
	})

	err := client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})

	if err == nil {
		t.Error("expected error after retries exhausted")
	}
	// Should have retried
	if attempts < 2 {
		t.Errorf("attempts = %d, expected at least 2", attempts)
	}
}

func TestRequestHeaders(t *testing.T) {
	store := createTestAuthStore(t)

	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"type":"response.done"}` + "\n\n"))
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL:   server.URL,
		SessionID: "test-session-123",
		UserAgent: "test-agent/1.0",
	})

	client.StreamResponses(context.Background(), protocol.ResponsesRequest{}, func(sse.Event) error {
		return nil
	})

	if capturedHeaders.Get("session_id") != "test-session-123" {
		t.Errorf("session_id = %q", capturedHeaders.Get("session_id"))
	}
	if capturedHeaders.Get("User-Agent") != "test-agent/1.0" {
		t.Errorf("User-Agent = %q", capturedHeaders.Get("User-Agent"))
	}
	if capturedHeaders.Get("chatgpt-account-id") != "test-account" {
		t.Errorf("chatgpt-account-id = %q", capturedHeaders.Get("chatgpt-account-id"))
	}
}

func TestRequestBody(t *testing.T) {
	store := createTestAuthStore(t)

	var capturedBody protocol.ResponsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"type":"response.done"}` + "\n\n"))
	}))
	defer server.Close()

	client := New(server.Client(), store, Config{
		BaseURL: server.URL,
	})

	req := protocol.ResponsesRequest{
		Model:        "gpt-5.2-codex",
		Instructions: "Be helpful",
	}

	client.StreamResponses(context.Background(), req, func(sse.Event) error {
		return nil
	})

	if capturedBody.Model != "gpt-5.2-codex" {
		t.Errorf("Model = %q", capturedBody.Model)
	}
	if capturedBody.Instructions != "Be helpful" {
		t.Errorf("Instructions = %q", capturedBody.Instructions)
	}
}
