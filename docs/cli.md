# CLI Usage

Godex exposes these primary commands:

- `godex exec` — run a single Responses API call (supports tools + streaming)
- `godex proxy` — run an OpenAI‑compatible proxy server
- `godex probe` — check if a model exists and get routing info
- `godex auth` — manage backend authentication
- `godex version` / `--version` — show build version

Config:
- `--config <path>` — use a specific YAML config file (default `~/.config/godex/config.yaml`)

## `godex exec`

### Required
- `--prompt <text>` — user prompt (ignored if `--input-json` is used)

### Common flags
- `--model <id>` — model id (e.g., `gpt-5.2-codex`, `sonnet`, `gemini`)
- `--provider-key <key>` — API key for non-OAuth backends (e.g., Gemini, Groq). Overrides `key_env` config.
- `--instructions <text>` — system prompt
- `--append-system-prompt <text>` — appended system prompt
- `--native-tools` — use Codex native tools (shell, apply_patch, update_plan) instead of proxy mode
- `--session-id <id>` — optional session identifier
- `--web-search` — enable `web_search` tool
- `--tool <name:spec>` — add a tool schema (see below)
- `--auto-tools` — run tool loop automatically
- `--tool-output name=value` — provide tool outputs for auto loop
- `--tool-choice <choice>` — enforce tool selection (Wire)
- `--input-json <file>` — full Responses input items JSON
- `--json` — JSONL streaming output (for programmatic parsing)
- `--mock` — enable mock mode
- `--mock-mode <echo|text|tool-call|tool-loop>` — mock flavor

### System prompt modes

By default, `godex exec` uses **proxy mode**: the Codex base prompt is included
(personality, planning, task execution, formatting guidance) but tool-specific
sections (shell, apply_patch, update_plan) are replaced with the caller's tools
and instructions.

Use `--native-tools` to get the full Codex prompt with all built-in tool
instructions. This is useful when running Codex as a standalone coding agent:

```bash
# Proxy mode (default) — caller controls tools
godex exec --prompt "Read /etc/hostname" --tool read_file:json=schema.json

# Native tools mode — full Codex with shell/apply_patch
godex exec --native-tools --prompt "Fix the bug in main.go"
```

### Multi-backend routing

`godex exec` routes requests to different backends based on the model name:

| Model pattern | Backend | Example models |
|---------------|---------|---------------|
| `gpt-*`, `o1-*`, `o3-*`, `codex-*` | Codex | `gpt-5.2-codex`, `o3-mini` |
| `claude-*`, `sonnet`, `opus`, `haiku` | Anthropic | `claude-sonnet-4-5-20250929`, `sonnet` |
| `gemini-*`, `gemini`, `flash` | Gemini (OpenAPI) | `gemini-2.5-pro`, `gemini-2.5-flash` |
| Custom patterns from config | Custom backend | `groq/llama-3.3-70b`, `llama-3.2-3b` |

Default (no match): Codex.

**Model aliases** (resolved before routing):

| Alias | Resolves to |
|-------|-------------|
| `sonnet` | `claude-sonnet-4-5-20250929` |
| `opus` | `claude-opus-4-5` |
| `haiku` | `claude-haiku-4-5` |
| `gemini` | `gemini-2.5-pro` |
| `flash` | `gemini-2.5-flash` |

### Tool schema specification

```
--tool add:json=schemas/add.json
```

Formats:
- `name:json=<path>` — JSON schema file for a function tool
- `name:inline=<json>` — inline JSON schema

### Examples

```bash
# Default model (Codex)
./godex exec --prompt "Hello"

# Anthropic Claude via alias
./godex exec --prompt "Explain monads" --model sonnet

# Anthropic full model name
./godex exec --prompt "Explain monads" --model claude-sonnet-4-5-20250929

# Gemini via alias (requires GEMINI_API_KEY env or --provider-key)
./godex exec --prompt "What is 2+2?" --model gemini

# Gemini with explicit key
./godex exec --prompt "What is 2+2?" --model gemini-2.5-flash --provider-key "$GEMINI_API_KEY"

# Gemini Flash alias
./godex exec --prompt "Quick summary" --model flash --provider-key "$GEMINI_API_KEY"

# Custom backend (Groq) with provider key
./godex exec --prompt "Hello" --model groq/llama-3.3-70b --provider-key "$GROQ_API_KEY"
```

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
- `--stats-path <file>` — usage JSONL history (empty disables)
- `--stats-summary <file>` — usage totals summary file
- `--stats-max-bytes <n>` — rotate history after size
- `--stats-max-backups <n>` — max rotated history files
- `--events-path <file>` — reset events JSONL file
- `--events-max-bytes <n>` — rotate events after size
- `--events-max-backups <n>` — max rotated events files
- `--meter-window <dur>` — metering window (resets totals each window)
- `--meter-window <duration>` — usage window (e.g., 24h)

See `docs/proxy.md` for full proxy documentation, including L402 payment flows.

## `godex auth`

Manage backend authentication credentials.

### `godex auth status`

Check current authentication status for all backends:

```bash
godex auth status
# godex authentication status
# ===========================
#
# Codex:       ✅ configured
#              Path: ~/.codex/auth.json
#
# Anthropic:   ✅ configured
#              Path: ~/.claude/.credentials.json
#              Expires: 2026-02-16 14:55
```

### `godex auth setup`

Interactive setup wizard for missing credentials:

```bash
godex auth setup
# Detects existing credentials
# Guides through missing ones (runs native CLI auth commands)
# Tests connections
```

The setup wizard will:
1. Check which backends are already configured
2. For missing backends, offer to run the native CLI auth command:
   - **Codex**: `codex auth` (requires `@anthropic/codex` npm package)
   - **Anthropic**: `claude auth login` (requires `@anthropic-ai/claude-code` npm package)
3. Show final status

### Credential Locations

| Backend | Path | Created By |
|---------|------|------------|
| Codex | `~/.codex/auth.json` | `codex auth` |
| Anthropic | `~/.claude/.credentials.json` | `claude auth login` |

## `godex probe`

Check if a model exists and which backend would handle it.

```bash
# Check a model (human-readable)
godex probe sonnet
# OK: sonnet → claude-sonnet-4-5-20250929 (anthropic) [Claude Sonnet 4.5]

# JSON output
godex probe --json o3-mini
# {"id":"o3-mini","object":"model","owned_by":"godex","backend":"codex","display_name":"o3 Mini"}

# With explicit key and URL
godex probe --url http://localhost:39001 --key $KEY sonnet

# Check non-existent model
godex probe fake-model
# ERROR: model "fake-model" not found
```

Flags (must come before model name):
- `--url <url>` — proxy URL (default: `http://127.0.0.1:39001`)
- `--key <key>` — API key (or set `GODEX_API_KEY` env var)
- `--json` — output as JSON

Exit codes:
- `0` — model found
- `1` — model not found or error

## Wire compliance
Godex supports Wire flags for compatibility with multi‑provider runners:
- `--tool-choice`, `--log-requests`, `--log-responses`, `--input-json`

See `docs/wire.md` for the full spec.
