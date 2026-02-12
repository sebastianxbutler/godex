# Godex Proxy

The Godex proxy exposes an OpenAI‑compatible API and forwards requests to the Codex
Responses backend. It supports `/v1/responses`, `/v1/models`, and
`/v1/chat/completions`, with SSE streaming, tool calls, and prompt cache reuse.

## Endpoints

- `GET /v1/models`
- `POST /v1/responses`
- `POST /v1/chat/completions`
- `GET /health`

## Quick start

```bash
# Start proxy on 127.0.0.1:39001
./godex proxy --api-key "local-dev-key"
```

## Authentication & API keys

By default the proxy requires `Authorization: Bearer <api-key>`.

### Managed keys
You can manage multiple keys via CLI:

```bash
# Create a key
./godex proxy keys add --label "agent-a" --rate 60/m --burst 10

# List keys
./godex proxy keys list

# Revoke or rotate
./godex proxy keys revoke key_abc123
./godex proxy keys rotate key_abc123
```

Keys are stored hashed (no plaintext) in:
- `~/.codex/proxy-keys.json` (or `--keys-path`)

### Allow any key (dev only)
```bash
./godex proxy --allow-any-key
```

## Rate limiting
Per‑key rate limiting is enforced with defaults:
- **60 requests/minute**, **burst 10**

Override globally:
```bash
./godex proxy --rate 120/m --burst 20
```

Override per key on creation:
```bash
./godex proxy keys add --label "agent-b" --rate 30/m --burst 5
```

When exceeded, proxy returns **429** with `Retry-After`.

## Token metering & quotas
Godex records per‑key token usage from upstream Responses usage fields.

- Usage log: `~/.codex/proxy-usage.jsonl` (or `--stats-path`)
- Optional per‑key quotas: `--quota-tokens`

Example:
```bash
./godex proxy keys add --label "agent-c" --quota-tokens 1000000
```

Quota exceeded returns **429**.

## Usage reports

```bash
# Summary for all keys (last 24h)
./godex proxy usage list --since 24h

# Summary for one key
./godex proxy usage show key_abc123
```

## Multi‑agent setup (example)

```bash
# Create keys for two agents
./godex proxy keys add --label "agent-a" --rate 60/m --burst 10
./godex proxy keys add --label "agent-b" --rate 30/m --burst 5

# Start proxy (local)
./godex proxy --listen 127.0.0.1:39001 --allow-any-key=false
```

Agents then set their API keys when calling the proxy:

```bash
export OPENAI_API_KEY="gxk_..."
export OPENAI_BASE_URL="http://127.0.0.1:39001/v1"
```

## Proxy flags

- `--listen` (default: `127.0.0.1:39001`)
- `--api-key` (required unless `--allow-any-key`)
- `--allow-any-key` (accept any bearer token)
- `--model` (default: `gpt-5.2-codex`)
- `--base-url` (default: `https://chatgpt.com/backend-api/codex`)
- `--allow-refresh` (enable network refresh on 401)
- `--auth-path` (override auth file; default `~/.codex/auth.json`)
- `--cache-ttl` (prompt cache TTL; default `6h`)
- `--log-level` (`debug|info|warn|error`, default `info`)
- `--log-requests` (emit per-request log lines)
- `--keys-path` (default: `~/.codex/proxy-keys.json`)
- `--rate` (default: `60/m`)
- `--burst` (default: `10`)
- `--quota-tokens` (default: `0` = disabled)
- `--stats-path` (default: `~/.codex/proxy-usage.jsonl`)

## Environment variables

- `GODEX_PROXY_LISTEN`
- `GODEX_PROXY_API_KEY`
- `GODEX_PROXY_ALLOW_ANY_KEY`
- `GODEX_PROXY_MODEL`
- `GODEX_PROXY_BASE_URL`
- `GODEX_PROXY_ALLOW_REFRESH`
- `GODEX_PROXY_AUTH_PATH`
- `GODEX_PROXY_CACHE_TTL`
- `GODEX_PROXY_LOG_LEVEL`
- `GODEX_PROXY_LOG_REQUESTS`
- `GODEX_PROXY_KEYS_PATH`
- `GODEX_PROXY_RATE`
- `GODEX_PROXY_BURST`
- `GODEX_PROXY_QUOTA_TOKENS`
- `GODEX_PROXY_STATS_PATH`

## Prompt cache reuse

The Codex backend requires `instructions` on every request. The proxy caches the
last system instructions per session key and reuses them if a client omits
`instructions` on a follow-up request. Session keys are derived from:

1. `user` field (preferred)
2. `x-openclaw-session-key` header
3. remote IP

## Tool calls

- Tool calls are supported in both `/v1/responses` and `/v1/chat/completions`.
- If a follow-up request includes only `function_call_output`, the proxy
  reconstructs the missing `function_call` from cache to satisfy the Codex
  backend’s requirement.

## OpenClaw integration

Example provider config:

```json5
models: {
  mode: "merge",
  providers: {
    godex: {
      baseUrl: "http://127.0.0.1:39001/v1",
      apiKey: "local-dev-key",
      api: "openai-responses",
      models: [
        { id: "gpt-5.2-codex", name: "Codex (godex proxy)" }
      ]
    }
  }
},
agents: {
  defaults: {
    model: { primary: "godex/gpt-5.2-codex" },
    models: {
      "godex/gpt-5.2-codex": { alias: "Codex (proxy)" }
    }
  }
}
```
