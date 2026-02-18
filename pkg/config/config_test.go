package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Check exec defaults
	if cfg.Exec.Model != "gpt-5.2-codex" {
		t.Errorf("Exec.Model = %q, want %q", cfg.Exec.Model, "gpt-5.2-codex")
	}
	if cfg.Exec.Timeout != 90*time.Second {
		t.Errorf("Exec.Timeout = %v, want %v", cfg.Exec.Timeout, 90*time.Second)
	}
	if cfg.Exec.ToolChoice != "auto" {
		t.Errorf("Exec.ToolChoice = %q, want %q", cfg.Exec.ToolChoice, "auto")
	}

	// Check client defaults
	if cfg.Client.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("Client.BaseURL = %q, want codex URL", cfg.Client.BaseURL)
	}
	if cfg.Client.RetryMax != 1 {
		t.Errorf("Client.RetryMax = %d, want 1", cfg.Client.RetryMax)
	}

	// Check proxy defaults
	if cfg.Proxy.Listen != "127.0.0.1:39001" {
		t.Errorf("Proxy.Listen = %q, want %q", cfg.Proxy.Listen, "127.0.0.1:39001")
	}
	if cfg.Proxy.DefaultRate != "60/m" {
		t.Errorf("Proxy.DefaultRate = %q, want %q", cfg.Proxy.DefaultRate, "60/m")
	}

	// Check backends defaults
	if !cfg.Proxy.Backends.Codex.Enabled {
		t.Error("Codex backend should be enabled by default")
	}
	if cfg.Proxy.Backends.Anthropic.Enabled {
		t.Error("Anthropic backend should be disabled by default")
	}
	if cfg.Proxy.Backends.Routing.Default != "codex" {
		t.Errorf("Routing.Default = %q, want %q", cfg.Proxy.Backends.Routing.Default, "codex")
	}
}

func TestDefaultPath(t *testing.T) {
	// Save and restore env
	origEnv := os.Getenv("GODEX_CONFIG")
	origHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("GODEX_CONFIG", origEnv)
		os.Setenv("HOME", origHome)
	}()

	// Test with env var set
	os.Setenv("GODEX_CONFIG", "/custom/path/config.yaml")
	if got := DefaultPath(); got != "/custom/path/config.yaml" {
		t.Errorf("DefaultPath() with GODEX_CONFIG = %q, want %q", got, "/custom/path/config.yaml")
	}

	// Test without env var
	os.Unsetenv("GODEX_CONFIG")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	expected := filepath.Join(tmpHome, ".config", "godex", "config.yaml")
	if got := DefaultPath(); got != expected {
		t.Errorf("DefaultPath() = %q, want %q", got, expected)
	}
}

func TestLoadFrom(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `
exec:
  model: custom-model
  timeout: 120s
proxy:
  listen: "0.0.0.0:8080"
  backends:
    anthropic:
      enabled: true
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadFrom(configPath)

	// Check custom values loaded
	if cfg.Exec.Model != "custom-model" {
		t.Errorf("Exec.Model = %q, want %q", cfg.Exec.Model, "custom-model")
	}
	if cfg.Exec.Timeout != 120*time.Second {
		t.Errorf("Exec.Timeout = %v, want %v", cfg.Exec.Timeout, 120*time.Second)
	}
	if cfg.Proxy.Listen != "0.0.0.0:8080" {
		t.Errorf("Proxy.Listen = %q, want %q", cfg.Proxy.Listen, "0.0.0.0:8080")
	}
	if !cfg.Proxy.Backends.Anthropic.Enabled {
		t.Error("Anthropic should be enabled from config")
	}

	// Check defaults preserved for unset values
	if cfg.Client.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("Client.BaseURL should be default, got %q", cfg.Client.BaseURL)
	}
}

func TestLoadFromMissing(t *testing.T) {
	// Load from non-existent file should return defaults
	cfg := LoadFrom("/nonexistent/path/config.yaml")

	if cfg.Exec.Model != "gpt-5.2-codex" {
		t.Errorf("should return defaults for missing file, got Exec.Model = %q", cfg.Exec.Model)
	}
}

func TestLoadFromEmpty(t *testing.T) {
	// Load from empty path should return defaults
	cfg := LoadFrom("")

	if cfg.Exec.Model != "gpt-5.2-codex" {
		t.Errorf("should return defaults for empty path, got Exec.Model = %q", cfg.Exec.Model)
	}
}

func TestApplyEnv(t *testing.T) {
	// Save original env
	envVars := []string{
		"GODEX_EXEC_MODEL",
		"GODEX_EXEC_TIMEOUT",
		"GODEX_PROXY_LISTEN",
		"GODEX_PROXY_API_KEY",
		"GODEX_PROXY_ALLOW_ANY_KEY",
		"GODEX_PROXY_LOG_REQUESTS",
	}
	origValues := make(map[string]string)
	for _, v := range envVars {
		origValues[v] = os.Getenv(v)
	}
	defer func() {
		for k, v := range origValues {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	// Set test env vars
	os.Setenv("GODEX_EXEC_MODEL", "env-model")
	os.Setenv("GODEX_EXEC_TIMEOUT", "30s")
	os.Setenv("GODEX_PROXY_LISTEN", "localhost:9000")
	os.Setenv("GODEX_PROXY_API_KEY", "test-key")
	os.Setenv("GODEX_PROXY_ALLOW_ANY_KEY", "true")
	os.Setenv("GODEX_PROXY_LOG_REQUESTS", "1")

	cfg := DefaultConfig()
	ApplyEnv(&cfg)

	if cfg.Exec.Model != "env-model" {
		t.Errorf("Exec.Model = %q, want %q", cfg.Exec.Model, "env-model")
	}
	if cfg.Exec.Timeout != 30*time.Second {
		t.Errorf("Exec.Timeout = %v, want %v", cfg.Exec.Timeout, 30*time.Second)
	}
	if cfg.Proxy.Listen != "localhost:9000" {
		t.Errorf("Proxy.Listen = %q, want %q", cfg.Proxy.Listen, "localhost:9000")
	}
	if cfg.Proxy.APIKey != "test-key" {
		t.Errorf("Proxy.APIKey = %q, want %q", cfg.Proxy.APIKey, "test-key")
	}
	if !cfg.Proxy.AllowAnyKey {
		t.Error("Proxy.AllowAnyKey should be true")
	}
	if !cfg.Proxy.LogRequests {
		t.Error("Proxy.LogRequests should be true")
	}
}

func TestApplyEnvInvalidDuration(t *testing.T) {
	origTimeout := os.Getenv("GODEX_EXEC_TIMEOUT")
	defer os.Setenv("GODEX_EXEC_TIMEOUT", origTimeout)

	os.Setenv("GODEX_EXEC_TIMEOUT", "invalid")

	cfg := DefaultConfig()
	ApplyEnv(&cfg)

	// Should keep default value
	if cfg.Exec.Timeout != 90*time.Second {
		t.Errorf("Exec.Timeout = %v, want default %v", cfg.Exec.Timeout, 90*time.Second)
	}
}

func TestRoutingConfig(t *testing.T) {
	cfg := DefaultConfig()

	patterns := cfg.Proxy.Backends.Routing.Patterns
	if len(patterns) != 0 {
		t.Errorf("expected no default patterns, got %v", patterns)
	}

	aliases := cfg.Proxy.Backends.Routing.Aliases
	if len(aliases) != 0 {
		t.Errorf("expected no default aliases, got %v", aliases)
	}
}

func TestConfigYAMLRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write a complex config
	configYAML := `
exec:
  model: test-model
  instructions: "Custom instructions"
  timeout: 60s
  auto_tools: true
  auto_tools_max_steps: 10
client:
  base_url: https://custom.api/v1
  retry_max: 3
proxy:
  listen: "127.0.0.1:8888"
  allow_any_key: true
  log_level: debug
  backends:
    codex:
      enabled: true
    anthropic:
      enabled: true
      default_max_tokens: 8192
    routing:
      default: anthropic
      aliases:
        custom: custom-model-id
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadFrom(configPath)

	// Verify all fields loaded correctly
	if cfg.Exec.Model != "test-model" {
		t.Errorf("Exec.Model = %q", cfg.Exec.Model)
	}
	if cfg.Exec.Instructions != "Custom instructions" {
		t.Errorf("Exec.Instructions = %q", cfg.Exec.Instructions)
	}
	if !cfg.Exec.AutoToolsEnabled {
		t.Error("Exec.AutoToolsEnabled should be true")
	}
	if cfg.Exec.AutoToolsMax != 10 {
		t.Errorf("Exec.AutoToolsMax = %d", cfg.Exec.AutoToolsMax)
	}
	if cfg.Client.BaseURL != "https://custom.api/v1" {
		t.Errorf("Client.BaseURL = %q", cfg.Client.BaseURL)
	}
	if cfg.Client.RetryMax != 3 {
		t.Errorf("Client.RetryMax = %d", cfg.Client.RetryMax)
	}
	if !cfg.Proxy.AllowAnyKey {
		t.Error("Proxy.AllowAnyKey should be true")
	}
	if cfg.Proxy.LogLevel != "debug" {
		t.Errorf("Proxy.LogLevel = %q", cfg.Proxy.LogLevel)
	}
	if !cfg.Proxy.Backends.Anthropic.Enabled {
		t.Error("Anthropic should be enabled")
	}
	if cfg.Proxy.Backends.Anthropic.DefaultMaxTokens != 8192 {
		t.Errorf("Anthropic.DefaultMaxTokens = %d", cfg.Proxy.Backends.Anthropic.DefaultMaxTokens)
	}
	if cfg.Proxy.Backends.Routing.Default != "anthropic" {
		t.Errorf("Routing.Default = %q", cfg.Proxy.Backends.Routing.Default)
	}
	if cfg.Proxy.Backends.Routing.Aliases["custom"] != "custom-model-id" {
		t.Errorf("custom alias = %q", cfg.Proxy.Backends.Routing.Aliases["custom"])
	}
}
