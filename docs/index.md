# Godex Docs

Welcome to the Godex docs. Godex is a minimal Go client for the Codex (ChatGPT backend) Responses API with tool calls, SSE streaming, and an OpenAI‑compatible proxy.

## What this covers
- Core CLI usage (`exec`, `proxy`)
- Tool calling and tool loops
- Logging and observability
- L402 Lightning payments
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

## Sections
- **CLI Usage:** `cli.md`
- **Proxy (payments via token-meter):** `proxy.md`
- **Logging & Observability:** `logging.md`
- **Testing:** `testing.md`
- **Debugging:** `debugging.md`
- **Architecture:** `architecture.md`
- **Cookbook:** `cookbook.md`
- **Glossary:** `glossary.md`

## Related docs
- `wire.md` — wire protocol spec
- `proxy.md` — OpenAI‑compatible proxy
