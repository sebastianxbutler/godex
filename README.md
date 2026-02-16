# godex

**Multi-backend LLM proxy with OpenAI-compatible API.**

Godex is a lightweight Go proxy that routes requests to multiple LLM backends:
- **Codex/ChatGPT** — GPT models via ChatGPT backend API
- **Anthropic** — Claude models via official Anthropic SDK (OAuth supported)

Features:
- Streaming SSE responses
- Tool calls + tool loops
- Automatic model routing by prefix
- Model aliases (`sonnet` → `claude-sonnet-4-5-20250929`)
- Dynamic model discovery from backends
- OpenAI-compatible `/v1/chat/completions` and `/v1/responses` endpoints
- Wire flags for multi-provider compatibility

If you want a unified API for multiple LLM providers, this is it.

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
- **Multi-backend:** Route to Codex or Anthropic with a single API.
- **Minimal:** No heavy SDK dependency, pure Go.
- **Transparent:** JSONL streaming output for easy inspection.
- **Tool‑ready:** Native tool calls, tool loop helpers, strict output schema.
- **Testable:** Mock modes + request/response logs.
- **Compatible:** Wire flags + OpenAI‑compatible proxy.

## Multi-Backend Support

Godex routes requests to different backends based on model name:

| Model Pattern | Backend | Examples |
|---------------|---------|----------|
| `claude-*`, `sonnet`, `opus`, `haiku` | Anthropic | `claude-sonnet-4-5-20250929`, `sonnet` |
| `gpt-*`, `o1-*`, `o3-*`, `codex-*` | Codex | `gpt-5.2-codex`, `o3-mini` |

### Anthropic Setup

Godex uses Claude Code OAuth tokens (no API key needed):

```bash
# Ensure you're logged into Claude Code
claude auth status

# Tokens are read from ~/.claude/.credentials.json
```

### Configuration

```yaml
# ~/.config/godex/config.yaml
proxy:
  backends:
    codex:
      enabled: true
    anthropic:
      enabled: true
    routing:
      default: codex
      patterns:
        anthropic: ["claude-", "sonnet", "opus", "haiku"]
        codex: ["gpt-", "o1-", "o3-"]
      aliases:
        sonnet: claude-sonnet-4-5-20250929
        opus: claude-opus-4-5
        haiku: claude-haiku-4-5
```

### Example Requests

```bash
# Use Codex (default)
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -d '{"model": "gpt-5.2-codex", "messages": [{"role": "user", "content": "Hi"}]}'

# Use Anthropic (via alias)
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -d '{"model": "sonnet", "messages": [{"role": "user", "content": "Hi"}]}'

# List available models (dynamic discovery)
curl http://localhost:39001/v1/models -H "Authorization: Bearer $KEY"
```

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
