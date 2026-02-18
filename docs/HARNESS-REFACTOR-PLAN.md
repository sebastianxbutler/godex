# godex Harness Refactor Plan

## Context

godex currently has `pkg/backend/{codex,anthropic,openapi}` which are thin API clients that translate between a common `protocol.ResponsesRequest` and each provider's wire format. They handle streaming and auth but don't inject provider-specific instructions, handle provider-specific tool formats, or expose structured agentic events.

The goal: evolve these into full **harnesses** — provider-aware agentic runtimes that can be imported independently by Chrysalis or any other consumer.

## Current State

```
pkg/
  backend/
    backend.go          # Backend interface (StreamResponses, StreamAndCollect, ListModels)
    router.go           # Model → backend routing
    registry.go         # Backend factory/registry
    toolloop.go         # Generic tool loop
    codex/
      client.go         # Codex/ChatGPT Responses API client
      toolloop.go       # Codex-specific tool loop (duplicate of generic)
    anthropic/
      client.go         # Anthropic Messages API client
      translate.go      # Responses → Messages format translation
      auth.go           # OAuth token management
    openapi/
      client.go         # Generic OpenAI Chat Completions client
  protocol/
    types.go            # Shared wire types (ResponsesRequest, StreamEvent, etc.)
  proxy/                # HTTP proxy server
  auth/                 # Auth store
  config/               # Configuration
  payments/             # Token metering
```

**13,551 lines of Go** across the project.

## Problems with Current Design

1. **Backends are just API clients** — they don't understand agentic patterns (thinking, plans, preambles, tool execution)
2. **No structured event types** — everything flows as `sse.Event` / `protocol.StreamEvent`, which is Codex-shaped. Claude thinking blocks, plan updates, etc. have no representation.
3. **No prompt injection** — backends don't inject provider-specific system prompts, permission instructions, or AGENTS.md context
4. **Tool format mismatch** — Codex uses freeform `apply_patch` tool, Claude uses standard function calling. No abstraction for this.
5. **Duplicated tool loop** — `backend/toolloop.go` and `codex/toolloop.go` are nearly identical
6. **Tight coupling** — consumers must import the whole package; can't use just the Claude harness

## Target Architecture

```
pkg/
  harness/
    harness.go          # Common Harness interface + Event types
    events.go           # Structured event types (Thinking, Text, ToolCall, PlanUpdate, etc.)
    prompt/
      prompt.go         # Prompt builder (instructions, permissions, environment context)
      templates/        # Provider-agnostic prompt templates (from Codex analysis)
    codex/
      harness.go        # Codex harness (Responses API, apply_patch, plans)
      prompt.go         # Codex-specific prompt patterns
      tools.go          # Freeform tool definitions (apply_patch grammar)
      client.go         # HTTP/WebSocket client (extracted from current)
    claude/
      harness.go        # Claude harness (Messages API, thinking, tool_use)
      prompt.go         # Claude-specific prompt patterns
      translate.go      # Format translation (current, cleaned up)
      client.go         # API client (extracted from current)
      auth.go           # OAuth tokens (current)
    openai/
      harness.go        # OpenAI-compatible harness (Chat Completions)
      translate.go      # Format translation (current, cleaned up)
      client.go         # API client (extracted from current)
  protocol/
    types.go            # Wire types (keep, extend)
  proxy/                # Proxy server (uses harness interface)
  router/               # Model → harness routing (extracted from backend/)
  auth/                 # Shared auth
  config/               # Configuration
  payments/             # Token metering
```

## The Harness Interface

```go
package harness

// Harness is the core interface that all provider harnesses implement.
// It handles the full agentic loop: prompt injection, streaming, tool
// execution, and structured event emission.
type Harness interface {
    // Name returns the harness identifier ("codex", "claude", "openai").
    Name() string

    // StreamTurn executes a single agentic turn and streams structured events.
    // The turn includes messages, tools, and configuration. The harness handles
    // provider-specific prompt injection, API calls, and event translation.
    StreamTurn(ctx context.Context, turn *Turn, onEvent func(Event) error) error

    // StreamAndCollect executes a turn and returns the collected result.
    StreamAndCollect(ctx context.Context, turn *Turn) (*TurnResult, error)

    // RunToolLoop executes the full agentic loop: model call → tool execution →
    // follow-up → ... until the model produces a final response or max steps.
    RunToolLoop(ctx context.Context, turn *Turn, handler ToolHandler, opts LoopOptions) (*TurnResult, error)

    // ListModels returns available models for this harness.
    ListModels(ctx context.Context) ([]ModelInfo, error)
}

// Turn represents a single agentic turn request.
type Turn struct {
    Model        string
    Instructions string            // Base system instructions (can be overridden by harness)
    Messages     []Message         // Conversation history
    Tools        []ToolSpec        // Available tools
    Environment  *EnvironmentCtx   // cwd, shell, network constraints
    Permissions  *PermissionsCtx   // Sandbox mode, approval policy
    Reasoning    *ReasoningConfig  // Effort level, summaries
    UserContext  *UserContext       // AGENTS.md, SOUL.md content if available
    Metadata     map[string]any    // Harness-specific extensions
}

// Event is a structured agentic event emitted during a turn.
type Event struct {
    Kind      EventKind
    Timestamp time.Time

    // Populated based on Kind:
    Text       *TextEvent       // Kind == EventText
    Thinking   *ThinkingEvent   // Kind == EventThinking
    ToolCall   *ToolCallEvent   // Kind == EventToolCall
    ToolResult *ToolResultEvent // Kind == EventToolResult
    Plan       *PlanEvent       // Kind == EventPlanUpdate
    Preamble   *PreambleEvent   // Kind == EventPreamble
    Usage      *UsageEvent      // Kind == EventUsage
    Error      *ErrorEvent      // Kind == EventError
}

type EventKind int

const (
    EventText       EventKind = iota // Model text output
    EventThinking                     // Thinking/reasoning (Claude extended thinking, Codex reasoning)
    EventToolCall                     // Model wants to call a tool
    EventToolResult                   // Tool execution result
    EventPlanUpdate                   // Plan step added/completed (Codex update_plan)
    EventPreamble                     // Brief status update before action
    EventUsage                        // Token usage info
    EventError                        // Error during turn
    EventDone                         // Turn complete
)
```

## Testing & Debugging Infrastructure

Every harness gets two companion tools: a **mock** and a **logger**. These live in the shared `pkg/harness/` package so they work uniformly across all providers.

### Mock Harness

Each harness provides a mock implementation for deterministic testing without API calls.

```go
package harness

// MockConfig configures a mock harness.
type MockConfig struct {
    // Scripted event sequences returned by StreamTurn.
    // Each call to StreamTurn pops the next sequence.
    Responses [][]Event

    // Simulated latency between events.
    EventDelay time.Duration

    // If set, StreamTurn returns this error after N events.
    FailAfterN int
    FailErr    error

    // Record all turns for assertion.
    Record bool
}

// Mock implements Harness with scripted responses.
type Mock struct { ... }

func NewMock(cfg MockConfig) *Mock

// Recorded returns all Turn requests received (when Record is true).
func (m *Mock) Recorded() []*Turn
```

Each provider harness also exposes a `NewMock()` that pre-loads provider-specific event patterns:

```go
// Codex mock that simulates apply_patch + plan update flow
mock := codex.NewMock(codex.WithApplyPatchFlow("file.go", "+new line"))

// Claude mock that simulates thinking + tool_use flow
mock := claude.NewMock(claude.WithThinkingFlow("reasoning here..."))
```

### Event Logger

A logging wrapper that records the full request/response lifecycle for debugging.

```go
package harness

type LoggerConfig struct {
    // Output directory. One .jsonl file per turn.
    Dir string

    // Format: "jsonl" (machine-readable) or "pretty" (human-readable).
    Format string // "jsonl" | "pretty"

    // Redact sensitive fields (API keys, auth tokens) from logged requests.
    Redact bool

    // Real-time event hook for live debugging.
    OnEvent func(Event)

    // Also log the raw provider wire data (SSE lines, HTTP headers).
    // Useful for debugging translation issues.
    IncludeWire bool
}

// WithLogger wraps any Harness with event logging.
func WithLogger(h Harness, cfg LoggerConfig) Harness
```

A logged turn produces a file like:

```jsonl
{"ts":"...","type":"turn_start","turn":{"model":"gpt-5.2-codex","instructions":"...","messages":[...]}}
{"ts":"...","type":"event","kind":"preamble","text":"Checking the test files...","latency_ms":245}
{"ts":"...","type":"event","kind":"tool_call","name":"shell","arguments":"{\"command\":\"pytest\"}","latency_ms":120}
{"ts":"...","type":"event","kind":"tool_result","call_id":"...","output":"3 passed","latency_ms":2100}
{"ts":"...","type":"event","kind":"text","text":"All tests pass.","latency_ms":340}
{"ts":"...","type":"turn_end","usage":{"input_tokens":1200,"output_tokens":85},"total_ms":2805}
```

### Replay

Logged turns can be replayed through the mock harness for offline reproduction:

```go
events, turn := harness.LoadLog("logs/codex/turn-2026-02-18-001.jsonl")
mock := harness.NewMockFromLog(events)

// Now re-run the same turn through your consumer code
// to reproduce the exact event sequence without API calls
```

This is critical for:
- **Debugging translation bugs** — "Claude sent X, harness emitted Y, but Z was expected"
- **Regression testing** — save problematic turns as test fixtures
- **Performance analysis** — timestamp gaps show where latency lives (API wait vs tool execution vs translation)

### Provider-Specific Debug Hooks

Each harness exposes an optional `DebugHook` for provider-specific introspection:

```go
type DebugHook struct {
    // Called with raw HTTP request before sending to provider.
    OnRequest func(method, url string, headers http.Header, body []byte)

    // Called with raw HTTP response / SSE line from provider.
    OnWire func(raw []byte)

    // Called with raw provider event before translation to Event.
    OnRawEvent func(providerEvent any)
}
```

This lets you see exactly what godex sends to OpenAI/Anthropic and what comes back, before any translation. When the Claude harness mistranslates a thinking block, you see the raw `ContentBlockStartEvent` alongside the translated `Event`.

## Migration Steps

### Phase 1: Define Interface + Events + Testing Tools (non-breaking)
- Create `pkg/harness/harness.go` with `Harness` interface
- Create `pkg/harness/events.go` with structured `Event` types
- Create `pkg/harness/mock.go` with `Mock` and `NewMockFromLog`
- Create `pkg/harness/logger.go` with `WithLogger` wrapper
- Create `pkg/harness/replay.go` with log loading/replay
- Create `pkg/harness/prompt/` with prompt builder and templates
- **No changes to existing code** — this is additive

### Phase 2: Codex Harness
- Create `pkg/harness/codex/harness.go` wrapping existing `pkg/backend/codex/client.go`
- Add Codex-specific prompt injection (from our source analysis):
  - `<permissions instructions>` with sandbox/approval policy
  - Environment context XML (`<environment_context>`)
  - AGENTS.md instructions wrapping
  - Collaboration mode (default/plan)
- Add `apply_patch` freeform tool definition
- Translate raw SSE events → structured `Event` stream
- Add plan update event parsing

### Phase 3: Claude Harness
- Create `pkg/harness/claude/harness.go` wrapping existing Anthropic client
- Add Claude-specific prompt patterns
- Map thinking blocks → `EventThinking`
- Handle Claude's tool_use natively (no translation to Codex format)
- Proper extended thinking configuration

### Phase 4: OpenAI-Compatible Harness
- Create `pkg/harness/openai/harness.go` wrapping existing openapi client
- Standard Chat Completions with function calling
- Generic prompt injection

### Phase 5: Router + Proxy Migration
- Extract `pkg/router/` from `pkg/backend/router.go`
- Update proxy to use `Harness` interface instead of `Backend`
- Deprecate `pkg/backend/` (keep as thin compatibility shim)
- Old `Backend` interface becomes internal detail of each harness

### Phase 6: Cleanup
- Remove duplicated tool loop code
- Remove `pkg/backend/` compatibility shims
- Update examples and docs
- Ensure each harness is independently importable

## Key Design Decisions

### 1. Harness vs Backend
The `Backend` interface stays internal to each harness as the raw API client. The `Harness` wraps a backend and adds agentic capabilities. This keeps the HTTP/streaming code separate from prompt engineering.

### 2. Structured Events over SSE
Consumers get typed `Event` structs, not raw SSE. This means:
- Claude thinking blocks are `EventThinking`, not hacked into text deltas
- Codex plan updates are `EventPlanUpdate`, not buried in tool calls
- Every harness emits the same event types; consumers don't need provider-specific parsing

### 3. Prompt Templates are Data
Prompt templates (permissions, environment context, collaboration modes) live as embedded `.md` files in `pkg/harness/prompt/templates/`. They're the same patterns we extracted from Codex source. Each harness can use the shared templates or override with provider-specific ones.

### 4. Independent Importability
After refactor, Chrysalis can do:
```go
import "godex/pkg/harness/codex"
import "godex/pkg/harness/claude"

// No need to import proxy, auth, payments, config
codexH := codex.New(codex.Config{...})
claudeH := claude.New(claude.Config{...})
```

### 5. Tool Handler Interface
```go
type ToolHandler interface {
    Handle(ctx context.Context, call ToolCallEvent) (*ToolResultEvent, error)
    Available() []ToolSpec  // Tools this handler provides
}
```
Chrysalis provides the tool handler; the harness manages the loop.

## Codex Prompt Patterns to Port

From our source analysis of Codex CLI (`~/repos/codex-source/`):

| Pattern | Source File | Priority |
|---------|-------------|----------|
| Base instructions (identity, coding guidelines, validation) | `protocol/src/prompts/base_instructions/default.md` | P0 |
| Permission instructions (sandbox, approval) | `protocol/src/prompts/permissions/` | P0 |
| Environment context XML | `core/src/environment_context.rs` | P0 |
| AGENTS.md injection format | `core/src/instructions/user_instructions.rs` | P1 |
| Collaboration modes (default, plan) | `core/templates/collaboration_mode/` | P1 |
| apply_patch freeform tool + Lark grammar | `core/src/tools/handlers/tool_apply_patch.lark` | P1 |
| Preamble message convention | Base instructions | P2 |
| update_plan tool spec | Base instructions | P2 |
| Personality spec injection | `protocol/src/models.rs` | P3 |
| MCP tool search instructions | `core/templates/search_tool/` | P3 |

## Estimated Effort

| Phase | Files | Lines (est) | Dependencies |
|-------|-------|-------------|--------------|
| 1: Interface + Events + Mock/Logger | 6-7 | ~600 | None |
| 2: Codex Harness | 4-5 | ~600 | Phase 1 |
| 3: Claude Harness | 3-4 | ~400 | Phase 1 |
| 4: OpenAI Harness | 2-3 | ~300 | Phase 1 |
| 5: Router Migration | 2-3 | ~200 | Phases 2-4 |
| 6: Cleanup | — | net negative | Phase 5 |

**Total: ~1,800 new lines**, with ~800 lines of old code removed = net ~1,000 lines added.

Each phase is independently shippable. Phase 1-2 is the critical path for Chrysalis integration.

## Open Questions

1. **WebSocket vs SSE for Codex?** — Codex CLI uses WebSocket for streaming. Current godex uses SSE (HTTP). The Responses API supports both. WebSocket has lower latency for multi-turn. Decision: start with SSE (simpler), add WebSocket in a follow-up.

2. **Should harnesses manage conversation history?** — Currently stateless (caller manages history). For proper agentic loops, the harness might need to track turn state. Decision: keep stateless for now; Chrysalis manages state.

3. **go.mod module path?** — Currently `module godex`. For independent importability, might need `module github.com/rimuru/godex` or similar. Decision: defer until we publish.

---

## Status: ✅ Complete (v0.8.4)

All phases implemented:
- Phase 1: Core harness interface + events + mock + logger + replay
- Phase 2: Codex harness with prompt system
- Phase 3: Claude harness for Anthropic Messages API
- Phase 4: OpenAI-compatible harness (Gemini, Groq, etc.)
- Phase 5: Router + Proxy migration to harness interface
- Phase 6: Legacy `pkg/backend/` removed

Post-refactor improvements (v0.8.1–v0.8.4):
- Fixed Gemini tool call flushing in OpenAI harness
- Dynamic system prompt with marker-based section replacement
- `--native-tools` flag for both exec and proxy
- `godex exec` routes through harness (consistent prompt treatment)
