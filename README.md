# ForgeIQ

[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/workflow/status/your-org/forgeiq/CI)](https://github.com/your-org/forgeiq/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/your-org/forgeiq)](https://goreportcard.com/report/github.com/your-org/forgeiq)

ForgeIQ is a production-ready framework for orchestrating AI agents with policy enforcement, workflow management, and support for external A2A agents and MCP servers.

## Features

- ✅ **Temporal Workflows**: Reliable, stateful workflow orchestration
- ✅ **Agentic Loop**: Iterative refinement with human-in-the-loop
- ✅ **Policy Enforcement**: Write operations require explicit approval
- ✅ **Persistent Storage**: PostgreSQL with automatic migrations
- ✅ **External Services**: Connect to any cloud-hosted agents (AWS, GCP, Azure, Hugging Face)
- ✅ **Plugin System**: Register custom agents and tools
- ✅ **Observability**: Prometheus metrics, structured logging, distributed tracing
- ✅ **Production Ready**: Error handling, rate limiting, health checks, graceful shutdown

## Quick Start

### Local Development

Run 4 services in separate terminals:

1) Data Plane MCP Server:
   ```bash
   go run ./cmd/data-mcp-server
   ```

2) Rule Agent (A2A):
   ```bash
   go run ./cmd/control-rule-agent
   ```

3) Decision Agent (A2A):
   ```bash
   go run ./cmd/control-decision-agent
   ```

4) Temporal Worker:
   ```bash
   go run ./cmd/control-temporal-worker
   ```

5) Orchestrator (Control Plane):
   ```bash
   go run ./cmd/control-orchestrator
   ```

Then trigger a workflow:
```bash
curl -s -X POST localhost:8080/run -d '{"incident_id":"INC-123","service":"payments","symptom":"high error rate"}' | jq
```

## Connecting to External Services

The platform supports connecting to external A2A agents and MCP servers hosted on cloud platforms (AWS, GCP, Azure, Hugging Face, etc.).

### Configuration

Set environment variables to configure external services:

```bash
# Rule Agent (external)
export RULE_AGENT_URL="https://your-rule-agent.example.com"
export RULE_AGENT_API_KEY="your-api-key"

# Decision Agent (external)
export DECISION_AGENT_URL="https://your-decision-agent.example.com"
export DECISION_AGENT_TOKEN="your-bearer-token"

# MCP Server (external)
export MCP_BASE_URL="https://mcp-server.example.com"
export MCP_API_KEY="your-mcp-api-key"
```

See [examples/external-services.md](examples/external-services.md) for detailed examples and platform-specific configurations.

### Authentication Methods

- **API Key**: Set `*_API_KEY` environment variables
- **Bearer Token**: Set `*_TOKEN` environment variables  
- **Custom Headers**: Set `*_HEADERS` as comma-separated `key=value` pairs

## Features

- ✅ **Policy Enforcement**: Write operations require explicit approval
- ✅ **Workflow Orchestration**: Temporal-based reliable workflows with state persistence
- ✅ **Persistent Storage**: PostgreSQL support with automatic migrations
- ✅ **External Service Support**: Connect to any cloud-hosted agents and MCP servers (AWS, GCP, Azure, Hugging Face, etc.)
- ✅ **Plugin System**: Register custom agents and tools without modifying core code
- ✅ **Observability**: Prometheus metrics, structured logging, distributed tracing
- ✅ **Error Handling**: Comprehensive error recovery and retry logic
- ✅ **Input Validation**: Request validation and sanitization
- ✅ **Rate Limiting**: Protect APIs from abuse
- ✅ **Health Checks**: `/health` and `/ready` endpoints
- ✅ **Graceful Shutdown**: Proper signal handling
- ✅ **Deployment Ready**: Docker, Docker Compose, and Kubernetes manifests
- ✅ **Production Ready**: All production features implemented

## API Endpoints

### Orchestrator (`:8080`)

- `POST /run` - Start a workflow
- `GET /status/{taskID}` - Query workflow status
- `GET /result/{taskID}` - Get workflow result
- `POST /approve/{taskID}` - Approve write operations
- `GET /health` - Health check
- `GET /ready` - Readiness probe
- `GET /metrics` - Prometheus metrics

### Agents (`:8081`, `:8082`)

- `GET /.well-known/agent.json` - Agent discovery
- `POST /task` - Execute agent task
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics

### MCP Server (`:8090`)

- `POST /rpc` - JSON-RPC endpoint
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics

## Configuration

See [examples/.env.example](examples/.env.example) for all configuration options.

## Documentation

- 📖 [Getting Started](docs/GETTING_STARTED.md) - Quick start guide
- 🏗️ [Architecture](docs/ARCHITECTURE.md) - System architecture overview
- 🚀 [Production Guide](docs/PRODUCTION_READY.md) - Production deployment guide
- 🔄 [Agentic Loop](docs/AGENTIC_LOOP.md) - Agentic loop pattern guide
- 🔌 [External Services](examples/external-services.md) - Connect to cloud services
- 🧩 [Plugin System](examples/plugin_example.go) - Create custom agents/tools
- ❓ [FAQ](docs/FAQ.md) - Frequently asked questions
- 🗺️ [Roadmap](ROADMAP.md) - Planned features

## Quick Links

- [Contributing](CONTRIBUTING.md) - How to contribute
- [Code of Conduct](CODE_OF_CONDUCT.md) - Community guidelines
- [Security](SECURITY.md) - Security policy
- [Changelog](CHANGELOG.md) - Version history

## Quick Start

### Using Docker Compose (Recommended)

```bash
docker-compose up
```

### Manual Setup

1. Start Temporal:
```bash
docker run -p 7233:7233 temporalio/auto-setup:latest
```

2. Start PostgreSQL (optional, for persistence):
```bash
docker run -e POSTGRES_PASSWORD=changeme -p 5432:5432 postgres:15-alpine
```

3. Configure environment:
```bash
export STORAGE_TYPE=postgres
export STORAGE_DSN="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable"
export TEMPORAL_HOSTPORT=localhost:7233
```

4. Start services:
```bash
go run ./cmd/control-orchestrator
go run ./cmd/control-temporal-worker
go run ./cmd/control-rule-agent
go run ./cmd/control-decision-agent
go run ./cmd/data-mcp-server
```

5. Trigger a workflow:
```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"incident_id":"INC-123","service":"payments","symptom":"high error rate"}'
```
