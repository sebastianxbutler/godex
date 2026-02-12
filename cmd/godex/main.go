package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/protocol"
	"godex/pkg/proxy"
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
	default:
		usage()
		os.Exit(2)
	}
}

func runExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

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
	var tools toolFlags
	var outputs outputFlags
	var sessionID string
	var images toolFlags
	var logRequests string
	var logResponses string

	fs.StringVar(&prompt, "prompt", "", "User prompt")
	fs.StringVar(&model, "model", "gpt-5.2-codex", "Model name")
	fs.StringVar(&instructions, "instructions", "", "Optional system instructions")
	fs.StringVar(&instructionsAlt, "system", "", "Alias for --instructions")
	fs.StringVar(&appendSystemPrompt, "append-system-prompt", "", "Append to system instructions")
	fs.BoolVar(&trace, "trace", false, "Print raw SSE event JSON")
	fs.BoolVar(&jsonOnly, "json", false, "Emit JSON events only (no text output)")
	fs.BoolVar(&allowRefresh, "allow-refresh", false, "Allow network token refresh on 401")
	fs.BoolVar(&autoTools, "auto-tools", false, "Automatically run tool loop with static outputs")
	fs.BoolVar(&webSearch, "web-search", false, "Enable web_search tool")
	fs.StringVar(&toolChoice, "tool-choice", "", "Tool choice: auto|required|function:<name>")
	fs.StringVar(&inputJSON, "input-json", "", "JSON array of response input items (overrides --prompt)")
	fs.BoolVar(&mock, "mock", false, "Mock mode: no network, emit synthetic stream")
	fs.StringVar(&mockMode, "mock-mode", "echo", "Mock mode: echo|text|tool-call|tool-loop")
	fs.Var(&tools, "tool", "Tool spec (repeatable): web_search or name:json=/path/schema.json")
	fs.Var(&outputs, "tool-output", "Static tool output: name=value or name=$args (repeatable)")
	fs.StringVar(&sessionID, "session-id", "", "Optional session id (reuses prompt cache key)")
	fs.Var(&images, "image", "Image path (ignored; accepted for OpenClaw CLI compatibility)")
	fs.StringVar(&logRequests, "log-requests", "", "Write JSON request payload to file")
	fs.StringVar(&logResponses, "log-responses", "", "Append JSONL response events to file")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" && strings.TrimSpace(inputJSON) == "" {
		return errors.New("--prompt is required unless --input-json is provided")
	}

	authPath, err := auth.DefaultPath()
	if err != nil {
		return err
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

	cl := client.New(nil, store, client.Config{
		SessionID:    sessionID,
		AllowRefresh: allowRefresh,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if autoTools {
		outputs, err := parseToolOutputs(outputs)
		if err != nil {
			return err
		}
		handler := staticToolHandler{outputs: outputs}
		result, err := cl.RunToolLoop(ctx, req, handler, client.ToolLoopOptions{MaxSteps: 4})
		if err != nil {
			return err
		}
		if !jsonOnly {
			fmt.Print(result.Text)
		}
		return nil
	}

	collector := sse.NewCollector()
	return cl.StreamResponses(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if logResponses != "" {
			if f, err := os.OpenFile(logResponses, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				_, _ = f.Write(append(ev.Raw, '\n'))
				_ = f.Close()
			}
		}
		if jsonOnly {
			switch ev.Value.Type {
			case "error":
				message := extractErrorMessage(ev.Raw)
				if message == "" {
					message = "stream error"
				}
				payload := struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				}{Type: "error", Message: message}
				buf, _ := json.Marshal(payload)
				fmt.Println(string(buf))
				return nil
			case "response.output_item.done":
				if ev.Value.Item != nil && ev.Value.Item.Type == "function_call" {
					if ev.Value.Item.Arguments == "" {
						ev.Value.Item.Arguments = collector.FunctionArgs(ev.Value.Item.CallID)
					}
					buf, _ := json.Marshal(ev.Value)
					fmt.Println(string(buf))
					return nil
				}
			}
			fmt.Println(string(ev.Raw))
			return nil
		}
		if trace {
			fmt.Println(string(ev.Raw))
		}
		switch ev.Value.Type {
		case "response.output_text.delta":
			fmt.Print(ev.Value.Delta)
		case "response.content_part.added":
			if ev.Value.Part != nil && ev.Value.Part.Type == "output_text" {
				fmt.Print(ev.Value.Part.Text)
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
	var statsMaxBytes int64
	var statsMaxBackups int

	fs.StringVar(&listen, "listen", envOrDefault("GODEX_PROXY_LISTEN", "127.0.0.1:39001"), "Listen address")
	fs.StringVar(&apiKey, "api-key", os.Getenv("GODEX_PROXY_API_KEY"), "API key")
	fs.StringVar(&model, "model", envOrDefault("GODEX_PROXY_MODEL", "gpt-5.2-codex"), "Model name")
	fs.StringVar(&baseURL, "base-url", envOrDefault("GODEX_PROXY_BASE_URL", "https://chatgpt.com/backend-api/codex"), "Upstream base URL")
	fs.StringVar(&originator, "originator", envOrDefault("GODEX_PROXY_ORIGINATOR", "codex_cli_rs"), "Originator header")
	fs.StringVar(&userAgent, "user-agent", envOrDefault("GODEX_PROXY_USER_AGENT", "godex/0.0"), "User-Agent header")
	fs.BoolVar(&allowRefresh, "allow-refresh", envBool("GODEX_PROXY_ALLOW_REFRESH"), "Allow network token refresh on 401")
	fs.BoolVar(&allowAnyKey, "allow-any-key", envBool("GODEX_PROXY_ALLOW_ANY_KEY"), "Allow any bearer token")
	fs.StringVar(&authPath, "auth-path", os.Getenv("GODEX_PROXY_AUTH_PATH"), "Auth file path (defaults to ~/.codex/auth.json)")
	fs.StringVar(&cacheTTL, "cache-ttl", envOrDefault("GODEX_PROXY_CACHE_TTL", "6h"), "Prompt cache TTL")
	fs.StringVar(&logLevel, "log-level", envOrDefault("GODEX_PROXY_LOG_LEVEL", "info"), "Log level (debug|info|warn|error)")
	fs.BoolVar(&logRequests, "log-requests", envBool("GODEX_PROXY_LOG_REQUESTS"), "Log HTTP requests")
	fs.StringVar(&keysPath, "keys-path", envOrDefault("GODEX_PROXY_KEYS_PATH", proxy.DefaultKeysPath()), "API keys file")
	fs.StringVar(&rateLimit, "rate", envOrDefault("GODEX_PROXY_RATE", "60/m"), "Default rate limit (e.g. 60/m)")
	fs.IntVar(&burst, "burst", envInt("GODEX_PROXY_BURST", 10), "Default rate burst")
	fs.Int64Var(&quotaTokens, "quota-tokens", envInt64("GODEX_PROXY_QUOTA_TOKENS", 0), "Default token quota (0 = none)")
	fs.StringVar(&statsPath, "stats-path", envOrDefault("GODEX_PROXY_STATS_PATH", proxy.DefaultStatsPath()), "Usage stats JSONL path")
	fs.Int64Var(&statsMaxBytes, "stats-max-bytes", envInt64("GODEX_PROXY_STATS_MAX_BYTES", 10*1024*1024), "Max stats file size before rotation")
	fs.IntVar(&statsMaxBackups, "stats-max-backups", envInt("GODEX_PROXY_STATS_MAX_BACKUPS", 3), "Max rotated stats files to keep")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(apiKey) == "" && !allowAnyKey {
		return fmt.Errorf("--api-key is required")
	}
	if strings.TrimSpace(cacheTTL) == "" {
		cacheTTL = "6h"
	}
	ttl, err := time.ParseDuration(cacheTTL)
	if err != nil {
		return fmt.Errorf("invalid --cache-ttl: %w", err)
	}

	cfg := proxy.Config{
		Listen:          listen,
		APIKey:          apiKey,
		Model:           model,
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
		StatsMaxBytes:   statsMaxBytes,
		StatsMaxBackups: statsMaxBackups,
	}
	return proxy.Run(cfg)
}

func runProxyKeys(args []string) error {
	if len(args) == 0 {
		return errors.New("proxy keys requires a subcommand")
	}
	cmd := args[0]

	fs := flag.NewFlagSet("proxy keys", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	keysPath := fs.String("keys-path", envOrDefault("GODEX_PROXY_KEYS_PATH", proxy.DefaultKeysPath()), "API keys file")
	label := fs.String("label", "", "Key label")
	providedKey := fs.String("key", "", "Use a pre-generated API key (BYOK)")
	rate := fs.String("rate", "60/m", "Rate limit")
	burst := fs.Int("burst", 10, "Burst")
	quota := fs.Int64("quota-tokens", 0, "Token quota")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	store, err := proxy.LoadKeyStore(*keysPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "add":
		rec, secret, err := store.Add(*label, *rate, *burst, *quota, *providedKey)
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
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%d\t%d\n", rec.ID, rec.Label, rec.CreatedAt.Format(time.RFC3339), revoked, rec.Rate, rec.Burst, rec.QuotaTokens)
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
		rec, err := store.Update(fs.Args()[0], *label, *rate, *burst, *quota)
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
	statsPath := fs.String("stats-path", envOrDefault("GODEX_PROXY_STATS_PATH", proxy.DefaultStatsPath()), "Usage JSONL path")
	sinceStr := fs.String("since", "", "Lookback duration (e.g. 24h)")
	keyID := fs.String("key", "", "Key id filter")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var since time.Duration
	if strings.TrimSpace(*sinceStr) != "" {
		d, err := time.ParseDuration(*sinceStr)
		if err != nil {
			return err
		}
		since = d
	}
	if cmd == "show" {
		if len(fs.Args()) == 0 && strings.TrimSpace(*keyID) == "" {
			return errors.New("show requires --key or id")
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: godex exec --prompt \"...\" [--model gpt-5.2-codex] [--tool web_search] [--tool name:json=schema.json] [--web-search] [--tool-choice auto|required|function:<name>] [--input-json path] [--mock --mock-mode echo|text|tool-call|tool-loop] [--auto-tools --tool-output name=value] [--trace] [--json] [--log-requests path] [--log-responses path]")
	fmt.Fprintln(os.Stderr, "       godex proxy --api-key <key> [--listen 127.0.0.1:39001] [--model gpt-5.2-codex] [--base-url https://chatgpt.com/backend-api/codex] [--allow-any-key] [--auth-path ~/.codex/auth.json] [--log-requests]")
	fmt.Fprintln(os.Stderr, "       godex proxy keys add --label <label> [--rate 60/m] [--burst 10] [--quota-tokens N]")
	fmt.Fprintln(os.Stderr, "       godex proxy keys list | revoke <id|key> | rotate <id|key>")
	fmt.Fprintln(os.Stderr, "       godex proxy usage list [--since 24h] [--key <id>] | show <id>")
}
