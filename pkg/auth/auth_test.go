package auth

import (
	"context"
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
