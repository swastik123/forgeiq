.PHONY: help build test fmt lint vet clean run docker-build docker-up docker-down

# Variables
BINARY_NAME=agent-platform
VERSION?=0.1.0
DOCKER_IMAGE?=agent-platform:latest

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all binaries
	@echo "Building binaries..."
	@go build -o bin/orchestrator ./cmd/control-orchestrator
	@go build -o bin/worker ./cmd/control-temporal-worker
	@go build -o bin/rule-agent ./cmd/control-rule-agent
	@go build -o bin/decision-agent ./cmd/control-decision-agent
	@go build -o bin/mcp-server ./cmd/data-mcp-server
	@echo "Build complete!"

test: ## Run tests
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Test coverage report: coverage.html"

test-unit: ## Run unit tests only
	@go test -v -short ./...

test-integration: ## Run integration tests
	@go test -v -tags=integration ./...

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Done!"

lint: ## Run linters
	@echo "Running linters..."
	@golangci-lint run ./... || echo "Install golangci-lint: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@go clean ./...
	@echo "Done!"

run: ## Run all services locally
	@echo "Starting services..."
	@make -j5 run-orchestrator run-worker run-rule-agent run-decision-agent run-mcp-server

run-orchestrator: ## Run orchestrator
	@go run ./cmd/control-orchestrator

run-worker: ## Run temporal worker
	@go run ./cmd/control-temporal-worker

run-rule-agent: ## Run rule agent
	@go run ./cmd/control-rule-agent

run-decision-agent: ## Run decision agent
	@go run ./cmd/control-decision-agent

run-mcp-server: ## Run MCP server
	@go run ./cmd/data-mcp-server

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Done!"

docker-up: ## Start services with Docker Compose
	@echo "Starting Docker Compose..."
	@docker-compose up -d
	@echo "Services started!"

docker-down: ## Stop Docker Compose services
	@echo "Stopping Docker Compose..."
	@docker-compose down
	@echo "Done!"

docker-logs: ## View Docker Compose logs
	@docker-compose logs -f

install-deps: ## Install dependencies
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy
	@echo "Done!"

update-deps: ## Update dependencies
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy
	@echo "Done!"

check: fmt vet lint test ## Run all checks

ci: check build ## Run CI pipeline locally

release: ## Create a release (requires VERSION variable)
	@echo "Creating release v$(VERSION)..."
	@git tag -a v$(VERSION) -m "Release v$(VERSION)"
	@git push origin v$(VERSION)
	@echo "Release v$(VERSION) created!"

docs: ## Generate documentation
	@echo "Generating documentation..."
	@go doc -all ./... > docs/api.txt
	@echo "Done!"

.PHONY: install-tools
install-tools: ## Install development tools
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "Tools installed!"


