# CLI Usage

Godex exposes these primary commands:

- `godex exec` — run a single Responses API call (supports tools + streaming)
- `godex proxy` — run an OpenAI‑compatible proxy server
- `godex version` / `--version` — show build version

## `godex exec`

### Required
- `--prompt <text>` — user prompt (ignored if `--input-json` is used)

### Common flags
- `--model <id>` — model id (e.g., `gpt-5.2-codex`)
- `--instructions <text>` — system prompt
- `--append-system-prompt <text>` — appended system prompt
- `--session-id <id>` — optional session identifier
- `--image <path>` — attach image to the prompt
- `--web-search` — enable `web_search` tool
- `--tool <name:spec>` — add a tool schema (see below)
- `--auto-tools` — run tool loop automatically
- `--tool-output name=value` — provide tool outputs for auto loop
- `--tool-choice <choice>` — enforce tool selection (Wire)
- `--input-json <file>` — full Responses input items JSON
- `--json` — JSONL streaming output (for programmatic parsing)
- `--mock` — enable mock mode
- `--mock-mode <echo|text|tool-call|tool-loop>` — mock flavor

### Tool schema specification

```
--tool add:json=schemas/add.json
```

Formats:
- `name:json=<path>` — JSON schema file for a function tool
- `name:inline=<json>` — inline JSON schema

### Tool loop usage

```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5
```

To echo args:
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=$args
```

### Input‑item mode
If you already have Responses input items (message/function_call/function_call_output), use:

```bash
./godex exec --input-json ./input.json
```

This bypasses prompt building and uses your exact input items.

## `godex proxy`

Run an OpenAI‑compatible proxy that forwards to the Responses API.

```bash
./godex proxy --api-key "local-dev-key"
```

Key management:
```bash
./godex proxy keys add --label "agent-a" --rate 60/m --burst 10
./godex proxy keys add --label "agent-x" --key "gxk_..."   # BYOK
./godex proxy keys add --label "agent-exp" --expires-in 24h   # expires after 24h
./godex proxy keys list
./godex proxy keys update key_abc123 --label "agent-new" --rate 30/m --burst 5 --quota-tokens 100000 --expires-in 72h
./godex proxy keys revoke key_abc123
./godex proxy keys rotate key_abc123
```

Usage reporting:
```bash
./godex proxy usage list --since 24h
./godex proxy usage show key_abc123
```

Useful flags:
- `--listen :8080` — bind address
- `--allow-any-key` — accept any incoming API key (dev only)
- `--auth-path <file>` — override auth.json path
- `--log-requests` — write request logs
- `--log-level <debug|info|warn|error>` — verbosity
- `--keys-path <file>` — key store path
- `--rate <spec>` / `--burst <n>` — rate limits (default 60/m, burst 10)
- `--quota-tokens <n>` — per‑key token quota
- `--stats-path <file>` — usage JSONL path
- `--stats-max-bytes <n>` — rotate stats after size
- `--stats-max-backups <n>` — max rotated files
- `--meter-window <dur>` — metering window (resets totals each window)
- `--meter-window <duration>` — usage window (e.g., 24h)

See `docs/proxy.md` for full proxy documentation.

## Wire compliance
Godex supports Wire flags for compatibility with multi‑provider runners:
- `--tool-choice`, `--log-requests`, `--log-responses`, `--input-json`

See `docs/wire.md` for the full spec.
