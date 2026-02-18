# Testing

Godex provides mock modes, E2E test scripts, and deterministic logs to make provider‑facing tests reliable.

## Unit tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Specific package
go test ./pkg/harness/...
```

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

## E2E testing

The `scripts/e2e-test.sh` script runs comprehensive end-to-end tests against both backends:

```bash
./scripts/e2e-test.sh
```

**What it tests:**
- Proxy startup and health check
- Codex backend (GPT models)
- Anthropic backend (Claude models)
- Model routing and alias expansion
- Streaming responses
- Tool calls

**Requirements:**
- Valid Codex credentials (`~/.codex/auth.json`)
- Valid Anthropic credentials (`~/.claude/.credentials.json`)
- Both backends enabled in config

## Golden log testing

Use `--log-requests` and `--log-responses` to capture fixtures for replay:

```bash
./godex exec --prompt "Hello" \
  --log-requests /tmp/godex-request.json \
  --log-responses /tmp/godex-response.jsonl
```

## Multi-backend testing

Test model routing manually:

```bash
# Verify backend selection
godex probe sonnet   # Should route to anthropic
godex probe gpt-5.2-codex  # Should route to codex

# Test via proxy
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -d '{"model":"sonnet","messages":[{"role":"user","content":"Hi"}]}'
```

## Integration tests in downstream systems

In Chrysalis, these logs are used to validate:
- Prompt compilation order
- Tool call wiring
- Streaming chunk behavior
- Error propagation

## Test coverage targets

| Package | Target | Current |
|---------|--------|---------|
| pkg/admin | 70%+ | 75.9% |
| pkg/client | 70%+ | 75.6% |
| pkg/sse | 70%+ | 74.6% |
| pkg/config | 50%+ | 55.0% |
| pkg/harness | 50%+ | 45.8% |
| pkg/proxy | 50%+ | 29.9% |

## Suggestions
- Keep mock‑mode tests in CI
- Use live API only in nightly runs
- Assert for **types and ordering** rather than exact text
- Run `godex auth status` before E2E tests to verify credentials
