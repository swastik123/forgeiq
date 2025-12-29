# Contributing to ForgeIQ

Thank you for your interest in contributing to ForgeIQ! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## How to Contribute

### Reporting Bugs

1. Check if the bug has already been reported in [Issues](https://github.com/your-org/forgeiq/issues)
2. If not, create a new issue with:
   - Clear title and description
   - Steps to reproduce
   - Expected vs actual behavior
   - Environment details (OS, Go version, etc.)
   - Logs or error messages

### Suggesting Features

1. Check if the feature has already been suggested
2. Create an issue with:
   - Clear description of the feature
   - Use case and motivation
   - Proposed implementation (if you have ideas)

### Pull Requests

1. **Fork the repository**
2. **Create a feature branch**: `git checkout -b feature/amazing-feature`
3. **Make your changes**:
   - Follow the code style (run `make fmt`)
   - Add tests for new functionality
   - Update documentation
   - Ensure all tests pass (`make test`)
4. **Commit your changes**: `git commit -m 'Add amazing feature'`
   - Use conventional commits format
5. **Push to your fork**: `git push origin feature/amazing-feature`
6. **Open a Pull Request**

## Development Setup

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose
- Temporal CLI (optional)

### Getting Started

```bash
# Clone your fork
git clone https://github.com/your-username/forgeiq.git
cd forgeiq

# Install dependencies
go mod download

# Run tests
make test

# Start development environment
docker-compose up -d

# Run services locally
make run
```

## Code Style

- Follow Go conventions: `gofmt`, `golint`
- Use meaningful variable and function names
- Add comments for exported functions
- Keep functions focused and small
- Write tests for new code

### Formatting

```bash
make fmt        # Format code
make lint       # Run linters
make vet        # Run go vet
```

## Testing

- Write unit tests for new functionality
- Add integration tests for complex workflows
- Ensure test coverage doesn't decrease
- Run tests before submitting PR: `make test`

## Documentation

- Update README.md for user-facing changes
- Add code comments for complex logic
- Update API documentation if endpoints change
- Add examples for new features

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat: add agentic loop pattern
fix: resolve memory leak in storage
docs: update API documentation
test: add tests for plugin system
refactor: simplify error handling
chore: update dependencies
```

## Pull Request Process

1. Ensure your PR addresses an open issue (or create one)
2. Update CHANGELOG.md with your changes
3. Ensure all CI checks pass
4. Request review from maintainers
5. Address review feedback
6. Once approved, maintainers will merge

## Project Structure

```
agent-platform/
├── cmd/              # Application entry points
├── internal/         # Private application code
│   ├── config/      # Configuration management
│   ├── controlplane/# Control plane logic
│   ├── dataplane/   # Data plane logic
│   ├── storage/     # Storage abstractions
│   └── ...
├── examples/        # Example code and use cases
├── docs/            # Documentation
├── k8s/             # Kubernetes manifests
└── tests/           # Test files
```

## Questions?

- Open an issue for questions
- Join our community discussions
- Check existing documentation

Thank you for contributing! 🎉


