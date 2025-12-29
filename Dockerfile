# Build stage
# NOTE: go.mod requires Go >= 1.24 (see `go 1.24.0` + `toolchain go1.24.11`)
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binaries
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/orchestrator ./cmd/control-orchestrator
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/worker ./cmd/control-temporal-worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/rule-agent ./cmd/control-rule-agent
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/decision-agent ./cmd/control-decision-agent
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/mcp-server ./cmd/data-mcp-server

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binaries
COPY --from=builder /bin/orchestrator /bin/worker /bin/rule-agent /bin/decision-agent /bin/mcp-server /app/

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080 8081 8082 8090

CMD ["/app/orchestrator"]


