# Logging & Observability

Godex is designed for deterministic logging when you need to debug provider calls.

## Request/response logs

### Exec
```bash
./godex exec --prompt "Hello" --log-requests /tmp/godex-request.json --log-responses /tmp/godex-response.jsonl
```

- **Request log**: JSON payload sent to the Responses API.
- **Response log**: JSONL stream events returned by the API (SSE normalized).

### Proxy
```bash
./godex proxy --log-requests --log-level debug
```

Proxy logs include HTTP request/response details and upstream errors.

## JSONL stream format
When `--json` is enabled, `exec` outputs a JSONL stream of SSE events:
- `response.created`
- `response.output_text.delta`
- `response.output_item.added`
- `response.output_item.done`
- `response.completed`
- `error`

Use this stream to build tool‑aware consumers.

## Common log locations
- `/tmp/godex-request.json` — request body (recommended for tests)
- `/tmp/godex-response.jsonl` — stream event log

## Tips
- Use `--log-requests` when testing prompt compilation.
- Use `--log-responses` to validate tool call sequences.
- Pair with `--mock` to generate deterministic streams for CI.
