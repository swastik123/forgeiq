# Workflow-Controlled Auto-Correction (Stateless Agents)

ForgeIQ’s agent services are intentionally **stateless**: they do not accumulate private “learning state” between requests. If we want the system to improve reliability over time, we do it **outside the agents**, using the workflow as the controller.

This document describes the **evaluation-driven auto-correction loop** implemented in ForgeIQ:

- **Execution**: workflows run agents/tools using minimal handoff context.
- **Evaluation**: an evaluator checks outputs against constraints.
- **Correction artifacts**: violations become **structured artifacts** (not free-form reflections).
- **Governed memory**: artifacts are stored in the canonical memory store with policy enforcement.
- **Selective reinjection**: on later runs, only relevant artifacts are fetched and injected (delta-only).

## Why this architecture

- **Deterministic + auditable**: workflows remain the only writer; each correction has provenance and TTL.
- **Safer than reflection**: avoids “prompt injection persistence” and unverified self-modification.
- **Cheaper than replay**: reinject small, relevant deltas instead of full historical context.

## Components and responsibilities

### Orchestration engine (Temporal workflow)
- Executes agent steps and tool calls.
- Runs evaluation after phases/steps.
- Stores correction artifacts via `memory.propose:v1`.
- Fetches and injects relevant artifacts via `memory.search:v1` → `memory.get:v1`.

### Evaluation engine (external to agents)
- Analyzes structured outputs (plan/tool results/errors).
- Produces **structured** `correction_artifact` payloads.
- Returns a **verdict** (e.g., proceed/replan/stop).

### Memory governance + canonical store
- Stores correction artifacts as canonical records:
  - `record_type = "correction_artifact"`
  - payload is structured JSON (no executable instructions)
- Enforces:
  - confidence policy (server-side computed)
  - evidence requirements / TTL defaults
  - sensitivity limits

### Reinjection (delta-only)
- The workflow retrieves candidates by vector search (IDs only).
- The workflow fetches canonical records (redacted if needed).
- The workflow injects only a small set into the next agent call (e.g. `task.input["corrections"]`).

## What’s implemented in code

### Autocorrect engine (deterministic evaluator)
- `internal/autocorrect/engine.go`
- `internal/autocorrect/types.go`

Produces structured `CorrectionArtifact` objects and a `Verdict`.

### Temporal activities
- `internal/controlplane/temporal/autocorrect_activities.go`

Provides:
- `EvaluateAndStoreCorrections(...)`: evaluate → store via `memory.propose:v1`
- `FetchCorrections(...)`: search/get → return redacted canonical records

### Workflow wiring (Incident workflow)
- `internal/controlplane/temporal/workflow.go`

Current wiring:
- After policy allow: **fetch corrections** and add them to `task.input["corrections"]` before planning.
- After planning: evaluate/store corrections based on `plan_confidence` (best-effort).
- After tool step errors: evaluate/store corrections (best-effort).

## Tool contracts used
- `memory.search:v1`: returns IDs-only matches + rank/score + safe metadata.
- `memory.get:v1`: returns canonical record (payload may be redacted/expired).
- `memory.propose:v1`: stores canonical records and updates discovery index; handler computes confidence/TTL safely.

## Notes / extension points

- **Applicability**: keep applicability filters explicit (tenant/task_type/agent/tool) to prevent over-injection.
- **Conflict resolution**: if multiple corrections conflict, the policy engine should pick one or require approval.
- **Rerank**: vector search is recall; add metadata reranking (recency, evidence strength) for precision.
- **Security**: never store or reinject raw “instructions”; store structured constraints/guardrails instead.

