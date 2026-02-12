# godex

Minimal Go client for Codex (ChatGPT backend) Responses API with tool calls and an
OpenAI-compatible proxy server.

## Docs
- `protocol-spec.md`
- `sdk-outline.md`
- `docs/proxy.md`

## CLI usage
```bash
# Simple prompt
./godex exec --prompt "Hello"

# Enable web_search tool
./godex exec --prompt "What is the weather in Austin, TX?" --web-search

# Provide a function tool schema
./godex exec --prompt "Call add(a=2,b=3)" --tool add:json=schemas/add.json

# Auto tool loop with static outputs
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5

# Echo tool args back
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=$args

# OpenClaw compatibility flags (accepted)
./godex exec --prompt "Hello" --model gpt-5.2-codex \
  --instructions "System prompt here" \
  --append-system-prompt "Extra system notes" \
  --session-id "optional-session-id" \
  --image /path/to/image.png

# Run the OpenAI-compatible proxy
./godex proxy --api-key "local-dev-key"

# Proxy with logging + allow any key
./godex proxy --allow-any-key --log-requests --log-level debug

# Proxy with a custom auth file
./godex proxy --api-key "local-dev-key" --auth-path /path/to/auth.json
```

## Examples
- `examples/basic` — minimal request
- `examples/tool-loop` — tool call + follow‑up
- `examples/web-search` — web_search tool

## Test harnesses
- `test_request.py`
- `test_web_search.py`
- `edge_cases.py`
