package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgeiq/internal/agent/strategy"
	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	"forgeiq/internal/controlplane/temporal"
	"forgeiq/internal/eval"
	"forgeiq/internal/middleware"
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

		// Validate request shape early (before any network calls).
		if err := validateStrategyRunRequest(req.Strategy, req.State, req.Budget.MaxIterations, req.Budget.MaxToolCalls, req.Budget.MaxWallTimeMS, req.Model); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mode := strings.ToLower(strings.TrimSpace(req.Strategy))
		if mode == "" {
			mode = "function_calling"
		}

		// Determine model provider (request overrides config).
		provider := strings.ToLower(strings.TrimSpace(req.Model.Provider))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(cfg.Reasoning.Provider))
		}
		if provider == "" {
			provider = "mcp"
		}
		req.Model.Provider = provider

		// Sanity check MCP base URL (gives a clearer error than "connection refused" later).
		if err := validateHTTPBaseURL(cfg.MCPBaseURL); err != nil {
			http.Error(w, "invalid MCP_BASE_URL: "+err.Error(), http.StatusBadRequest)
			return
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
		// If the strategy model is implemented via MCP tool `llm.chat:v1`, fail fast if missing.
		if provider == "mcp" && !hasTool(tools, "llm.chat", "v1") {
			http.Error(w, "required tool missing from MCP: llm.chat:v1 (ensure data-mcp-server is running and registered)", http.StatusBadGateway)
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
		switch provider {
		case "mcp":
			// Model client uses MCP tool `llm.chat` (demo tool registered by data-mcp-server).
			in.Modeler = strategy.NewMCPModelClient(tc)
		case "dspy":
			if err := validateHTTPBaseURL(cfg.Reasoning.DSPyURL); err != nil {
				http.Error(w, "invalid DSPY_URL: "+err.Error(), http.StatusBadRequest)
				return
			}
			c := strategy.NewDSPyModelClient(cfg.Reasoning.DSPyURL)
			c.APIKey = cfg.Reasoning.DSPyKey
			in.Modeler = c
		default:
			http.Error(w, "unsupported model.provider: "+provider, http.StatusBadRequest)
			return
		}

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
			if typ != "" && typ != "rule" && typ != "decision" {
				http.Error(w, "invalid type (must be 'rule' or 'decision')", http.StatusBadRequest)
				return
			}
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
	// POST /run
	// New body: {"tenant_id":"t1","type":"incident_triage","input":{...},"metadata":{...}}
	// Backward compatible: body can be any JSON object; it will be treated as task.input with default type.
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			logger.Error("failed to decode request body", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		tenantID := ""

		// Parse request in a backward-compatible way.
		// Preferred: {"tenant_id":"...","type":"...","input":{...},"metadata":{...}}
		taskType := ""
		taskInput := map[string]any{}
		taskMeta := map[string]string{}

		if v, ok := raw["tenant_id"].(string); ok {
			tenantID = strings.TrimSpace(v)
		}
		if v, ok := raw["type"].(string); ok {
			taskType = strings.TrimSpace(v)
		}
		if v, ok := raw["metadata"].(map[string]any); ok && v != nil {
			for k, vv := range v {
				ks := strings.TrimSpace(k)
				if ks == "" {
					continue
				}
				// metadata is string->string for now; coerce scalars to string
				taskMeta[ks] = fmt.Sprint(vv)
			}
		}
		if v, ok := raw["input"].(map[string]any); ok && v != nil {
			taskInput = v
		} else {
			// Backward compat: treat entire request as input if "input" is not present.
			taskInput = raw
		}

		if taskType == "" {
			taskType = "incident_triage"
		}

		// Enforce tenant id derived from authentication (if present).
		authed := middleware.TenantFromContext(r.Context())
		if strings.TrimSpace(authed.TenantID) != "" {
			if tenantID != "" && tenantID != authed.TenantID {
				http.Error(w, "tenant_id does not match authenticated tenant", http.StatusForbidden)
				return
			}
			tenantID = authed.TenantID
		}

		taskID := makeTaskID(tenantID)
		task := contracts.Task{
			ID:        taskID,
			TenantID:  tenantID,
			Type:      taskType,
			Input:     taskInput,
			Metadata:  taskMeta,
			CreatedAt: time.Now(),
		}

		// Workflow registry (task.type -> workflow)
		var wf any
		switch task.Type {
		case "incident_triage":
			wf = temporal.IncidentWorkflow
		case "incident_triage_iterative":
			wf = temporal.IncidentWorkflowIterative
		case "incident_triage_agentic":
			wf = temporal.IncidentWorkflowWithAgenticLoop
		case "runbook_automation":
			wf = temporal.RunbookAutomationWorkflow
		default:
			http.Error(w, "unknown task type: "+task.Type, http.StatusBadRequest)
			return
		}

		we, err := tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:        task.ID,
			TaskQueue: cfg.Temporal.TaskQueue,
		}, wf, task)

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
			"task_type":   task.Type,
			"tenant_id":   task.TenantID,
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
		var lastUpdated string
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
				// Emit a "status" event whenever the workflow reports a new UpdatedAtRFC or state change.
				// This makes waiting-for-human status changes (prompt, waiting_since, etc.) visible immediately.
				if st.State != lastState || st.UpdatedAtRFC != lastUpdated {
					lastState = st.State
					lastUpdated = st.UpdatedAtRFC
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
	handler := http.Handler(mux)
	handler = middleware.TenantAuthMiddleware(cfg)(handler)
	handler = observability.HTTPLoggingMiddleware(logger, handler)
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

var tenantIDRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeTenantID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = tenantIDRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func makeTaskID(tenantID string) string {
	ts := time.Now().Format("20060102-150405.000")
	t := sanitizeTenantID(tenantID)
	if t == "" {
		return "task-" + ts
	}
	return "tenant-" + t + "-task-" + ts
}

func hasTool(tools []interfaces.ToolInfo, name, version string) bool {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	for _, t := range tools {
		if t.Name == name && t.Version == version {
			return true
		}
	}
	return false
}

func validateHTTPBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

func validateStrategyRunRequest(mode string, state strategy.ConversationState, maxIterations, maxToolCalls, maxWallTimeMS int, model strategy.ModelConfig) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "function_calling"
	}
	if mode != "function_calling" && mode != "react" {
		return fmt.Errorf("unknown strategy: %s", mode)
	}
	if maxIterations < 0 || maxToolCalls < 0 || maxWallTimeMS < 0 {
		return fmt.Errorf("budget values must be >= 0")
	}
	// Provider is validated here only for "obvious wrong values".
	// The handler may still override empty provider from config and will do final routing.
	if p := strings.ToLower(strings.TrimSpace(model.Provider)); p != "" && p != "mcp" && p != "dspy" {
		return fmt.Errorf("unsupported model.provider %q for /strategy/run (supported: mcp|dspy)", model.Provider)
	}
	if len(state.Messages) == 0 {
		return fmt.Errorf("state.messages is required")
	}
	foundUser := false
	for _, m := range state.Messages {
		if m.Role == strategy.RoleUser && strings.TrimSpace(m.Content) != "" {
			foundUser = true
			break
		}
	}
	if !foundUser {
		return fmt.Errorf("state.messages must include at least one non-empty user message")
	}
	return nil
}
