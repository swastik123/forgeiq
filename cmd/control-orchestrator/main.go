package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgeiq/internal/agent/strategy"
	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/temporal"
	"forgeiq/internal/eval"
	"forgeiq/internal/observability"
	"forgeiq/internal/registry"
	mcpclient "forgeiq/internal/transport/mcp"

	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Optional: registry + eval stores (Postgres)
	var agentStore *registry.PostgresAgentStore
	var evalStore *eval.Store
	if cfg.Storage.Type == "postgres" && cfg.Storage.DSN != "" {
		if s, err := registry.NewPostgresAgentStore(cfg.Storage.DSN); err == nil {
			agentStore = s
			defer agentStore.Close()
		}
		if es, err := eval.NewPostgresStore(cfg.Storage.DSN); err == nil {
			evalStore = es
			defer evalStore.Close()
		}
	}

	// Initialize observability
	logger, err := observability.NewLogger(cfg.Observability.LogLevel == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger = logger.WithComponent("control-orchestrator")

	metrics := observability.NewMetrics()
	healthChecker := observability.NewHealthChecker(logger)

	// Initialize Temporal client
	tc, err := client.Dial(client.Options{HostPort: cfg.Temporal.HostPort})
	if err != nil {
		logger.Fatal("unable to create Temporal client", zap.Error(err))
	}
	defer tc.Close()

	// Register health check for Temporal connection
	healthChecker.RegisterCheck("temporal", func() error {
		// Simple check - if client is not nil, assume healthy
		if tc == nil {
			return fmt.Errorf("temporal client is nil")
		}
		return nil
	})

	mux := http.NewServeMux()

	// Health and metrics endpoints
	mux.HandleFunc("/health", healthChecker.HealthHandler())
	mux.HandleFunc("/ready", healthChecker.ReadyHandler())
	mux.Handle("/metrics", observability.PrometheusHandler())

	// Strategy API (v0.1): run an Agent Strategy loop and return final answer + tool outputs + trace.
	// POST /strategy/run
	// Body: {"strategy":"function_calling|react","state":{"messages":[{"role":"user","content":"..."}]},"budget":{...},"model":{...}}
	mux.HandleFunc("/strategy/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Strategy string                     `json:"strategy"`
			State    strategy.ConversationState `json:"state"`
			Budget   struct {
				MaxIterations int `json:"max_iterations"`
				MaxToolCalls  int `json:"max_tool_calls"`
				MaxWallTimeMS int `json:"max_wall_time_ms"`
			} `json:"budget"`
			Model strategy.ModelConfig `json:"model"`
			Meta  map[string]any       `json:"meta"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		mode := strings.ToLower(strings.TrimSpace(req.Strategy))
		if mode == "" {
			mode = "function_calling"
		}

		// Build tool client (MCP) and fetch tool catalog
		tc := mcpclient.NewClientFromConfig(cfg)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		tools, err := tc.ListTools(ctx)
		if err != nil {
			http.Error(w, "failed to list tools: "+err.Error(), http.StatusBadGateway)
			return
		}

		b := strategy.Budget{
			MaxIterations: req.Budget.MaxIterations,
			MaxToolCalls:  req.Budget.MaxToolCalls,
		}
		if req.Budget.MaxWallTimeMS > 0 {
			b.MaxWallTime = time.Duration(req.Budget.MaxWallTimeMS) * time.Millisecond
		}

		in := strategy.Input{
			State:  req.State,
			Tools:  strategy.ToolCatalog{Tools: tools},
			Model:  req.Model,
			Budget: b,
			Meta:   req.Meta,
			Tooler: tc,
		}
		// Model client uses MCP tool `llm.chat` (demo tool registered by data-mcp-server).
		in.Modeler = strategy.NewMCPModelClient(tc)

		var strat strategy.AgentStrategy
		switch mode {
		case "function_calling":
			strat = &strategy.FunctionCallingStrategy{}
		case "react":
			strat = &strategy.ReActStrategy{}
		default:
			http.Error(w, "unknown strategy: "+mode, http.StatusBadRequest)
			return
		}

		out, err := strat.Run(ctx, in)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": out,
			"error":  errString(err),
		})
	})

	// Agent Registry endpoints (for registering your own agents or external company agents)
	// GET /registry/agents?type=rule|decision
	// POST /registry/agents  body: AgentRecord
	mux.HandleFunc("/registry/agents", func(w http.ResponseWriter, r *http.Request) {
		if agentStore == nil {
			http.Error(w, "agent registry not configured (set STORAGE_TYPE=postgres and STORAGE_DSN)", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			typ := r.URL.Query().Get("type")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			agents, err := agentStore.ListAgents(ctx, registry.AgentFilter{Type: typ, Limit: 200})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": agents, "count": len(agents)})
		case http.MethodPost:
			var in registry.AgentRecord
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if err := agentStore.UpsertAgent(ctx, in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Eval endpoints (compare Decision Agent versions)
	// GET /eval/compare?decision_agent_id=decision-local&versions=0.1,0.2
	mux.HandleFunc("/eval/compare", func(w http.ResponseWriter, r *http.Request) {
		if evalStore == nil {
			http.Error(w, "eval store not configured (set STORAGE_TYPE=postgres and STORAGE_DSN)", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("decision_agent_id")
		vs := strings.TrimSpace(r.URL.Query().Get("versions"))
		if id == "" || vs == "" {
			http.Error(w, "decision_agent_id and versions are required", http.StatusBadRequest)
			return
		}
		versions := strings.Split(vs, ",")
		for i := range versions {
			versions[i] = strings.TrimSpace(versions[i])
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, err := evalStore.CompareDecisionAgentVersions(ctx, id, versions)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
	})

	// Start workflow (async)
	// POST /run  body: {"incident_id":"INC-123","service":"payments","symptom":"high error rate"}
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			logger.Error("failed to decode request body", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		taskID := "task-" + time.Now().Format("20060102-150405.000")

		task := contracts.Task{
			ID:        taskID,
			Type:      "incident_triage",
			Input:     in,
			Metadata:  map[string]string{},
			CreatedAt: time.Now(),
		}

		we, err := tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:        task.ID,
			TaskQueue: cfg.Temporal.TaskQueue,
		}, temporal.IncidentWorkflow, task)

		if err != nil {
			logger.Error("failed to execute workflow", zap.Error(err), zap.String("task_id", taskID))
			metrics.WorkflowFailed.WithLabelValues(task.Type, "execution_error").Inc()
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		metrics.WorkflowStarted.WithLabelValues(task.Type).Inc()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"workflow_id": we.GetID(),
			"run_id":      we.GetRunID(),
			"task_id":     task.ID,
			"status_url":  "/status/" + task.ID,
			"result_url":  "/result/" + task.ID,
			"approve_url": "/approve/" + task.ID,
		}); err != nil {
			logger.Error("failed to encode response", zap.Error(err))
		}
	})

	// Query status
	// GET /status/{taskID}
	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/status/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		resp, err := tc.QueryWorkflow(ctx, taskID, "", temporal.QueryStatus)
		if err != nil {
			logger.Error("failed to query workflow status", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		var st temporal.WorkflowStatus
		if err := resp.Get(&st); err != nil {
			logger.Error("failed to get workflow status", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(st); err != nil {
			logger.Error("failed to encode status response", zap.Error(err))
		}
	})

	// Real-time status updates via Server-Sent Events (SSE)
	// GET /events/{taskID}?interval_ms=750
	mux.HandleFunc("/events/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := strings.TrimPrefix(r.URL.Path, "/events/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		interval := 750 * time.Millisecond
		if v := r.URL.Query().Get("interval_ms"); v != "" {
			if ms, err := strconv.Atoi(v); err == nil {
				if ms < 250 {
					ms = 250
				}
				if ms > 5000 {
					ms = 5000
				}
				interval = time.Duration(ms) * time.Millisecond
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// If behind a proxy (nginx), this helps avoid buffering SSE
		w.Header().Set("X-Accel-Buffering", "no")

		writeEvent := func(event string, data any) {
			// event: <name>\n
			// data: <json>\n\n
			if event != "" {
				_, _ = fmt.Fprintf(w, "event: %s\n", event)
			}
			if data != nil {
				b, err := json.Marshal(data)
				if err == nil {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
				} else {
					_, _ = fmt.Fprintf(w, "data: %q\n\n", err.Error())
				}
			} else {
				_, _ = fmt.Fprintf(w, "data: {}\n\n")
			}
			flusher.Flush()
		}

		// initial hello
		writeEvent("hello", map[string]any{"task_id": taskID, "interval_ms": int(interval / time.Millisecond)})

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastState string
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				resp, err := tc.QueryWorkflow(ctx, taskID, "", temporal.QueryStatus)
				cancel()
				if err != nil {
					writeEvent("error", map[string]any{"error": err.Error()})
					continue
				}
				var st temporal.WorkflowStatus
				if err := resp.Get(&st); err != nil {
					writeEvent("error", map[string]any{"error": err.Error()})
					continue
				}
				if st.State != lastState {
					lastState = st.State
					writeEvent("status", st)
				} else {
					// lightweight heartbeat with latest timestamp so UI can show it's alive
					writeEvent("tick", map[string]any{"state": st.State, "updated_at_rfc": st.UpdatedAtRFC})
				}

				if st.State == "completed" || st.State == "failed" || st.State == "denied" {
					// best-effort: send final result
					ctx2, cancel2 := context.WithTimeout(r.Context(), 5*time.Second)
					q, err := tc.QueryWorkflow(ctx2, taskID, "", temporal.QueryResult)
					if err == nil {
						var art contracts.Artifact
						if err := q.Get(&art); err == nil && art.TaskID != "" {
							writeEvent("result", art)
						}
					}
					cancel2()
					writeEvent("done", map[string]any{"state": st.State})
					return
				}
			}
		}
	})

	// Query result
	// GET /result/{taskID}
	mux.HandleFunc("/result/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/result/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		// First try query handler
		q, err := tc.QueryWorkflow(ctx, taskID, "", temporal.QueryResult)
		if err == nil {
			var art contracts.Artifact
			if err := q.Get(&art); err == nil && art.TaskID != "" {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(art); err != nil {
					logger.Error("failed to encode result response", zap.Error(err))
				}
				return
			}
		}

		// Fallback: if workflow completed, GetResult
		we := tc.GetWorkflow(ctx, taskID, "")
		var art contracts.Artifact
		if err := we.Get(ctx, &art); err != nil {
			logger.Error("failed to get workflow result", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(art); err != nil {
			logger.Error("failed to encode result response", zap.Error(err))
		}
	})

	// Approve (signal)
	// POST /approve/{taskID}  body optional: {"approved":true}
	mux.HandleFunc("/approve/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/approve/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		approved := true
		if r.Body != nil {
			var in map[string]any
			if err := json.NewDecoder(r.Body).Decode(&in); err == nil {
				if v, ok := in["approved"].(bool); ok {
					approved = v
				}
			}
		}

		err := tc.SignalWorkflow(ctx, taskID, "", temporal.SignalApproval, approved)
		if err != nil {
			logger.Error("failed to signal workflow", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"task_id": taskID, "approved": approved}); err != nil {
			logger.Error("failed to encode approval response", zap.Error(err))
		}
	})

	// Agentic Loop Interaction Endpoints
	// POST /feedback/{taskID} - Provide feedback to agentic loop
	mux.HandleFunc("/feedback/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/feedback/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		var feedback map[string]any
		if err := json.NewDecoder(r.Body).Decode(&feedback); err != nil {
			logger.Error("failed to decode feedback", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		err := tc.SignalWorkflow(ctx, taskID, "", temporal.SignalFeedback, feedback)
		if err != nil {
			logger.Error("failed to send feedback signal", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"task_id":           taskID,
			"feedback_received": true,
		}); err != nil {
			logger.Error("failed to encode feedback response", zap.Error(err))
		}
	})

	// POST /refine/{taskID} - Request plan refinement
	mux.HandleFunc("/refine/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/refine/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		err := tc.SignalWorkflow(ctx, taskID, "", temporal.SignalRefine, true)
		if err != nil {
			logger.Error("failed to send refine signal", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"task_id":              taskID,
			"refinement_requested": true,
		}); err != nil {
			logger.Error("failed to encode refine response", zap.Error(err))
		}
	})

	// POST /stop/{taskID} - Stop the agentic loop
	mux.HandleFunc("/stop/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID := strings.TrimPrefix(r.URL.Path, "/stop/")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		err := tc.SignalWorkflow(ctx, taskID, "", temporal.SignalStop, true)
		if err != nil {
			logger.Error("failed to send stop signal", zap.Error(err), zap.String("task_id", taskID))
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(map[string]any{"error": err.Error()}); err != nil {
				logger.Error("failed to encode error response", zap.Error(err))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"task_id": taskID,
			"stopped": true,
		}); err != nil {
			logger.Error("failed to encode stop response", zap.Error(err))
		}
	})

	// Apply middleware
	handler := observability.HTTPLoggingMiddleware(logger, mux)
	handler = observability.HTTPMetricsMiddleware(metrics, handler)

	// Create server with timeouts
	port := ":" + cfg.Server.Port
	srv := observability.NewServer(port, handler)

	logger.Info("Starting control-plane orchestrator", zap.String("addr", ":8080"))
	if err := observability.StartServer(logger, srv, "control-orchestrator"); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
