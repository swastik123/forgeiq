    ## Memory (Canonical + Discovery) via MCP tools

    This doc explains the **memory changes** added to ForgeIQ as an **MCP-first MVP**.

    ### What changed (high level)

    ForgeIQ now supports a governed “long-term memory” split into:
    - **Canonical Store (authoritative)**: Postgres table that stores structured records + metadata.
    - **Discovery Index (semantic)**: pgvector index that supports similarity search but returns **IDs only**.
    - **MCP tools** exposed by `data-mcp-server` to use both of the above.

    Agents remain **stateless**; workflows/controllers can call these tools via MCP.

    ---

    ### New MCP tools

    #### 1) `memory.propose:v1`

    **Purpose**
    - Write/update a canonical memory record in Postgres.
    - Optionally index a **sanitized** text string into pgvector (for discovery).

    **Key properties**
    - Canonical payload is **structured JSON** (`payload` object).
    - Indexing uses `sanitized_text` and stores **no content** in the vector store row (`content` is empty).

    #### 2) `memory.search:v1` (IDs only)

    **Purpose**
    - Semantic search over the discovery index.

    **Hard rule**
    - Returns **only** `record_id`, `score`, and safe `metadata`.
    - Does **not** return any text content.

**Additional fields**
- `rank`: 1..N (stable within the response)
- Optional `reason_codes` if post-filtering is enabled (e.g. `expired_filtered`, `low_confidence_filtered`, `sensitivity_filtered`)
- If `include_filtered=false` (default), filtered matches are returned under `filtered[]` with `filtered_count`.

    #### 3) `memory.get:v1` (redacted)

    **Purpose**
    - Fetch a canonical memory record from Postgres.

    **Redaction rules**
    - If the record is **expired** (`expires_at` in the past): payload is removed.
    - If the record’s `sensitivity` is **higher** than `max_sensitivity`: payload is removed.
    - Response includes booleans `expired` and `redacted`.

    ---

    ### Canonical Store (Postgres) implementation

    Implemented in:
    - `internal/memory/postgres_store.go`
    - `internal/memory/types.go`

    The store auto-creates this table:
    - `memory_records`

    Notable fields:
    - `payload` (JSONB)
    - `evidence_refs` (JSONB array)
    - `confidence` (float)
    - `sensitivity` (string enum-ish: `public|internal|restricted|secret`)
    - `expires_at` (TIMESTAMPTZ)
    - `version` (auto-incremented on upsert)

    ---

    ### Discovery Index (pgvector) implementation

    Reuses existing pgvector store:
    - `internal/vector/pgvector`

    Indexing behavior:
    - namespace: `memory:<tenant_id>` (defaults to `memory:default`)
    - id: `record_id`
    - content: always `""` (empty) for memory discovery rows
    - metadata: `record_type`, `sensitivity`, `confidence`, `expires_at`, `version`, etc.

    ---

    ### How tools are registered

    `data-mcp-server` calls:
    - `tools.RegisterAll(s, cfg, logger)`

    Tool registration entry point:
    - `internal/dataplane/tools/tools.go`

    Memory tools are registered by:
    - `internal/dataplane/tools/memory_tools.go`

    ---

    ### Required configuration (env vars)

    Canonical store (Postgres):
    - **`STORAGE_DSN`**: Postgres DSN used by canonical memory store (required for `memory.propose`/`memory.get`)

    Discovery index (pgvector):
    - **`VECTOR_ENABLED=true`**
    - **`VECTOR_DSN`**: Postgres DSN for pgvector (defaults to `STORAGE_DSN` if `VECTOR_DSN` unset in config)
    - **`VECTOR_DIM`**: must match the embedder dim (existing config uses `Embeddings.Dim`)

    Embeddings:
    - **Dev/Test**: deterministic hash embedder (works without external services), but it is **NOT semantic** (similar meaning will not reliably be “near” in vector space).
      - `EMBEDDINGS_PROVIDER=hash` (default)
    - **Production semantic retrieval**: configure an embeddings endpoint:
      - `EMBEDDINGS_PROVIDER=http`
      - `EMBEDDINGS_URL=...`
      - `EMBEDDINGS_API_KEY=...` (if needed)
      - `EMBEDDINGS_MODEL=...` (if needed)

    Notes:
    - If `EMBEDDINGS_PROVIDER=http` is set but the HTTP embedder fails to initialize, ForgeIQ will **fail fast** (no silent fallback to hash), so you don’t accidentally run “fake semantic” retrieval in production.

    ---

    ### How workflows/controllers use it

    Workflows already have a tool-calling path (MCP `tools.call`) via control-plane activities.
    So the controller can call:
    - `memory.propose` after a tool run / agent delta
    - `memory.search` to find candidate record IDs
    - `memory.get` to fetch a redacted canonical record for a safe handoff pack

    Agents should not call these tools directly (keep “memory” controller-owned).

    ---

    ### Quick test with `curl` (MCP JSON-RPC)

    Assuming `data-mcp-server` is running on `:8090`:

    #### Propose a record

    ```json
    {
    "jsonrpc": "2.0",
    "id": "1",
    "method": "tools.call",
    "params": {
        "name": "memory.propose",
        "version": "v1",
        "args": {
        "tenant_id": "default",
        "record_type": "incident.fact",
        "payload": {
            "service": "payments",
            "symptom": "elevated 5xx",
            "root_cause_hint": "db pool exhaustion"
        },
        "evidence_refs": ["artifact:logs:abc123"],
        "confidence": 0.7,
        "sensitivity": "internal",
        "sanitized_text": "payments service elevated 5xx errors db pool exhaustion"
        }
    }
    }
    ```

    #### Search (IDs only)

    ```json
    {
    "jsonrpc": "2.0",
    "id": "2",
    "method": "tools.call",
    "params": {
        "name": "memory.search",
        "version": "v1",
        "args": {
        "tenant_id": "default",
        "query": "payments 5xx db pool",
        "top_k": 5
        }
    }
    }
    ```

    #### Get (redacted)

    ```json
    {
    "jsonrpc": "2.0",
    "id": "3",
    "method": "tools.call",
    "params": {
        "name": "memory.get",
        "version": "v1",
        "args": {
        "tenant_id": "default",
        "record_id": "mem_...",
        "max_sensitivity": "internal",
        "include_payload": true
        }
    }
    }
    ```

---

### Notes / limitations (MVP)

- This MVP does **not** yet implement:
  - proposal validation workflows (policy engine, conflict resolution)
  - a separate audit/event log table (beyond version increments)
  - Elasticsearch-backed discovery for memory
- The critical safety guarantees are enforced:
  - discovery search is **IDs only**
  - canonical get is **redacted** based on sensitivity/expiry

