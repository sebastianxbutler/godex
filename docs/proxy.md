# Godex Proxy

The Godex proxy exposes an OpenAI-compatible API and forwards requests to the Codex
Responses backend. It supports `/v1/responses`, `/v1/models`, and
`/v1/chat/completions`, with SSE streaming, tool calls, and prompt cache suppression.

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

## Authentication

By default the proxy requires `Authorization: Bearer <api-key>`.

To allow any bearer token (useful for local-only):

```bash
./godex proxy --allow-any-key
```

## Flags

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
