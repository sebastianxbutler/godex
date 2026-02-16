# Godex Proxy

The Godex proxy exposes an OpenAI‑compatible API and forwards requests to the Codex
Responses backend. It supports `/v1/responses`, `/v1/models`, and
`/v1/chat/completions`, with SSE streaming, tool calls, and prompt cache reuse.

## Endpoints

- `GET /v1/models`
- `GET /v1/pricing`
- `POST /v1/responses`
- `POST /v1/chat/completions`
- `GET /health`

## Pricing endpoint

`GET /v1/pricing`

- Proxies pricing data from token‑meter when available.
- Returns a fallback JSON message if token‑meter is unavailable or payments are disabled.

Examples:
```bash
curl http://127.0.0.1:39001/v1/pricing
```

```json
{"status":"ok","btc_usd":68840.9,"updated_at":"2026-02-13T23:25:10Z","prices":{"gpt-5.2-codex":{"input_usd_per_1m":1.75,"cached_input_usd_per_1m":0.175,"output_usd_per_1m":14.0,"billing_mode":"blended"}}}
```

```json
{"status":"unavailable","message":"token-meter not running"}
```

```json
{"status":"disabled","message":"payments not enabled"}
```

## Model probe

Check if a model exists and get routing info:

### Endpoint: `GET /v1/models/{model_id}`

```bash
curl http://127.0.0.1:39001/v1/models/sonnet -H "Authorization: Bearer $KEY"
```

Response:
```json
{
  "id": "claude-sonnet-4-5-20250929",
  "object": "model",
  "owned_by": "godex",
  "display_name": "Claude Sonnet 4.5",
  "backend": "anthropic",
  "alias": "sonnet"
}
```

- **404** if model not found
- **alias** field present if input was an alias
- **backend** shows which backend handles the model

### CLI: `godex probe`

```bash
# Check a model
godex probe sonnet
# OK: sonnet → claude-sonnet-4-5-20250929 (anthropic) [Claude Sonnet 4.5]

# JSON output
godex probe o3-mini --json
# {"id":"o3-mini","object":"model","owned_by":"godex","backend":"codex"}

# Custom proxy URL
godex probe claude-opus-4-5 --url http://localhost:39001 --key $KEY
```

Options:
- `--url` — proxy URL (default: `http://127.0.0.1:39001`)
- `--key` — API key (or set `GODEX_API_KEY`)
- `--json` — output as JSON

## Multi-model support

Godex can serve multiple models. Configure in YAML:

```yaml
proxy:
  model: gpt-5.2-codex           # default model
  base_url: https://chatgpt.com/backend-api/codex
  models:
    - id: gpt-5.2-codex
    - id: chat-gpt-5-3
      base_url: https://other-backend/api  # optional per-model URL
```

- `GET /v1/models` returns all configured models
- Requests can specify any available model
- If model not in list, returns 400 error
- Each model can have its own `base_url` (falls back to default)

## Multi-backend support

Godex supports routing requests to different LLM backends based on model name.

### Configuration

```yaml
proxy:
  backends:
    codex:
      enabled: true
      base_url: "https://chatgpt.com/backend-api/codex"
    anthropic:
      enabled: true
      # Uses ~/.claude/.credentials.json by default (Claude Code OAuth)
      credentials_path: ""
      default_max_tokens: 4096
    routing:
      default: "codex"  # fallback for unknown models
      patterns:
        anthropic: ["claude-", "sonnet", "opus", "haiku"]
        codex: ["gpt-", "o1-", "o3-", "codex-"]
      aliases:
        sonnet: "claude-sonnet-4-5-20250929"
        opus: "claude-opus-4-5"
        haiku: "claude-haiku-4-5"
```

### Routing behavior

1. **Model alias expansion**: `sonnet` → `claude-sonnet-4-5-20250929`
2. **Pattern matching**: `claude-*` → Anthropic backend
3. **Fallback**: Unknown models go to default backend

### Anthropic backend

The Anthropic backend uses the official `anthropic-sdk-go` SDK:
- **Authentication**: OAuth tokens from Claude Code (`~/.claude/.credentials.json`)
- **API**: Anthropic Messages API (`api.anthropic.com/v1/messages`)
- **Features**: Streaming, tool calls, all Claude models

Requirements:
- Active Claude Code subscription (Max or Pro)
- Valid OAuth token (auto-refreshed by Claude Code CLI)

### Example: Using Claude via godex

```bash
# Start proxy with Anthropic enabled
./godex proxy --config config.yaml

# Call with model alias
curl http://127.0.0.1:39001/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"sonnet","messages":[{"role":"user","content":"Hello"}]}'

# Or with full model name
curl http://127.0.0.1:39001/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-5-20250929","messages":[{"role":"user","content":"Hello"}]}'
```

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

# Bring your own key (BYOK)
./godex proxy keys add --label "agent-x" --key "gxk_..."

# Set a key expiration
./godex proxy keys add --label "agent-exp" --expires-in 24h

# List keys
./godex proxy keys list

# Revoke or rotate
./godex proxy keys revoke key_abc123
./godex proxy keys update key_abc123 --label "agent-new" --rate 30/m --burst 5 --quota-tokens 100000 --expires-in 72h
./godex proxy keys rotate key_abc123
```

Keys are stored hashed (no plaintext) in:
- `~/.codex/proxy-keys.json` (or `--keys-path`)

If `--expires-in` is set, keys expire automatically and are pruned on proxy restart.

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

# Reset usage for a key
./godex proxy usage reset key_abc123
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
- `--stats-path` (default: empty; disables history)
- `--stats-summary` (default: `~/.codex/proxy-usage.json`)
- `--stats-max-bytes` (default: `10485760`)
- `--stats-max-backups` (default: `3`)
- `--events-path` (default: `~/.codex/proxy-events.jsonl`)
- `--events-max-bytes` (default: `1048576`)
- `--events-max-backups` (default: `3`)
- `--meter-window` (default: empty; disables windowed reset)

When `--stats-path` is set, JSONL history is written and rotated to `.1`, `.2`, ...
The summary file always tracks totals. Reset events are written to `--events-path`
(using a rolling cache). When `--meter-window` is set, totals reset at the end of
each window.

Metering totals are rebuilt on startup by scanning the usage log within the window.

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
- `GODEX_PROXY_STATS_SUMMARY`
- `GODEX_PROXY_STATS_MAX_BYTES`
- `GODEX_PROXY_STATS_MAX_BACKUPS`
- `GODEX_PROXY_EVENTS_PATH`
- `GODEX_PROXY_EVENTS_MAX_BYTES`
- `GODEX_PROXY_EVENTS_MAX_BACKUPS`
- `GODEX_PROXY_METER_WINDOW`
- `GODEX_PROXY_METER_WINDOW`

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

## Payments (L402 via token-meter)

Godex delegates L402 challenges and redemption to **token-meter**. Godex remains authoritative for balances and allowances, while token-meter handles Lightning payments and pricing.

Configuration lives under `proxy.payments` in the config template.

### Setup guide
1) **Run token-meter** with phoenixd configured.
2) **Configure godex** to point at token-meter:
```yaml
proxy:
  payments:
    enabled: true
    provider: l402
    token_meter_url: "http://127.0.0.1:39900"
  admin_socket: "~/.godex/admin.sock"
```
3) **Start godex proxy**:
```bash
./godex proxy --config ~/.config/godex/config.yaml
```

### Usage guide
Usage is identical to L402 flows, but handled by token-meter:
- Missing key → 402 with `WWW-Authenticate: L402 ...`
- Pay invoice → retry with `Authorization: L402 <macaroon>:<preimage>`
- Receive API key + tokens, then call with `Authorization: Bearer <api_key>`

See token-meter docs for payment configuration details.

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
