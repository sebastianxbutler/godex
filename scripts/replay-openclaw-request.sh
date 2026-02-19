#!/usr/bin/env bash
set -euo pipefail

TRACE_FILE="${TRACE_FILE:-$HOME/.godex/proxy-trace.jsonl}"
AUDIT_FILE="${AUDIT_FILE:-$HOME/.godex/audit.jsonl}"
PROXY_URL="${PROXY_URL:-http://127.0.0.1:39001}"
REQUEST_ID="latest"
LIST_ONLY=0
LIST_COUNT=20
SAVE_PATH=""

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

usage() {
  cat <<USAGE
Usage:
  scripts/replay-openclaw-request.sh [request_id|latest] [options]

Options:
  --list [N]      List last N captured request IDs (default: 20)
  --save <path>   Save payload JSON to file before replay
  -h, --help      Show this help

Env:
  TRACE_FILE, AUDIT_FILE, PROXY_URL, OPENCLAW_BEARER_TOKEN
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --list)
      LIST_ONLY=1
      if [[ "${2:-}" =~ ^[0-9]+$ ]]; then
        LIST_COUNT="$2"
        shift 2
      else
        shift 1
      fi
      ;;
    --save)
      SAVE_PATH="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      REQUEST_ID="$1"
      shift 1
      ;;
  esac
done

if [[ "$LIST_ONLY" == "1" ]]; then
  if [[ -f "$TRACE_FILE" ]]; then
    jq -r '
      select(.phase=="openclaw_request" and (.path=="/v1/responses" or .path=="/v1/chat/completions"))
      | [.request_id, .path, .ts]
      | @tsv
    ' "$TRACE_FILE" | tail -n "$LIST_COUNT"
    exit 0
  fi
  echo "trace file not found: $TRACE_FILE" >&2
  exit 1
fi

line=""
source_kind=""

if [[ -f "$TRACE_FILE" ]]; then
  selector='.phase=="openclaw_request" and (.path=="/v1/responses" or .path=="/v1/chat/completions")'
  if [[ "$REQUEST_ID" == "latest" ]]; then
    line="$(jq -c "select($selector)" "$TRACE_FILE" | tail -n 1)"
  else
    line="$(jq -c "select($selector and .request_id==\"$REQUEST_ID\")" "$TRACE_FILE" | tail -n 1)"
  fi
  if [[ -n "${line}" ]]; then
    source_kind="trace"
  fi
fi

if [[ -z "${line}" ]] && [[ -f "$AUDIT_FILE" ]]; then
  selector='.request != null and (.path=="/v1/responses" or .path=="/v1/chat/completions")'
  if [[ "$REQUEST_ID" == "latest" ]]; then
    line="$(jq -c "select($selector)" "$AUDIT_FILE" | tail -n 1)"
  else
    line="$(jq -c "select($selector and .request_id==\"$REQUEST_ID\")" "$AUDIT_FILE" | tail -n 1)"
  fi
  if [[ -n "${line}" ]]; then
    source_kind="audit"
  fi
fi

if [[ -z "${line}" ]]; then
  echo "no matching request found in trace or audit (request_id=$REQUEST_ID)" >&2
  exit 1
fi

request_id="$(jq -r '.request_id' <<<"$line")"
path="$(jq -r '.path' <<<"$line")"
if [[ "$source_kind" == "trace" ]]; then
  payload="$(jq -c '.payload' <<<"$line")"
else
  payload="$(jq -c '.request' <<<"$line")"
fi
if [[ -z "$request_id" || "$request_id" == "null" ]]; then
  request_id="${source_kind}_latest"
fi

auth_header=()
if [[ -z "${OPENCLAW_BEARER_TOKEN:-}" ]] && [[ -f "${HOME}/.openclaw/openclaw.json" ]]; then
  OPENCLAW_BEARER_TOKEN="$(jq -r '.models.providers.godex.apiKey // empty' "${HOME}/.openclaw/openclaw.json")"
fi
if [[ -n "${OPENCLAW_BEARER_TOKEN:-}" ]]; then
  auth_header=(-H "Authorization: Bearer ${OPENCLAW_BEARER_TOKEN}")
fi

echo "Replaying source=${source_kind} request_id=${request_id} path=${path}"
tmp_payload="$(mktemp)"
trap 'rm -f "$tmp_payload"' EXIT
printf '%s' "$payload" > "$tmp_payload"
if [[ -n "$SAVE_PATH" ]]; then
  cp "$tmp_payload" "$SAVE_PATH"
  echo "Saved payload to: $SAVE_PATH"
fi
curl -sS -N \
  -H "Content-Type: application/json" \
  "${auth_header[@]}" \
  -X POST "${PROXY_URL}${path}" \
  --data-binary "@${tmp_payload}"
echo
