# godex

**Multi-backend LLM proxy with OpenAI-compatible API.**

Godex is a lightweight Go proxy that routes requests to multiple LLM backends:
- **Codex/ChatGPT** — GPT models via ChatGPT backend API
- **Anthropic** — Claude models via official Anthropic SDK (OAuth supported)
- **Gemini** — Google Gemini models via OpenAI-compatible endpoint
- **Custom** — Any OpenAI-compatible provider (Groq, Ollama, vLLM, …)

Features:
- **Harness architecture** — pluggable backends with structured event streaming
- **Dynamic system prompts** — marker-based section replacement for Codex base prompt
- **Proxy mode** (default) — Codex base prompt preserved, tool sections swapped for caller's tools
- **Native tools mode** — full Codex prompt with shell/apply_patch (`--native-tools`)
- Streaming SSE responses
- Tool calls + tool loops (generic `harness.RunToolLoop()`)
- Automatic model routing by prefix
- Model aliases (`sonnet` → `claude-sonnet-4-6`, `gemini` → `gemini-2.5-pro`, `flash` → `gemini-2.5-flash`)
- Dynamic model discovery from backends
- OpenAI-compatible `/v1/chat/completions` and `/v1/responses` endpoints
- `--provider-key` / `X-Provider-Key` for per-request API key override
- Audit logging (JSONL with rotation)

If you want a unified API for multiple LLM providers, this is it.

---

## Quick Start

### 1. Install
```bash
# From source
go install github.com/sebastianxbutler/godex/cmd/godex@latest

# Or download binary
curl -L -o godex https://github.com/sebastianxbutler/godex/releases/latest/download/godex-linux-amd64
chmod +x godex
```

### 2. Authenticate
```bash
# For GPT models (Codex)
npm install -g @anthropic/codex && codex auth

# For Claude models (Anthropic)
curl -fsSL https://claude.ai/install.sh | bash && claude auth login

# Verify
./godex auth status
```

### 3. Run
```bash
# Start proxy
./godex proxy --api-key "my-local-key"

# Test it
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer my-local-key" \
  -d '{"model":"sonnet","messages":[{"role":"user","content":"Hello!"}]}'
```

---

## CLI Examples

```bash
# Simple prompt (default Codex backend)
./godex exec --prompt "Hello"

# Anthropic Claude via alias
./godex exec --prompt "Explain monads" --model sonnet

# Gemini via alias (reads GEMINI_API_KEY from env)
./godex exec --prompt "What is 2+2?" --model gemini

# Gemini with explicit provider key
./godex exec --prompt "Quick summary" --model flash --provider-key "$GEMINI_API_KEY"

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
| `gpt-*`, `o1-*`, `o3-*`, `codex-*` | Codex | `gpt-5.2-codex`, `o3-mini` |
| `claude-*`, `sonnet`, `opus`, `haiku` | Anthropic | `claude-sonnet-4-5-20250929`, `sonnet` |
| `gemini-*`, `gemini`, `flash` | Gemini | `gemini-2.5-pro`, `gemini-2.5-flash` |
| Custom patterns from config | Custom OpenAPI | `groq/llama-3.3-70b`, `llama-3.2-3b` |

### Codex Setup

Godex uses Codex CLI OAuth tokens:

```bash
# Install Codex CLI (requires Node.js)
npm install -g @anthropic/codex

# Authenticate
codex auth

# Tokens are read from ~/.codex/auth.json
```

### Anthropic Setup

Godex uses Claude Code OAuth tokens (no API key needed):

```bash
# Install Claude Code
curl -fsSL https://claude.ai/install.sh | bash

# Authenticate
claude auth login

# Tokens are read from ~/.claude/.credentials.json
# Godex auto-refreshes expired tokens!
```

### Gemini Setup

Godex routes `gemini-*` models through Google's OpenAI-compatible endpoint:

```bash
# Set your Gemini API key
export GEMINI_API_KEY="AIza..."

# Or pass per-request via --provider-key (exec) or X-Provider-Key header (proxy)
./godex exec --prompt "Hello" --model gemini --provider-key "$GEMINI_API_KEY"
```

No config changes needed for basic Gemini usage — the `gemini-*` prefix is
built-in. For proxy use, add a `gemini` custom backend entry to your config
(see `docs/config.template.yaml`).

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

# Use Gemini with per-request key (X-Provider-Key)
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H "X-Provider-Key: $GEMINI_API_KEY" \
  -d '{"model": "gemini-2.5-pro", "messages": [{"role": "user", "content": "Hi"}]}'

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

### Check Auth Status
```bash
# Verify both backends are configured
./godex auth status

# Example output:
# Codex:       ✅ configured
# Anthropic:   ✅ configured (expires 2026-02-16 22:58)
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
