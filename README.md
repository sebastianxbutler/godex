# godex

**Minimal Go client for the Codex (ChatGPT backend) Responses API.**

Godex is designed to be small, reliable, and easy to embed in larger systems. It supports:
- streaming SSE responses
- tool calls + tool loops
- deterministic JSONL output
- an OpenAI‑compatible proxy
- Wire flags for multi‑provider compatibility

If you want a pragmatic, inspectable client for the Responses API, this is it.

---

## Quick Start

```bash
# Simple prompt
./godex exec --prompt "Hello"

# Enable web_search tool
./godex exec --prompt "Weather in Austin" --web-search

# Provide a function tool schema
./godex exec --prompt "Call add(a=2,b=3)" --tool add:json=schemas/add.json

# Auto tool loop with static outputs
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5
```

## Why godex
- **Minimal:** no heavy SDK dependency, pure Go.
- **Transparent:** JSONL streaming output for easy inspection.
- **Tool‑ready:** native tool calls, tool loop helpers, strict output schema.
- **Testable:** mock modes + request/response logs.
- **Compatible:** Wire flags + OpenAI‑compatible proxy.

## Install

```bash
go build ./cmd/godex
```

## Usage

### Exec
```bash
./godex exec --prompt "Hello" --json
```

### Proxy
```bash
./godex proxy --api-key "local-dev-key"
```

## Logging

```bash
./godex exec --prompt "Hello" \
  --log-requests /tmp/godex-request.json \
  --log-responses /tmp/godex-response.jsonl
```

## Testing

```bash
./godex exec --prompt "test" --mock --mock-mode tool-loop --json
```

## Docs

- Manual: `docs/manual/index.md`
- Wire spec: `docs/wire.md`
- Proxy guide: `docs/proxy.md`

## Examples
- `examples/basic`
- `examples/tool-loop`
- `examples/web-search`

## License
Private repository.
