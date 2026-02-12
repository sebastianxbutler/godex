# CLI Usage

Godex exposes two primary commands:

- `godex exec` — run a single Responses API call (supports tools + streaming)
- `godex proxy` — run an OpenAI‑compatible proxy server

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
- `--tool-choice <choice>` — enforce tool selection (Intelliwire)
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

Useful flags:
- `--listen :8080` — bind address
- `--allow-any-key` — accept any incoming API key (dev only)
- `--auth-path <file>` — override auth.json path
- `--log-requests` — write request logs
- `--log-level <debug|info|warn|error>` — verbosity

## Intelliwire compliance
Godex supports Intelliwire flags for compatibility with multi‑provider runners:
- `--tool-choice`, `--log-requests`, `--log-responses`, `--input-json`

See `docs/intelliwire.md` for the full spec.
