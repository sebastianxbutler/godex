# Cookbook

Practical recipes for common tasks.

## Basic Usage

### 1) Basic prompt
```bash
./godex exec --prompt "Hello"
```

### 2) Web search tool
```bash
./godex exec --prompt "Weather in Austin" --web-search
```

### 3) Function tool with schema
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json
```

### 4) Auto tool loop (static output)
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5
```

### 5) Auto tool loop (echo args)
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=$args
```

### 6) Capture request/response logs
```bash
./godex exec --prompt "Hello" \
  --log-requests /tmp/godex-request.json \
  --log-responses /tmp/godex-response.jsonl
```

### 7) Deterministic mock stream
```bash
./godex exec --prompt "test" --mock --mock-mode tool-loop --json
```

### 8) Use input‑items directly
```bash
./godex exec --input-json ./input.json
```

## Proxy

### 9) Run proxy locally
```bash
./godex proxy --api-key "local-dev-key" --log-level debug
```

### 10) Create and manage API keys
```bash
# Add a new key with rate limits
./godex proxy keys add --label "my-agent" --rate 60/m --burst 10

# List all keys
./godex proxy keys list

# Check usage
./godex proxy usage list --since 24h
```

## Multi-Backend (Claude + GPT)

### 11) Check authentication status
```bash
godex auth status
# Codex:       ✅ configured
# Anthropic:   ✅ configured (expires 2026-02-16 14:55)
```

### 12) Set up missing credentials
```bash
godex auth setup
# Interactive wizard guides through missing backends
```

### 13) Use Claude via proxy
```bash
# Using model alias
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"sonnet","messages":[{"role":"user","content":"Hello"}]}'

# Using full model name
curl http://localhost:39001/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -d '{"model":"claude-sonnet-4-5-20250929","messages":[{"role":"user","content":"Hello"}]}'
```

### 14) Check if a model exists
```bash
# Human-readable output
godex probe sonnet
# OK: sonnet → claude-sonnet-4-5-20250929 (anthropic) [Claude Sonnet 4.5]

# JSON output for scripting
godex probe o3-mini --json
# {"id":"o3-mini","object":"model","owned_by":"godex","backend":"codex"}

# Check a non-existent model
godex probe fake-model
# ERROR: model "fake-model" not found
```

### 15) List all available models
```bash
curl http://localhost:39001/v1/models -H "Authorization: Bearer $KEY"
```

## Debugging

### 16) Debug a failing request
```bash
# Enable debug logging
./godex proxy --log-level debug --log-requests

# Check auth
godex auth status

# Test model routing
godex probe <model-name>
```

### 17) Run E2E tests
```bash
# Full multi-backend test suite
./scripts/e2e-test.sh
```
