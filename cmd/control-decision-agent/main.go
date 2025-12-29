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

	logger = logger.WithComponent("decision-agent")

	metrics := observability.NewMetrics()
	healthChecker := observability.NewHealthChecker(logger)

	card := a2a.AgentCard{
		Name:        "decision-agent",
		Description: "Creates an execution plan (steps) using policy + evidence.",
		TaskTypes:   []string{"incident_triage"},
		Endpoint:    "http://localhost:8082/task",
		Version:     "0.1",
	}

	// Optional self-registration into control-plane Agent Registry
	if regURL := os.Getenv("AGENT_REGISTRY_URL"); regURL != "" {
		baseURL := os.Getenv("AGENT_BASE_URL")
		if baseURL == "" {
			baseURL = "http://decision-agent:8082"
		}
		payload := map[string]any{
			"id":       os.Getenv("AGENT_ID"),
			"type":     "decision",
			"base_url": baseURL,
			"name":     card.Name,
			"version":  card.Version,
			"capabilities": map[string]any{
				"task_types": card.TaskTypes,
				"tags":       []string{"planning"},
			},
			"status": "unknown",
		}
		if payload["id"] == "" {
			payload["id"] = "decision-local"
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

		logger.Info("Creating plan", zap.String("task_id", t.ID), zap.String("task_type", t.Type))

		service, _ := t.Input["service"].(string)
		symptom, _ := t.Input["symptom"].(string)

		// Plan: always gather logs; then create ticket; then restart service (if allowed).
		plan := contracts.Plan{
			Confidence: 0.75,
			Steps: []contracts.PlanStep{
				{
					StepID:      "s1",
					Goal:        "Gather evidence from logs",
					ToolName:    "logs.search",
					ToolVersion: "v1",
					Tags:        []string{"read", "logs"},
					Scopes:      []string{"logs:read"},
					Args: map[string]any{
						"query": service + " " + symptom,
						"limit": 50,
					},
					StopOnErr: true,
				},
				{
					StepID:      "s2",
					Goal:        "Create incident ticket",
					ToolName:    "incidents.create_ticket",
					ToolVersion: "v1",
					Tags:        []string{"write", "incidents"},
					Scopes:      []string{"incidents:write"},
					Args: map[string]any{
						"title":       "Incident: " + service,
						"description": "Symptom: " + symptom,
					},
					StopOnErr: true,
				},
				{
					StepID:      "s3",
					Goal:        "Restart affected service",
					ToolName:    "remediation.restart_service",
					ToolVersion: "v1",
					Tags:        []string{"write", "remediation"},
					Scopes:      []string{"remediation:write"},
					Args: map[string]any{
						"service": service,
					},
					StopOnErr: false,
				},
			},
		}

		art := contracts.Artifact{
			TaskID:  t.ID,
			Type:    "Plan",
			Payload: map[string]any{"plan": plan},
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
	srv := observability.NewServer(":8082", handler)

	logger.Info("Starting decision agent", zap.String("addr", ":8082"))
	if err := observability.StartServer(logger, srv, "decision-agent"); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}
