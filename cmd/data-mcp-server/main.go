package main

import (
	"fmt"
	"net/http"
	"os"

	"forgeiq/internal/config"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/dataplane/tools"
	"forgeiq/internal/observability"

	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()

	// Initialize observability
	logger, err := observability.NewLogger(cfg.Observability.LogLevel == "debug" || os.Getenv("LOG_LEVEL") == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger = logger.WithComponent("data-mcp-server")

	metrics := observability.NewMetrics()
	healthChecker := observability.NewHealthChecker(logger)

	s := mcpserver.New()
	tools.RegisterAll(s, cfg, logger)

	// Register health check
	healthChecker.RegisterCheck("mcp_server", func() error {
		// Server is healthy if it's initialized
		if s == nil {
			return fmt.Errorf("MCP server is nil")
		}
		return nil
	})

	mux := http.NewServeMux()

	// Health and metrics endpoints
	mux.HandleFunc("/health", healthChecker.HealthHandler())
	mux.HandleFunc("/ready", healthChecker.ReadyHandler())
	mux.Handle("/metrics", observability.PrometheusHandler())

	// MCP RPC endpoint
	mux.Handle("/rpc", s.Handler())

	// Apply middleware
	handler := observability.HTTPLoggingMiddleware(logger, mux)
	handler = observability.HTTPMetricsMiddleware(metrics, handler)

	// Create server with timeouts
	addr := ":" + cfg.Server.Port
	srv := observability.NewServer(addr, handler)

	logger.Info("Starting data-plane MCP server", zap.String("addr", addr))
	if err := observability.StartServer(logger, srv, "data-mcp-server"); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}
