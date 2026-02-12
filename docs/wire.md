# Wire Protocol Spec (v0)

This document defines the **stable binary interface** for `godex exec` so orchestrators (like Chrysalis) can integrate reliably without running a proxy.

---

## Goals

- Provide a **minimal, stable, streaming** interface to LLMs.
- Support **tool calling** with JSON Schema tool definitions.
- Keep JSONL output aligned with **Responses API** event types.
- Allow orchestrators to handle tool execution and follow‑up runs.

Non‑goals:
- Defining a full HTTP API (proxy covers that).
- Requiring server mode.

---

## Command

```
 godex exec \
  --prompt "<user text>" \
  [--model <model>] \
  [--instructions "<system>" | --system "<system>"] \
  [--append-system-prompt "<text>"] \
  [--tool name:json=/path/schema.json]... \
  [--web-search] \
  [--tool-choice auto|required|function:<name>] \
  [--input-json <path>] \
  [--json] \
  [--trace] \
  [--log-requests <path>] \
  [--log-responses <path>] \
  [--session-id <id>]
```

### Required Flags
- `--prompt` (string): user input.

### Optional Flags
- `--model` (string): model name (default: `gpt-5.2-codex`).
- `--instructions` / `--system` (string): system prompt.
- `--append-system-prompt` (string): appended to system prompt.
- `--tool` (repeatable): tool spec in the form `name:json=/path/schema.json`.
- `--web-search` (bool): adds `web_search` tool with `external_web_access=true`.
- `--tool-choice` (string): `auto`, `required`, or `function:<name>`.
- `--input-json` (path): JSON array of `responses` input items (overrides `--prompt`).
- `--json` (bool): emit JSONL events only (no text output).
- `--trace` (bool): emit raw upstream SSE event JSON (for debugging).
- `--log-requests` (path): write the JSON request payload to a file.
- `--log-responses` (path): append JSONL event lines to a file.
- `--mock` (bool): emit a synthetic stream (no network).
- `--mock-mode` (string): `echo`, `text`, `tool-call`, or `tool-loop`.
- `--session-id` (string): prompt cache key / session key.

---

## Output Contract

When `--json` is set, `godex exec` **must** emit JSONL events to stdout. Each line is one JSON object, matching a **subset of Responses API streaming events**.

- **stdout**: JSONL events only (one JSON object per line).
- **stderr**: diagnostics, logging, errors (not JSON events).
- **exit code**: `0` for success, `!=0` on fatal error.

### Required Event Types

```
response.output_text.delta
response.content_part.added
response.output_item.added
response.function_call_arguments.delta
response.output_item.done
response.completed
error
```

### Event Shape (subset)

Each event is a JSON object with a `type` field and type‑specific properties.

#### 1) Text

```json
{"type":"response.output_text.delta","delta":"hello"}
```

```json
{
  "type":"response.content_part.added",
  "part": {"type":"output_text","text":"hello"}
}
```

#### 2) Tool Call Lifecycle

**Start**
```json
{
  "type":"response.output_item.added",
  "item": {
    "id":"item_123",
    "type":"function_call",
    "call_id":"call_abc",
    "name":"web_fetch"
  }
}
```

**Arguments (streamed)**
```json
{
  "type":"response.function_call_arguments.delta",
  "item_id":"item_123",
  "delta":"{\"url\":\"https://example.com\"}"
}
```

**Done**
```json
{
  "type":"response.output_item.done",
  "item": {
    "id":"item_123",
    "type":"function_call",
    "call_id":"call_abc",
    "name":"web_fetch",
    "arguments":"{\"url\":\"https://example.com\"}"
  }
}
```

#### 3) Completion

```json
{
  "type":"response.completed",
  "response": {
    "usage": {
      "input_tokens": 123,
      "output_tokens": 456,
      "cached_tokens": 0
    }
  }
}
```

#### 4) Error

```json
{
  "type":"error",
  "message":"<human-readable error>"
}
```

---

## Tool Schema Contract

Each `--tool` flag defines a tool by **name** and **JSON Schema**:

```
--tool <name>:json=/path/schema.json
```

The schema file **must** be a valid JSON Schema object. The CLI passes it through as:

```json
{"type":"function","name":"<name>","parameters":<schema>}
```

Notes:
- Tools are treated as **functions**.
- `strict=false` by default.

---

## Orchestrator Loop (Recommended)

Orchestrators (e.g. Chrysalis) should:

1. Run `godex exec` with tools + user prompt.
2. Parse JSONL tool events.
3. Execute the requested tool(s).
4. Call `godex exec` again, supplying:
   - the original conversation context
   - a `function_call` message
   - a `function_call_output` message

This spec leaves follow‑ups to the orchestrator (not the CLI).

---

## Compatibility Notes

- The event subset is chosen to match **Responses API** (ChatGPT backend).
- Future versions may add new event types, but **must not break** the required fields above.

---

## Versioning

- This spec is **v0**.
- Backward‑compatible additions are allowed.
- Breaking changes require **v1** with a migration guide.

---

## Example Run

```bash
godex exec \
  --prompt "web_fetch https://example.com" \
  --instructions "You are a tool-using assistant" \
  --tool web_fetch:json=/tmp/web_fetch.schema.json \
  --json
```

Possible output:
```
{"type":"response.output_item.added","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"web_fetch"}}
{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"url\":\"https://example.com\"}"}
{"type":"response.output_item.done","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"web_fetch","arguments":"{\"url\":\"https://example.com\"}"}}
{"type":"response.completed","response":{"usage":{"input_tokens":128,"output_tokens":0,"cached_tokens":0}}}
```

---

## Implementation Checklist

- [ ] `godex exec --json` emits JSONL events per spec
- [ ] tool schema parsing supports `name:json=/path/schema.json`
- [ ] tool call events include `call_id` and `arguments`
- [ ] `response.completed` includes usage
- [ ] stderr contains diagnostics only
