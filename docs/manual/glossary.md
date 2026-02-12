# Glossary

**Responses API** — ChatGPT backend API used by Codex. Supports streaming, tools, and input items.

**SSE (Server‑Sent Events)** — Streaming protocol used by Responses API.

**Tool Call** — Model output requesting a function execution (`function_call`).

**Tool Result** — Follow‑up input item containing `function_call_output`.

**Input Items** — Structured inputs to Responses API: message, function_call, function_call_output.

**Intelliwire** — CLI interface spec for provider‑agnostic tooling (Codex/Claude).

**Proxy** — OpenAI‑compatible HTTP server that forwards to Responses API.

**JSONL** — JSON Lines format; one JSON object per line.

**Auto‑tools** — Godex mode that runs a tool loop automatically using `--tool-output`.

**Mock Mode** — Godex mode that emits synthetic streams for deterministic tests.
