# Codex‑Lite Protocol Spec (ChatGPT backend)

## Overview
Codex‑Lite is a minimal client for the Codex Responses API using ChatGPT auth tokens from `~/.codex/auth.json` (or API key). It supports streaming, function tools, tool outputs, and web_search.

Base URL (ChatGPT auth):
- `https://chatgpt.com/backend-api/codex`

Endpoint:
- `POST /backend-api/codex/responses`

---

## Auth
Read `~/.codex/auth.json`:

```json
{
  "auth_mode": "chatgpt" | "api_key",
  "OPENAI_API_KEY": "...",
  "tokens": {
    "access_token": "...",
    "refresh_token": "...",
    "account_id": "...",
    "id_token": {
      "raw_jwt": "...",
      "chatgpt_account_id": "..."
    }
  }
}
```

Use:
- **ChatGPT**: `Authorization: Bearer <access_token>`
- **API Key**: `Authorization: Bearer <OPENAI_API_KEY>`

If chatgpt auth, include `chatgpt-account-id` header using `tokens.account_id` or `tokens.id_token.chatgpt_account_id`.

### Token Refresh (ChatGPT)
Codex refreshes ChatGPT tokens via:
- **URL:** `https://auth.openai.com/oauth/token`
- **Method:** `POST`
- **Content-Type:** `application/json`

**Request body** (from codex‑rs):
```json
{
  "client_id": "app_EMoamEEZ73f0CkXaXp7hrann",
  "grant_type": "refresh_token",
  "refresh_token": "<refresh_token>",
  "scope": "openid profile email"
}
```

**Response body**:
```json
{
  "access_token": "...",
  "refresh_token": "...",
  "id_token": "..."
}
```

Errors indicating refresh invalid/expired (from codex-rs):
- "Your access token could not be refreshed because your refresh token has expired. Please log out and sign in again."
- "... refresh token was already used ..."
- "... refresh token was revoked ..."

Spec requirement:
- On 401/Unauthorized from responses endpoint, attempt refresh once.
- If refresh fails, require re-login and regenerate `auth.json`.

---

## Required Headers
- `Authorization: Bearer <token>`
- `originator: codex_cli_rs` (or custom string)
- `User-Agent: codex_cli_rs/<ver>`
- `session_id: <uuid>`
- `chatgpt-account-id: <account_id>` (chatgpt auth only)

---

## Request Body (Responses API)
```json
{
  "model": "gpt-5.2-codex",
  "instructions": "system text",
  "input": [<ResponseItem>],
  "tools": [<ToolSpec>],
  "tool_choice": "auto",
  "parallel_tool_calls": false,
  "reasoning": {"effort":"medium","summary":"auto"},
  "store": false,
  "stream": true,
  "include": ["reasoning.encrypted_content"],
  "prompt_cache_key": "<conversation-id>",
  "text": {"verbosity":"medium","format":{"type":"json_schema","strict":true,"schema":{...}}}
}
```

**Note:** `previous_response_id` is **not supported** by the ChatGPT backend.

---

## Tool Specs
### Function tool
```json
{
  "type": "function",
  "name": "add",
  "description": "Add two numbers",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {"a": {"type":"number"}, "b": {"type":"number"}},
    "required": ["a","b"]
  }
}
```

### Web search tool
```json
{"type":"web_search","external_web_access":true}
```

### Custom/freeform tool
```json
{
  "type":"custom",
  "name":"apply_patch",
  "description":"...",
  "format":{"type":"text","syntax":"diff","definition":"..."}
}
```

---

## Response Streaming (SSE)
Key events:
- `response.created`
- `response.in_progress`
- `response.output_item.added`
- `response.function_call_arguments.delta`
- `response.output_text.delta`
- `response.output_item.done`
- `response.completed`

### Tool call sequence
1) `response.output_item.added` with:
```json
{"type":"function_call","name":"add","call_id":"call_...","arguments":"","status":"in_progress"}
```
2) `response.function_call_arguments.delta` chunks build JSON string

### Tool output follow‑up
**Must include both** the function_call and function_call_output in the next request `input`:
```json
[
  {"type":"function_call","name":"add","arguments":"{\"a\":2,\"b\":3}","call_id":"call_..."},
  {"type":"function_call_output","call_id":"call_...","output":"5"}
]
```

If only `function_call_output` is provided, backend returns **400**.

### Error output format
Plain text works:
```json
{"type":"function_call_output","call_id":"call_...","output":"err: mul failed"}
```

### Web search events
- `response.output_item.added` with `type: web_search_call`
- `response.web_search_call.in_progress/searching/completed`
- `response.output_item.done` with action

---

## Edge Case Behavior (Observed)
1. Invalid tool schema → **200**, tool ignored
2. Invalid `arguments` JSON → **200**, model still responds
3. Missing `function_call` in follow‑up → **400**
4. No tools but asked to call → **200**, model replies it can’t
5. Large tool output (20k chars) → **200**
6. Parallel calls, mixed output order → **200** (call_id association)

---

## Minimal Test Scripts
- `test_request.py` (function tool + follow‑up)
- `edge_cases.py`
- `test_web_search.py`
