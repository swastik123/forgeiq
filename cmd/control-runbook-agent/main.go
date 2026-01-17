package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/runbooks"
)

type RunbookAgent struct {
	Port      string
	Runbooks  map[string]contracts.Runbook
	DefaultID string
}

func main() {
	port := os.Getenv("RUNBOOK_AGENT_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8084"
	}

	dir := os.Getenv("RUNBOOKS_DIR")
	if dir == "" {
		dir = "runbooks"
	}
	rbs, err := runbooks.LoadDir(dir)
	if err != nil {
		log.Fatalf("failed to load runbooks from %s: %v", dir, err)
	}
	defaultID := strings.TrimSpace(os.Getenv("RUNBOOK_DEFAULT_ID"))
	if defaultID == "" {
		// Prefer the ID the demo rule-agent returns, otherwise stable first id.
		if _, ok := rbs["rb-default-k8s"]; ok {
			defaultID = "rb-default-k8s"
		} else {
			defaultID = runbooks.StableFirstID(rbs)
		}
	}

	agent := &RunbookAgent{
		Port:      port,
		Runbooks:  rbs,
		DefaultID: defaultID,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", agent.handleDiscovery)
	mux.HandleFunc("/task", agent.handleTask)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	addr := ":" + port
	log.Printf("Runbook Agent listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (a *RunbookAgent) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	meta := map[string]any{
		"name":     "runbook-agent",
		"version":  "v1",
		"kind":     "runbook",
		"endpoint": "http://localhost:" + a.Port + "/task",
		"runbooks": len(a.Runbooks),
		"default":  a.DefaultID,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (a *RunbookAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req contracts.A2ATaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Agent != contracts.AgentKindRunbook {
		writeA2AError(w, "invalid agent kind")
		return
	}
	if strings.TrimSpace(req.TaskType) != "get_runbook" {
		writeA2AError(w, "unsupported task_type")
		return
	}

	var in contracts.RunbookAgentInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := strings.TrimSpace(in.RunbookID)
	if id == "" {
		id = a.DefaultID
	}
	rb, ok := a.Runbooks[id]
	if !ok {
		writeA2AError(w, "unknown runbook_id: "+id)
		return
	}
	rb = runbooks.Render(rb, map[string]string{
		"service":     in.Service,
		"incident_id": in.IncidentID,
		"runbook_id":  id,
	})
	rb = runbooks.NormalizeIDs(rb)

	out := contracts.RunbookAgentOutput{Runbook: rb}
	respBytes, _ := json.Marshal(out)
	resp := contracts.A2ATaskResponse{
		Status: "ok",
		Output: respBytes,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeA2AError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contracts.A2ATaskResponse{
		Status: "error",
		Error:  msg,
	})
}
