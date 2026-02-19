#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage:
  scripts/replay-audit-responses.sh --ts <RFC3339Nano timestamp> [options]

Options:
  --audit <path>   Audit JSONL file (default: ~/.godex/audit.jsonl)
  --url <url>      Responses endpoint URL (default: http://127.0.0.1:39001/v1/responses)
  --key <token>    Bearer API key (default: from ~/.openclaw/openclaw.json)
  --save <path>    Save extracted request JSON to this file

Example:
  scripts/replay-audit-responses.sh --ts 2026-02-18T23:45:45.237114272Z
USAGE
}

AUDIT_FILE="${HOME}/.godex/audit.jsonl"
URL="http://127.0.0.1:39001/v1/responses"
TS=""
API_KEY="${API_KEY:-}"
SAVE_PATH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ts)
      TS="${2:-}"
      shift 2
      ;;
    --audit)
      AUDIT_FILE="${2:-}"
      shift 2
      ;;
    --url)
      URL="${2:-}"
      shift 2
      ;;
    --key)
      API_KEY="${2:-}"
      shift 2
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
      echo "Unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$TS" ]]; then
  echo "--ts is required" >&2
  usage
  exit 2
fi

if [[ -z "$API_KEY" ]]; then
  if [[ -f "${HOME}/.openclaw/openclaw.json" ]]; then
    API_KEY="$(jq -r '.models.providers.godex.apiKey // empty' "${HOME}/.openclaw/openclaw.json")"
  fi
fi

if [[ -z "$API_KEY" ]]; then
  echo "API key not found. Pass --key or set API_KEY." >&2
  exit 2
fi

if [[ ! -f "$AUDIT_FILE" ]]; then
  echo "Audit file not found: $AUDIT_FILE" >&2
  exit 2
fi

REQ_FILE="$(mktemp)"
EVENTS_FILE="$(mktemp)"
META_FILE="$(mktemp)"
cleanup() {
  rm -f "$REQ_FILE" "$EVENTS_FILE" "$META_FILE"
}
trap cleanup EXIT

jq -c --arg ts "$TS" 'select(.ts == $ts and .path == "/v1/responses") | {request, tool_call_names}' "$AUDIT_FILE" | head -n 1 > "$META_FILE"

if [[ ! -s "$META_FILE" ]]; then
  echo "No /v1/responses request found in $AUDIT_FILE for ts=$TS" >&2
  exit 1
fi

jq -c '.request' "$META_FILE" > "$REQ_FILE"

if [[ ! -s "$REQ_FILE" ]]; then
  echo "No /v1/responses request found in $AUDIT_FILE for ts=$TS" >&2
  exit 1
fi

if [[ -n "$SAVE_PATH" ]]; then
  cp "$REQ_FILE" "$SAVE_PATH"
  echo "Saved request to: $SAVE_PATH"
fi

echo "Replaying request from ts=$TS"
echo "URL: $URL"

curl -sN \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  --data @"$REQ_FILE" \
  "$URL" \
  | sed -n 's/^data: //p' \
  | grep -v '^\[DONE\]$' \
  | jq -cr '
      if .type == "response.output_item.added" and .item.type == "function_call" then
        {event:.type, call_id:.item.call_id, name:.item.name, arguments:(.item.arguments // "")}
      elif .type == "response.function_call_arguments.done" then
        {event:.type, call_id:(.item.call_id // .call_id // ""), name:(.item.name // .name // ""), arguments:(.item.arguments // .arguments // "")}
      elif .type == "response.output_item.done" and .item.type == "function_call" then
        {event:.type, call_id:.item.call_id, name:.item.name, arguments:(.item.arguments // "")}
      elif .type == "error" then
        {event:"error", message:(.message // "unknown")}
      else empty end
    ' | tee "$EVENTS_FILE"

if jq -e 'select((.name // "") == "exec" and ((.event == "response.function_call_arguments.done") or (.event == "response.output_item.done")) and (((.arguments // "") == "{}") or ((.arguments // "") == "")))' "$EVENTS_FILE" >/dev/null; then
  echo "FAIL: replay produced empty exec arguments in function-call events" >&2
  exit 3
fi

if jq -e '.tool_call_names != null and (.tool_call_names | index("exec")) != null' "$META_FILE" >/dev/null; then
  if ! jq -e 'select((.name // "") == "exec" and ((.event == "response.function_call_arguments.done") or (.event == "response.output_item.done")))' "$EVENTS_FILE" >/dev/null; then
    echo "FAIL: original request had exec tool call, but replay emitted no exec done-events" >&2
    exit 4
  fi
fi

echo "PASS: replay did not produce empty exec arguments"
