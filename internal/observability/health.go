package observability

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HealthChecker manages health check state
type HealthChecker struct {
	logger    *Logger
	checks    map[string]Check
	mu        sync.RWMutex
	startTime time.Time
}

// Check represents a health check function
type Check func() error

// Status represents health check status
type Status struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Uptime    string            `json:"uptime"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger *Logger) *HealthChecker {
	return &HealthChecker{
		logger:    logger,
		checks:    make(map[string]Check),
		startTime: time.Now(),
	}
}

// RegisterCheck registers a health check function
func (h *HealthChecker) RegisterCheck(name string, check Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// HealthHandler returns HTTP handler for /health endpoint
func (h *HealthChecker) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		defer h.mu.RUnlock()

		status := Status{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Uptime:    time.Since(h.startTime).String(),
			Checks:    make(map[string]string),
		}

		allHealthy := true
		for name, check := range h.checks {
			if err := check(); err != nil {
				status.Checks[name] = "unhealthy: " + err.Error()
				status.Status = "unhealthy"
				allHealthy = false
			} else {
				status.Checks[name] = "healthy"
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if !allHealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		if err := json.NewEncoder(w).Encode(status); err != nil {
			h.logger.Error("Failed to encode health status", zap.Error(err))
		}
	}
}

// ReadyHandler returns HTTP handler for /ready endpoint (readiness probe)
func (h *HealthChecker) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Simple readiness check - service is ready if it's running
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	}
}


