# Contributing to godex

Thanks for helping improve godex. This guide is for both humans and agents.

## Principles
- **Keep it minimal.** No heavy SDK dependencies.
- **Prefer transparency.** Streaming JSONL output is a feature, not a bug.
- **Determinism matters.** Tests should be stable in CI.
- **Fix the root cause.** Avoid shallow patches that hide protocol issues.

## Project layout
```
cmd/godex/         CLI entrypoints
pkg/auth/          auth loader + refresh flow
pkg/protocol/      request/response types
pkg/sse/           SSE parsing
pkg/harness/codex/ Codex client + streaming + tool loop helpers
```

## Development workflow
1. Create a feature branch
2. Add/adjust tests
3. Run:
   ```bash
   go test ./...
   ```
4. Update docs if behavior changes

## Test guidelines
- Use **mock modes** for deterministic tests.
- Use request/response logs for debugging.
- Avoid relying on live API in CI.

## Docs
Docs live in `docs/` and the manual is in `docs/manual/`.
If you change behavior, update:
- `docs/manual/` pages
- `README.md` (if user‑facing)
- `CHANGELOG.md`

## Code style
- Keep changes small and focused
- Prefer explicit types in protocol structs
- Avoid hidden magic in the client loop

## Agent notes
If you are an automated agent:
- Follow the repository conventions above
- Keep PRs concise and well‑scoped
- Always include a short summary of changes and why
- Avoid invasive refactors unless requested

## Security
- Do **not** commit auth tokens or `auth.json`
- Avoid logging secrets in request/response logs

## Questions
Open an issue or ping the maintainer.
