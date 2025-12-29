# Getting Started Guide

This guide will help you get started with ForgeIQ in minutes.

## Prerequisites

- **Go 1.22+**: [Install Go](https://golang.org/doc/install)
- **Docker**: [Install Docker](https://docs.docker.com/get-docker/)
- **Docker Compose**: Usually included with Docker Desktop
- **Temporal** (optional): For local Temporal server

## Quick Start (5 minutes)

### Option 1: Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/your-org/forgeiq.git
cd forgeiq

# Start all services
docker-compose up -d

# Check services are running
curl http://localhost:8080/health
```

### Option 2: Local Development

```bash
# 1. Clone and enter directory
git clone https://github.com/your-org/forgeiq.git
cd forgeiq

# 2. Install dependencies
go mod download

# 3. Start Temporal (in one terminal)
docker run -p 7233:7233 temporalio/auto-setup:latest

# 4. Start services (in separate terminals)
# Terminal 1: Orchestrator
go run ./cmd/control-orchestrator

# Terminal 2: Worker
go run ./cmd/control-temporal-worker

# Terminal 3: Rule Agent
go run ./cmd/control-rule-agent

# Terminal 4: Decision Agent
go run ./cmd/control-decision-agent

# Terminal 5: MCP Server
go run ./cmd/data-mcp-server
```

## Your First Workflow

### 1. Start a Workflow

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "INC-123",
    "service": "payments",
    "symptom": "high error rate"
  }'
```

Response:
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

### 2. Check Status

```bash
TASK_ID="task-20240101-120000.000"
curl http://localhost:8080/status/$TASK_ID
```

### 3. Approve Write Operations (if needed)

```bash
curl -X POST http://localhost:8080/approve/$TASK_ID \
  -H "Content-Type: application/json" \
  -d '{"approved": true}'
```

### 4. Get Result

```bash
curl http://localhost:8080/result/$TASK_ID
```

## Configuration

### Environment Variables

Create a `.env` file:

```bash
# Server
PORT=8080

# Temporal
TEMPORAL_HOSTPORT=localhost:7233

# Storage (optional - defaults to memory)
STORAGE_TYPE=postgres
STORAGE_DSN=postgres://user:pass@localhost:5432/forgeiq?sslmode=disable

# External Services (optional)
RULE_AGENT_URL=http://localhost:8081
DECISION_AGENT_URL=http://localhost:8082
MCP_BASE_URL=http://localhost:8090
```

### Using Configuration File

```bash
export $(cat .env | xargs)
go run ./cmd/control-orchestrator
```

## Next Steps

### 1. Connect to External Services

See [External Services Guide](examples/external-services.md)

### 2. Create Custom Agents

See [Plugin Examples](examples/plugin_example.go)

### 3. Use Agentic Loop

See [Agentic Loop Guide](docs/AGENTIC_LOOP.md)

### 4. Deploy to Production

See [Production Guide](docs/PRODUCTION_READY.md)

## Common Tasks

### View Metrics

```bash
curl http://localhost:8080/metrics
```

### Health Check

```bash
curl http://localhost:8080/health
```

### View Logs

```bash
# Docker Compose
docker-compose logs -f orchestrator

# Local
# Logs appear in stdout
```

## Troubleshooting

### Services Won't Start

1. Check ports are available:
```bash
lsof -i :8080
lsof -i :8081
lsof -i :8082
lsof -i :8090
```

2. Check Temporal is running:
```bash
curl http://localhost:7233/health
```

### Workflow Fails

1. Check workflow status:
```bash
curl http://localhost:8080/status/$TASK_ID
```

2. Check logs:
```bash
docker-compose logs worker
```

### Database Connection Issues

1. Test connection:
```bash
psql $STORAGE_DSN -c "SELECT 1"
```

2. Check health:
```bash
curl http://localhost:8080/health
```

## Examples

### Complete Example Script

```bash
#!/bin/bash

# Start workflow
RESPONSE=$(curl -s -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "INC-123",
    "service": "payments",
    "symptom": "high error rate"
  }')

TASK_ID=$(echo $RESPONSE | jq -r '.task_id')

echo "Started workflow: $TASK_ID"

# Wait and check status
sleep 2
curl -s http://localhost:8080/status/$TASK_ID | jq

# Approve if needed
curl -s -X POST http://localhost:8080/approve/$TASK_ID \
  -H "Content-Type: application/json" \
  -d '{"approved": true}'

# Get final result
sleep 5
curl -s http://localhost:8080/result/$TASK_ID | jq
```

## Resources

- [Documentation](docs/)
- [API Reference](docs/API.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Examples](examples/)
- [FAQ](docs/FAQ.md)

## Need Help?

- [Open an Issue](https://github.com/your-org/agent-platform/issues)
- [Check Documentation](docs/)
- [Join Discussions](https://github.com/your-org/agent-platform/discussions)


