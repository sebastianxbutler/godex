# Cookbook

Practical recipes for common tasks.

## 1) Basic prompt
```bash
./godex exec --prompt "Hello"
```

## 2) Web search tool
```bash
./godex exec --prompt "Weather in Austin" --web-search
```

## 3) Function tool with schema
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json
```

## 4) Auto tool loop (static output)
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=5
```

## 5) Auto tool loop (echo args)
```bash
./godex exec --prompt "Call add(a=2,b=3)" \
  --tool add:json=schemas/add.json \
  --auto-tools --tool-output add=$args
```

## 6) Capture request/response logs
```bash
./godex exec --prompt "Hello" \
  --log-requests /tmp/godex-request.json \
  --log-responses /tmp/godex-response.jsonl
```

## 7) Deterministic mock stream
```bash
./godex exec --prompt "test" --mock --mock-mode tool-loop --json
```

## 8) Use input‑items directly
```bash
./godex exec --input-json ./input.json
```

## 9) Run proxy locally
```bash
./godex proxy --api-key "local-dev-key" --log-level debug
```
