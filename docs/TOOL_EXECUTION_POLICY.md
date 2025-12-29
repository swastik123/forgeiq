# Tool Execution Policy (ForgeIQ)

This repo executes “tools” primarily via **MCP** (Model Context Protocol) and optionally via **runtime HTTP tool plugins** loaded from `plugins/manifest.yaml`. Tools can be slow or fail; this policy defines concrete behavior so the agent can act reliably, safely, and cost‑effectively.

## Goals

- **Reliability**: recover from transient failures; avoid infinite loops.
- **Safety**: prevent unintended side effects from “write” tools; enforce policy/scopes.
- **Predictability**: consistent timeouts, retries, and fallbacks by tool class.
- **Observability**: structured traces to explain what happened without leaking secrets.

## Tool classes used in this repo

### Core MCP tools (built-in)

- **Read**:
  - `logs.search:v1` (Elasticsearch if configured; otherwise simulated fallback)
  - `vectors.search:v1` (requires pgvector enabled)
  - `rag.single_retrieve:v1` / `rag.multiple_retrieve:v1`
  - `llm.router:v1` (router stub)
  - `rag.rerank_model:v1` (reranker stub)
  - `llm.chat:v1` (demo deterministic “LLM tool”)
- **Write**:
  - `logs.ingest:v1` (requires Elasticsearch configured)
  - `incidents.create_ticket:v1` (simulated ticket creation)
  - `remediation.restart_service:v1` (dangerous; must be policy/approval gated)
  - `vectors.upsert:v1` (requires pgvector enabled)

### HTTP plugin tools (optional; loaded only if `PLUGINS_ENABLED=true`)

Loaded from `plugins/tools/*.json` via `internal/dataplane/tools/plugin_loader.go`.

- **Read**:
  - `web.search:v1`, `web.fetch:v1`
  - `github.search_issues:v1`
  - `math.query:v1`
- **Write**:
  - `image.generate:v1`

## Global invariants (apply to every tool call)

- **Policy gate first**:
  - If tool tags include `write`, require `policy.AllowWrites=true`.
  - Enforce `policy.ToolTagAllow` and `policy.Scopes` (already implemented in `internal/controlplane/temporal/activies.go`).
- **Input validation**:
  - Validate required args are present (schema required fields).
  - Enforce size caps:
    - **Max args JSON size**: 32 KiB (read), 8 KiB (write), 16 KiB (external HTTP plugin).
    - **Max string field length**: 8 KiB unless tool schema explicitly allows more.
  - Reject/trim obviously unbounded inputs (raw logs dumps, huge HTML, base64 blobs).
- **Secret hygiene**:
  - Never include secrets/tokens in tool args unless the tool explicitly requires them (prefer env/config).
  - Redact known secret patterns in logs/traces (API keys, bearer tokens, basic auth).
- **Deterministic stopping**:
  - Respect strategy budgets: `MaxWallTime`, `MaxToolCalls`, `MaxIterations` (already present in `internal/agent/strategy/*`).
  - If budget is hit, stop and return “best available” partial output + evidence of what succeeded/failed.

## Timeouts (concrete defaults)

Time limits are per tool call and must not exceed the parent context deadline.

- **Core MCP (local/internal)**
  - `logs.search` / `logs.ingest`: 8s (current behavior)
  - `vectors.search` / `vectors.upsert`: 8s (current behavior)
  - `rag.single_retrieve`: 15s (current behavior)
  - `rag.multiple_retrieve`: 25s (current behavior)
  - `llm.router`, `rag.rerank_model`, `llm.chat` (local stubs): 1s (recommended)

- **HTTP plugin tools**
  - `web.search`: 8s (current tool pack)
  - `web.fetch`: 10s (current tool pack)
  - `github.search_issues`: 8s (current tool pack)
  - `math.query`: 8s (current tool pack)
  - `image.generate`: 20s (current tool pack)

## Retries (concrete rules)

Retries are **allowed only for idempotent operations** and only on **transient failures**.

### Transient failures (retryable)

- Network: DNS failure, connection refused/reset, TLS handshake, EOF, timeouts.
- HTTP: `429`, `502`, `503`, `504`.
- MCP: transport errors (client cannot reach MCP server), or tool returns an error classified as transient.

### Non‑transient failures (not retryable)

- Validation errors (missing args, schema mismatch).
- Authorization/policy errors (missing scope, write blocked).
- Tool-not-found, unsupported tool type, endpoint not allowlisted.
- HTTP `4xx` other than `429` (treat as caller error).

### Retry budget by tool class

- **Read + idempotent** (`logs.search`, `vectors.search`, `rag.*`, `web.*`, `github.*`, `math.query`):
  - Up to **2 retries** (3 total attempts)
  - Backoff: 200ms → 800ms (+ jitter 0–200ms)
  - If HTTP `429` has `Retry-After`, honor it (capped at 2s).

- **Write + idempotent** (`logs.ingest`, `vectors.upsert` *if* caller provides stable id):
  - Up to **1 retry** (2 total attempts)
  - Only if caller includes an **idempotency key** (see below), or the write is naturally idempotent (upsert by stable ID).

- **Write + non-idempotent** (`incidents.create_ticket`, `remediation.restart_service`, `image.generate`):
  - **No automatic retries**.
  - If it fails transiently, the agent must ask for user confirmation before re-attempting.

## Idempotency rules (write safety)

For any tool tagged `write`, the agent should supply:

- **`request_id`**: stable per “user intent” (e.g., task ID + step ID)
- **`idempotency_key`**: stable for the exact write action (same inputs ⇒ same key)

If a tool does not support these fields today, treat the write as **non-idempotent** for retry purposes.

## Fallback policy (what to do when tools fail/are slow)

- **Degrade in this order**:
  1. Retry (if allowed by the retry policy)
  2. Call an alternative *equivalent* tool (e.g., prefer internal RAG over external when possible)
  3. Return partial/approximate output with explicit “missing evidence” notes

- **Repo-specific fallbacks already present**:
  - `logs.search` falls back to simulated results if Elasticsearch errors.
  - RAG embedder falls back from HTTP embedder to hash embedder.

- **Recommended fallbacks**:
  - If `web.fetch` fails, attempt `web.search` for alternate sources (read-only).
  - If `rag.multiple_retrieve` times out, retry with lower `top_k` / fewer datasets, then fall back to `rag.single_retrieve`.

## Output quality checks (before choosing next step)

After any tool returns, the agent must classify the result:

- **Accept**: output matches schema expectations and is consistent with prior evidence.
- **Partial**: output is present but incomplete (timeouts, truncated lists, “simulated” engines).
- **Reject**: output is malformed, contradictory, or clearly unrelated to the query.

If Partial/Reject, the next step must be one of: retry (if allowed), narrower query, different tool, or stop with partial answer.

## Default strategy budgets (recommended)

These complement existing budgets in `internal/agent/strategy/function_calling.go` and `internal/agent/strategy/react.go`.

- **Interactive agent run**:
  - `MaxWallTime`: 30s
  - `MaxToolCalls`: 8
  - `MaxIterations`: 6

- **Workflow (Temporal) plan execution**:
  - Keep the workflow loop bounded (max iterations) and keep per-step timeouts at tool-level values above.

## Checklist for adding a new tool

- Assign **tags**: `read` vs `write`, and `external` if it crosses trust boundary.
- Define **schema** with required fields and tight types.
- Pick a **timeout** and whether it is **retryable**.
- Provide **idempotency** fields if it is a write tool.
- Ensure **policy/scopes** are meaningful and enforced.


