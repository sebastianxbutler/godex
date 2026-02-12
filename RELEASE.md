# Release Guide — godex

This document describes the release process for godex. It is written for both humans and agents.

## Versioning
Godex follows **Semantic Versioning**: `MAJOR.MINOR.PATCH`.

- **PATCH**: bug fixes, small internal refactors, docs-only changes, test additions that do not change behavior.
- **MINOR**: new features, new flags, new commands, behavior changes that remain backward compatible.
- **MAJOR**: breaking changes, removed flags, incompatible protocol changes, or behavior changes that require client updates.

## Release checklist

1) **Make sure main is green**
- `go test ./...`
- Any CI checks must pass

2) **Update docs**
- `CHANGELOG.md` (add entries under Unreleased)
- `README.md` and `docs/` if behavior changed

3) **Pick the version bump**
- Decide PATCH/MINOR/MAJOR using the rules above

4) **Tag the release**
```bash
git tag vX.Y.Z
```

5) **Build with version**
```bash
make build
./godex --version   # should show vX.Y.Z
```

6) **Push tags**
```bash
git push --tags
```

7) **Post‑release sanity**
- Confirm `--version` prints expected tag
- Optional: attach release notes

## Agent notes
- Keep releases small and focused
- Don’t skip tests
- Update `CHANGELOG.md` before tagging
- If unsure, choose a **minor** bump

## Quick examples

- **Patch**: fix a parsing bug, tweak error messages
- **Minor**: add proxy key management, new CLI subcommand
- **Major**: change request/response formats or remove `proxy` flags
