# Connecting to External A2A Agents and MCP Servers

This guide explains how to connect to external A2A agents and MCP servers hosted on cloud platforms like Google Cloud, AWS, Hugging Face, or any other platform.

## Configuration via Environment Variables

All external service connections are configured via environment variables. Set these before starting your services.

### Basic Configuration

```bash
# Rule Agent (A2A)
export RULE_AGENT_URL="https://your-rule-agent.example.com"
export RULE_AGENT_API_KEY="your-api-key-here"

# Decision Agent (A2A)
export DECISION_AGENT_URL="https://your-decision-agent.example.com"
export DECISION_AGENT_API_KEY="your-api-key-here"

# MCP Server
export MCP_BASE_URL="https://your-mcp-server.example.com"
export MCP_API_KEY="your-api-key-here"
```

## Authentication Methods

### 1. API Key Authentication

Most cloud platforms use API keys:

```bash
# For services using X-API-Key header
export RULE_AGENT_API_KEY="sk-1234567890abcdef"
export DECISION_AGENT_API_KEY="sk-1234567890abcdef"
export MCP_API_KEY="sk-1234567890abcdef"
```

### 2. Bearer Token Authentication

For OAuth2 or JWT tokens:

```bash
# For services using Authorization: Bearer header
export RULE_AGENT_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
export DECISION_AGENT_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
export MCP_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

### 3. Custom Headers

For services requiring custom headers:

```bash
# Format: Header-Name=value,Another-Header=value
export RULE_AGENT_HEADERS="X-Custom-Header=value,X-Platform=aws"
export DECISION_AGENT_HEADERS="X-Custom-Header=value"
export MCP_HEADERS="X-Custom-Header=value"
```

## Platform-Specific Examples

### AWS (API Gateway / Lambda)

```bash
export RULE_AGENT_URL="https://abc123.execute-api.us-east-1.amazonaws.com/prod/agent"
export RULE_AGENT_API_KEY="your-aws-api-key"
# Or use AWS Signature v4 via custom headers
export RULE_AGENT_HEADERS="X-Amz-Security-Token=...,Authorization=AWS4-HMAC-SHA256..."
```

### Google Cloud (Cloud Run / Cloud Functions)

```bash
export RULE_AGENT_URL="https://rule-agent-abc123-uc.a.run.app"
export RULE_AGENT_TOKEN="$(gcloud auth print-identity-token)"
# Or use API key
export RULE_AGENT_API_KEY="your-gcp-api-key"
```

### Hugging Face Inference API

```bash
export RULE_AGENT_URL="https://api-inference.huggingface.co/models/your-model"
export RULE_AGENT_TOKEN="hf_your_huggingface_token"
# Hugging Face uses Authorization: Bearer
```

### Azure (Functions / App Service)

```bash
export RULE_AGENT_URL="https://your-function-app.azurewebsites.net/api/agent"
export RULE_AGENT_API_KEY="your-function-key"
# Or use Azure AD token
export RULE_AGENT_TOKEN="your-azure-ad-token"
```

### Custom Platform

```bash
export RULE_AGENT_URL="https://your-platform.com/api/v1/agents/rule"
export RULE_AGENT_API_KEY="your-api-key"
export RULE_AGENT_HEADERS="X-API-Version=1.0,X-Client-ID=your-client-id"
```

## MCP Server Examples

### External MCP Server

```bash
export MCP_BASE_URL="https://mcp-server.example.com"
export MCP_API_KEY="your-mcp-api-key"
```

### Hugging Face MCP Server

```bash
export MCP_BASE_URL="https://mcp.huggingface.co"
export MCP_TOKEN="hf_your_token"
```

### AWS Lambda MCP Server

```bash
export MCP_BASE_URL="https://lambda-mcp.execute-api.us-east-1.amazonaws.com/prod"
export MCP_API_KEY="your-api-key"
```

## Complete Example

Here's a complete example connecting to external services:

```bash
#!/bin/bash

# Rule Agent on AWS
export RULE_AGENT_URL="https://rule-agent.execute-api.us-east-1.amazonaws.com/prod"
export RULE_AGENT_API_KEY="sk-aws-123456"

# Decision Agent on Google Cloud
export DECISION_AGENT_URL="https://decision-agent-abc123-uc.a.run.app"
export DECISION_AGENT_TOKEN="$(gcloud auth print-identity-token)"

# MCP Server on Hugging Face
export MCP_BASE_URL="https://mcp.huggingface.co"
export MCP_TOKEN="hf_your_huggingface_token"

# Temporal (local or cloud)
export TEMPORAL_HOSTPORT="your-temporal-cluster.temporal.io:7233"

# Start services
go run ./cmd/control-orchestrator
go run ./cmd/control-temporal-worker
```

## Security Best Practices

1. **Never commit secrets**: Use environment variables or secret management systems
2. **Use HTTPS**: Always use `https://` URLs for external services
3. **Rotate credentials**: Regularly rotate API keys and tokens
4. **Use least privilege**: Only grant necessary permissions
5. **Monitor access**: Log and monitor all external service calls

## Using Secret Management

### AWS Secrets Manager

```bash
export RULE_AGENT_API_KEY="$(aws secretsmanager get-secret-value --secret-id rule-agent-key --query SecretString --output text)"
```

### Google Secret Manager

```bash
export RULE_AGENT_API_KEY="$(gcloud secrets versions access latest --secret=rule-agent-key)"
```

### HashiCorp Vault

```bash
export RULE_AGENT_API_KEY="$(vault kv get -field=api_key secret/agents/rule)"
```

## Troubleshooting

### Connection Issues

1. **Check URL format**: Ensure URLs start with `http://` or `https://`
2. **Verify authentication**: Check API keys/tokens are correct
3. **Check network**: Ensure firewall allows outbound connections
4. **Test connectivity**: Use `curl` to test endpoints directly

```bash
# Test rule agent
curl -H "X-API-Key: $RULE_AGENT_API_KEY" \
     -H "Content-Type: application/json" \
     -X POST "$RULE_AGENT_URL/task" \
     -d '{"id":"test","type":"test","input":{},"metadata":{},"created_at":"2024-01-01T00:00:00Z"}'
```

### SSL/TLS Issues

If you encounter SSL certificate issues:

```bash
# For development only - skip TLS verification (NOT recommended for production)
export SSL_SKIP_VERIFY="true"
```

## Environment Variable Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `RULE_AGENT_URL` | Rule agent endpoint URL | `http://localhost:8081` |
| `RULE_AGENT_API_KEY` | API key for rule agent | - |
| `RULE_AGENT_TOKEN` | Bearer token for rule agent | - |
| `RULE_AGENT_HEADERS` | Custom headers (comma-separated) | - |
| `DECISION_AGENT_URL` | Decision agent endpoint URL | `http://localhost:8082` |
| `DECISION_AGENT_API_KEY` | API key for decision agent | - |
| `DECISION_AGENT_TOKEN` | Bearer token for decision agent | - |
| `DECISION_AGENT_HEADERS` | Custom headers (comma-separated) | - |
| `MCP_BASE_URL` | MCP server base URL | `http://localhost:8090` |
| `MCP_API_KEY` | API key for MCP server | - |
| `MCP_TOKEN` | Bearer token for MCP server | - |
| `MCP_HEADERS` | Custom headers (comma-separated) | - |


