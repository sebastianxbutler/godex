package anthropic

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
)

func TestTokenStore(t *testing.T) {
	// Create a temp credentials file
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Write test credentials (using Unix millis format like Claude Code)
	expiresAt := time.Now().Add(1 * time.Hour).UnixMilli()
	data := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"test-token-123","refreshToken":"refresh-456","expiresAt":%d,"subscriptionType":"max"}}`, expiresAt))
	os.WriteFile(credPath, data, 0600)

	// Test loading
	store := NewTokenStore(credPath)
	if err := store.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Test access token
	token, err := store.AccessToken()
	if err != nil {
		t.Fatalf("AccessToken failed: %v", err)
	}
	if token != "test-token-123" {
		t.Errorf("expected test-token-123, got %s", token)
	}

	// Test subscription type
	if store.SubscriptionType() != "max" {
		t.Errorf("expected max, got %s", store.SubscriptionType())
	}

	// Test not expired
	if store.IsExpired() {
		t.Error("token should not be expired")
	}

	// Test CanRefresh
	if !store.CanRefresh() {
		t.Error("should be able to refresh with refresh token")
	}

	// Test RefreshToken
	if store.RefreshToken() != "refresh-456" {
		t.Errorf("expected refresh-456, got %s", store.RefreshToken())
	}
}

func TestTokenStoreExpiredNoRefreshToken(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Write expired credentials WITHOUT refresh token
	expiresAt := time.Now().Add(-1 * time.Hour).UnixMilli()
	data := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"expired-token","expiresAt":%d}}`, expiresAt))
	os.WriteFile(credPath, data, 0600)

	store := NewTokenStore(credPath)
	store.Load()

	if !store.IsExpired() {
		t.Error("token should be expired")
	}

	if store.CanRefresh() {
		t.Error("should not be able to refresh without refresh token")
	}

	// AccessToken should return error for expired token without refresh token
	_, err := store.AccessToken()
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestTokenStoreMissingFile(t *testing.T) {
	store := NewTokenStore("/nonexistent/path/credentials.json")
	err := store.Load()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestTokenStoreRefresh(t *testing.T) {
	// Create mock OAuth server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)

		if body["grant_type"] != "refresh_token" {
			t.Errorf("expected refresh_token grant type, got %s", body["grant_type"])
		}
		if body["refresh_token"] != "old-refresh-token" {
			t.Errorf("expected old-refresh-token, got %s", body["refresh_token"])
		}
		if body["client_id"] != OAuthClientID {
			t.Errorf("expected %s client_id, got %s", OAuthClientID, body["client_id"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	// Create temp credentials file
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	expiresAt := time.Now().Add(-1 * time.Hour).UnixMilli() // Expired
	data := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"old-access-token","refreshToken":"old-refresh-token","expiresAt":%d}}`, expiresAt))
	os.WriteFile(credPath, data, 0600)

	store := NewTokenStore(credPath)
	store.Load()

	// We use a custom HTTP client to redirect to test server
	// (can't override the const OAuthTokenURL)

	// Create custom HTTP client that redirects to test server
	customClient := &http.Client{
		Transport: &testTransport{url: server.URL},
	}

	err := store.Refresh(context.Background(), RefreshOptions{HTTPClient: customClient})
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Verify tokens were updated
	token, err := store.AccessToken()
	if err != nil {
		t.Fatalf("AccessToken after refresh failed: %v", err)
	}
	if token != "new-access-token" {
		t.Errorf("expected new-access-token, got %s", token)
	}

	if store.RefreshToken() != "new-refresh-token" {
		t.Errorf("expected new-refresh-token, got %s", store.RefreshToken())
	}

	if store.IsExpired() {
		t.Error("token should not be expired after refresh")
	}

	// Verify file was updated
	data, _ = os.ReadFile(credPath)
	var cf credentialsFile
	json.Unmarshal(data, &cf)
	if cf.ClaudeAIOAuth.AccessToken != "new-access-token" {
		t.Errorf("file not updated: expected new-access-token, got %s", cf.ClaudeAIOAuth.AccessToken)
	}
}

func TestTokenStoreRefreshError(t *testing.T) {
	// Create mock OAuth server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Refresh token expired",
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	expiresAt := time.Now().Add(-1 * time.Hour).UnixMilli()
	data := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"old-token","refreshToken":"bad-refresh","expiresAt":%d}}`, expiresAt))
	os.WriteFile(credPath, data, 0600)

	store := NewTokenStore(credPath)
	store.Load()

	customClient := &http.Client{
		Transport: &testTransport{url: server.URL},
	}

	err := store.Refresh(context.Background(), RefreshOptions{HTTPClient: customClient})
	if err == nil {
		t.Error("expected error for failed refresh")
	}
}

func TestTokenStoreAutoRefreshOnExpired(t *testing.T) {
	// Create mock OAuth server
	refreshCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalled = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "auto-refreshed-token",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Expired token with refresh token
	expiresAt := time.Now().Add(-1 * time.Hour).UnixMilli()
	data := []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"valid-refresh","expiresAt":%d}}`, expiresAt))
	os.WriteFile(credPath, data, 0600)

	store := &TokenStore{
		path: credPath,
		creds: &Credentials{
			AccessToken:  "expired-token",
			RefreshToken: "valid-refresh",
			ExpiresAt:    UnixMillis(time.Now().Add(-1 * time.Hour)),
		},
	}

	// Override refresh to use test server
	// We need to manually call refresh since AccessToken uses the real URL
	customClient := &http.Client{
		Transport: &testTransport{url: server.URL},
	}
	err := store.Refresh(context.Background(), RefreshOptions{HTTPClient: customClient})
	if err != nil {
		t.Fatalf("Auto-refresh failed: %v", err)
	}

	if !refreshCalled {
		t.Error("refresh endpoint was not called")
	}

	token, _ := store.AccessToken()
	if token != "auto-refreshed-token" {
		t.Errorf("expected auto-refreshed-token, got %s", token)
	}
}

// testTransport redirects all requests to the test server URL
type testTransport struct {
	url string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.url[7:] // strip "http://"
	return http.DefaultTransport.RoundTrip(req)
}
