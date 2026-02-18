# Glossary

## Core Concepts

**Responses API** — ChatGPT backend API used by Codex. Supports streaming, tools, and input items.

**Messages API** — Anthropic's API for Claude models. Godex translates to/from OpenAI format.

**SSE (Server‑Sent Events)** — Streaming protocol used by both Responses API and Anthropic API.

**Tool Call** — Model output requesting a function execution (`function_call`).

**Tool Result** — Follow‑up input item containing `function_call_output`.

**Input Items** — Structured inputs to Responses API: message, function_call, function_call_output.

**Wire protocol** — CLI interface spec for provider‑agnostic tooling (Codex/Claude).

**Proxy** — OpenAI‑compatible HTTP server that forwards to LLM backends.

**JSONL** — JSON Lines format; one JSON object per line.

**Auto‑tools** — Godex mode that runs a tool loop automatically using `--tool-output`.

**Mock Mode** — Godex mode that emits synthetic streams for deterministic tests.

## Multi-Backend

**Backend** — An LLM provider implementation (Codex, Anthropic, OpenAPI). Implements the `Backend` interface.

**Router** — Component that selects which backend handles a request based on model name patterns.

**Model Alias** — Shorthand name that resolves to a full model ID (e.g., `sonnet` → `claude-sonnet-4-5-20250929`, `gemini` → `gemini-2.5-pro`, `flash` → `gemini-2.5-flash`).

**Dynamic Model Discovery** — Querying backends at runtime for available models (cached 5 min).

**OpenAPI Backend** — The generic OpenAI-compatible backend in `pkg/harness/openai/`. Used for Gemini, Groq, vLLM, Ollama, and any user-defined custom backend that speaks the OpenAI wire format. Formerly named `openai/` (renamed to reflect it targets the protocol, not the service).

**Custom Backend** — A user-defined backend entry under `proxy.backends.custom` in the config. Backed by the OpenAPI backend implementation. Custom backends that fail to initialize (e.g., missing API key env var) are skipped with a warning rather than crashing the proxy.

**Gemini** — Google's Gemini family of models, accessed via the OpenAPI backend at `https://generativelanguage.googleapis.com/v1beta/openai`. Requires a `GEMINI_API_KEY`. Aliases: `gemini` → `gemini-2.5-pro`, `flash` → `gemini-2.5-flash`.

**Generic Tool Loop** — `backend.RunToolLoop()` in `pkg/harness/toolloop.go`. A backend-agnostic tool execution loop that works with any `Backend` interface implementation. Calls `StreamAndCollect`, dispatches tool calls via a `ToolHandler`, and sends follow-up requests until the model produces a final response or `MaxSteps` is reached.

**Provider Key (X-Provider-Key)** — A per-request API key override for non-OAuth backends. Supplied as the `X-Provider-Key` HTTP header on proxy requests, or via the `--provider-key` CLI flag for `godex exec`. Injected into the request context via `backend.WithProviderKey()` and extracted by the OpenAPI backend client. Takes precedence over the `key_env` configured value.

## Authentication

**OAuth** — Authentication flow used by Claude Code for Anthropic API access. Tokens stored in `~/.claude/.credentials.json`.

**Codex Auth** — OpenAI-style tokens stored in `~/.codex/auth.json`. Created by `codex auth`.

**Claude Code** — Anthropic's CLI tool. Provides OAuth tokens that godex uses for Claude models.

## Payments

**L402** — Lightning-based HTTP 402 payment protocol for API access.

**token-meter** — External service that handles L402 challenges, pricing, and Lightning payments.

**Admin Socket** — Unix socket API for programmatic key/balance management.

**Proxy Mode** — Default system prompt mode for the Codex harness. Preserves the Codex base prompt (personality, planning, task execution, formatting) but dynamically replaces tool-specific sections with caller-provided tool context. Used by both `godex exec` and `godex proxy`.

**Native Tools Mode** — Optional system prompt mode enabled via `--native-tools` flag or `native_tools: true` in config. Uses the full Codex base prompt including shell, apply_patch, and update_plan tool instructions. Useful when running Codex as a standalone coding agent.

**Prompt Markers** — HTML comment tags in `base_instructions.md` that delimit replaceable sections (e.g., `<!-- TOOL_GUIDELINES_START -->` / `<!-- TOOL_GUIDELINES_END -->`). The harness uses these to swap tool-specific content in proxy mode. When updating to a new Codex prompt version, preserve the markers.

**Harness** — A pluggable backend implementation in `pkg/harness/` that translates between the unified godex API and a specific LLM provider's format. Each harness owns model discovery, alias expansion, system prompt construction, and SSE event translation. Current harnesses: Codex, Claude, OpenAI-compatible (Gemini/Groq/etc).
