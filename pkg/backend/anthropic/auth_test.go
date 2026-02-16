package anthropic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStore(t *testing.T) {
	// Create a temp credentials file
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Write test credentials
	creds := credentialsFile{
		ClaudeAIOAuth: &Credentials{
			AccessToken:      "test-token-123",
			RefreshToken:     "refresh-456",
			ExpiresAt:        time.Now().Add(1 * time.Hour),
			SubscriptionType: "max",
		},
	}
	data, _ := json.Marshal(creds)
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
}

func TestTokenStoreExpired(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Write expired credentials
	creds := credentialsFile{
		ClaudeAIOAuth: &Credentials{
			AccessToken:  "expired-token",
			RefreshToken: "refresh-456",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // expired
		},
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credPath, data, 0600)

	store := NewTokenStore(credPath)
	store.Load()

	if !store.IsExpired() {
		t.Error("token should be expired")
	}

	// AccessToken should return error for expired token
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
