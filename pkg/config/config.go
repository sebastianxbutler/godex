package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Exec   ExecConfig   `yaml:"exec"`
	Client ClientConfig `yaml:"client"`
	Auth   AuthConfig   `yaml:"auth"`
	Proxy  ProxyConfig  `yaml:"proxy"`
}

type ExecConfig struct {
	Model            string        `yaml:"model"`
	Instructions     string        `yaml:"instructions"`
	AppendSystem     string        `yaml:"append_system_prompt"`
	ToolChoice       string        `yaml:"tool_choice"`
	Timeout          time.Duration `yaml:"timeout"`
	AllowRefresh     bool          `yaml:"allow_refresh"`
	AutoToolsEnabled bool          `yaml:"auto_tools"`
	AutoToolsMax     int           `yaml:"auto_tools_max_steps"`
	MockEnabled      bool          `yaml:"mock"`
	MockMode         string        `yaml:"mock_mode"`
	WebSearch        bool          `yaml:"web_search"`
}

type ClientConfig struct {
	BaseURL    string        `yaml:"base_url"`
	Originator string        `yaml:"originator"`
	UserAgent  string        `yaml:"user_agent"`
	RetryMax   int           `yaml:"retry_max"`
	RetryDelay time.Duration `yaml:"retry_delay"`
}

type AuthConfig struct {
	Path       string `yaml:"path"`
	RefreshURL string `yaml:"refresh_url"`
	ClientID   string `yaml:"client_id"`
	Scope      string `yaml:"scope"`
}

type ModelConfig struct {
	ID      string `yaml:"id"`
	BaseURL string `yaml:"base_url"`
}

type ProxyConfig struct {
	Listen            string         `yaml:"listen"`
	APIKey            string         `yaml:"api_key"`
	AllowAnyKey       bool           `yaml:"allow_any_key"`
	AllowRefresh      bool           `yaml:"allow_refresh"`
	Model             string         `yaml:"model"`
	Models            []ModelConfig  `yaml:"models"`
	BaseURL           string         `yaml:"base_url"`
	Originator        string         `yaml:"originator"`
	UserAgent         string         `yaml:"user_agent"`
	AuthPath          string         `yaml:"auth_path"`
	CacheTTL          time.Duration  `yaml:"cache_ttl"`
	LogLevel          string         `yaml:"log_level"`
	LogRequests       bool           `yaml:"log_requests"`
	KeysPath          string         `yaml:"keys_path"`
	DefaultRate       string         `yaml:"default_rate"`
	DefaultBurst      int            `yaml:"default_burst"`
	DefaultQuota      int64          `yaml:"default_quota_tokens"`
	StatsPath         string         `yaml:"stats_path"`
	StatsSummary      string         `yaml:"stats_summary"`
	StatsMaxBytes     int64          `yaml:"stats_max_bytes"`
	StatsBackups      int            `yaml:"stats_max_backups"`
	EventsPath        string         `yaml:"events_path"`
	EventsMax         int64          `yaml:"events_max_bytes"`
	EventsBackups     int            `yaml:"events_max_backups"`
	AuditPath         string         `yaml:"audit_path"`
	AuditMaxBytes     int64          `yaml:"audit_max_bytes"`
	AuditBackups      int            `yaml:"audit_max_backups"`
	TracePath         string         `yaml:"trace_path"`
	TraceMaxBytes     int64          `yaml:"trace_max_bytes"`
	TraceBackups      int            `yaml:"trace_max_backups"`
	UpstreamAuditPath string         `yaml:"upstream_audit_path"`
	MeterWindow       time.Duration  `yaml:"meter_window"`
	AdminSocket       string         `yaml:"admin_socket"`
	Payments          PaymentsConfig `yaml:"payments"`
	Backends          BackendsConfig `yaml:"backends"`
	Metrics           MetricsConfig  `yaml:"metrics"`
}

// MetricsConfig configures per-backend metrics collection.
type MetricsConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"`         // persist metrics to file
	LogRequests bool   `yaml:"log_requests"` // log individual requests
}

type PaymentsConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`
	TokenMeterURL string `yaml:"token_meter_url"`
}

// BackendsConfig configures available LLM backends.
type BackendsConfig struct {
	Codex     CodexBackendConfig             `yaml:"codex"`
	Anthropic AnthropicBackendConfig         `yaml:"anthropic"`
	Custom    map[string]CustomBackendConfig `yaml:"custom"`
	Routing   RoutingConfig                  `yaml:"routing"`
}

// CustomBackendConfig configures a user-defined OpenAI-compatible backend.
type CustomBackendConfig struct {
	Type      string            `yaml:"type"`    // "openai"
	Enabled   *bool             `yaml:"enabled"` // default true
	BaseURL   string            `yaml:"base_url"`
	Auth      BackendAuthConfig `yaml:"auth"`
	Timeout   time.Duration     `yaml:"timeout"`
	Discovery *bool             `yaml:"discovery"` // auto-probe /v1/models
	Models    []BackendModelDef `yaml:"models"`    // hard-coded models
}

// IsEnabled returns true if the backend is enabled (default true).
func (c CustomBackendConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// HasDiscovery returns true if model discovery is enabled.
func (c CustomBackendConfig) HasDiscovery() bool {
	if c.Discovery == nil {
		return true // default to discovery if no models specified
	}
	return *c.Discovery
}

// BackendAuthConfig configures authentication for a custom backend.
type BackendAuthConfig struct {
	Type    string            `yaml:"type"`    // "api_key", "bearer", "header", "none"
	Key     string            `yaml:"key"`     // literal key
	KeyEnv  string            `yaml:"key_env"` // env var name for key
	Headers map[string]string `yaml:"headers"` // custom headers (for type: header)
}

// BackendModelDef defines a model for hard-coded model lists.
type BackendModelDef struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"display_name"`
}

// CodexBackendConfig configures the Codex/ChatGPT backend.
type CodexBackendConfig struct {
	Enabled         bool   `yaml:"enabled"`
	BaseURL         string `yaml:"base_url"`
	CredentialsPath string `yaml:"credentials_path"`
	// NativeTools forces Codex's built-in tools (shell, apply_patch, update_plan)
	// even when the caller provides their own tools. Default false (proxy mode
	// uses caller's tools).
	NativeTools bool `yaml:"native_tools"`
}

// AnthropicBackendConfig configures the Anthropic backend.
type AnthropicBackendConfig struct {
	Enabled          bool   `yaml:"enabled"`
	CredentialsPath  string `yaml:"credentials_path"`
	DefaultMaxTokens int    `yaml:"default_max_tokens"`
}

// RoutingConfig configures model-to-backend routing.
type RoutingConfig struct {
	Patterns map[string][]string `yaml:"patterns"`
	Aliases  map[string]string   `yaml:"aliases"`
}

func DefaultConfig() Config {
	return Config{
		Exec: ExecConfig{
			Model:            "gpt-5.2-codex",
			Instructions:     "You are a helpful assistant.",
			ToolChoice:       "auto",
			Timeout:          90 * time.Second,
			AllowRefresh:     false,
			AutoToolsEnabled: false,
			AutoToolsMax:     4,
			MockEnabled:      false,
			MockMode:         "echo",
			WebSearch:        false,
		},
		Client: ClientConfig{
			BaseURL:    "https://chatgpt.com/backend-api/codex",
			Originator: "codex_cli_rs",
			UserAgent:  "codex_cli_rs/0.0",
			RetryMax:   1,
			RetryDelay: 300 * time.Millisecond,
		},
		Auth: AuthConfig{
			Path:       "",
			RefreshURL: "https://auth.openai.com/oauth/token",
			ClientID:   "app_EMoamEEZ73f0CkXaXp7hrann",
			Scope:      "openid profile email",
		},
		Proxy: ProxyConfig{
			Listen:            "127.0.0.1:39001",
			APIKey:            "",
			AllowAnyKey:       false,
			AllowRefresh:      false,
			Model:             "gpt-5.2-codex",
			BaseURL:           "https://chatgpt.com/backend-api/codex",
			Originator:        "codex_cli_rs",
			UserAgent:         "godex/0.0",
			AuthPath:          "",
			CacheTTL:          6 * time.Hour,
			LogLevel:          "info",
			LogRequests:       false,
			KeysPath:          "",
			DefaultRate:       "60/m",
			DefaultBurst:      10,
			DefaultQuota:      0,
			StatsPath:         "",
			StatsSummary:      "",
			StatsMaxBytes:     10 * 1024 * 1024,
			StatsBackups:      3,
			EventsPath:        "",
			EventsMax:         1024 * 1024,
			EventsBackups:     3,
			AuditPath:         "",
			AuditMaxBytes:     10 * 1024 * 1024,
			AuditBackups:      3,
			TracePath:         "",
			TraceMaxBytes:     25 * 1024 * 1024,
			TraceBackups:      5,
			UpstreamAuditPath: "",
			MeterWindow:       0,
			AdminSocket:       "~/.godex/admin.sock",
			Payments: PaymentsConfig{
				Enabled:       false,
				Provider:      "l402",
				TokenMeterURL: "",
			},
			Backends: BackendsConfig{
				Codex: CodexBackendConfig{
					Enabled:         true,
					BaseURL:         "https://chatgpt.com/backend-api/codex",
					CredentialsPath: "",
				},
				Anthropic: AnthropicBackendConfig{
					Enabled:          false,
					CredentialsPath:  "",
					DefaultMaxTokens: 4096,
				},
				Routing: RoutingConfig{
					Patterns: map[string][]string{},
					Aliases:  map[string]string{},
				},
			},
		},
	}
}

func DefaultPath() string {
	if v := strings.TrimSpace(os.Getenv("GODEX_CONFIG")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "godex", "config.yaml")
}

func Load() Config {
	return LoadFrom(DefaultPath())
}

func LoadFrom(path string) Config {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) != "" {
		if buf, err := os.ReadFile(path); err == nil {
			_ = yaml.Unmarshal(buf, &cfg)
		}
	}
	ApplyEnv(&cfg)
	return cfg
}

func ApplyEnv(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_MODEL")); v != "" {
		cfg.Exec.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_INSTRUCTIONS")); v != "" {
		cfg.Exec.Instructions = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_APPEND_SYSTEM_PROMPT")); v != "" {
		cfg.Exec.AppendSystem = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_TOOL_CHOICE")); v != "" {
		cfg.Exec.ToolChoice = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Exec.Timeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_ALLOW_REFRESH")); v != "" {
		cfg.Exec.AllowRefresh = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_AUTO_TOOLS_MAX_STEPS")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Exec.AutoToolsMax = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_EXEC_MOCK_MODE")); v != "" {
		cfg.Exec.MockMode = v
	}

	if v := strings.TrimSpace(os.Getenv("GODEX_BASE_URL")); v != "" {
		cfg.Client.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_ORIGINATOR")); v != "" {
		cfg.Client.Originator = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_USER_AGENT")); v != "" {
		cfg.Client.UserAgent = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_RETRY_MAX")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Client.RetryMax = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_RETRY_DELAY")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Client.RetryDelay = d
		}
	}

	if v := strings.TrimSpace(os.Getenv("GODEX_AUTH_PATH")); v != "" {
		cfg.Auth.Path = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_AUTH_REFRESH_URL")); v != "" {
		cfg.Auth.RefreshURL = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_AUTH_CLIENT_ID")); v != "" {
		cfg.Auth.ClientID = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_AUTH_SCOPE")); v != "" {
		cfg.Auth.Scope = v
	}

	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_LISTEN")); v != "" {
		cfg.Proxy.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_API_KEY")); v != "" {
		cfg.Proxy.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_ALLOW_ANY_KEY")); v != "" {
		cfg.Proxy.AllowAnyKey = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_ALLOW_REFRESH")); v != "" {
		cfg.Proxy.AllowRefresh = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_MODEL")); v != "" {
		cfg.Proxy.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_BASE_URL")); v != "" {
		cfg.Proxy.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_ORIGINATOR")); v != "" {
		cfg.Proxy.Originator = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_USER_AGENT")); v != "" {
		cfg.Proxy.UserAgent = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_AUTH_PATH")); v != "" {
		cfg.Proxy.AuthPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_CACHE_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Proxy.CacheTTL = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_LOG_LEVEL")); v != "" {
		cfg.Proxy.LogLevel = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_LOG_REQUESTS")); v != "" {
		cfg.Proxy.LogRequests = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_KEYS_PATH")); v != "" {
		cfg.Proxy.KeysPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_RATE")); v != "" {
		cfg.Proxy.DefaultRate = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_BURST")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Proxy.DefaultBurst = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_QUOTA_TOKENS")); v != "" {
		if n, err := parseInt64(v); err == nil {
			cfg.Proxy.DefaultQuota = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_STATS_PATH")); v != "" {
		cfg.Proxy.StatsPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_STATS_SUMMARY")); v != "" {
		cfg.Proxy.StatsSummary = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_STATS_MAX_BYTES")); v != "" {
		if n, err := parseInt64(v); err == nil {
			cfg.Proxy.StatsMaxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_STATS_MAX_BACKUPS")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Proxy.StatsBackups = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_EVENTS_PATH")); v != "" {
		cfg.Proxy.EventsPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_EVENTS_MAX_BYTES")); v != "" {
		if n, err := parseInt64(v); err == nil {
			cfg.Proxy.EventsMax = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_EVENTS_MAX_BACKUPS")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Proxy.EventsBackups = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_AUDIT_PATH")); v != "" {
		cfg.Proxy.AuditPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_AUDIT_MAX_BYTES")); v != "" {
		if n, err := parseInt64(v); err == nil {
			cfg.Proxy.AuditMaxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_AUDIT_MAX_BACKUPS")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Proxy.AuditBackups = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_TRACE_PATH")); v != "" {
		cfg.Proxy.TracePath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_TRACE_MAX_BYTES")); v != "" {
		if n, err := parseInt64(v); err == nil {
			cfg.Proxy.TraceMaxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_TRACE_MAX_BACKUPS")); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Proxy.TraceBackups = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_UPSTREAM_AUDIT_PATH")); v != "" {
		cfg.Proxy.UpstreamAuditPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_METER_WINDOW")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Proxy.MeterWindow = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_ADMIN_SOCKET")); v != "" {
		cfg.Proxy.AdminSocket = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PAYMENTS_ENABLED")); v != "" {
		cfg.Proxy.Payments.Enabled = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PAYMENTS_PROVIDER")); v != "" {
		cfg.Proxy.Payments.Provider = v
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_TOKEN_METER_URL")); v != "" {
		cfg.Proxy.Payments.TokenMeterURL = v
	}
}

func parseInt(val string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(val))
}

func parseInt64(val string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(val), 10, 64)
}

func parseBool(val string) bool {
	val = strings.TrimSpace(strings.ToLower(val))
	return val == "1" || val == "true" || val == "yes"
}
