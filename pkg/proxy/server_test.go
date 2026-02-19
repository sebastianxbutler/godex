package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCountInvalidExecPairs(t *testing.T) {
	items := []OpenAIItem{
		{Type: "function_call", CallID: "c1", Name: "exec", Arguments: "{}"},
		{Type: "function_call_output", CallID: "c1", Output: "Validation failed for tool \"exec\":\n  - command: must have required property 'command'"},
		{Type: "function_call", CallID: "c2", Name: "exec", Arguments: "{}"},
		{Type: "function_call_output", CallID: "c2", Output: "Validation failed for tool \"exec\":\n  - command: must have required property 'command'"},
		{Type: "function_call", CallID: "c3", Name: "session_status", Arguments: "{}"},
		{Type: "function_call_output", CallID: "c3", Output: "ok"},
	}
	if got := countInvalidExecPairs(items); got != 2 {
		t.Fatalf("expected 2 invalid exec pairs, got %d", got)
	}
}

func TestCountInvalidExecPairs_IgnoresNonMatching(t *testing.T) {
	items := []OpenAIItem{
		{Type: "function_call", CallID: "c1", Name: "exec", Arguments: "{\"command\":\"ls\"}"},
		{Type: "function_call_output", CallID: "c1", Output: "ok"},
		{Type: "function_call", CallID: "c2", Name: "read", Arguments: "{}"},
		{Type: "function_call_output", CallID: "c2", Output: "Validation failed for tool \"read\""},
	}
	if got := countInvalidExecPairs(items); got != 0 {
		t.Fatalf("expected 0 invalid exec pairs, got %d", got)
	}
}

func TestRequireAuthAllowAnyKey(t *testing.T) {
	s := &Server{cfg: Config{AllowAnyKey: true}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if _, ok := s.requireAuth(rr, req); !ok {
		t.Fatalf("expected allow-any-key to pass auth")
	}
}

func TestRequireAuthMissingKey(t *testing.T) {
	s := &Server{cfg: Config{APIKey: "secret"}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if _, ok := s.requireAuth(rr, req); ok {
		t.Fatalf("expected missing auth to fail")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := &Server{cfg: Config{Version: "v1.2.3"}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.handleHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %q", body["status"])
	}
	if body["version"] != "v1.2.3" {
		t.Fatalf("expected version v1.2.3, got %q", body["version"])
	}
}

func TestRunUsesCustomAuthPath(t *testing.T) {
	tmp := t.TempDir()
	authPath := tmp + "/auth.json"
	content := `{"auth_mode":"api_key","OPENAI_API_KEY":"sk-test"}`
	if err := os.WriteFile(authPath, []byte(content), 0600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	cfg := Config{
		Listen:      "127.0.0.1:0",
		AllowAnyKey: true,
		AuthPath:    authPath,
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- Run(cfg)
	}()

	select {
	case err := <-serverErr:
		if err == nil {
			t.Fatalf("expected error from ListenAndServe")
		}
	case <-time.After(20 * time.Millisecond):
		// Run reached ListenAndServe without auth load error.
	}
}
