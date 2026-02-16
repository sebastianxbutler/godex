package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthorizationTokenAndAccountID(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	content := `{
  "auth_mode": "chatgpt",
  "tokens": {
    "access_token": "at",
    "refresh_token": "rt",
    "account_id": "acct"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.AuthorizationToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "at" {
		t.Fatalf("token mismatch: got %q", tok)
	}
	if got := store.AccountID(); got != "acct" {
		t.Fatalf("account id mismatch: got %q", got)
	}
}

func TestRefreshIsGuarded(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	content := `{
  "auth_mode": "chatgpt",
  "tokens": {
    "access_token": "at",
    "refresh_token": "rt"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refresh(context.Background(), RefreshOptions{AllowNetwork: false}); err == nil {
		t.Fatal("expected guarded refresh error")
	}
}

func TestDefaultPath(t *testing.T) {
	// Save and restore env
	origCodexHome := os.Getenv("CODEX_HOME")
	origHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("CODEX_HOME", origCodexHome)
		os.Setenv("HOME", origHome)
	}()

	// Test with CODEX_HOME set
	os.Setenv("CODEX_HOME", "/custom/codex")
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/custom/codex/auth.json" {
		t.Errorf("path = %q, want /custom/codex/auth.json", path)
	}

	// Test with HOME only
	os.Unsetenv("CODEX_HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	path, err = DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(tmpHome, ".codex", "auth.json")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/auth.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	if err := os.WriteFile(path, []byte("invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadAPIKeyMode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	content := `{
  "auth_mode": "api_key",
  "OPENAI_API_KEY": "sk-testkey123"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	tok, err := store.AuthorizationToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "sk-testkey123" {
		t.Errorf("token = %q, want %q", tok, "sk-testkey123")
	}

	if store.IsChatGPT() {
		t.Error("IsChatGPT should be false for api_key mode")
	}
}

func TestIsChatGPT(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "chatgpt_mode",
			content:  `{"auth_mode": "chatgpt", "tokens": {"access_token": "at"}}`,
			expected: true,
		},
		{
			name:     "api_key_mode",
			content:  `{"auth_mode": "api_key", "OPENAI_API_KEY": "sk-test"}`,
			expected: false,
		},
		{
			name:     "empty_mode_defaults_to_chatgpt",
			content:  `{"tokens": {"access_token": "at"}}`,
			expected: true, // empty auth_mode defaults to chatgpt
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, "auth.json")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			store, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}

			if got := store.IsChatGPT(); got != tt.expected {
				t.Errorf("IsChatGPT() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAuthorizationTokenNoToken(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	content := `{"auth_mode": "chatgpt", "tokens": {}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.AuthorizationToken()
	if err != ErrNoToken {
		t.Errorf("expected ErrNoToken, got %v", err)
	}
}

func TestFileStruct(t *testing.T) {
	// Test JSON marshaling/unmarshaling
	file := File{
		AuthMode: ModeChatGPT,
		APIKey:   "",
		Tokens: Tokens{
			AccessToken:  "access",
			RefreshToken: "refresh",
			AccountID:    "account",
		},
	}

	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}

	var decoded File
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.AuthMode != ModeChatGPT {
		t.Errorf("AuthMode = %q, want %q", decoded.AuthMode, ModeChatGPT)
	}
	if decoded.Tokens.AccessToken != "access" {
		t.Errorf("AccessToken = %q, want %q", decoded.Tokens.AccessToken, "access")
	}
}

func TestIDTokenUnmarshalString(t *testing.T) {
	// Test ID token as plain string
	content := `{
  "auth_mode": "chatgpt",
  "tokens": {
    "access_token": "at",
    "id_token": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test"
  }
}`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if store.File.Tokens.IDToken.RawJWT == "" {
		t.Error("expected RawJWT to be set")
	}
}

func TestIDTokenUnmarshalObject(t *testing.T) {
	// Test ID token as object
	content := `{
  "auth_mode": "chatgpt",
  "tokens": {
    "access_token": "at",
    "id_token": {
      "raw_jwt": "eyJhbGciOiJIUzI1NiJ9.test",
      "chatgpt_account_id": "acct_123"
    }
  }
}`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if store.File.Tokens.IDToken.RawJWT == "" {
		t.Error("expected RawJWT to be set")
	}
	if store.File.Tokens.IDToken.ChatGPTAccountID != "acct_123" {
		t.Errorf("ChatGPTAccountID = %q, want %q", store.File.Tokens.IDToken.ChatGPTAccountID, "acct_123")
	}
}

func TestSetRefreshConfig(t *testing.T) {
	// Save original values
	origURL := refreshURL
	origClientID := refreshClientID
	origScope := refreshScope
	defer func() {
		refreshURL = origURL
		refreshClientID = origClientID
		refreshScope = origScope
	}()

	SetRefreshConfig("https://custom.url", "custom_id", "custom scope")

	if refreshURL != "https://custom.url" {
		t.Errorf("refreshURL = %q", refreshURL)
	}
	if refreshClientID != "custom_id" {
		t.Errorf("refreshClientID = %q", refreshClientID)
	}
	if refreshScope != "custom scope" {
		t.Errorf("refreshScope = %q", refreshScope)
	}
}

func TestSetRefreshConfigEmpty(t *testing.T) {
	// Save original values
	origURL := refreshURL
	defer func() { refreshURL = origURL }()

	// Empty values should not change
	current := refreshURL
	SetRefreshConfig("", "", "")

	if refreshURL != current {
		t.Error("empty string should not change refreshURL")
	}
}
