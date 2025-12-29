package observability

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// GracefulShutdown handles graceful shutdown of HTTP servers
func GracefulShutdown(logger *Logger, srv *http.Server, timeout time.Duration) {
	// Create a channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Block until signal is received
	<-quit
	logger.Info("Shutting down server...")

	// Create a context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Shutdown the server
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("Server exited gracefully")
}

// ShutdownHandler wraps a server with graceful shutdown
func ShutdownHandler(logger *Logger, srv *http.Server, timeout time.Duration) {
	go func() {
		GracefulShutdown(logger, srv, timeout)
	}()
}

// NewServer creates an HTTP server with proper timeouts
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     nil, // We'll use our structured logger
	}
}

// StartServer starts an HTTP server with graceful shutdown
func StartServer(logger *Logger, srv *http.Server, component string) error {
	logger.Info("Starting server",
		zap.String("component", component),
		zap.String("addr", srv.Addr),
	)

	// Setup graceful shutdown
	ShutdownHandler(logger, srv, 30*time.Second)

	// Start server
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
}


