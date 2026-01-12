package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/observability"
	"forgeiq/internal/transport/a2a"

	"go.uber.org/zap"
)

func main() {
	// Initialize observability
	logger, err := observability.NewLogger(os.Getenv("LOG_LEVEL") == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger = logger.WithComponent("rule-agent")

	metrics := observability.NewMetrics()
	healthChecker := observability.NewHealthChecker(logger)

	card := a2a.AgentCard{
		Name:        "rule-agent",
		Description: "Evaluates policy for tasks (allow/deny, budgets, scopes).",
		TaskTypes:   []string{"incident_triage"},
		Endpoint:    "http://localhost:8081/task",
		Version:     "0.1",
	}

	// Optional self-registration into control-plane Agent Registry
	if regURL := os.Getenv("AGENT_REGISTRY_URL"); regURL != "" {
		baseURL := os.Getenv("AGENT_BASE_URL")
		if baseURL == "" {
			baseURL = "http://rule-agent:8081"
		}
		payload := map[string]any{
			"id":       os.Getenv("AGENT_ID"),
			"type":     "rule",
			"base_url": baseURL,
			"name":     card.Name,
			"version":  card.Version,
			"capabilities": map[string]any{
				"task_types": card.TaskTypes,
				"tags":       []string{"rules", "policy"},
			},
			"status": "unknown",
		}
		if payload["id"] == "" {
			payload["id"] = "rule-local"
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(regURL, "/")+"/registry/agents", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		_, _ = http.DefaultClient.Do(req) // best-effort
	}

	mux := http.NewServeMux()

	// Health and metrics endpoints
	mux.HandleFunc("/health", healthChecker.HealthHandler())
	mux.HandleFunc("/ready", healthChecker.ReadyHandler())
	mux.Handle("/metrics", observability.PrometheusHandler())

	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(card); err != nil {
			logger.Error("failed to encode agent card", zap.Error(err))
		}
	})

	mux.HandleFunc("/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var t contracts.Task
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			logger.Error("failed to decode task", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		logger.Info("Evaluating policy", zap.String("task_id", t.ID), zap.String("task_type", t.Type))

		// Simple policy: always allow reading; writes require "approved" metadata.
		approved := t.Metadata["approved"] == "true"

		pd := contracts.PolicyDecision{
			Allowed:      true,
			AllowWrites:  approved,
			Scopes:       []string{"logs:read", "incidents:write", "remediation:write"},
			ToolTagAllow: []string{"read"},
			Budgets:      contracts.Budget{MaxToolCalls: 10, MaxSeconds: 60},
			Reasons: map[string]string{
				"rule.write_gate": "writes require approval",
			},
		}
		if approved {
			pd.ToolTagAllow = []string{"read", "write"}
			pd.Reasons["rule.write_gate"] = "approval present; writes allowed"
		}

		// Record metrics
		result := "allowed"
		if !pd.Allowed {
			result = "denied"
			metrics.PolicyDenials.WithLabelValues(t.Type, "policy_check").Inc()
		}
		metrics.PolicyEvaluations.WithLabelValues(t.Type, result).Inc()

		art := contracts.Artifact{
			TaskID:  t.ID,
			Type:    "PolicyDecision",
			Payload: map[string]any{"policy": pd},
			TS:      time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(art); err != nil {
			logger.Error("failed to encode artifact", zap.Error(err))
		}
	})

	// Apply middleware
	handler := observability.HTTPLoggingMiddleware(logger, mux)
	handler = observability.HTTPMetricsMiddleware(metrics, handler)

	// Create server with timeouts
	srv := observability.NewServer(":8081", handler)

	logger.Info("Starting rule agent", zap.String("addr", ":8081"))
	if err := observability.StartServer(logger, srv, "rule-agent"); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}

type RuleAgent struct{}

func (a *RuleAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	var req contracts.A2ATaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var in contracts.RuleAgentInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	out := contracts.RuleAgentOutput{
		RunbookID:             "rb-default-k8s",
		Severity:              "medium",
		RequiresHumanApproval: true, // for demo: always require approval
	}

	respBytes, _ := json.Marshal(out)
	resp := contracts.A2ATaskResponse{
		Status: "ok",
		Output: respBytes,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
