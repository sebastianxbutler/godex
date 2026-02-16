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

### From source
```bash
make build
```

### From GitHub Releases
```bash
# Example: download the latest Linux amd64 binary
curl -L -o godex https://github.com/sebastianxbutler/godex/releases/latest/download/godex-linux-amd64
chmod +x godex
./godex --version
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

### L402 (Lightning) flags
```bash
./godex proxy --macaroon-key ~/.godex/macaroon-keys.json
./godex proxy --macaroon-rotate
./godex proxy --l402-redeemed-path ~/.godex/l402-redeemed.jsonl
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

- Config template: `docs/config.template.yaml`
- Wire spec: `docs/wire.md`
- Proxy guide: `docs/proxy.md`

## Examples
- `examples/basic`
- `examples/tool-loop`
- `examples/web-search`

## License
MIT. See `LICENSE`.
