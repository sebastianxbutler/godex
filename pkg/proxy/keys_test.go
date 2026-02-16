package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadKeyStoreEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, err := LoadKeyStore(path)
	if err != nil {
		t.Fatalf("LoadKeyStore error: %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
	if store.path != path {
		t.Errorf("path = %q", store.path)
	}
}

func TestLoadKeyStoreExisting(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	content := `{
		"version": 1,
		"keys": [
			{
				"id": "key_123",
				"label": "test-key",
				"hash": "sha256:abc123",
				"created_at": "2024-01-01T00:00:00Z",
				"rate": "60/m",
				"burst": 10
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	store, err := LoadKeyStore(path)
	if err != nil {
		t.Fatalf("LoadKeyStore error: %v", err)
	}

	// Verify via List
	keys := store.List()
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}
	if keys[0].ID != "key_123" {
		t.Errorf("key ID = %q", keys[0].ID)
	}
}

func TestLoadKeyStoreInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	if err := os.WriteFile(path, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadKeyStore(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestKeyStoreSave(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add a key
	_, _, err := store.Add("test-label", "60/m", 10, 1000, "", 0)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		t.Error("file is empty")
	}
}

func TestKeyStoreAdd(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	info, secret, err := store.Add("test-key", "30/m", 5, 500, "", 0)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	if info.ID == "" {
		t.Error("ID is empty")
	}
	if secret == "" {
		t.Error("secret is empty")
	}
	if !hasPrefix(secret, "gxk_") {
		t.Errorf("secret prefix wrong: %q", secret[:10])
	}
}

func TestKeyStoreAddWithProvidedKey(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	providedKey := "gxk_custom_key_12345"
	info, secret, err := store.Add("custom", "60/m", 10, 0, providedKey, 0)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	if secret != providedKey {
		t.Errorf("secret = %q, want provided key", secret)
	}
	if info.ID == "" {
		t.Error("ID is empty")
	}
}

func TestKeyStoreAddWithTTL(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	rec, _, err := store.Add("expiring", "60/m", 10, 0, "", 24*time.Hour)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Verify key has expiration via List
	keys := store.List()
	var found *KeyRecord
	for i := range keys {
		if keys[i].ID == rec.ID {
			found = &keys[i]
			break
		}
	}
	if found == nil {
		t.Fatal("key not found")
	}
	if found.ExpiresAt.IsZero() {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestKeyStoreSetTokenPolicy(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add a key first
	info, _, _ := store.Add("test", "60/m", 10, 0, "", 0)

	// Set policy
	newInfo, err := store.SetTokenPolicy(info.ID, 1000, 500, 24*time.Hour)
	if err != nil {
		t.Fatalf("SetTokenPolicy error: %v", err)
	}

	if newInfo.TokenBalance != 1000 {
		t.Errorf("TokenBalance = %d", newInfo.TokenBalance)
	}
	if newInfo.TokenAllowance != 500 {
		t.Errorf("TokenAllowance = %d", newInfo.TokenAllowance)
	}
}

func TestKeyStoreSetTokenPolicyNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	_, err := store.SetTokenPolicy("nonexistent", 100, 100, time.Hour)
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestKeyStoreAddTokens(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add a key (starts with 0 balance)
	info, _, _ := store.Add("test", "60/m", 10, 0, "", 0)

	// Add tokens
	newInfo, err := store.AddTokens(info.ID, 100)
	if err != nil {
		t.Fatalf("AddTokens error: %v", err)
	}
	if newInfo.TokenBalance != 100 {
		t.Errorf("TokenBalance = %d, want 100", newInfo.TokenBalance)
	}

	// Add more tokens
	newInfo, err = store.AddTokens(info.ID, 50)
	if err != nil {
		t.Fatalf("AddTokens error: %v", err)
	}
	if newInfo.TokenBalance != 150 {
		t.Errorf("TokenBalance = %d, want 150", newInfo.TokenBalance)
	}
}

func TestKeyStoreAddTokensNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	_, err := store.AddTokens("nonexistent", 100)
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestKeyStoreValidate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add a key
	info, secret, _ := store.Add("test", "60/m", 10, 0, "", 0)

	// Validate with correct secret
	rec, ok := store.Validate(secret)
	if !ok {
		t.Error("validation failed for valid key")
	}
	if rec.ID != info.ID {
		t.Errorf("ID = %q", rec.ID)
	}

	// Validate with wrong secret
	_, ok = store.Validate("wrong-secret")
	if ok {
		t.Error("validation should fail for wrong secret")
	}
}

func TestKeyStoreValidateRevokedKey(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add and revoke a key
	info, secret, _ := store.Add("test", "60/m", 10, 0, "", 0)
	store.Revoke(info.ID)

	// Validate should fail
	_, ok := store.Validate(secret)
	if ok {
		t.Error("validation should fail for revoked key")
	}
}

func TestKeyStoreRevoke(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	info, _, _ := store.Add("test", "60/m", 10, 0, "", 0)

	_, ok := store.Revoke(info.ID)
	if !ok {
		t.Error("revoke should return true")
	}

	// Revoke again should still work (idempotent)
	_, ok = store.Revoke(info.ID)
	if !ok {
		t.Error("revoke of already-revoked key should return true")
	}

	// Revoke nonexistent
	_, ok = store.Revoke("nonexistent")
	if ok {
		t.Error("revoke of nonexistent key should return false")
	}
}

func TestKeyStoreList(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "keys.json")

	store, _ := LoadKeyStore(path)

	// Add some keys
	store.Add("key1", "60/m", 10, 0, "", 0)
	store.Add("key2", "30/m", 5, 0, "", 0)
	store.Add("key3", "120/m", 20, 0, "", 0)

	keys := store.List()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
