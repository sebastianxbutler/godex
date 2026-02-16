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

**Backend** — An LLM provider implementation (Codex, Anthropic). Implements the `Backend` interface.

**Router** — Component that selects which backend handles a request based on model name patterns.

**Model Alias** — Shorthand name that resolves to a full model ID (e.g., `sonnet` → `claude-sonnet-4-5-20250929`).

**Dynamic Model Discovery** — Querying backends at runtime for available models (cached 5 min).

## Authentication

**OAuth** — Authentication flow used by Claude Code for Anthropic API access. Tokens stored in `~/.claude/.credentials.json`.

**Codex Auth** — OpenAI-style tokens stored in `~/.codex/auth.json`. Created by `codex auth`.

**Claude Code** — Anthropic's CLI tool. Provides OAuth tokens that godex uses for Claude models.

## Payments

**L402** — Lightning-based HTTP 402 payment protocol for API access.

**token-meter** — External service that handles L402 challenges, pricing, and Lightning payments.

**Admin Socket** — Unix socket API for programmatic key/balance management.
