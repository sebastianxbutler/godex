# Changelog

## Unreleased

## 0.3.0 - 2026-02-12
- Added wire protocol spec (docs/wire.md).
- Added exec flags: --tool-choice, --input-json, --log-requests/--log-responses.
- Added mock modes: echo, text, tool-call, tool-loop.
- Normalized JSONL error events and tool argument completion.
- Added proxy API key management (keys, usage, rate limiting, token metering).
- Added build-time version flag (--version).
- Added BYOK support for proxy keys (--key).

## 0.2.0 - 2026-02-12
- Add OpenAI-compatible proxy (`/v1/responses`, `/v1/models`, `/v1/chat/completions`)
- Support tool calls + follow-up reconstruction in proxy
- Add prompt cache reuse for system instructions
- Add proxy logging, health check, and auth options
- Add proxy documentation
