# Frequently Asked Questions

## General

### What is ForgeIQ?

ForgeIQ is a production-ready framework for orchestrating AI agents with policy enforcement, workflow management, and support for external services. It uses Temporal for reliable workflow execution and provides a plugin system for extensibility.

### What problem does it solve?

ForgeIQ solves the challenge of:
- Orchestrating multiple AI agents reliably
- Enforcing policies and approvals
- Managing long-running workflows
- Integrating with external services
- Providing observability and monitoring

### Who should use ForgeIQ?

- Teams building agentic AI systems
- Organizations needing policy enforcement
- Developers requiring reliable workflow orchestration
- Teams integrating multiple AI services

## Technical

### Why Temporal?

Temporal provides:
- **Reliability**: Automatic retries and state management
- **Durability**: State persists across failures
- **Scalability**: Handle millions of workflows
- **Observability**: Built-in visibility
- **Long-running**: Support workflows that run for days

### Can I use it without Temporal?

No, Temporal is a core dependency. However, you can run Temporal locally or use Temporal Cloud.

### What databases are supported?

Currently:
- **PostgreSQL** (production-ready)
- **Memory** (development/testing)

MongoDB and other databases can be added via the storage interface.

### How do I add a custom agent?

Implement the `AgentPlugin` interface and register it:

```go
type MyAgent struct{}

func (a *MyAgent) Name() string { return "my-agent" }
func (a *MyAgent) Version() string { return "1.0.0" }
func (a *MyAgent) TaskTypes() []string { return []string{"my_task"} }
func (a *MyAgent) Execute(ctx context.Context, task Task) (Artifact, error) {
    // Your logic
}

registry.RegisterAgent(&MyAgent{})
```

See [Plugin Examples](examples/plugin_example.go) for details.

### How do I connect to external services?

Set environment variables:

```bash
export RULE_AGENT_URL="https://your-agent.com"
export RULE_AGENT_API_KEY="your-key"
```

See [External Services Guide](examples/external-services.md) for details.

## Deployment

### How do I deploy to production?

1. **Docker**: Use provided Dockerfile
2. **Kubernetes**: Use provided manifests
3. **Cloud**: Deploy to AWS/GCP/Azure

See [Production Guide](docs/PRODUCTION_READY.md) for details.

### What are the resource requirements?

Minimum:
- **CPU**: 100m per service
- **Memory**: 128Mi per service
- **Storage**: Depends on workflow volume

Recommended:
- **CPU**: 500m per service
- **Memory**: 512Mi per service
- **Storage**: 10GB+ for PostgreSQL

### How do I scale?

- **Horizontal**: Run multiple worker instances
- **Vertical**: Increase resources per instance
- **Database**: Use PostgreSQL read replicas

## Security

### How are credentials managed?

- Environment variables
- Secret management systems (AWS Secrets Manager, etc.)
- Kubernetes secrets
- Never commit secrets to git

### Is data encrypted?

- **In transit**: Use HTTPS for external services
- **At rest**: Depends on database configuration
- **Temporal**: Uses TLS for cluster communication

### How do I secure the API?

- Use reverse proxy (nginx, traefik)
- Enable authentication middleware
- Use rate limiting
- Enable CORS if needed

## Troubleshooting

### Workflow stuck in "running" state

1. Check worker logs: `docker-compose logs worker`
2. Check workflow status: `curl /status/{taskID}`
3. Verify Temporal is running
4. Check for approval signals needed

### "Connection refused" errors

1. Verify services are running
2. Check port conflicts
3. Verify network connectivity
4. Check firewall rules

### Database connection errors

1. Verify database is running
2. Check connection string format
3. Verify credentials
4. Check network connectivity

### High memory usage

1. Check workflow count
2. Review evidence collection
3. Enable storage persistence
4. Adjust worker count

## Performance

### How many workflows can it handle?

Depends on:
- Temporal cluster capacity
- Database performance
- Worker count
- Workflow complexity

Typical: 1000+ concurrent workflows per worker.

### How do I improve performance?

1. **Scale workers**: Add more worker instances
2. **Optimize database**: Use indexes, connection pooling
3. **Cache**: Cache agent responses
4. **Batch**: Batch similar operations

### What's the latency?

- **Workflow start**: < 100ms
- **Agent call**: 100-500ms (depends on external service)
- **Tool execution**: 50-200ms
- **Total workflow**: Seconds to minutes (depends on complexity)

## Integration

### Can I use it with LangChain?

Yes! Create a LangChain agent and expose it via A2A protocol.

### Can I use it with OpenAI?

Yes! Create an OpenAI-based agent and connect via A2A.

### Can I use it with Hugging Face?

Yes! Connect to Hugging Face Inference API via MCP or A2A.

### Can I use it with AWS Bedrock?

Yes! Create a Bedrock agent and connect via A2A.

## Licensing

### What license is used?

MIT License - see [LICENSE](LICENSE) file.

### Can I use it commercially?

Yes, MIT License allows commercial use.

### Do I need to contribute back?

No, but contributions are welcome!

## Support

### Where can I get help?

- **GitHub Issues**: Bug reports and feature requests
- **Discussions**: Questions and discussions
- **Documentation**: Comprehensive guides
- **Examples**: Code examples

### How do I report a bug?

1. Check existing issues
2. Create new issue with:
   - Clear description
   - Steps to reproduce
   - Environment details
   - Logs/errors

### How do I request a feature?

1. Check existing issues
2. Create feature request with:
   - Use case
   - Proposed solution
   - Benefits

## Roadmap

### What's coming next?

See [ROADMAP.md](ROADMAP.md) for planned features.

### Can I contribute?

Yes! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.


