# Godex Manual

Welcome to the Godex manual. Godex is a minimal Go client for the Codex (ChatGPT backend) Responses API with tool calls, SSE streaming, and an OpenAI‑compatible proxy.

## What this manual covers
- Core CLI usage (`exec`, `proxy`)
- Tool calling and tool loops
- Logging and observability
- Testing workflows and mock modes
- Debugging common failures
- Glossary of key terms

## Quick start
```bash
# Simple prompt
./godex exec --prompt "Hello"

# Use the web_search tool
./godex exec --prompt "Weather in Austin" --web-search

# Provide a function tool schema
./godex exec --prompt "Call add(a=2,b=3)" --tool add:json=schemas/add.json

# Auto tool loop with static outputs
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5
```

## Manual sections
- **CLI Usage:** `cli.md`
- **Logging & Observability:** `logging.md`
- **Testing:** `testing.md`
- **Debugging:** `debugging.md`
- **Architecture:** `architecture.md`
- **Cookbook:** `cookbook.md`
- **Protocol:** `protocol.md`
- **Glossary:** `glossary.md`

## Related docs
- `../intelliwire.md` — Intelliwire CLI interface
- `../proxy.md` — OpenAI‑compatible proxy
