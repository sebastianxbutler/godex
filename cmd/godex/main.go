package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"godex/pkg/aliases"
	"godex/pkg/auth"
	"godex/pkg/config"
	"godex/pkg/harness"
	harnessClaudeP "godex/pkg/harness/claude"
	harnessCodexP "godex/pkg/harness/codex"
	harnessOpenaiP "godex/pkg/harness/openai"
	"godex/pkg/payments"
	"godex/pkg/protocol"
	"godex/pkg/proxy"
	"godex/pkg/router"
	"godex/pkg/sse"
)

type toolFlags []string

type outputFlags []string

func (t *toolFlags) String() string { return strings.Join(*t, ",") }
func (t *toolFlags) Set(v string) error {
	*t = append(*t, v)
	return nil
}

func (o *outputFlags) String() string { return strings.Join(*o, ",") }
func (o *outputFlags) Set(v string) error {
	*o = append(*o, v)
	return nil
}

var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "--version", "version", "-v":
		fmt.Println(Version)
		return
	case "exec":
		if err := runExec(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "proxy":
		if err := runProxy(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "probe":
		if err := runProbe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "auth":
		if err := runAuth(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "aliases":
		if err := runAliases(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func runExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := config.LoadFrom(configPathFromArgs(args))

	var prompt string
	var model string
	var instructions string
	var instructionsAlt string
	var appendSystemPrompt string
	var trace bool
	var jsonOnly bool
	var allowRefresh bool
	var autoTools bool
	var webSearch bool
	var toolChoice string
	var inputJSON string
	var mock bool
	var mockMode string
	var nativeTools bool
	var tools toolFlags
	var outputs outputFlags
	var sessionID string
	var images toolFlags
	var logRequests string
	var logResponses string
	var providerKey string

	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	fs.StringVar(&prompt, "prompt", "", "User prompt")
	fs.StringVar(&model, "model", cfg.Exec.Model, "Model name")
	fs.StringVar(&instructions, "instructions", cfg.Exec.Instructions, "Optional system instructions")
	fs.StringVar(&instructionsAlt, "system", "", "Alias for --instructions")
	fs.StringVar(&appendSystemPrompt, "append-system-prompt", cfg.Exec.AppendSystem, "Append to system instructions")
	fs.BoolVar(&trace, "trace", false, "Print raw SSE event JSON")
	fs.BoolVar(&jsonOnly, "json", false, "Emit JSON events only (no text output)")
	fs.BoolVar(&allowRefresh, "allow-refresh", cfg.Exec.AllowRefresh, "Allow network token refresh on 401")
	fs.BoolVar(&autoTools, "auto-tools", cfg.Exec.AutoToolsEnabled, "Automatically run tool loop with static outputs")
	fs.BoolVar(&webSearch, "web-search", cfg.Exec.WebSearch, "Enable web_search tool")
	fs.StringVar(&toolChoice, "tool-choice", cfg.Exec.ToolChoice, "Tool choice: auto|required|function:<name>")
	fs.StringVar(&inputJSON, "input-json", "", "JSON array of response input items (overrides --prompt)")
	fs.BoolVar(&mock, "mock", cfg.Exec.MockEnabled, "Mock mode: no network, emit synthetic stream")
	fs.StringVar(&mockMode, "mock-mode", cfg.Exec.MockMode, "Mock mode: echo|text|tool-call|tool-loop")
	fs.Var(&tools, "tool", "Tool spec (repeatable): web_search or name:json=/path/schema.json")
	fs.Var(&outputs, "tool-output", "Static tool output: name=value or name=$args (repeatable)")
	fs.StringVar(&sessionID, "session-id", "", "Optional session id (reuses prompt cache key)")
	fs.Var(&images, "image", "Image path (ignored; accepted for OpenClaw CLI compatibility)")
	fs.StringVar(&logRequests, "log-requests", "", "Write JSON request payload to file")
	fs.StringVar(&logResponses, "log-responses", "", "Append JSONL response events to file")
	fs.StringVar(&providerKey, "provider-key", "", "API key for non-Codex backends (or set via env per provider)")
	fs.BoolVar(&nativeTools, "native-tools", false, "Use Codex native tools (shell, apply_patch, update_plan) instead of proxy mode")

	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = configPath
	if strings.TrimSpace(prompt) == "" && strings.TrimSpace(inputJSON) == "" {
		return errors.New("--prompt is required unless --input-json is provided")
	}

	if cfg.Auth.RefreshURL != "" || cfg.Auth.ClientID != "" || cfg.Auth.Scope != "" {
		auth.SetRefreshConfig(cfg.Auth.RefreshURL, cfg.Auth.ClientID, cfg.Auth.Scope)
	}
	authPath := cfg.Auth.Path
	if strings.TrimSpace(authPath) == "" {
		var err error
		authPath, err = auth.DefaultPath()
		if err != nil {
			return err
		}
	}
	store, err := auth.Load(authPath)
	if err != nil {
		return err
	}

	if strings.TrimSpace(sessionID) == "" {
		sessionID, err = newSessionID()
		if err != nil {
			return err
		}
	}

	toolSpecs, err := parseToolSpecs(tools)
	if err != nil {
		return err
	}
	if webSearch {
		toolSpecs = append(toolSpecs, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
	}

	if strings.TrimSpace(instructions) == "" && strings.TrimSpace(instructionsAlt) != "" {
		instructions = instructionsAlt
	}
	if strings.TrimSpace(instructions) == "" {
		instructions = "You are a helpful assistant."
	}
	if strings.TrimSpace(appendSystemPrompt) != "" {
		instructions = strings.TrimSpace(instructions) + "\n\n" + strings.TrimSpace(appendSystemPrompt)
	}

	inputItems := []protocol.ResponseInputItem{protocol.UserMessage(prompt)}
	if strings.TrimSpace(inputJSON) != "" {
		buf, err := os.ReadFile(inputJSON)
		if err != nil {
			return fmt.Errorf("read input json: %w", err)
		}
		if err := json.Unmarshal(buf, &inputItems); err != nil {
			return fmt.Errorf("parse input json: %w", err)
		}
	}

	// Build the harness Turn from exec args
	turn := &harness.Turn{
		Model:        model,
		Instructions: instructions,
	}
	// Convert input items to harness messages
	for _, item := range inputItems {
		switch item.Type {
		case "message":
			text := ""
			for _, part := range item.Content {
				text += part.Text
			}
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    item.Role,
				Content: text,
			})
		case "function_call":
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    "assistant",
				Content: item.Arguments,
				Name:    item.Name,
				ToolID:  item.CallID,
			})
		case "function_call_output":
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    "tool",
				Content: item.Output,
				ToolID:  item.CallID,
			})
		}
	}
	// Convert tool specs to harness format
	for _, t := range toolSpecs {
		if t.Type == "function" {
			var params map[string]any
			if t.Parameters != nil {
				_ = json.Unmarshal(t.Parameters, &params)
			}
			turn.Tools = append(turn.Tools, harness.ToolSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			})
		}
	}

	// Build protocol request for mock/logging
	req := protocol.ResponsesRequest{
		Model:             model,
		Instructions:      instructions,
		Input:             inputItems,
		Tools:             toolSpecs,
		ToolChoice:        normalizeToolChoice(toolChoice),
		ParallelToolCalls: false,
		Store:             false,
		Stream:            true,
		Include:           []string{},
		PromptCacheKey:    sessionID,
	}

	if logRequests != "" {
		if payload, err := json.MarshalIndent(req, "", "  "); err == nil {
			_ = os.WriteFile(logRequests, payload, 0o600)
		}
	}

	if mock {
		return emitMockStream(req, jsonOnly, logResponses, mockMode)
	}

	// Build the Codex harness
	baseURL := cfg.Client.BaseURL
	if baseURL == "" {
		baseURL = "https://chatgpt.com/backend-api/codex"
	}
	codexClient := harnessCodexP.NewClient(nil, store, harnessCodexP.ClientConfig{
		SessionID:    sessionID,
		AllowRefresh: allowRefresh,
		BaseURL:      baseURL,
		Originator:   cfg.Client.Originator,
		UserAgent:    cfg.Client.UserAgent,
		RetryMax:     cfg.Client.RetryMax,
		RetryDelay:   cfg.Client.RetryDelay,
	})
	h := harnessCodexP.New(harnessCodexP.Config{
		Client:      codexClient,
		NativeTools: nativeTools,
	})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Exec.Timeout)
	defer cancel()

	// Inject provider key into context if provided
	if providerKey != "" {
		ctx = harness.WithProviderKey(ctx, providerKey)
	}

	if autoTools {
		outputs, err := parseToolOutputs(outputs)
		if err != nil {
			return err
		}
		handler := execToolHandler{outputs: outputs}
		result, err := h.RunToolLoop(ctx, turn, handler, harness.LoopOptions{MaxTurns: cfg.Exec.AutoToolsMax})
		if err != nil {
			return err
		}
		if !jsonOnly {
			fmt.Print(result.FinalText)
		}
		return nil
	}

	return h.StreamTurn(ctx, turn, func(ev harness.Event) error {
		if logResponses != "" {
			if f, err := os.OpenFile(logResponses, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				buf, _ := json.Marshal(ev)
				_, _ = f.Write(append(buf, '\n'))
				_ = f.Close()
			}
		}
		if jsonOnly {
			switch ev.Kind {
			case harness.EventError:
				errMsg := "unknown error"
				if ev.Error != nil {
					errMsg = ev.Error.Message
				}
				payload := struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				}{Type: "error", Message: errMsg}
				buf, _ := json.Marshal(payload)
				fmt.Println(string(buf))
				return nil
			case harness.EventToolCall:
				if ev.ToolCall != nil {
					buf, _ := json.Marshal(ev)
					fmt.Println(string(buf))
				}
				return nil
			}
			buf, _ := json.Marshal(ev)
			fmt.Println(string(buf))
			return nil
		}
		if trace {
			buf, _ := json.Marshal(ev)
			fmt.Println(string(buf))
		}
		switch ev.Kind {
		case harness.EventText:
			if ev.Text != nil {
				fmt.Print(ev.Text.Delta)
			}
		}
		return nil
	})
}

func extractErrorMessage(raw json.RawMessage) string {
	var payload struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.Message) != "" {
		return payload.Message
	}
	if strings.TrimSpace(payload.Error.Message) != "" {
		return payload.Error.Message
	}
	return ""
}

func normalizeToolChoice(choice string) string {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return "auto"
	}
	return choice
}

func emitMockStream(req protocol.ResponsesRequest, jsonOnly bool, logResponses string, mode string) error {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "echo"
	}

	created := map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     "mock-response",
			"object": "response",
			"status": "in_progress",
		},
	}
	completed := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "mock-response",
			"object": "response",
			"status": "completed",
		},
	}

	chunks := []map[string]any{created}

	switch mode {
	case "text":
		for _, piece := range splitText("mock response text", 800) {
			chunks = append(chunks, map[string]any{
				"type":  "response.output_text.delta",
				"delta": piece,
			})
		}
	case "tool-call":
		chunks = append(chunks,
			map[string]any{
				"type": "response.output_item.added",
				"item": map[string]any{
					"id":      "fc_mock",
					"type":    "function_call",
					"call_id": "call_mock",
					"name":    "mock_tool",
				},
			},
			map[string]any{
				"type":    "response.function_call_arguments.delta",
				"item_id": "fc_mock",
				"delta":   "{\"value\":42}",
			},
			map[string]any{
				"type": "response.output_item.done",
				"item": map[string]any{
					"id":        "fc_mock",
					"type":      "function_call",
					"call_id":   "call_mock",
					"name":      "mock_tool",
					"arguments": "{\"value\":42}",
				},
			},
		)
	case "tool-loop":
		chunks = append(chunks,
			map[string]any{
				"type": "response.output_item.added",
				"item": map[string]any{
					"id":      "fc_mock",
					"type":    "function_call",
					"call_id": "call_mock",
					"name":    "mock_tool",
				},
			},
			map[string]any{
				"type":    "response.function_call_arguments.delta",
				"item_id": "fc_mock",
				"delta":   "{\"value\":42}",
			},
			map[string]any{
				"type": "response.output_item.done",
				"item": map[string]any{
					"id":        "fc_mock",
					"type":      "function_call",
					"call_id":   "call_mock",
					"name":      "mock_tool",
					"arguments": "{\"value\":42}",
				},
			},
			map[string]any{
				"type":  "response.output_text.delta",
				"delta": "mock tool result: ok",
			},
		)
	default:
		payload, _ := json.Marshal(req)
		text := string(payload)
		for _, piece := range splitText(text, 800) {
			chunks = append(chunks, map[string]any{
				"type":  "response.output_text.delta",
				"delta": piece,
			})
		}
	}

	chunks = append(chunks, completed)

	for _, ev := range chunks {
		buf, _ := json.Marshal(ev)
		if logResponses != "" {
			if f, err := os.OpenFile(logResponses, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				_, _ = f.Write(append(buf, '\n'))
				_ = f.Close()
			}
		}
		if jsonOnly {
			fmt.Println(string(buf))
		} else if ev["type"] == "response.output_text.delta" {
			fmt.Print(ev["delta"].(string))
		}
	}
	return nil
}

func splitText(text string, size int) []string {
	if size <= 0 {
		return []string{text}
	}
	var out []string
	for len(text) > size {
		out = append(out, text[:size])
		text = text[size:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

func parseToolSpecs(flags []string) ([]protocol.ToolSpec, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	tools := make([]protocol.ToolSpec, 0, len(flags))
	for _, raw := range flags {
		if raw == "web_search" {
			tools = append(tools, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
			continue
		}
		name, path, ok := strings.Cut(raw, ":json=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("invalid --tool %q; expected web_search or name:json=path", raw)
		}
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read tool schema %s: %w", path, err)
		}
		var rawSchema json.RawMessage
		if err := json.Unmarshal(buf, &rawSchema); err != nil {
			return nil, fmt.Errorf("parse tool schema %s: %w", path, err)
		}
		tools = append(tools, protocol.ToolSpec{
			Type:       "function",
			Name:       name,
			Parameters: rawSchema,
			Strict:     false,
		})
	}
	return tools, nil
}

func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func runProxy(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "keys":
			return runProxyKeys(args[1:])
		case "usage":
			return runProxyUsage(args[1:])
		}
	}

	fs := flag.NewFlagSet("proxy", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := config.LoadFrom(configPathFromArgs(args))

	var listen string
	var apiKey string
	var model string
	var baseURL string
	var originator string
	var userAgent string
	var allowRefresh bool
	var allowAnyKey bool
	var authPath string
	var cacheTTL string
	var logLevel string
	var logRequests bool
	var keysPath string
	var rateLimit string
	var burst int
	var quotaTokens int64
	var statsPath string
	var statsSummary string
	var statsMaxBytes int64
	var statsMaxBackups int
	var eventsPath string
	var eventsMaxBytes int64
	var eventsBackups int
	var meterWindow string
	var syncAliases bool
	var proxyNativeTools bool

	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	fs.StringVar(&listen, "listen", cfg.Proxy.Listen, "Listen address")
	fs.StringVar(&apiKey, "api-key", cfg.Proxy.APIKey, "API key")
	fs.StringVar(&model, "model", cfg.Proxy.Model, "Model name")
	fs.StringVar(&baseURL, "base-url", cfg.Proxy.BaseURL, "Upstream base URL")
	fs.StringVar(&originator, "originator", cfg.Proxy.Originator, "Originator header")
	fs.StringVar(&userAgent, "user-agent", cfg.Proxy.UserAgent, "User-Agent header")
	fs.BoolVar(&allowRefresh, "allow-refresh", cfg.Proxy.AllowRefresh, "Allow network token refresh on 401")
	fs.BoolVar(&allowAnyKey, "allow-any-key", cfg.Proxy.AllowAnyKey, "Allow any bearer token")
	fs.StringVar(&authPath, "auth-path", cfg.Proxy.AuthPath, "Auth file path (defaults to ~/.codex/auth.json)")
	fs.StringVar(&cacheTTL, "cache-ttl", cfg.Proxy.CacheTTL.String(), "Prompt cache TTL")
	fs.StringVar(&logLevel, "log-level", cfg.Proxy.LogLevel, "Log level (debug|info|warn|error)")
	fs.BoolVar(&logRequests, "log-requests", cfg.Proxy.LogRequests, "Log HTTP requests")
	fs.StringVar(&keysPath, "keys-path", cfg.Proxy.KeysPath, "API keys file")
	fs.StringVar(&rateLimit, "rate", cfg.Proxy.DefaultRate, "Default rate limit (e.g. 60/m)")
	fs.IntVar(&burst, "burst", cfg.Proxy.DefaultBurst, "Default rate burst")
	fs.Int64Var(&quotaTokens, "quota-tokens", cfg.Proxy.DefaultQuota, "Default token quota (0 = none)")
	fs.StringVar(&statsPath, "stats-path", cfg.Proxy.StatsPath, "Usage stats JSONL path (empty disables history)")
	fs.StringVar(&statsSummary, "stats-summary", cfg.Proxy.StatsSummary, "Usage summary JSON path")
	fs.Int64Var(&statsMaxBytes, "stats-max-bytes", cfg.Proxy.StatsMaxBytes, "Max stats file size before rotation")
	fs.IntVar(&statsMaxBackups, "stats-max-backups", cfg.Proxy.StatsBackups, "Max rotated stats files to keep")
	fs.StringVar(&eventsPath, "events-path", cfg.Proxy.EventsPath, "Proxy events JSONL path")
	fs.Int64Var(&eventsMaxBytes, "events-max-bytes", cfg.Proxy.EventsMax, "Max events file size before rotation")
	fs.IntVar(&eventsBackups, "events-max-backups", cfg.Proxy.EventsBackups, "Max rotated events files to keep")
	fs.StringVar(&meterWindow, "meter-window", cfg.Proxy.MeterWindow.String(), "Metering window duration (e.g. 24h); empty disables window")
	fs.BoolVar(&syncAliases, "sync-aliases", false, "Update model aliases from providers on startup")
	fs.BoolVar(&proxyNativeTools, "native-tools", cfg.Proxy.Backends.Codex.NativeTools, "Use Codex native tools (shell, apply_patch) instead of proxy mode")

	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = configPath
	// api-key optional when using key store; --allow-any-key bypasses auth entirely
	if strings.TrimSpace(cacheTTL) == "" {
		cacheTTL = "6h"
	}
	ttl, err := time.ParseDuration(cacheTTL)
	if err != nil {
		return fmt.Errorf("invalid --cache-ttl: %w", err)
	}
	var window time.Duration
	if strings.TrimSpace(meterWindow) != "" {
		window, err = time.ParseDuration(meterWindow)
		if err != nil {
			return fmt.Errorf("invalid --meter-window: %w", err)
		}
	}

	payCfg := payments.Config{
		Enabled:       cfg.Proxy.Payments.Enabled,
		Provider:      cfg.Proxy.Payments.Provider,
		TokenMeterURL: cfg.Proxy.Payments.TokenMeterURL,
	}
	// Convert models config
	var models []proxy.ModelEntry
	for _, m := range cfg.Proxy.Models {
		models = append(models, proxy.ModelEntry{ID: m.ID, BaseURL: m.BaseURL})
	}
	proxyCfg := proxy.Config{
		Listen:          listen,
		APIKey:          apiKey,
		Model:           model,
		Models:          models,
		BaseURL:         baseURL,
		AllowRefresh:    allowRefresh,
		AllowAnyKey:     allowAnyKey,
		AuthPath:        authPath,
		Originator:      originator,
		UserAgent:       userAgent,
		CacheTTL:        ttl,
		LogLevel:        logLevel,
		LogRequests:     logRequests,
		KeysPath:        keysPath,
		RateLimit:       rateLimit,
		Burst:           burst,
		QuotaTokens:     quotaTokens,
		StatsPath:       statsPath,
		StatsSummary:    statsSummary,
		StatsMaxBytes:   statsMaxBytes,
		StatsMaxBackups: statsMaxBackups,
		EventsPath:      eventsPath,
		EventsMaxBytes:  eventsMaxBytes,
		EventsBackups:   eventsBackups,
		AuditPath:       cfg.Proxy.AuditPath,
		AuditMaxBytes:   cfg.Proxy.AuditMaxBytes,
		AuditBackups:    cfg.Proxy.AuditBackups,
		MeterWindow:     window,
		AdminSocket:     cfg.Proxy.AdminSocket,
		Payments:        payCfg,
		Backends: proxy.BackendsConfig{
			Codex: proxy.CodexBackendConfig{
				Enabled:         cfg.Proxy.Backends.Codex.Enabled,
				BaseURL:         cfg.Proxy.Backends.Codex.BaseURL,
				CredentialsPath: cfg.Proxy.Backends.Codex.CredentialsPath,
			},
			Anthropic: proxy.AnthropicBackendConfig{
				Enabled:          cfg.Proxy.Backends.Anthropic.Enabled,
				CredentialsPath:  cfg.Proxy.Backends.Anthropic.CredentialsPath,
				DefaultMaxTokens: cfg.Proxy.Backends.Anthropic.DefaultMaxTokens,
			},
			Custom: cfg.Proxy.Backends.Custom,
			Routing: proxy.RoutingConfig{
				Default:  cfg.Proxy.Backends.Routing.Default,
				Patterns: cfg.Proxy.Backends.Routing.Patterns,
				Aliases:  cfg.Proxy.Backends.Routing.Aliases,
			},
		},
		Metrics: proxy.MetricsConfig{
			Enabled:     cfg.Proxy.Metrics.Enabled,
			Path:        cfg.Proxy.Metrics.Path,
			LogRequests: cfg.Proxy.Metrics.LogRequests,
		},
	}
	// Apply CLI flag overrides to config
	if proxyNativeTools {
		cfg.Proxy.Backends.Codex.NativeTools = true
	}
	if syncAliases {
		if err := syncAliasesOnStartup(cfg, *configPath, &proxyCfg); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  alias sync: %v\n", err)
		}
	}

	// Build harness router
	harnessRouter := buildHarnessRouter(cfg, proxyCfg)
	if harnessRouter != nil {
		proxyCfg.HarnessRouter = harnessRouter
	}

	return proxy.Run(proxyCfg)
}

// buildHarnessRouter creates a harness router with all configured providers.
func buildHarnessRouter(cfg config.Config, proxyCfg proxy.Config) *router.Router {
	routingCfg := router.Config{
		UserAliases:  proxyCfg.Backends.Routing.Aliases,
		UserPatterns: proxyCfg.Backends.Routing.Patterns,
	}

	r := router.New(routingCfg)
	registered := 0

	// Register Codex harness
	if cfg.Proxy.Backends.Codex.Enabled {
		baseURL := cfg.Proxy.Backends.Codex.BaseURL
		if baseURL == "" {
			baseURL = proxyCfg.BaseURL
		}
		authPath := cfg.Auth.Path
		if authPath == "" {
			authPath, _ = auth.DefaultPath()
		}
		store, err := auth.Load(authPath)
		if err == nil {
			codexClient := harnessCodexP.NewClient(nil, store, harnessCodexP.ClientConfig{
				BaseURL:      baseURL,
				Originator:   proxyCfg.Originator,
				UserAgent:    proxyCfg.UserAgent,
				AllowRefresh: proxyCfg.AllowRefresh,
			})
			h := harnessCodexP.New(harnessCodexP.Config{
				Client:        codexClient,
				NativeTools:   cfg.Proxy.Backends.Codex.NativeTools,
				ExtraAliases:  cfg.Proxy.Backends.Routing.Aliases,
				ExtraPrefixes: cfg.Proxy.Backends.Routing.Patterns["codex"],
			})
			r.Register("codex", h)
			registered++
		}
	}

	// Register Claude harness
	if cfg.Proxy.Backends.Anthropic.Enabled {
		anthTokens := harnessClaudeP.NewTokenStore(cfg.Proxy.Backends.Anthropic.CredentialsPath)
		if err := anthTokens.Load(); err == nil {
			wrapper := harnessClaudeP.NewClientWrapper(anthTokens, harnessClaudeP.ClientConfig{
				DefaultMaxTokens: cfg.Proxy.Backends.Anthropic.DefaultMaxTokens,
			})
			h := harnessClaudeP.New(harnessClaudeP.Config{
				Client:           wrapper,
				DefaultMaxTokens: cfg.Proxy.Backends.Anthropic.DefaultMaxTokens,
				ExtraAliases:     cfg.Proxy.Backends.Routing.Aliases,
			})
			r.Register("anthropic", h)
			registered++
		}
	}

	// Register custom OpenAI-compatible harnesses
	for name, bcfg := range cfg.Proxy.Backends.Custom {
		if !bcfg.IsEnabled() || bcfg.Type != "openai" {
			continue
		}
		oaiClient, err := harnessOpenaiP.NewClient(harnessOpenaiP.ClientConfig{
			Name:      name,
			BaseURL:   bcfg.BaseURL,
			Auth:      bcfg.Auth,
			Timeout:   bcfg.Timeout,
			Discovery: bcfg.HasDiscovery(),
			Models:    bcfg.Models,
		})
		if err != nil {
			continue
		}
		h := harnessOpenaiP.New(harnessOpenaiP.Config{
			Client:   oaiClient,
			Aliases:  cfg.Proxy.Backends.Routing.Aliases,
			Prefixes: cfg.Proxy.Backends.Routing.Patterns[name],
		})
		r.Register(name, h)
		registered++
	}

	if registered == 0 {
		return nil
	}
	return r
}

// aliasModelLister adapts a harness to the aliases.ModelLister interface.
type aliasModelLister struct {
	listFn func(ctx context.Context) ([]aliases.ModelInfo, error)
}

func (a *aliasModelLister) ListModels(ctx context.Context) ([]aliases.ModelInfo, error) {
	return a.listFn(ctx)
}

func syncAliasesOnStartup(cfg config.Config, configPath string, proxyCfg *proxy.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	backends := map[string]aliases.ModelLister{}

	if cfg.Proxy.Backends.Codex.Enabled {
		codexClient := harnessCodexP.NewClient(nil, nil, harnessCodexP.ClientConfig{})
		backends["codex"] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
			models, err := codexClient.ListModels(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]aliases.ModelInfo, len(models))
			for i, m := range models {
				out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
			}
			return out, nil
		}}
	}

	if cfg.Proxy.Backends.Anthropic.Enabled {
		anthTokens := harnessClaudeP.NewTokenStore(cfg.Proxy.Backends.Anthropic.CredentialsPath)
		if err := anthTokens.Load(); err == nil {
			wrapper := harnessClaudeP.NewClientWrapper(anthTokens, harnessClaudeP.ClientConfig{
				DefaultMaxTokens: cfg.Proxy.Backends.Anthropic.DefaultMaxTokens,
			})
			backends["anthropic"] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
				models, err := wrapper.ListModels(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]aliases.ModelInfo, len(models))
				for i, m := range models {
					out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
				}
				return out, nil
			}}
		}
	}

	for name, bcfg := range cfg.Proxy.Backends.Custom {
		if !bcfg.IsEnabled() {
			continue
		}
		authCfg := bcfg.Auth
		if authCfg.Key == "" && authCfg.KeyEnv != "" {
			authCfg.Key = os.Getenv(authCfg.KeyEnv)
		}
		oaiClient, err := harnessOpenaiP.NewClient(harnessOpenaiP.ClientConfig{
			Name:      name,
			BaseURL:   bcfg.BaseURL,
			Auth:      authCfg,
			Discovery: bcfg.HasDiscovery(),
			Models:    bcfg.Models,
		})
		if err == nil {
			c := oaiClient // capture for closure
			backends[name] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
				models, err := c.ListModels(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]aliases.ModelInfo, len(models))
				for i, m := range models {
					out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
				}
				return out, nil
			}}
		}
	}

	if len(backends) == 0 {
		return nil
	}

	current := cfg.Proxy.Backends.Routing.Aliases
	if current == nil {
		current = map[string]string{}
	}

	results := aliases.Resolve(ctx, backends, current, nil)
	n := aliases.ApplyResolutions(current, results)
	if n > 0 {
		proxyCfg.Backends.Routing.Aliases = current
		if err := config.UpdateAliases(configPath, current); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  alias save: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "✅ synced %d alias(es)\n", n)
		}
	}
	return nil
}

func runProxyKeys(args []string) error {
	if len(args) == 0 {
		return errors.New("proxy keys requires a subcommand")
	}
	cmd := args[0]

	fs := flag.NewFlagSet("proxy keys", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := config.LoadFrom(configPathFromArgs(args))
	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	keysPath := fs.String("keys-path", defaultString(cfg.Proxy.KeysPath, proxy.DefaultKeysPath()), "API keys file")
	label := fs.String("label", "", "Key label")
	providedKey := fs.String("key", "", "Use a pre-generated API key (BYOK)")
	rate := fs.String("rate", defaultString(cfg.Proxy.DefaultRate, "60/m"), "Rate limit")
	burst := fs.Int("burst", defaultInt(cfg.Proxy.DefaultBurst, 10), "Burst")
	quota := fs.Int64("quota-tokens", defaultInt64(cfg.Proxy.DefaultQuota, 0), "Token quota")
	expiresIn := fs.String("expires-in", "", "Key TTL (e.g. 24h); empty = no expiry")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	_ = configPath

	store, err := proxy.LoadKeyStore(*keysPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "add":
		var ttl time.Duration
		if strings.TrimSpace(*expiresIn) != "" {
			d, err := time.ParseDuration(*expiresIn)
			if err != nil {
				return err
			}
			ttl = d
		}
		rec, secret, err := store.Add(*label, *rate, *burst, *quota, *providedKey, ttl)
		if err != nil {
			return err
		}
		fmt.Printf("id=%s label=%s key=%s\n", rec.ID, rec.Label, secret)
	case "list":
		for _, rec := range store.List() {
			revoked := ""
			if rec.RevokedAt != nil {
				revoked = rec.RevokedAt.Format(time.RFC3339)
			}
			expires := ""
			if rec.ExpiresAt != nil {
				expires = rec.ExpiresAt.Format(time.RFC3339)
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n", rec.ID, rec.Label, rec.CreatedAt.Format(time.RFC3339), revoked, rec.Rate, rec.Burst, rec.QuotaTokens, expires)
		}
	case "revoke":
		if len(fs.Args()) == 0 {
			return errors.New("revoke requires id or key")
		}
		if _, ok := store.Revoke(fs.Args()[0]); !ok {
			return errors.New("key not found")
		}
		fmt.Println("revoked")
	case "update":
		if len(fs.Args()) == 0 {
			return errors.New("update requires id")
		}
		var ttl time.Duration
		if strings.TrimSpace(*expiresIn) != "" {
			d, err := time.ParseDuration(*expiresIn)
			if err != nil {
				return err
			}
			ttl = d
		}
		rec, err := store.Update(fs.Args()[0], *label, *rate, *burst, *quota, ttl)
		if err != nil {
			return err
		}
		fmt.Printf("id=%s label=%s rate=%s burst=%d quota=%d\n", rec.ID, rec.Label, rec.Rate, rec.Burst, rec.QuotaTokens)
	case "rotate":
		if len(fs.Args()) == 0 {
			return errors.New("rotate requires id or key")
		}
		rec, secret, err := store.Rotate(fs.Args()[0])
		if err != nil {
			return err
		}
		fmt.Printf("id=%s label=%s key=%s\n", rec.ID, rec.Label, secret)
	default:
		return fmt.Errorf("unknown proxy keys command: %s", cmd)
	}
	return nil
}

func runProxyUsage(args []string) error {
	if len(args) == 0 {
		return errors.New("proxy usage requires a subcommand")
	}
	cmd := args[0]

	fs := flag.NewFlagSet("proxy usage", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := config.LoadFrom(configPathFromArgs(args))
	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	statsPath := fs.String("stats-path", defaultString(cfg.Proxy.StatsPath, ""), "Usage JSONL path")
	sinceStr := fs.String("since", "", "Lookback duration (e.g. 24h)")
	keyID := fs.String("key", "", "Key id filter")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	_ = configPath
	var since time.Duration
	if strings.TrimSpace(*sinceStr) != "" {
		d, err := time.ParseDuration(*sinceStr)
		if err != nil {
			return err
		}
		since = d
	}
	if cmd == "show" || cmd == "reset" {
		if len(fs.Args()) == 0 && strings.TrimSpace(*keyID) == "" {
			return fmt.Errorf("%s requires --key or id", cmd)
		}
		if strings.TrimSpace(*keyID) == "" {
			*keyID = fs.Args()[0]
		}
	}
	events, err := proxy.ReadUsage(*statsPath, since, *keyID)
	if err != nil {
		return err
	}
	if cmd == "list" {
		sums := proxy.SummarizeUsage(events)
		for _, s := range sums {
			fmt.Printf("%s\t%s\t%d\t%d\t%s\n", s.KeyID, s.Label, s.Requests, s.TotalTokens, s.LastSeen.Format(time.RFC3339))
		}
		return nil
	}
	if cmd == "show" {
		sums := proxy.SummarizeUsage(events)
		if len(sums) == 0 {
			fmt.Println("no usage")
			return nil
		}
		s := sums[0]
		fmt.Printf("key=%s label=%s requests=%d total_tokens=%d last_seen=%s\n", s.KeyID, s.Label, s.Requests, s.TotalTokens, s.LastSeen.Format(time.RFC3339))
		return nil
	}
	if cmd == "reset" {
		if strings.TrimSpace(*keyID) == "" {
			if len(fs.Args()) == 0 {
				return errors.New("reset requires --key or id")
			}
			*keyID = fs.Args()[0]
		}
		store := proxy.NewUsageStore(*statsPath, proxy.DefaultStatsSummaryPath(), 10*1024*1024, 3, 0, proxy.DefaultEventsPath(), 1024*1024, 3)
		store.ResetKey(*keyID)
		fmt.Println("reset")
		return nil
	}
	return fmt.Errorf("unknown proxy usage command: %s", cmd)
}

func envOrDefault(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func envBool(key string) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return false
	}
	val = strings.ToLower(val)
	return val == "1" || val == "true" || val == "yes"
}

func envInt(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	out, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return out
}

func envInt64(key string, fallback int64) int64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	out, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return fallback
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultInt64(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return strings.Replace(path, "~", home, 1)
		}
	}
	return path
}

func configPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return config.DefaultPath()
}

func runProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var url string
	var apiKey string
	var jsonOutput bool

	fs.StringVar(&url, "url", "http://127.0.0.1:39001", "proxy URL")
	fs.StringVar(&apiKey, "key", "", "API key (or set GODEX_API_KEY)")
	fs.BoolVar(&jsonOutput, "json", false, "output as JSON")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: godex probe <model> [--url URL] [--key KEY] [--json]")
	}
	model := fs.Arg(0)

	// Get API key from env if not provided
	if apiKey == "" {
		apiKey = os.Getenv("GODEX_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("API key required: use --key or set GODEX_API_KEY")
	}

	// Build request URL
	reqURL := strings.TrimRight(url, "/") + "/v1/models/" + model

	// Make request
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		if jsonOutput {
			fmt.Printf(`{"status":"not_found","model":%q}%s`, model, "\n")
		} else {
			fmt.Printf("ERROR: model %q not found\n", model)
		}
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
		Backend     string `json:"backend,omitempty"`
		Alias       string `json:"alias,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if jsonOutput {
		fmt.Println(string(body))
	} else {
		// Human-readable output
		if result.Alias != "" {
			fmt.Printf("OK: %s → %s", result.Alias, result.ID)
		} else {
			fmt.Printf("OK: %s", result.ID)
		}
		if result.Backend != "" {
			fmt.Printf(" (%s)", result.Backend)
		}
		if result.DisplayName != "" {
			fmt.Printf(" [%s]", result.DisplayName)
		}
		fmt.Println()
	}

	return nil
}

func runAuth(args []string) error {
	if len(args) == 0 {
		return runAuthStatus()
	}

	switch args[0] {
	case "status":
		return runAuthStatus()
	case "setup":
		return runAuthSetup()
	default:
		return fmt.Errorf("unknown auth command: %s (use 'status' or 'setup')", args[0])
	}
}

// AuthStatus holds the status of a backend's authentication.
type AuthStatus struct {
	Backend     string
	Configured  bool
	Path        string
	ExpiresAt   time.Time
	Error       string
}

func runAuthStatus() error {
	fmt.Println("godex authentication status")
	fmt.Println("===========================")
	fmt.Println()

	// Check Codex
	codexStatus := checkCodexAuth()
	printAuthStatus("Codex", codexStatus)

	// Check Anthropic
	anthropicStatus := checkAnthropicAuth()
	printAuthStatus("Anthropic", anthropicStatus)

	return nil
}

func printAuthStatus(name string, status AuthStatus) {
	if status.Configured {
		fmt.Printf("%-12s ✅ configured\n", name+":")
		fmt.Printf("             Path: %s\n", status.Path)
		if !status.ExpiresAt.IsZero() {
			if status.ExpiresAt.After(time.Now()) {
				fmt.Printf("             Expires: %s\n", status.ExpiresAt.Format("2006-01-02 15:04"))
			} else {
				fmt.Printf("             ⚠️  Expired: %s\n", status.ExpiresAt.Format("2006-01-02 15:04"))
			}
		}
	} else {
		fmt.Printf("%-12s ❌ not configured\n", name+":")
		if status.Path != "" {
			fmt.Printf("             Expected: %s\n", status.Path)
		}
		if status.Error != "" {
			fmt.Printf("             Error: %s\n", status.Error)
		}
	}
	fmt.Println()
}

func checkCodexAuth() AuthStatus {
	home, _ := os.UserHomeDir()
	path := home + "/.codex/auth.json"

	status := AuthStatus{
		Backend: "codex",
		Path:    path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			status.Error = "file not found"
		} else {
			status.Error = err.Error()
		}
		return status
	}

	// Codex auth.json structure: { auth_mode, tokens: { access_token, ... } }
	var auth struct {
		AuthMode string `json:"auth_mode"`
		APIKey   string `json:"OPENAI_API_KEY"`
		Tokens   struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		status.Error = "invalid JSON: " + err.Error()
		return status
	}

	// Check for API key mode
	if auth.AuthMode == "api_key" && auth.APIKey != "" {
		status.Configured = true
		return status
	}

	// Check for OAuth/ChatGPT mode
	if auth.Tokens.AccessToken != "" {
		status.Configured = true
		return status
	}

	status.Error = "no credentials found (no access_token or API key)"
	return status
}

func checkAnthropicAuth() AuthStatus {
	home, _ := os.UserHomeDir()
	path := home + "/.claude/.credentials.json"

	status := AuthStatus{
		Backend: "anthropic",
		Path:    path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			status.Error = "file not found"
		} else {
			status.Error = err.Error()
		}
		return status
	}

	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		status.Error = "invalid JSON: " + err.Error()
		return status
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		status.Error = "no accessToken found"
		return status
	}

	status.Configured = true
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		// Claude uses milliseconds
		status.ExpiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	}
	return status
}

func runAuthSetup() error {
	fmt.Println("godex authentication setup")
	fmt.Println("==========================")
	fmt.Println()

	// Check current status
	codexStatus := checkCodexAuth()
	anthropicStatus := checkAnthropicAuth()

	allConfigured := codexStatus.Configured && anthropicStatus.Configured

	if allConfigured {
		fmt.Println("✅ All backends are already configured!")
		fmt.Println()
		runAuthStatus()
		return nil
	}

	// Setup missing backends
	if !codexStatus.Configured {
		fmt.Println("Setting up Codex authentication...")
		fmt.Println("──────────────────────────────────")
		fmt.Println()
		fmt.Println("Codex uses OAuth authentication via the Codex CLI.")
		fmt.Println()
		fmt.Println("To authenticate:")
		fmt.Println("  1. Install Codex CLI:  npm install -g @anthropic/codex")
		fmt.Println("  2. Run:                codex auth")
		fmt.Println("  3. Follow the browser prompts to sign in")
		fmt.Println()
		fmt.Printf("  Credentials will be saved to: %s\n", codexStatus.Path)
		fmt.Println()

		if promptYesNo("Would you like to run 'codex auth' now?") {
			fmt.Println()
			fmt.Println("Running: codex auth")
			fmt.Println()
			cmd := execCommand("codex", "auth")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("⚠️  codex auth failed: %v\n", err)
				fmt.Println("   You may need to install it first: npm install -g @anthropic/codex")
			} else {
				fmt.Println()
				fmt.Println("✅ Codex authentication complete!")
			}
		}
		fmt.Println()
	} else {
		fmt.Println("✅ Codex: already configured")
		fmt.Println()
	}

	if !anthropicStatus.Configured {
		fmt.Println("Setting up Anthropic authentication...")
		fmt.Println("───────────────────────────────────────")
		fmt.Println()
		fmt.Println("Anthropic uses OAuth via the Claude Code CLI.")
		fmt.Println()
		fmt.Println("To authenticate:")
		fmt.Println("  1. Install Claude Code: npm install -g @anthropic-ai/claude-code")
		fmt.Println("  2. Run:                 claude auth login")
		fmt.Println("  3. Follow the browser prompts to sign in")
		fmt.Println()
		fmt.Printf("  Credentials will be saved to: %s\n", anthropicStatus.Path)
		fmt.Println()

		if promptYesNo("Would you like to run 'claude auth login' now?") {
			fmt.Println()
			fmt.Println("Running: claude auth login")
			fmt.Println()
			cmd := execCommand("claude", "auth", "login")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("⚠️  claude auth login failed: %v\n", err)
				fmt.Println("   You may need to install it first: npm install -g @anthropic-ai/claude-code")
			} else {
				fmt.Println()
				fmt.Println("✅ Anthropic authentication complete!")
			}
		}
		fmt.Println()
	} else {
		fmt.Println("✅ Anthropic: already configured")
		fmt.Println()
	}

	// Final status
	fmt.Println("─────────────────────────────────")
	fmt.Println("Final status:")
	fmt.Println()
	return runAuthStatus()
}

func promptYesNo(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// execCommand wraps exec.Command for testability
var execCommand = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// streamClient is a unified interface for exec streaming.
// Both codex.Client and openai.Client implement StreamResponses and StreamAndCollect.
type streamClient interface {
	StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error
	RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler harnessCodexP.ToolLoopHandler, opts harnessCodexP.ToolLoopOptions) (harnessCodexP.StreamResult, error)
}

// resolveClient picks the right client based on model name.
// For Codex models, uses OAuth. For others, uses the OpenAI-compatible client.
func resolveClient(model string, store *auth.Store, cfg config.Config, allowRefresh bool, sessionID, providerKey string) (*harnessCodexP.Client, error) {
	// For now, all exec paths use the Codex-wire-format client.
	// The Codex endpoint handles routing for non-Codex models via the proxy.
	// Direct Anthropic/Gemini exec would need the harness path, but that's a future enhancement.
	baseURL := cfg.Client.BaseURL
	if baseURL == "" {
		baseURL = "https://chatgpt.com/backend-api/codex"
	}
	c := harnessCodexP.NewClient(nil, store, harnessCodexP.ClientConfig{
		SessionID:    sessionID,
		AllowRefresh: allowRefresh,
		BaseURL:      baseURL,
		Originator:   cfg.Client.Originator,
		UserAgent:    cfg.Client.UserAgent,
		RetryMax:     cfg.Client.RetryMax,
		RetryDelay:   cfg.Client.RetryDelay,
	})
	return c, nil
}

func runAliases(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list":
		return runAliasesList(args[1:])
	case "update":
		return runAliasesUpdate(args[1:])
	default:
		return fmt.Errorf("unknown aliases command: %s (use 'list' or 'update')", args[0])
	}
}

func runAliasesList(args []string) error {
	fs := flag.NewFlagSet("aliases list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := config.LoadFrom(*configPath)

	if len(cfg.Proxy.Backends.Routing.Aliases) == 0 {
		fmt.Println("No aliases configured.")
		return nil
	}

	// Sort for deterministic output
	keys := make([]string, 0, len(cfg.Proxy.Backends.Routing.Aliases))
	for k := range cfg.Proxy.Backends.Routing.Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Printf("%-12s → %s\n", k, cfg.Proxy.Backends.Routing.Aliases[k])
	}
	return nil
}

func runAliasesUpdate(args []string) error {
	fs := flag.NewFlagSet("aliases update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	dryRun := fs.Bool("dry-run", false, "Show what would change without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := config.LoadFrom(*configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build available backends
	backends := map[string]aliases.ModelLister{}

	if cfg.Proxy.Backends.Codex.Enabled {
		codexClient := harnessCodexP.NewClient(nil, nil, harnessCodexP.ClientConfig{})
		backends["codex"] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
			models, err := codexClient.ListModels(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]aliases.ModelInfo, len(models))
			for i, m := range models {
				out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
			}
			return out, nil
		}}
	}

	if cfg.Proxy.Backends.Anthropic.Enabled {
		anthTokens := harnessClaudeP.NewTokenStore(cfg.Proxy.Backends.Anthropic.CredentialsPath)
		if err := anthTokens.Load(); err == nil {
			wrapper := harnessClaudeP.NewClientWrapper(anthTokens, harnessClaudeP.ClientConfig{
				DefaultMaxTokens: cfg.Proxy.Backends.Anthropic.DefaultMaxTokens,
			})
			backends["anthropic"] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
				models, err := wrapper.ListModels(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]aliases.ModelInfo, len(models))
				for i, m := range models {
					out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
				}
				return out, nil
			}}
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  anthropic: %v\n", err)
		}
	}

	for name, bcfg := range cfg.Proxy.Backends.Custom {
		if !bcfg.IsEnabled() {
			continue
		}
		authCfg := bcfg.Auth
		if authCfg.Key == "" && authCfg.KeyEnv != "" {
			authCfg.Key = os.Getenv(authCfg.KeyEnv)
		}
		oaiClient, err := harnessOpenaiP.NewClient(harnessOpenaiP.ClientConfig{
			Name:      name,
			BaseURL:   bcfg.BaseURL,
			Auth:      authCfg,
			Discovery: bcfg.HasDiscovery(),
			Models:    bcfg.Models,
		})
		if err == nil {
			c := oaiClient
			backends[name] = &aliasModelLister{listFn: func(ctx context.Context) ([]aliases.ModelInfo, error) {
				models, err := c.ListModels(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]aliases.ModelInfo, len(models))
				for i, m := range models {
					out[i] = aliases.ModelInfo{ID: m.ID, DisplayName: m.Name}
				}
				return out, nil
			}}
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  %s: %v\n", name, err)
		}
	}

	if len(backends) == 0 {
		return fmt.Errorf("no backends available for model discovery")
	}

	current := cfg.Proxy.Backends.Routing.Aliases
	if current == nil {
		current = map[string]string{}
	}

	results := aliases.Resolve(ctx, backends, current, nil)

	// Display
	anyChanged := false
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(os.Stderr, "⚠️  %-12s %s\n", r.Alias+":", r.Error)
			continue
		}
		if r.Changed {
			fmt.Printf("✅ %-12s %s → %s\n", r.Alias+":", r.Previous, r.Resolved)
			anyChanged = true
		} else {
			fmt.Printf("   %-12s %s (unchanged)\n", r.Alias+":", r.Resolved)
		}
	}

	if !anyChanged {
		fmt.Println("\nAll aliases are up to date.")
		return nil
	}

	if *dryRun {
		fmt.Println("\n(dry run — no changes written)")
		return nil
	}

	// Apply and save
	aliases.ApplyResolutions(current, results)
	if err := config.UpdateAliases(*configPath, current); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Println("\n✅ Config updated.")
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: godex exec --config <path> --prompt \"...\" [--model gpt-5.2-codex] [--tool web_search] [--tool name:json=schema.json] [--web-search] [--tool-choice auto|required|function:<name>] [--input-json path] [--mock --mock-mode echo|text|tool-call|tool-loop] [--auto-tools --tool-output name=value] [--trace] [--json] [--log-requests path] [--log-responses path]")
	fmt.Fprintln(os.Stderr, "       godex proxy --config <path> --api-key <key> [--listen 127.0.0.1:39001] [--model gpt-5.2-codex] [--base-url https://chatgpt.com/backend-api/codex] [--allow-any-key] [--auth-path ~/.codex/auth.json] [--log-requests]")
	fmt.Fprintln(os.Stderr, "       godex proxy keys --config <path> add --label <label> [--rate 60/m] [--burst 10] [--quota-tokens N]")
	fmt.Fprintln(os.Stderr, "       godex proxy keys list | update <id> | revoke <id|key> | rotate <id|key>")
	fmt.Fprintln(os.Stderr, "       godex proxy usage --config <path> list [--since 24h] [--key <id>] | show <id>")
	fmt.Fprintln(os.Stderr, "       godex probe <model> [--url http://127.0.0.1:39001] [--key <api-key>] [--json]")
	fmt.Fprintln(os.Stderr, "       godex auth status | setup")
	fmt.Fprintln(os.Stderr, "       godex aliases list | update [--dry-run]")
}
