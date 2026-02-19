# Debugging

## Common failures

### "no messages provided"
The Responses API requires either:
- `input` items (message/function_call/function_call_output), or
- `prompt` + `instructions` for older flows.

Fix: pass `--prompt` or use `--input-json`.

### "Invalid value: 'input_text'… supported values are output_text/refusal"
This is caused by sending assistant content using `input_text`.

Fix: assistant messages must use `output_text` (Godex already normalizes).

### Tool call never completes
The model can emit `function_call` without a follow‑up tool result.

Fix: in auto‑loop, supply `--tool-output` or implement a real tool runner.

### Streaming never ends
If the stream ends without `response.completed`, check upstream transport issues.

Fix: enable `--log-responses` and inspect the JSONL for an `error` event.

### "model not found" on Claude models
The Anthropic backend may not be enabled, or credentials are missing.

Fix:
```bash
# Check auth status
godex auth status

# Enable in config
backends:
  anthropic:
    enabled: true
```

### "oauth token expired" (Anthropic)
Claude Code OAuth tokens expire periodically (typically every ~1 hour).

**Godex handles this automatically!** As of v0.5.5, godex will:
1. Detect expired tokens
2. Use the refresh token to get a new access token
3. Save the refreshed credentials to disk

If automatic refresh fails (e.g., refresh token also expired), re-authenticate:
```bash
claude auth login
```

## Debug checklist
- ✅ Enable `--log-requests` and inspect payload
- ✅ Enable `--log-responses` for SSE stream
- ✅ Re‑run with `--mock` to isolate client vs upstream
- ✅ Validate auth credentials (`godex auth status`)
- ✅ Check model routing (`godex probe <model>`)

## Auth debugging

### Codex (GPT models)
Godex reads `~/.codex/auth.json` by default. If calls fail with 401/403:
- ensure `access_token` is valid
- ensure `id_token` is present (string or object form)
- re‑run `codex auth` if needed

### Anthropic (Claude models)
Godex reads `~/.claude/.credentials.json` for OAuth tokens. If calls fail:
- check token expiry with `godex auth status`
- re-authenticate: `claude auth login`
- ensure `anthropic.enabled: true` in config

### Quick auth check
```bash
godex auth status
# Shows status for all backends + expiry times
```

## Backend routing debugging

If a model routes to the wrong backend:
```bash
# Check which backend handles a model
godex probe sonnet
# OK: sonnet → claude-sonnet-4-5-20250929 (anthropic)

godex probe gpt-5.2-codex
# OK: gpt-5.2-codex → gpt-5.2-codex (codex)
```

Verify routing patterns in config:
```yaml
backends:
  routing:
    patterns:
      anthropic: ["claude-", "sonnet", "opus", "haiku"]
      codex: ["gpt-", "o1-", "o3-"]
```

## Model discovery debugging

If `/v1/models` is missing expected models:
- Anthropic: Check OAuth token validity
- Codex: Models are hardcoded (no API discovery)
- Cache: Results cached 5 min; restart proxy to refresh

## Proxy debugging
- Use `--log-level debug`
- Try `--allow-any-key` in local dev
- Confirm credentials are accessible
- Check backend status: `curl /health`

## Replay a failing `/v1/responses` request
When debugging tool-call argument issues, replay the exact audited request directly
to the proxy (without involving OpenClaw):

```bash
scripts/replay-audit-responses.sh --ts 2026-02-18T23:45:45.237114272Z
```

What it does:
- extracts `.request` from `~/.godex/audit.jsonl` for the timestamp
- POSTs it to `http://127.0.0.1:39001/v1/responses`
- prints tool-call SSE contract events
- fails if `exec` has empty `{}` arguments in `function_call_arguments.done` or `output_item.done`
