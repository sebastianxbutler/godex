# Repository Guidelines

## Project Structure & Module Organization
`godex` is a Go CLI and proxy. Core entrypoint is `cmd/godex/` (`main.go`, CLI handlers). Reusable logic lives in `pkg/` (notably `pkg/harness/`, `pkg/proxy/`, `pkg/auth/`, `pkg/config/`, `pkg/sse/`). Documentation is in `docs/`, runnable examples are in `examples/`, automation scripts are in `scripts/`, and contributor notes/design drafts are in `contrib/` and `scratch/`.

## Build, Test, and Development Commands
- `make build`: builds `./cmd/godex` into `./godex` with version ldflags.
- `make test`: runs the full unit test suite (`go test ./...`).
- `go test -cover ./...`: run tests with coverage output.
- `./scripts/e2e-test.sh`: end-to-end checks for proxy routing/streaming across backends (requires valid local auth credentials).
- `./godex exec --prompt "Hello" --mock --mock-mode text --json`: fast deterministic smoke test without live providers.

## Coding Style & Naming Conventions
Use standard Go formatting (`gofmt`) and idiomatic Go structure. Prefer small focused packages, explicit types for protocol/config shapes, and clear error propagation. Use tabs/formatting as produced by `gofmt`; do not hand-align. Name files by feature (`router.go`, `router_test.go`) and keep tests adjacent to implementation. For CLI/config flags and YAML keys, follow existing kebab-case/lowercase conventions.

## Testing Guidelines
Testing is Go `testing` package-based with `*_test.go` files across `cmd/` and `pkg/`. Prefer deterministic unit tests and mock-mode flows over live API calls in CI. Validate behavior for routing, SSE streaming, tool loops, and error handling. Run package-specific tests during development (example: `go test ./pkg/harness/...`) before full-suite runs.

## Commit & Pull Request Guidelines
Follow the repository’s observed commit style: concise prefixes like `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:` plus a clear scope/impact summary. Keep commits atomic and behavior-focused. PRs should include:
- what changed and why,
- linked issue(s) when applicable,
- test evidence (`go test ./...`, coverage, or e2e notes),
- docs/changelog updates for user-facing changes.

## Security & Configuration Tips
Never commit credentials (`~/.codex/auth.json`, `~/.claude/.credentials.json`) or API keys. Avoid logging secrets in request/response artifacts. Use `docs/config.template.yaml` as the baseline for local config and prefer env vars for sensitive values.
