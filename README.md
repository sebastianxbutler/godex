# godex

Minimal Go client for Codex (ChatGPT backend) Responses API with tool calls.

## Docs
- `protocol-spec.md`
- `sdk-outline.md`

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
```

## Examples
- `examples/basic` — minimal request
- `examples/tool-loop` — tool call + follow‑up
- `examples/web-search` — web_search tool

## Test harnesses
- `test_request.py`
- `test_web_search.py`
- `edge_cases.py`
