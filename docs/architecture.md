# Architecture

Godex is intentionally small. The architecture is built to keep the Responses API flow explicit and inspectable.

## High‑level layout

```
cmd/godex/              CLI entrypoints (exec, proxy)
pkg/auth/               auth.json loader + refresh handling
pkg/protocol/           request/response types + tool schema
pkg/sse/                SSE parser + collector
pkg/harness/            Backend interface + router + generic tool loop
pkg/harness/backend.go  Backend interface definition
pkg/harness/toolloop.go Generic RunToolLoop (works with any Backend)
pkg/harness/context.go  WithProviderKey / ProviderKey context helpers
pkg/harness/codex/      Codex/ChatGPT backend + client/tool loop
pkg/harness/claude/  Anthropic Messages API backend
pkg/harness/openai/    Generic OpenAI-compatible backend (Gemini, Groq, etc.)
```

## Data flow (exec)

1. **CLI parses flags** (including `--model`, `--provider-key`, `--native-tools`)
2. **Harness Turn is built** from prompt/instructions/tools
3. **System prompt resolved** — proxy mode (default) strips tool sections; `--native-tools` uses full Codex prompt
4. **Harness.StreamTurn** routes to the appropriate backend
5. **SSE stream parsed** into structured harness events
6. **Tool events** optionally handled in a loop via `harness.RunToolLoop()`
7. **Output streamed** to stdout (JSONL or text)

### Backend routing (exec)

| Model pattern | Backend | Notes |
|---------------|---------|-------|
| `gpt-*`, `o1-*`, `o3-*`, `codex-*` | Codex | Default route when no custom override matches |
| `claude-*`, `sonnet`, `opus`, `haiku` | Anthropic | OAuth via Claude Code |
| `gemini-*`, `gemini`, `flash` | Gemini (OpenAPI) | Requires `GEMINI_API_KEY` or `--provider-key` |
| Custom patterns from config | Custom OpenAPI backend | Skipped if auth missing |

## Generic tool loop

`backend.RunToolLoop()` in `pkg/harness/toolloop.go` is a backend-agnostic tool
loop that works with any implementation of the `Backend` interface:

```go
result, err := backend.RunToolLoop(ctx, be, req, handler, backend.ToolLoopOptions{
    MaxSteps: 4,
})
```

The loop:
1. Calls `be.StreamAndCollect(ctx, req)`
2. If the response contains tool calls, invokes `handler.Handle()` for each
3. Builds follow-up input items (`function_call` + `function_call_output` pairs)
4. Repeats until no tool calls remain or `MaxSteps` is exceeded

This replaces the Codex-specific tool loop that lived in `pkg/harness/codex/toolloop.go`.

## Provider key context helpers

`pkg/harness/context.go` provides two functions for threading per-request API
keys through the call stack without changing function signatures:

```go
// Inject a key (e.g., from --provider-key flag or X-Provider-Key header)
ctx = backend.WithProviderKey(ctx, key)

// Extract within a backend implementation
if key, ok := backend.ProviderKey(ctx); ok {
    // use key instead of configured default
}
```

The proxy injects the key from the `X-Provider-Key` HTTP header; `godex exec`
injects it from the `--provider-key` flag.

## Tool calling loop

When `--auto-tools` is enabled:
1. Receive `function_call`
2. Resolve output using `--tool-output` (or `$args`)
3. Send `function_call_output` as next input item
4. Continue until `response.completed`

This keeps tool execution deterministic in tests.

## Proxy architecture

`godex proxy` exposes an OpenAI‑compatible API, translating:
- `/v1/chat/completions` → Responses API inputs
- streaming → SSE pass‑through JSONL

The proxy supports multiple backends with model-based routing.

## Multi-backend support

The proxy can route requests to different LLM backends based on model name:

```
                    ┌─────────────────┐
                    │  OpenAI API     │
                    │  /v1/chat/...   │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │     Router      │
                    │  model → backend│
                    └────────┬────────┘
        ┌───────────────────┬┴──────────────────┬──────────────────┐
        ▼                   ▼                   ▼                  ▼
┌───────────────┐ ┌───────────────┐ ┌─────────────────┐ ┌──────────────────┐
│    Codex      │ │   Anthropic   │ │     Gemini      │ │  Custom OpenAPI  │
│  gpt-*, o1-*  │ │   claude-*    │ │   gemini-*      │ │  (Groq, Ollama,  │
│  o3-*, codex-*│ │  sonnet/opus  │ │   flash, gemini │ │   vLLM, etc.)   │
└───────────────┘ └───────────────┘ └─────────────────┘ └──────────────────┘
```

### Backend interface

```go
type Backend interface {
    Name() string
    StreamResponses(ctx, req, onEvent) error
    StreamAndCollect(ctx, req) (StreamResult, error)
    ListModels(ctx) ([]ModelInfo, error)
}
```

### Routing rules

- Model prefix matching: `claude-*` → Anthropic, `gpt-*` → Codex, `gemini-*` → Gemini
- Aliases: `sonnet` → `claude-sonnet-4-5-20250929`, `gemini` → `gemini-2.5-pro`, `flash` → `gemini-2.5-flash`
- Custom backend patterns read from config (`routing.patterns`)
- Unknown models are rejected with a `400` validation error

### Anthropic backend

Uses the official `anthropic-sdk-go` with OAuth authentication:
- Reads tokens from `~/.claude/.credentials.json` (Claude Code)
- **Automatic token refresh** when expired (no manual re-auth needed)
- Requires `anthropic-beta: oauth-2025-04-20` header
- Translates OpenAI format ↔ Anthropic Messages API

### OpenAPI backend (`pkg/harness/openai/`)

A generic backend for any OpenAI-compatible endpoint:
- Used for Gemini, Groq, vLLM, Ollama, and user-defined custom backends
- Supports API key auth via `key_env`, literal `key`, or per-request `X-Provider-Key` / `--provider-key`
- Custom backends that fail to initialize (e.g., missing env var) are **skipped with a warning** — the proxy continues serving other backends

### Dynamic model discovery

The `/v1/models` endpoint queries backends for available models:
- **Anthropic**: Calls `GET /v1/models` API (live discovery)
- **Codex**: Returns known model list (hardcoded)
- **OpenAPI backends**: Optionally call `GET /v1/models` if `discovery: true`
- Results cached for 5 minutes

## Design goals
- **Minimal surface area**
- **Deterministic output** for tests
- **Easy debugging** via log files
- **No heavy SDK dependencies**
