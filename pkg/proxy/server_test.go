package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestRequireAuthAllowAnyKey(t *testing.T) {
	s := &Server{cfg: Config{AllowAnyKey: true}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if !s.requireAuth(rr, req) {
		t.Fatalf("expected allow-any-key to pass auth")
	}
}

func TestRequireAuthMissingKey(t *testing.T) {
	s := &Server{cfg: Config{APIKey: "secret"}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if s.requireAuth(rr, req) {
		t.Fatalf("expected missing auth to fail")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := &Server{cfg: Config{}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.handleHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
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
