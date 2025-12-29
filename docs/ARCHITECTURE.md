# Architecture

## Overview

ForgeIQ is a production-ready framework for orchestrating AI agents with policy enforcement, workflow management, and support for external services.

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Control Plane                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ Orchestrator │  │ Rule Agent   │  │Decision Agent│     │
│  │   (HTTP)     │  │   (A2A)      │  │   (A2A)      │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
│         │                  │                  │              │
│         └──────────────────┼──────────────────┘              │
│                            │                                 │
│                    ┌───────▼────────┐                        │
│                    │ Temporal Worker│                        │
│                    │  (Workflows)   │                        │
│                    └───────┬────────┘                        │
└────────────────────────────┼─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│                    Data Plane                               │
│  ┌────────────────────────────────────────────────────┐    │
│  │         MCP Server (Tools)                         │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐         │    │
│  │  │logs.search│  │incidents │  │remediation│        │    │
│  │  └──────────┘  └──────────┘  └──────────┘         │    │
│  └────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│              External Services                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                │
│  │AWS Agents│  │GCP Agents │  │HF Agents │                │
│  └──────────┘  └──────────┘  └──────────┘                │
└─────────────────────────────────────────────────────────────┘
```

## Components

### Control Plane

#### Orchestrator
- **Purpose**: HTTP API for workflow management
- **Port**: 8080
- **Responsibilities**:
  - Start workflows
  - Query workflow status
  - Handle approvals
  - Provide health checks

#### Rule Agent
- **Purpose**: Policy evaluation
- **Port**: 8081
- **Protocol**: A2A (Agent-to-Agent)
- **Responsibilities**:
  - Evaluate task permissions
  - Set budgets and scopes
  - Gate write operations

#### Decision Agent
- **Purpose**: Plan generation
- **Port**: 8082
- **Protocol**: A2A
- **Responsibilities**:
  - Create execution plans
  - Define steps and tools
  - Set confidence levels

#### Temporal Worker
- **Purpose**: Execute workflows
- **Connection**: Temporal cluster
- **Responsibilities**:
  - Run workflow activities
  - Handle retries
  - Manage state

### Data Plane

#### MCP Server
- **Purpose**: Tool execution
- **Port**: 8090
- **Protocol**: JSON-RPC (MCP)
- **Tools**:
  - `logs.search` - Read logs
  - `incidents.create_ticket` - Create tickets
  - `remediation.restart_service` - Restart services

## Data Flow

### Workflow Execution

```
1. User → POST /run → Orchestrator
2. Orchestrator → Temporal → Start Workflow
3. Workflow → Activity: EvalPolicy → Rule Agent
4. Workflow → Activity: GetPlan → Decision Agent
5. Workflow → Activity: CallTool → MCP Server
6. MCP Server → Execute Tool → Return Result
7. Workflow → Collect Evidence → Complete
8. User → GET /result → Get Final Artifact
```

### Agentic Loop Flow

```
1. Start Workflow
2. Get Initial Plan
3. Execute Plan (Iteration 1)
   ├─ Collect Evidence
   ├─ Check for Errors
   └─ Wait for Feedback (if needed)
4. Refine Plan (if needed)
5. Execute Refined Plan (Iteration 2)
6. Repeat until Goal Achieved
```

## Storage Layer

### PostgreSQL
- **Tables**:
  - `workflows` - Workflow state
  - `artifacts` - Execution artifacts
  - `audit_logs` - Audit trail

### Memory Storage
- For development/testing
- No persistence

## External Integration

### A2A Protocol
- HTTP-based agent communication
- Supports API keys, tokens, custom headers
- Configurable timeouts

### MCP Protocol
- JSON-RPC for tool execution
- Tool discovery via `tools.list`
- Tool execution via `tools.call`

## Security

### Authentication
- API keys for external services
- Bearer tokens for OAuth
- Custom headers support

### Authorization
- Policy-based access control
- Scope-based permissions
- Write operation gates

### Input Validation
- Request validation
- Input sanitization
- Type checking

## Observability

### Metrics (Prometheus)
- HTTP request metrics
- Workflow metrics
- Tool call metrics
- Policy evaluation metrics

### Logging (Structured)
- JSON format
- Contextual information
- Error tracking

### Tracing (OpenTelemetry)
- Distributed tracing
- Jaeger integration
- Request correlation

## Deployment

### Docker
- Multi-stage builds
- Optimized images
- Non-root user

### Kubernetes
- Deployments
- Services
- ConfigMaps
- Secrets

### Docker Compose
- Local development
- All services included
- Easy setup

## Extension Points

### Plugin System
- Custom agents
- Custom tools
- Registry-based

### Storage Backends
- PostgreSQL (implemented)
- MongoDB (planned)
- Custom backends (via interface)

### External Services
- Any HTTPS endpoint
- Multiple auth methods
- Configurable timeouts

## Scalability

### Horizontal Scaling
- Stateless services
- Multiple workers
- Load balancing

### State Management
- Temporal for workflow state
- PostgreSQL for persistence
- Distributed architecture

## Reliability

### Error Handling
- Retry logic
- Error recovery
- Graceful degradation

### High Availability
- Health checks
- Readiness probes
- Graceful shutdown

### Data Durability
- Temporal persistence
- PostgreSQL backups
- Audit logging


