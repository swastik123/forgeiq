# API Reference

## Orchestrator API (`:8080`)

### Start Workflow

Start a new workflow execution.

**Endpoint**: `POST /run`

**Request Body**:
```json
{
  "incident_id": "INC-123",
  "service": "payments",
  "symptom": "high error rate"
}
```

**Response**:
```json
{
  "workflow_id": "task-20240101-120000.000",
  "run_id": "...",
  "task_id": "task-20240101-120000.000",
  "status_url": "/status/task-20240101-120000.000",
  "result_url": "/result/task-20240101-120000.000",
  "approve_url": "/approve/task-20240101-120000.000"
}
```

### Query Status

Get current workflow status.

**Endpoint**: `GET /status/{taskID}`

**Response**:
```json
{
  "state": "running",
  "task_id": "task-20240101-120000.000",
  "tool_calls": 2,
  "evidence": {...},
  "updated_at_rfc": "2024-01-01T12:00:00Z"
}
```

### Get Result

Get workflow result (if completed).

**Endpoint**: `GET /result/{taskID}`

**Response**:
```json
{
  "task_id": "task-20240101-120000.000",
  "type": "Result",
  "payload": {
    "status": "completed",
    "evidence": {...}
  },
  "ts": "2024-01-01T12:05:00Z"
}
```

### Approve Write Operations

Approve write operations for a workflow.

**Endpoint**: `POST /approve/{taskID}`

**Request Body** (optional):
```json
{
  "approved": true
}
```

**Response**:
```json
{
  "task_id": "task-20240101-120000.000",
  "approved": true
}
```

### Provide Feedback (Agentic Loop)

Provide feedback during agentic loop execution.

**Endpoint**: `POST /feedback/{taskID}`

**Request Body**:
```json
{
  "step_id": "s1",
  "feedback": "Looks good, proceed",
  "action": "continue"
}
```

**Actions**: `continue`, `stop`, `modify`, `approve`, `reject`

### Request Plan Refinement

Request plan refinement during execution.

**Endpoint**: `POST /refine/{taskID}`

**Response**:
```json
{
  "task_id": "task-20240101-120000.000",
  "refinement_requested": true
}
```

### Stop Workflow

Stop the agentic loop.

**Endpoint**: `POST /stop/{taskID}`

**Response**:
```json
{
  "task_id": "task-20240101-120000.000",
  "stopped": true
}
```

### Health Check

Check service health.

**Endpoint**: `GET /health`

**Response**:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "uptime": "1h30m",
  "checks": {
    "temporal": "healthy"
  }
}
```

### Readiness Probe

Check if service is ready.

**Endpoint**: `GET /ready`

**Response**:
```json
{
  "status": "ready"
}
```

### Metrics

Prometheus metrics endpoint.

**Endpoint**: `GET /metrics`

**Response**: Prometheus format

## Agent APIs

### Agent Discovery

Get agent metadata.

**Endpoint**: `GET /.well-known/agent.json`

**Response**:
```json
{
  "name": "rule-agent",
  "description": "Evaluates policy",
  "task_types": ["incident_triage"],
  "endpoint": "http://localhost:8081/task",
  "version": "0.1"
}
```

### Execute Task

Execute an agent task.

**Endpoint**: `POST /task`

**Request Body**:
```json
{
  "id": "task-123",
  "type": "incident_triage",
  "input": {...},
  "metadata": {},
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Response**:
```json
{
  "task_id": "task-123",
  "type": "PolicyDecision",
  "payload": {...},
  "ts": "2024-01-01T12:00:01Z"
}
```

## MCP Server API (`:8090`)

### List Tools

List available tools.

**Endpoint**: `POST /rpc`

**Request**:
```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "tools.list",
  "params": {}
}
```

**Response**:
```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": [
    {
      "name": "logs.search",
      "version": "v1",
      "tags": ["read", "logs"],
      "scopes": ["logs:read"]
    }
  ]
}
```

### Call Tool

Execute a tool.

**Endpoint**: `POST /rpc`

**Request**:
```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "tools.call",
  "params": {
    "name": "logs.search",
    "version": "v1",
    "args": {
      "query": "error",
      "limit": 50
    }
  }
}
```

**Response**:
```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": {
    "logs": [...],
    "count": 10
  }
}
```

## Error Responses

All endpoints may return errors in this format:

```json
{
  "error": "Error message",
  "code": "ERROR_CODE",
  "details": "..."
}
```

**HTTP Status Codes**:
- `200` - Success
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `429` - Rate Limited
- `500` - Internal Server Error
- `503` - Service Unavailable


