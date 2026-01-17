# Runbook Automation Workflow (Temporal + A2A Agents)

This example shows how to use ForgeIQ to implement a **Runbook Automation** flow:

1. The **Orchestrator** accepts an incident (`/run`).
2. A Temporal **RunbookAutomationWorkflow**:
   - Consults the **Rule Agent** to pick a runbook.
   - Fetches the runbook steps from the **Runbook Agent**.
   - Runs diagnostics via the **Observability Agent**.
   - Executes actions via the **Exec Agent** (e.g., Kubernetes restart/scale).
   - Waits for **human approval** before risky actions.
3. Operators approve specific steps via `/approve/{taskID}`.

All agents use a **shared A2A contract** (`A2ATaskRequest` / `A2ATaskResponse`) and are real HTTP services, not in-process fakes.

---

## Components

### Control Plane

- **Orchestrator** (`cmd/control-orchestrator`)
  - `POST /run` – starts `RunbookAutomationWorkflow`
  - `POST /approve/{taskID}` – sends approval signals into the workflow
  - `GET /status/{taskID}` – shows workflow status (already present)
  - `GET /result/{taskID}` – fetches final result (already present)

- **Temporal Worker** (`cmd/control-temporal-worker`)
  - Registers:
    - `RunbookAutomationWorkflow`
    - `CallRuleAgentActivity`
    - `CallRunbookAgentActivity`
    - `CallObservabilityAgentActivity`
    - `CallExecAgentActivity`

### Data / Agent Plane

All agents expose:

- `GET /.well-known/agent.json` – discovery
- `POST /task` – executes an A2A task
- `GET /health` – health check

Agents used in this example:

- `cmd/control-rule-agent`
  - Decides which runbook to use for an incident.
- `cmd/control-runbook-agent`
  - Returns a runbook: a list of **diagnostic steps** + **action steps**.
- `cmd/data-observability-agent`
  - Runs Prometheus / Loki queries and returns **structured signals** the workflow can use (not just a string).
  - Configure backends:
    - `PROMETHEUS_URL` (e.g. `http://localhost:9090`)
    - `LOKI_URL` (e.g. `http://localhost:3100`)
    - Optional port: `OBS_AGENT_PORT` / `PORT` (default `8083`)
- `cmd/data-exec-agent`
  - Executes actions against Kubernetes using `client-go`
    - e.g. restart a Deployment, scale replicas, etc.

---

## A2A Contract

All agents share the same envelope types in:

```text
internal/controlplane/contracts/a2a.go
