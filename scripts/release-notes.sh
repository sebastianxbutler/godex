#!/usr/bin/env bash
set -euo pipefail

TAG="${1:-}"
if [[ -z "$TAG" ]]; then
  echo "usage: release-notes.sh <tag>" >&2
  exit 1
fi

CHANGELOG="CHANGELOG.md"
if [[ ! -f "$CHANGELOG" ]]; then
  echo "CHANGELOG.md not found" >&2
  exit 1
fi

# Extract section for the tag (e.g. v0.3.0 or 0.3.0)
TAG_NOV=${TAG#v}
awk -v tag="$TAG_NOV" '
  BEGIN { in_section=0 }
  /^## / {
    if (in_section) exit
    if ($2 ~ tag) { in_section=1; next }
  }
  in_section { print }
' "$CHANGELOG" | sed '/^$/d'
