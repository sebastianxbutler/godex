# Changelog

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
