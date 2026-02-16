package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckCodexAuth(t *testing.T) {
	// Save original home and restore after test
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tests := []struct {
		name       string
		fileContent string
		wantConfig bool
		wantError  string
	}{
		{
			name: "valid_oauth_tokens",
			fileContent: `{
				"auth_mode": "chatgpt",
				"tokens": {
					"access_token": "test-token-123"
				}
			}`,
			wantConfig: true,
		},
		{
			name: "valid_api_key",
			fileContent: `{
				"auth_mode": "api_key",
				"OPENAI_API_KEY": "sk-test-key"
			}`,
			wantConfig: true,
		},
		{
			name: "empty_tokens",
			fileContent: `{
				"auth_mode": "chatgpt",
				"tokens": {}
			}`,
			wantConfig: false,
			wantError:  "no credentials found",
		},
		{
			name: "invalid_json",
			fileContent: `{ invalid json }`,
			wantConfig: false,
			wantError:  "invalid JSON",
		},
		{
			name:       "missing_file",
			fileContent: "", // won't create file
			wantConfig: false,
			wantError:  "file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()
			os.Setenv("HOME", tmpDir)

			// Create .codex directory and auth.json if content provided
			if tt.fileContent != "" {
				codexDir := filepath.Join(tmpDir, ".codex")
				if err := os.MkdirAll(codexDir, 0755); err != nil {
					t.Fatal(err)
				}
				authPath := filepath.Join(codexDir, "auth.json")
				if err := os.WriteFile(authPath, []byte(tt.fileContent), 0644); err != nil {
					t.Fatal(err)
				}
			}

			status := checkCodexAuth()

			if status.Configured != tt.wantConfig {
				t.Errorf("Configured = %v, want %v", status.Configured, tt.wantConfig)
			}

			if tt.wantError != "" {
				if status.Error == "" {
					t.Errorf("expected error containing %q, got no error", tt.wantError)
				} else if !contains(status.Error, tt.wantError) {
					t.Errorf("error = %q, want containing %q", status.Error, tt.wantError)
				}
			}
		})
	}
}

func TestCheckAnthropicAuth(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tests := []struct {
		name        string
		fileContent string
		wantConfig  bool
		wantError   string
		wantExpiry  bool
	}{
		{
			name: "valid_oauth",
			fileContent: `{
				"claudeAiOauth": {
					"accessToken": "test-token-123",
					"expiresAt": 1897401600000
				}
			}`,
			wantConfig: true,
			wantExpiry: true,
		},
		{
			name: "valid_no_expiry",
			fileContent: `{
				"claudeAiOauth": {
					"accessToken": "test-token-123"
				}
			}`,
			wantConfig: true,
			wantExpiry: false,
		},
		{
			name: "empty_token",
			fileContent: `{
				"claudeAiOauth": {
					"accessToken": ""
				}
			}`,
			wantConfig: false,
			wantError:  "no accessToken found",
		},
		{
			name: "missing_oauth_section",
			fileContent: `{
				"otherField": "value"
			}`,
			wantConfig: false,
			wantError:  "no accessToken found",
		},
		{
			name:        "invalid_json",
			fileContent: `not json`,
			wantConfig:  false,
			wantError:   "invalid JSON",
		},
		{
			name:        "missing_file",
			fileContent: "",
			wantConfig:  false,
			wantError:   "file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			os.Setenv("HOME", tmpDir)

			if tt.fileContent != "" {
				claudeDir := filepath.Join(tmpDir, ".claude")
				if err := os.MkdirAll(claudeDir, 0755); err != nil {
					t.Fatal(err)
				}
				credsPath := filepath.Join(claudeDir, ".credentials.json")
				if err := os.WriteFile(credsPath, []byte(tt.fileContent), 0644); err != nil {
					t.Fatal(err)
				}
			}

			status := checkAnthropicAuth()

			if status.Configured != tt.wantConfig {
				t.Errorf("Configured = %v, want %v", status.Configured, tt.wantConfig)
			}

			if tt.wantError != "" {
				if status.Error == "" {
					t.Errorf("expected error containing %q, got no error", tt.wantError)
				} else if !contains(status.Error, tt.wantError) {
					t.Errorf("error = %q, want containing %q", status.Error, tt.wantError)
				}
			}

			if tt.wantExpiry && status.ExpiresAt.IsZero() {
				t.Error("expected ExpiresAt to be set")
			}
			if !tt.wantExpiry && !status.ExpiresAt.IsZero() {
				t.Errorf("expected ExpiresAt to be zero, got %v", status.ExpiresAt)
			}
		})
	}
}

func TestAuthStatus(t *testing.T) {
	status := AuthStatus{
		Backend:    "test",
		Configured: true,
		Path:       "/path/to/auth.json",
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}

	if status.Backend != "test" {
		t.Errorf("Backend = %q, want %q", status.Backend, "test")
	}
	if !status.Configured {
		t.Error("expected Configured to be true")
	}
}

func TestPromptYesNo(t *testing.T) {
	// Note: promptYesNo reads from stdin, so we can't easily test it
	// without mocking stdin. For now, just verify the function signature exists.
	// In a production codebase, we'd inject a reader.
	_ = promptYesNo // Ensure it compiles
}

func TestRunAuth(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr bool
	}{
		{[]string{}, false},           // defaults to status
		{[]string{"status"}, false},
		{[]string{"invalid"}, true},
	}

	// Save original home
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	for _, tt := range tests {
		t.Run(joinArgs(tt.args), func(t *testing.T) {
			// Use temp dir with valid auth files
			tmpDir := t.TempDir()
			os.Setenv("HOME", tmpDir)

			// Create valid auth files
			codexDir := filepath.Join(tmpDir, ".codex")
			os.MkdirAll(codexDir, 0755)
			os.WriteFile(filepath.Join(codexDir, "auth.json"), 
				[]byte(`{"auth_mode":"api_key","OPENAI_API_KEY":"test"}`), 0644)

			claudeDir := filepath.Join(tmpDir, ".claude")
			os.MkdirAll(claudeDir, 0755)
			os.WriteFile(filepath.Join(claudeDir, ".credentials.json"),
				[]byte(`{"claudeAiOauth":{"accessToken":"test"}}`), 0644)

			err := runAuth(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runAuth(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return "empty"
	}
	result := args[0]
	for _, a := range args[1:] {
		result += "_" + a
	}
	return result
}
