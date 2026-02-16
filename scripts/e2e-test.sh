#!/bin/bash
# E2E tests for godex multi-backend support
# Run before releasing to verify real backend connectivity
#
# Prerequisites:
# - Codex auth: ~/.codex/auth.json (or GODEX_AUTH_PATH)
# - Anthropic auth: ~/.claude/.credentials.json (Claude Code)
#
# Usage:
#   ./scripts/e2e-test.sh              # Run all tests
#   ./scripts/e2e-test.sh --anthropic  # Anthropic only
#   ./scripts/e2e-test.sh --codex      # Codex only

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
GODEX_BIN="${PROJECT_DIR}/godex"
PROXY_PORT=39099
PROXY_URL="http://127.0.0.1:${PROXY_PORT}"
PROXY_PID=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $1${NC}"; }
fail() { echo -e "${RED}✗ $1${NC}"; }
warn() { echo -e "${YELLOW}⚠ $1${NC}"; }
info() { echo -e "  $1"; }

cleanup() {
    if [ -n "$PROXY_PID" ]; then
        kill $PROXY_PID 2>/dev/null || true
    fi
    rm -f /tmp/godex-e2e-*.yaml /tmp/godex-e2e.log
}
trap cleanup EXIT

# Build if needed
build_godex() {
    echo "Building godex..."
    cd "$PROJECT_DIR"
    go build -o "$GODEX_BIN" ./cmd/godex
    pass "Build successful"
}

# Start proxy with given config
start_proxy() {
    local config="$1"
    "$GODEX_BIN" proxy --config "$config" > /tmp/godex-e2e.log 2>&1 &
    PROXY_PID=$!
    sleep 3
    
    if ! kill -0 $PROXY_PID 2>/dev/null; then
        fail "Proxy failed to start"
        cat /tmp/godex-e2e.log
        exit 1
    fi
    
    # Health check
    if ! curl -sf "${PROXY_URL}/health" > /dev/null; then
        fail "Proxy health check failed"
        cat /tmp/godex-e2e.log
        exit 1
    fi
    pass "Proxy started on port $PROXY_PORT"
}

stop_proxy() {
    if [ -n "$PROXY_PID" ]; then
        kill $PROXY_PID 2>/dev/null || true
        PROXY_PID=""
    fi
}

# Test helpers
test_chat() {
    local model="$1"
    local prompt="$2"
    local expected="$3"
    
    response=$(curl -sf --max-time 30 "${PROXY_URL}/v1/chat/completions" \
        -H "Authorization: Bearer e2e-test" \
        -H "Content-Type: application/json" \
        -d "{\"model\":\"$model\",\"messages\":[{\"role\":\"user\",\"content\":\"$prompt\"}]}" 2>&1) || {
        fail "Request failed for model $model"
        echo "$response"
        return 1
    }
    
    content=$(echo "$response" | jq -r '.choices[0].message.content // .error.message' 2>/dev/null)
    resp_model=$(echo "$response" | jq -r '.model // "unknown"' 2>/dev/null)
    
    if echo "$content" | grep -qi "$expected"; then
        pass "Model $model → $resp_model: contains '$expected'"
        return 0
    else
        fail "Model $model: expected '$expected', got: $content"
        return 1
    fi
}

test_streaming() {
    local model="$1"
    
    response=$(curl -sf --max-time 30 -N "${PROXY_URL}/v1/chat/completions" \
        -H "Authorization: Bearer e2e-test" \
        -H "Content-Type: application/json" \
        -d "{\"model\":\"$model\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}]}" 2>&1) || {
        fail "Streaming request failed for model $model"
        return 1
    }
    
    if echo "$response" | grep -q "data:.*delta"; then
        pass "Model $model: streaming works"
        return 0
    else
        fail "Model $model: no streaming data"
        echo "$response" | head -5
        return 1
    fi
}

test_tool_call() {
    local model="$1"
    
    response=$(curl -sf --max-time 30 "${PROXY_URL}/v1/chat/completions" \
        -H "Authorization: Bearer e2e-test" \
        -H "Content-Type: application/json" \
        -d '{
            "model":"'"$model"'",
            "messages":[{"role":"user","content":"Get the weather in Paris using the weather function"}],
            "tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}}}]
        }' 2>&1) || {
        fail "Tool call request failed for model $model"
        return 1
    }
    
    if echo "$response" | jq -e '.choices[0].message.tool_calls[0].function.name' > /dev/null 2>&1; then
        tool_name=$(echo "$response" | jq -r '.choices[0].message.tool_calls[0].function.name')
        pass "Model $model: tool call works (called $tool_name)"
        return 0
    else
        fail "Model $model: no tool call in response"
        echo "$response" | jq '.choices[0].message' 2>/dev/null || echo "$response"
        return 1
    fi
}

# Test suites
test_anthropic() {
    echo ""
    echo "=== Anthropic Backend Tests ==="
    
    # Check credentials
    if [ ! -f ~/.claude/.credentials.json ]; then
        warn "No Claude credentials found (~/.claude/.credentials.json)"
        warn "Run 'claude auth login' to authenticate"
        return 1
    fi
    
    cat > /tmp/godex-e2e-anthropic.yaml << EOF
proxy:
  listen: "127.0.0.1:${PROXY_PORT}"
  allow_any_key: true
  backends:
    codex:
      enabled: false
    anthropic:
      enabled: true
      default_max_tokens: 100
    routing:
      default: "anthropic"
      patterns:
        anthropic: ["claude-", "sonnet", "opus", "haiku"]
      aliases:
        sonnet: "claude-sonnet-4-5-20250929"
        opus: "claude-opus-4-5"
EOF
    
    start_proxy /tmp/godex-e2e-anthropic.yaml
    
    local failed=0
    
    # Basic chat
    test_chat "sonnet" "Reply with exactly: ANTHROPIC_OK" "ANTHROPIC" || ((failed++))
    test_chat "claude-sonnet-4-5-20250929" "Reply with exactly: FULL_MODEL_OK" "OK" || ((failed++))
    
    # Streaming
    test_streaming "sonnet" || ((failed++))
    
    # Tool calls
    test_tool_call "sonnet" || ((failed++))
    
    stop_proxy
    
    if [ $failed -eq 0 ]; then
        pass "All Anthropic tests passed"
    else
        fail "$failed Anthropic test(s) failed"
    fi
    return $failed
}

test_codex() {
    echo ""
    echo "=== Codex Backend Tests ==="
    
    # Check credentials
    if [ ! -f ~/.codex/auth.json ]; then
        warn "No Codex credentials found (~/.codex/auth.json)"
        return 1
    fi
    
    cat > /tmp/godex-e2e-codex.yaml << EOF
proxy:
  listen: "127.0.0.1:${PROXY_PORT}"
  allow_any_key: true
  backends:
    codex:
      enabled: true
    anthropic:
      enabled: false
    routing:
      default: "codex"
      patterns:
        codex: ["gpt-", "o1-", "codex-"]
EOF
    
    start_proxy /tmp/godex-e2e-codex.yaml
    
    local failed=0
    
    # Basic chat
    test_chat "gpt-5.2-codex" "Reply with exactly: CODEX_OK" "OK" || ((failed++))
    
    # Streaming
    test_streaming "gpt-5.2-codex" || ((failed++))
    
    stop_proxy
    
    if [ $failed -eq 0 ]; then
        pass "All Codex tests passed"
    else
        fail "$failed Codex test(s) failed"
    fi
    return $failed
}

test_routing() {
    echo ""
    echo "=== Multi-Backend Routing Tests ==="
    
    cat > /tmp/godex-e2e-both.yaml << EOF
proxy:
  listen: "127.0.0.1:${PROXY_PORT}"
  allow_any_key: true
  backends:
    codex:
      enabled: true
    anthropic:
      enabled: true
      default_max_tokens: 100
    routing:
      default: "codex"
      patterns:
        anthropic: ["claude-", "sonnet", "opus", "haiku"]
        codex: ["gpt-", "o1-", "codex-"]
      aliases:
        sonnet: "claude-sonnet-4-5-20250929"
EOF
    
    start_proxy /tmp/godex-e2e-both.yaml
    
    local failed=0
    
    # Route to Anthropic
    test_chat "sonnet" "Say ANTHROPIC" "ANTHROPIC" || ((failed++))
    
    # Route to Codex
    test_chat "gpt-5.2-codex" "Say CODEX" "CODEX" || ((failed++))
    
    stop_proxy
    
    if [ $failed -eq 0 ]; then
        pass "All routing tests passed"
    else
        fail "$failed routing test(s) failed"
    fi
    return $failed
}

# Main
main() {
    echo "======================================"
    echo "  Godex E2E Tests"
    echo "======================================"
    
    build_godex
    
    local total_failed=0
    local run_anthropic=true
    local run_codex=true
    
    # Parse args
    for arg in "$@"; do
        case $arg in
            --anthropic) run_codex=false ;;
            --codex) run_anthropic=false ;;
        esac
    done
    
    if $run_anthropic; then
        test_anthropic || ((total_failed++))
    fi
    
    if $run_codex; then
        test_codex || ((total_failed++))
    fi
    
    if $run_anthropic && $run_codex; then
        test_routing || ((total_failed++))
    fi
    
    echo ""
    echo "======================================"
    if [ $total_failed -eq 0 ]; then
        echo -e "${GREEN}All E2E tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}$total_failed test suite(s) failed${NC}"
        exit 1
    fi
}

main "$@"
