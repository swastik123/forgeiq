# MCP `llm.chat:v2` (Structured Output) – Spec Sketch

This document describes a proposed **backward-compatible upgrade** to the MCP tool `llm.chat`:

- Keep `llm.chat:v1` as-is (string output).
- Add `llm.chat:v2` with **structured output enforcement** (schema / JSON-object style).

## Goals

- **Typed results**: return a JSON object under `result`, not a JSON string.
- **Schema enforcement**: validate output against JSON Schema (subset or full).
- **Retries**: MCP performs validate→repair→retry so callers are simpler.
- **Provider abstraction**: callers don’t care if the backend is LiteLLM / OpenAI / Anthropic.

## Request shape (`tools.call` args)

`tools.call`:

```json
{
  "name": "llm.chat",
  "version": "v2",
  "args": {
    "messages": [
      { "role": "system", "content": "..." },
      { "role": "user", "content": "..." }
    ],
    "tools": [],
    "model": {
      "provider": "mcp",
      "model": "gpt-4.1-mini",
      "temperature": 0.2
    },
    "meta": {
      "task_id": "t-123",
      "trace_id": "..."
    },
    "response_format": {
      "type": "json_schema",
      "name": "pr_review",
      "schema": {
        "type": "object",
        "properties": {
          "summary": { "type": "string" },
          "findings": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "rule_id": { "type": "string" },
                "severity": { "type": "string", "enum": ["blocker","high","medium","low","nit"] },
                "path": { "type": "string" },
                "line": { "type": "integer" },
                "message": { "type": "string" },
                "suggestion": { "type": "string" }
              },
              "required": ["rule_id","severity","path","line","message"]
            }
          }
        },
        "required": ["summary","findings"]
      }
    },
    "strict": true,
    "retries": {
      "max_attempts": 2
    }
  }
}
```

### Request fields
- **messages** *(required)*: chat messages (same as v1).
- **tools** *(optional)*: tool schemas (same as v1).
- **model** *(optional)*: desired model. MCP may override based on policy.
- **meta** *(optional)*: passthrough metadata for observability/audit.
- **response_format** *(optional)*:
  - `type`: `"json_schema"` or `"json_object"`
  - `name`: logical schema name (for logging / caching)
  - `schema`: JSON Schema to validate output
- **strict** *(optional, default true)*:
  - If true: reject outputs that don’t validate after retries.
- **retries** *(optional)*:
  - `max_attempts`: number of provider attempts including first.

## Response shape (typed)

MCP returns a JSON object (as the JSON-RPC `"result"` of `tools.call`), shaped like:

```json
{
  "status": "ok",
  "attempts": 1,
  "result": {
    "summary": "…",
    "findings": []
  },
  "validation": {
    "ok": true
  }
}
```

### Errors
- If schema validation fails after retries:
  - return JSON-RPC error with message explaining validation failures, or
  - return `{ "status": "error", "error": "...", "validation": {...} }` if you prefer non-RPC errors.

## Backward compatibility

- **v1** stays:
  - returns `final_answer` as a string (often JSON embedded in a string)
- **v2** adds:
  - returns `result` as an object
- Callers can:
  - prefer v2
  - fall back to v1 on tool-not-found or parse failure

## Provider abstraction (LiteLLM vs direct) behind MCP

Recommended:
- MCP decides provider routing based on:
  - repo policy, tenant, budgets, model allowlist
  - reliability signals / fallbacks
- Callers keep sending:
  - `model.provider="mcp"` (or omit provider)
  - `model.model="gpt-4.1-mini"` (logical name)

