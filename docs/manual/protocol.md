# Protocol

This manual section summarizes the Responses API conventions as used by Godex.

## Inputs
Godex supports **Responses input items**:
- `message` (role: user/assistant, content: input_text/output_text)
- `function_call`
- `function_call_output`

`--input-json` accepts a JSON array of these items.

## Outputs
SSE events are normalized into JSONL lines:
- `response.created`
- `response.output_text.delta`
- `response.output_item.added`
- `response.output_item.done`
- `response.completed`
- `error`

Tool calls appear as:
- `response.output_item.added` with `item.type=function_call`

## Tool lifecycle
1. Model emits `function_call`
2. Client sends `function_call_output`
3. Model continues with `output_text` or more tool calls

## Errors
Errors are emitted as `error` events with a `message` field.

For full wire‑level details, see:
- `../intelliwire.md`
- `../../protocol-spec.md`
