# Architecture

Godex is intentionally small. The architecture is built to keep the Responses API flow explicit and inspectable.

## High‑level layout

```
cmd/godex/              CLI entrypoints (exec, proxy)
pkg/auth/               auth.json loader + refresh handling
pkg/protocol/           request/response types + tool schema
pkg/sse/                SSE parser + collector
pkg/client/             streaming + tool loop helpers (legacy)
pkg/backend/            Backend interface + router
pkg/backend/codex/      Codex/ChatGPT backend
pkg/backend/anthropic/  Anthropic Messages API backend
```

## Data flow (exec)

1. **CLI parses flags**
2. **Request is constructed** from prompt/instructions or `--input-json`
3. **Client sends request** to Responses API
4. **SSE stream parsed** into JSONL events
5. **Tool events** optionally handled in a loop
6. **Output streamed** to stdout (JSONL or text)

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
            ┌────────────────┼────────────────┐
            ▼                ▼                ▼
    ┌───────────────┐ ┌───────────────┐ ┌───────────┐
    │    Codex      │ │   Anthropic   │ │  (future) │
    │  gpt-*, o1-*  │ │  claude-*     │ │           │
    └───────────────┘ └───────────────┘ └───────────┘
```

### Backend interface

```go
type Backend interface {
    Name() string
    StreamResponses(ctx, req, onEvent) error
    StreamAndCollect(ctx, req) (StreamResult, error)
}
```

### Routing rules

- Model prefix matching: `claude-*` → Anthropic, `gpt-*` → Codex
- Aliases: `sonnet` → `claude-sonnet-4-5-20250929`
- Fallback to default backend for unknown models

### Anthropic backend

Uses the official `anthropic-sdk-go` with OAuth authentication:
- Reads tokens from `~/.claude/.credentials.json` (Claude Code)
- Requires `anthropic-beta: oauth-2025-04-20` header
- Translates OpenAI format ↔ Anthropic Messages API

## Design goals
- **Minimal surface area**
- **Deterministic output** for tests
- **Easy debugging** via log files
- **No heavy SDK dependencies**
