# Testing

Godex provides mock modes and deterministic logs to make provider‑facing tests reliable.

## Mock modes

```bash
./godex exec --prompt "test" --mock --mock-mode echo --json
./godex exec --prompt "test" --mock --mock-mode text --json
./godex exec --prompt "test" --mock --mock-mode tool-call --json
./godex exec --prompt "test" --mock --mock-mode tool-loop --json
```

**Modes:**
- `echo` — returns the request JSON as text
- `text` — emits a plain text response
- `tool-call` — emits a single function_call
- `tool-loop` — emits tool_call + output_text (tool loop shape)

## Golden log testing
Use `--log-requests` and `--log-responses` to capture fixtures for replay:

```bash
./godex exec --prompt "Hello" \
  --log-requests /tmp/godex-request.json \
  --log-responses /tmp/godex-response.jsonl
```

## Integration tests in downstream systems
In Chrysalis, these logs are used to validate:
- Prompt compilation order
- Tool call wiring
- Streaming chunk behavior
- Error propagation

## Suggestions
- Keep mock‑mode tests in CI
- Use live API only in nightly runs
- Assert for **types and ordering** rather than exact text
