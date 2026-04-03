## Memory Governance + Canonical Store + Discovery Index (LTM) for ForgeIQ

### Goal (what “memory” means)

ForgeIQ **workflow memory** is the *current workflow state* (small typed buckets) owned by the workflow.

This document adds **LTM (Long-Term Memory)** as a separate, governed subsystem:
- **Canonical Store**: authoritative, structured records with versioning + metadata.
- **Discovery Index**: semantic index for retrieval that returns **IDs only** (never execution-ready content).
- **Governance Layer**: validates writes/reads, enforces policy, and controls what can be injected into agent handoffs.

### Architecture (controller + stateless agents)

- **Workflow Orchestration Engine (Controller)**:
  - Schedules agent steps.
  - Owns workflow state + applies deltas.
  - Is the **only writer** to Canonical Store (via Governance APIs/tools).
  - Builds handoff packets (minimal, policy-filtered context).

- **Agent Runtime Layer (Stateless)**:
  - Receives structured inputs (handoff + pointers).
  - Produces structured outputs (deltas + memory proposals).
  - Has **no direct access** to Canonical Store or Discovery Index.

### Components

#### 1) Memory Governance Layer

**Responsibilities**
- Accept **memory proposals** (from workflows after tool/agent outputs).
- Validate proposals:
  - provenance + evidence references
  - conflict checks vs existing canonical record versions
  - confidence, expiry (TTL), sensitivity classification
  - tenant isolation + redaction rules
- Write canonical records + append audit/version entries.
- Maintain/update the Discovery Index using **sanitized text** only.
- Serve retrieval:
  - Discovery search returns **record IDs only**
  - Canonical fetch returns structured records with redaction enforced
- Provide **handoff-safe injection packs** (optional): minimal, structured, non-executable slices for specific agent types.

#### 2) Canonical Memory Store (authoritative)

**Recommended backing store in ForgeIQ**: **Postgres**.

**Record properties**
- `record_id` (stable identifier)
- `record_type` / `schema_version`
- `payload` (structured JSON; no “instructions”)
- `evidence_refs` (artifact IDs/URIs, tool run IDs)
- `confidence_score`
- `expires_at`
- `sensitivity` (public/internal/restricted/secret)
- `version` and audit history (who/why/when)

**Rule**: Canonical store can hold rich data, but agents never read it directly.

#### 3) Discovery Memory Index (semantic)

**Recommended backing store in ForgeIQ**:
- Default: **pgvector** (already present)
- Optional: Elasticsearch vector/hybrid (already present)

**Critical rule**: discovery index returns **only**
- `record_id`
- `score`
- minimal metadata (e.g., `record_type`, `tenant_id`, `sensitivity`)

It must **never** return:
- full text content
- code snippets
- prompts
- any execution-ready instructions

### Existing ForgeIQ mapping

- **pgvector**: `internal/vector/pgvector` (good fit for Discovery Index)
- **Elasticsearch**: `internal/dataplane/rag/retriever_external_elastic.go` (currently returns `Content` today; for LTM discovery you should use an ID-only contract)
- **Workflow-owned state (“memory buckets”)**: control-plane Temporal workflows (`internal/controlplane/temporal/*`)
- **Artifacts (big blobs)**: should remain in the artifact store; canonical records reference artifacts by ID/URI.

### Interfaces / API shape (controller-centric)

You can implement this either as:
- **HTTP service** (memory-governor), or
- **MCP tools** hosted by `data-mcp-server`.

Minimal endpoints/tools:
- `POST /memory/proposals`: submit proposals (with evidence refs + sensitivity + confidence + expiry)
- `POST /memory/validate`: validate/approve/reject (policy + conflicts)
- `POST /memory/search`: discovery search → returns `[record_id, score]` only
- `GET /memory/records/{id}`: canonical fetch (redacted by sensitivity)
- `POST /handoff/inject`: build an agent-specific safe context pack (optional)

### MCP tool names (MVP)

This repo provides an MCP-first MVP:
- `memory.propose:v1`: write canonical record + optionally update discovery index
- `memory.search:v1`: discovery search returning **IDs only**
- `memory.get:v1`: canonical fetch with **redaction**

### Safety constraints (non-negotiable)

- **Only workflow/controller can write canonical records** (agents emit proposals only).
- **Discovery returns IDs only**.
- **Canonical payloads are non-executable** (structured facts, evidence pointers; no instructions).
- **Injection is gated**:
  - redaction
  - token budgets
  - tenant boundaries
  - sensitivity checks

### Why this fits ForgeIQ well

- Aligns with the existing “workflow is the only writer” pattern.
- Enables parallel agents safely (they only emit deltas/proposals).
- Gives auditability + replay (versions + evidence references).
- Reduces prompt injection risk by preventing agents from free-form “memory retrieval”.

