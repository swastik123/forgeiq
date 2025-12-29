## Scripts

### Test MCP + pgvector + Elasticsearch (local)

Prereqs:
- `docker compose` running the stack from the repo root
- `jq` installed (used to pretty-print JSON)

Run:

```bash
chmod +x scripts/test_mcp_vector_elastic.sh
./scripts/test_mcp_vector_elastic.sh
```

Notes:
- `docker-compose.yml` sets `VECTOR_DIM=8` to make local testing easy. In production, set `VECTOR_DIM` to your embedding model dimension (e.g. 1536/3072/etc).
- `logs.ingest` is only meant for local smoke tests; real deployments typically ingest logs via your log pipeline.
- The script also exercises `rag.single_retrieve` and `rag.multiple_retrieve`. Internal vs external retrieval is driven by `RAG_DATASETS_JSON` dataset `"type"`.

### Agent Router (A2A discovery + routing)

Enable:
- `AGENT_ROUTER_ENABLED=true`
- `AGENT_ROUTER_AGENTS_JSON` set to a JSON array of agents (each with `id`, `type`, `base_url`, optional `tags`, `task_types`, and auth fields).

When enabled, the Temporal worker will route `rule` and `decision` agent calls dynamically instead of using only `RULE_AGENT_URL` / `DECISION_AGENT_URL`.

### Real-time updates (SSE)

The orchestrator exposes an SSE stream for workflow status updates:
- `GET /events/{taskID}` (optional query param `interval_ms`, clamped 250–5000)

Example:
`curl -N http://localhost:8080/events/task-...`


