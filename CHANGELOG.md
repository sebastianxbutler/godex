# Changelog

## 0.7.0 - 2026-02-18
### Added
- **Multi-backend `godex exec`**: The CLI now supports all backends, not just Codex. Model name determines the backend automatically:
  - `gpt-*`, `o1-*`, `o3-*` → Codex (OAuth)
  - `claude-*`, `sonnet`, `opus`, `haiku` → Anthropic (OAuth)
  - `gemini-*` → Gemini (API key)
  - Custom backends from config are also matched
- **`--provider-key` flag**: Pass API keys for non-OAuth backends directly:
  ```bash
  godex exec --model gemini-2.5-pro --provider-key AIza... --prompt "Hello"
  ```
- **Generic tool loop** (`pkg/backend/toolloop.go`): Works with any `Backend` interface. The `--auto-tools` flag now works for all backends, not just Codex.
- **`backend.RunToolLoop()`**: Standalone function for running tool loops with any backend.

## 0.6.1 - 2026-02-18
### Added
- **`X-Provider-Key` header**: Per-request API key override for proxy requests.

## 0.6.0 - 2026-02-18
### Added
- **Gemini backend support**: Added Gemini as a custom OpenAI-compatible backend. Configure with `GEMINI_API_KEY` env var. Supports `gemini-2.5-pro`, `gemini-2.5-flash`, `gemini-2.0-flash` with aliases `gemini` and `flash`.
- **Full tool call support in OpenAI-compatible backends**: Rewrote `pkg/backend/openai/` to properly translate between Codex Responses API format and OpenAI Chat Completions format, including:
  - Tool definitions in requests
  - Tool call history (function_call + function_call_output → assistant tool_calls + tool messages)
  - Streaming tool call responses translated to Codex events
- **Graceful custom backend initialization**: Custom backends that fail to initialize (e.g., missing API key) are now skipped with a warning instead of crashing the proxy.
- 11 new tests for OpenAI backend (text streaming, tool calls, request translation, event translation).

## 0.5.9 - 2026-02-18
### Fixed
- **Tools not passed to Codex via `/v1/responses`**: The Responses API uses a flat tool format (`{"type":"function","name":"exec",...}`) while the proxy only supported the Chat Completions nested format (`{"type":"function","function":{"name":"exec",...}}`). Tools were silently dropped, causing models to hallucinate tool usage instead of actually making tool calls. Now supports both formats via `ResolvedFunction()`.

## 0.5.8 - 2026-02-17
### Fixed
- **Assistant messages use correct content type**: Assistant messages now use `output_text` instead of `input_text` for their content. Codex rejects `input_text` for assistant role with "Invalid value: 'input_text'. Supported values are: 'output_text' and 'refusal'."

### Added
- Unit test for assistant message content type validation.

## 0.5.7 - 2026-02-17
### Fixed
- **Orphaned tool results no longer cause 400 errors**: When a tool call is aborted mid-stream and transcript repair leaves orphaned `function_call_output` items, godex now skips them gracefully with a warning instead of failing with "missing function_call for {id}". This prevents sessions from becoming permanently stuck after aborted tool calls.

### Added
- Unit tests for orphaned tool result handling in `mapping_test.go`.

## 0.5.6 - 2026-02-17
### Fixed
- **Tool results in `/v1/chat/completions`**: OpenAI-format `role: "tool"` messages are now properly translated to Codex's native `function_call_output` format. Previously, these were passed through unchanged, causing "Invalid value: 'tool'" errors from the Codex backend.
- Assistant messages with `tool_calls` arrays are now converted to `function_call` items.

### Added
- `ToolCallID` field in `OpenAIChatMessage` struct for proper tool result handling.
- Integration test for multi-turn tool interactions.

## 0.5.5 - 2026-02-16
### Added
- **Automatic Anthropic OAuth token refresh**: Expired tokens are now refreshed automatically using the refresh token. No manual `claude auth login` required!
- Token refresh uses the same OAuth flow as Claude Code CLI.
- Refreshed credentials are saved back to `~/.claude/.credentials.json`.

### Technical Details
- OAuth endpoint: `POST https://console.anthropic.com/v1/oauth/token`
- Grant type: `refresh_token`
- New methods: `TokenStore.Refresh()`, `TokenStore.Save()`, `TokenStore.CanRefresh()`

## 0.5.4 - 2026-02-16
### Changed
- **Documentation audit**: Updated glossary, debugging, cookbook, testing docs for v0.5.x features.
- Added 12 new glossary terms (Backend, Router, OAuth, Model Alias, etc.).
- Added multi-backend debugging guide (Anthropic OAuth, routing issues).
- Added 8 new cookbook recipes (Claude usage, auth, probe commands).
- Added E2E test documentation and coverage targets.

## 0.5.3 - 2026-02-16
### Added
- **`godex auth status`**: Check authentication status for all backends.
- **`godex auth setup`**: Interactive wizard for configuring missing credentials.
- Detects existing Codex and Anthropic credentials automatically.
- Guides users to run native CLI auth commands (`codex auth`, `claude auth login`).
- **GPT-5.3 Codex**: Added to known models list.

### Example
```bash
godex auth status
# Codex:       ✅ configured
# Anthropic:   ✅ configured (expires 2026-02-16 14:55)

godex auth setup
# Interactive setup for missing backends
```

## 0.5.2 - 2026-02-16
### Added
- **Model probe endpoint**: `GET /v1/models/{model_id}` returns model info or 404.
- **`godex probe` CLI command**: Check if a model exists and get routing info.
- Response includes `backend`, `display_name`, and `alias` fields.
- Alias expansion: `sonnet` → `claude-sonnet-4-5-20250929`.

### Example
```bash
godex probe sonnet --key $KEY
# OK: sonnet → claude-sonnet-4-5-20250929 (anthropic) [Claude Sonnet 4.5]

curl /v1/models/sonnet -H "Authorization: Bearer $KEY"
# {"id":"claude-sonnet-4-5-20250929","backend":"anthropic","alias":"sonnet",...}
```

## 0.5.1 - 2026-02-16
### Added
- **Dynamic model discovery**: `/v1/models` now queries backends for available models.
- **Anthropic model listing**: Live discovery via Anthropic `/v1/models` API.
- **Model caching**: Backend model lists cached for 5 minutes.
- **`ListModels` interface**: All backends implement `ListModels(ctx) ([]ModelInfo, error)`.

### Changed
- Updated README with multi-backend documentation.
- Updated architecture docs with model discovery details.

## 0.5.0 - 2026-02-16
### Added
- **Multi-backend architecture**: pluggable backend system supporting multiple LLM providers.
- **Anthropic backend**: full support for Claude models via official Anthropic SDK.
- **Claude Code OAuth integration**: authenticate using `~/.claude/.credentials.json` tokens.
- **Model routing**: automatic backend selection based on model prefix patterns.
- **Model aliases**: shorthand names (`sonnet`, `opus`, `haiku`) resolve to full model IDs.
- **Backend interface** (`pkg/backend/backend.go`): common interface for all backends.
- **Router** (`pkg/backend/router.go`): selects backend by model prefix patterns.
- **Integration tests**: comprehensive test suite for multi-backend scenarios.
- **E2E test script** (`scripts/e2e-test.sh`): automated testing for both backends.

### Changed
- Codex client refactored to `pkg/backend/codex/` following backend interface.
- Proxy server now initializes backends via config and routes requests through router.

### Configuration
```yaml
backends:
  anthropic:
    enabled: true
  routing:
    patterns:
      anthropic: ["claude-", "sonnet", "opus", "haiku"]
    aliases:
      sonnet: "claude-sonnet-4-5-20250929"
      opus: "claude-opus-4-20250514"
      haiku: "claude-haiku-4-20250414"
```

## 0.4.1 - 2026-02-16
### Added
- **Multi-model support**: configure multiple models via `proxy.models` list.
- Each model can have its own `base_url`.
- `/v1/models` returns all configured models.
- Model validation on requests (400 if model not available).

## 0.4.0 - 2026-02-16
### Added
- **Payments gateway integration**: L402 payments via external token-meter service.
- **Admin unix socket API**: `/admin/keys`, `/admin/keys/{id}/policy`, `/admin/keys/{id}/add-tokens` for programmatic key management.
- **`/v1/pricing` endpoint**: proxies pricing data from token-meter with graceful fallback.
- Token allowance and balance management for API keys.

### Changed
- L402 logic extracted to separate `token-meter` service (see token-meter repo).
- Payments disabled by default; enable via `proxy.payments.enabled`.

### Notes
- godex works standalone without token-meter; pricing returns "disabled" or "unavailable" as appropriate.

## 0.3.3 - 2026-02-12
- Proxy no longer requires --api-key; key store is default (allow-any-key for dev).

## 0.3.2 - 2026-02-12
- Added YAML config with env + CLI layering and config template.
- Added --config flag for exec/proxy subcommands.
- Added summary-only metering defaults and rolling reset-events log.

## 0.3.1 - 2026-02-12
- Added release workflow + changelog-based release notes.

## 0.3.0 - 2026-02-12
- Added wire protocol spec (docs/wire.md).
- Added exec flags: --tool-choice, --input-json, --log-requests/--log-responses.
- Added mock modes: echo, text, tool-call, tool-loop.
- Normalized JSONL error events and tool argument completion.
- Added proxy API key management (keys, usage, rate limiting, token metering).
- Added build-time version flag (--version).
- Added BYOK support for proxy keys (--key).
- Added stats log rotation for proxy usage logs.
- Added proxy key update command (keys update).
- Added persistent metering with windowed resets and usage reset command.
- Added key expiration with pruning on restart.
- Disabled history by default; added summary totals file for usage.
- Added rolling reset-events log (proxy-events.jsonl).
- Added persistent metering with windowed totals.

## 0.2.0 - 2026-02-12
- Add OpenAI-compatible proxy (`/v1/responses`, `/v1/models`, `/v1/chat/completions`)
- Support tool calls + follow-up reconstruction in proxy
- Add prompt cache reuse for system instructions
- Add proxy logging, health check, and auth options
- Add proxy documentation
