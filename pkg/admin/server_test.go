package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockKeyStore implements KeyStore for testing.
type mockKeyStore struct {
	keys      map[string]KeyInfo
	addErr    error
	policyErr error
	addTokErr error
}

func newMockKeyStore() *mockKeyStore {
	return &mockKeyStore{keys: make(map[string]KeyInfo)}
}

func (m *mockKeyStore) Add(label, rate string, burst int, quota int64, providedKey string, ttl time.Duration) (KeyInfo, string, error) {
	if m.addErr != nil {
		return KeyInfo{}, "", m.addErr
	}
	id := "key_test123"
	secret := "gxk_testsecret"
	info := KeyInfo{ID: id, TokenBalance: quota, TokenAllowance: 0}
	m.keys[id] = info
	return info, secret, nil
}

func (m *mockKeyStore) SetTokenPolicy(id string, balance int64, allowance int64, duration time.Duration) (KeyInfo, error) {
	if m.policyErr != nil {
		return KeyInfo{}, m.policyErr
	}
	info, ok := m.keys[id]
	if !ok {
		return KeyInfo{}, errors.New("key not found")
	}
	info.TokenBalance = balance
	info.TokenAllowance = allowance
	info.AllowanceDurationSec = int64(duration.Seconds())
	m.keys[id] = info
	return info, nil
}

func (m *mockKeyStore) AddTokens(id string, delta int64) (KeyInfo, error) {
	if m.addTokErr != nil {
		return KeyInfo{}, m.addTokErr
	}
	info, ok := m.keys[id]
	if !ok {
		return KeyInfo{}, errors.New("key not found")
	}
	info.TokenBalance += delta
	m.keys[id] = info
	return info, nil
}

func TestNew(t *testing.T) {
	keys := newMockKeyStore()
	srv := New("/tmp/test.sock", keys)
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.socketPath != "/tmp/test.sock" {
		t.Errorf("socketPath = %q, want %q", srv.socketPath, "/tmp/test.sock")
	}
}

func TestStartWithNilKeystore(t *testing.T) {
	srv := New("/tmp/test.sock", nil)
	err := srv.Start(context.Background())
	if err == nil {
		t.Error("expected error for nil keystore")
	}
}

func TestStartWithEmptyPath(t *testing.T) {
	keys := newMockKeyStore()
	srv := New("", keys)
	err := srv.Start(context.Background())
	if err == nil {
		t.Error("expected error for empty socket path")
	}
}

func TestServerIntegration(t *testing.T) {
	// Create temp socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "admin.sock")

	keys := newMockKeyStore()
	srv := New(socketPath, keys)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for socket to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Create HTTP client with unix socket transport
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	t.Run("create_key", func(t *testing.T) {
		resp, err := client.Post("http://unix/admin/keys", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /admin/keys failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if result["key_id"] == nil {
			t.Error("missing key_id in response")
		}
		if result["api_key"] == nil {
			t.Error("missing api_key in response")
		}
	})

	t.Run("set_policy", func(t *testing.T) {
		// First create a key
		keys.keys["key_test123"] = KeyInfo{ID: "key_test123"}

		payload := `{"token_allowance": 1000, "allowance_duration": "24h"}`
		resp, err := client.Post("http://unix/admin/keys/key_test123/policy",
			"application/json", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("POST policy failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["token_allowance"] != float64(1000) {
			t.Errorf("token_allowance = %v, want 1000", result["token_allowance"])
		}
	})

	t.Run("add_tokens", func(t *testing.T) {
		keys.keys["key_test123"] = KeyInfo{ID: "key_test123", TokenBalance: 100}

		payload := `{"tokens": 500}`
		resp, err := client.Post("http://unix/admin/keys/key_test123/add-tokens",
			"application/json", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("POST add-tokens failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["token_balance"] != float64(600) {
			t.Errorf("token_balance = %v, want 600", result["token_balance"])
		}
	})

	t.Run("method_not_allowed", func(t *testing.T) {
		resp, err := client.Get("http://unix/admin/keys")
		if err != nil {
			t.Fatalf("GET /admin/keys failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		resp, err := client.Post("http://unix/admin/keys/key_test123/invalid",
			"application/json", nil)
		if err != nil {
			t.Fatalf("POST invalid action failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
		}
	})

	t.Run("key_not_found", func(t *testing.T) {
		payload := `{"tokens": 100}`
		resp, err := client.Post("http://unix/admin/keys/nonexistent/add-tokens",
			"application/json", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("POST add-tokens failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
		}
	})

	// Cancel context to stop server
	cancel()
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~/.godex/admin.sock", home + "/.godex/admin.sock"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandPath(tt.input)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
