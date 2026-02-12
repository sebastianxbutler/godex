# Debugging

## Common failures

### “no messages provided”
The Responses API requires either:
- `input` items (message/function_call/function_call_output), or
- `prompt` + `instructions` for older flows.

Fix: pass `--prompt` or use `--input-json`.

### “Invalid value: 'input_text'… supported values are output_text/refusal”
This is caused by sending assistant content using `input_text`.

Fix: assistant messages must use `output_text` (Godex already normalizes).

### Tool call never completes
The model can emit `function_call` without a follow‑up tool result.

Fix: in auto‑loop, supply `--tool-output` or implement a real tool runner.

### Streaming never ends
If the stream ends without `response.completed`, check upstream transport issues.

Fix: enable `--log-responses` and inspect the JSONL for an `error` event.

## Debug checklist
- ✅ Enable `--log-requests` and inspect payload
- ✅ Enable `--log-responses` for SSE stream
- ✅ Re‑run with `--mock` to isolate client vs upstream
- ✅ Validate `auth.json` (token freshness, proper fields)

## Auth debugging
Godex reads `~/.codex/auth.json` by default. If calls fail with 401/403:
- ensure `access_token` is valid
- ensure `id_token` is present (string or object form)
- re‑run refresh flow if needed

## Proxy debugging
- Use `--log-level debug`
- Try `--allow-any-key` in local dev
- Confirm the proxy can read `auth.json`
