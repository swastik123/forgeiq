# Production-Ready Framework Guide

This document outlines all the production-ready features implemented in the ForgeIQ framework.

## ✅ Implemented Features

### 1. **Persistent Storage**
- **PostgreSQL Support**: Full CRUD operations for workflows, artifacts, and audit logs
- **Memory Storage**: In-memory storage for development/testing
- **Storage Abstraction**: Easy to add new storage backends (MongoDB, etc.)
- **Automatic Migrations**: Tables created automatically on startup

**Usage:**
```bash
export STORAGE_TYPE=postgres
export STORAGE_DSN="postgres://user:password@localhost:5432/forgeiq?sslmode=disable"
```

### 2. **Error Handling & Recovery**
- **Structured Errors**: Typed error codes and HTTP status mapping
- **Error Recovery**: Retry logic for transient failures
- **Comprehensive Logging**: All errors logged with context
- **Graceful Degradation**: System continues operating on non-critical failures

### 3. **Input Validation & Security**
- **Request Validation**: All inputs validated before processing
- **Input Sanitization**: Protection against injection attacks
- **Type Safety**: Strong typing throughout the system

### 4. **Plugin System**
- **Custom Agents**: Register your own agent implementations
- **Custom Tools**: Add new tools without modifying core code
- **Plugin Registry**: Centralized plugin management

**Example:**
```go
// Register custom agent
registry.RegisterAgent(&MyCustomAgent{})

// Register custom tool
registry.RegisterTool(&MyCustomTool{})
```

### 5. **Observability**
- **Structured Logging**: JSON logs with zap
- **Prometheus Metrics**: All operations instrumented
- **Distributed Tracing**: OpenTelemetry/Jaeger support
- **Health Checks**: `/health` and `/ready` endpoints

### 6. **Rate Limiting**
- **Token Bucket Algorithm**: Configurable rate limits
- **IP-based Limiting**: Per-client rate limiting
- **HTTP 429 Responses**: Proper rate limit error handling

### 7. **Deployment Ready**
- **Docker Support**: Multi-stage Dockerfile
- **Docker Compose**: Complete development environment
- **Kubernetes Manifests**: Production-ready K8s deployments
- **Health Probes**: Liveness and readiness checks

### 8. **External Service Support**
- **Multiple Auth Methods**: API keys, OAuth tokens, custom headers
- **HTTPS Support**: Secure connections to external services
- **Configurable Timeouts**: Per-service timeout configuration

## 🚀 Quick Start

### Local Development

```bash
# Start all services with Docker Compose
docker-compose up

# Or run individually
go run ./cmd/control-orchestrator
go run ./cmd/control-temporal-worker
go run ./cmd/control-rule-agent
go run ./cmd/control-decision-agent
go run ./cmd/data-mcp-server
```

### Production Deployment

```bash
# Build Docker image
docker build -t agent-platform:latest .

# Deploy to Kubernetes
kubectl apply -f k8s/deployment.yaml
```

## 📋 Configuration

All configuration via environment variables. See `examples/.env.example` for full list.

### Required Configuration

```bash
# Temporal
TEMPORAL_HOSTPORT=localhost:7233

# Storage (choose one)
STORAGE_TYPE=postgres
STORAGE_DSN=postgres://user:password@localhost:5432/forgeiq

# Or use memory for development
STORAGE_TYPE=memory
```

### External Services

```bash
# Connect to external agents
RULE_AGENT_URL=https://your-rule-agent.com
RULE_AGENT_API_KEY=your-api-key

DECISION_AGENT_URL=https://your-decision-agent.com
DECISION_AGENT_TOKEN=your-token

MCP_BASE_URL=https://your-mcp-server.com
MCP_API_KEY=your-api-key
```

## 🔌 Plugin Development

### Creating a Custom Agent

```go
package main

import (
    "context"
    "agent-platform/internal/controlplane/contracts"
    "agent-platform/internal/plugin"
)

type MyAgent struct{}

func (a *MyAgent) Name() string { return "my-agent" }
func (a *MyAgent) Version() string { return "1.0.0" }
func (a *MyAgent) TaskTypes() []string {
    return []string{"custom_task"}
}

func (a *MyAgent) Execute(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
    // Your agent logic here
    return contracts.Artifact{
        TaskID: task.ID,
        Type:   "Result",
        Payload: map[string]any{"result": "success"},
    }, nil
}

// Register
registry := plugin.NewRegistry()
registry.RegisterAgent(&MyAgent{})
```

### Creating a Custom Tool

```go
type MyTool struct{}

func (t *MyTool) Name() string { return "my-tool" }
func (t *MyTool) Version() string { return "v1" }
func (t *MyTool) Info() interfaces.ToolInfo {
    return interfaces.ToolInfo{
        Name:    "my-tool",
        Version: "v1",
        Tags:    []string{"read"},
        Scopes:  []string{"custom:read"},
    }
}

func (t *MyTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
    // Your tool logic here
    return map[string]any{"result": "done"}, nil
}

// Register
registry.RegisterTool(&MyTool{})
```

## 📊 Monitoring

### Metrics Endpoints

All services expose Prometheus metrics at `/metrics`:

- `http_requests_total` - Total HTTP requests
- `http_request_duration_seconds` - Request latency
- `workflow_started_total` - Workflows started
- `workflow_completed_total` - Workflows completed
- `tool_calls_total` - Tool invocations
- `policy_evaluations_total` - Policy checks

### Health Checks

```bash
# Health check
curl http://localhost:8080/health

# Readiness probe
curl http://localhost:8080/ready
```

## 🔒 Security Best Practices

1. **Use HTTPS**: Always use `https://` for external services
2. **Rotate Credentials**: Regularly rotate API keys and tokens
3. **Use Secrets Management**: Store credentials in secret managers
4. **Enable Rate Limiting**: Protect APIs from abuse
5. **Validate Inputs**: All inputs are validated and sanitized
6. **Audit Logging**: All actions are logged for compliance

## 🐛 Troubleshooting

### Database Connection Issues

```bash
# Test PostgreSQL connection
psql $STORAGE_DSN -c "SELECT 1"

# Check storage health
curl http://localhost:8080/health
```

### Temporal Connection Issues

```bash
# Check Temporal is running
curl http://localhost:8088/health

# Verify connection
tctl workflow list
```

### External Service Issues

```bash
# Test agent connectivity
curl -H "X-API-Key: $RULE_AGENT_API_KEY" \
     -H "Content-Type: application/json" \
     -X POST "$RULE_AGENT_URL/task" \
     -d '{"id":"test","type":"test","input":{},"metadata":{},"created_at":"2024-01-01T00:00:00Z"}'
```

## 📚 Additional Resources

- [External Services Guide](examples/external-services.md)
- [API Documentation](docs/API.md)
- [Architecture Overview](docs/ARCHITECTURE.md)


