//go:build !k8s
// +build !k8s

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"forgeiq/internal/controlplane/contracts"
)

func main() {
	agent := &stubExecAgent{
		cache: map[string]cacheEntry{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", agent.handleDiscovery)
	mux.HandleFunc("/task", agent.handleTask)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	port := os.Getenv("EXEC_AGENT_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8085"
	}
	addr := ":" + port
	log.Printf("Exec Agent (stub) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

type cacheEntry struct {
	Out      contracts.ExecOutput
	ExpireAt time.Time
}

type stubExecAgent struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
}

func (a *stubExecAgent) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	meta := map[string]any{
		"name":    "exec-agent-stub",
		"version": "v1",
		"kind":    "exec",
		"note":    "stub exec agent (no-k8s build); actions are simulated but idempotent",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (a *stubExecAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	var req contracts.A2ATaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in contracts.ExecInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Idempotency: if idempotency_key is provided and already seen, return cached output.
	if key := req.IdempotencyKey; key != "" {
		if out, ok := a.getCached(key); ok {
			a.writeOK(w, out)
			return
		}
	}

	out := contracts.ExecOutput{
		Success: true,
		Details: "stub executed action_type=" + in.ActionType,
	}
	if req.IdempotencyKey != "" {
		a.putCached(req.IdempotencyKey, out, 24*time.Hour)
	}
	a.writeOK(w, out)
}

func (a *stubExecAgent) getCached(key string) (contracts.ExecOutput, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.cache[key]; ok {
		if time.Now().Before(e.ExpireAt) {
			return e.Out, true
		}
		delete(a.cache, key)
	}
	return contracts.ExecOutput{}, false
}

func (a *stubExecAgent) putCached(key string, out contracts.ExecOutput, ttl time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[key] = cacheEntry{Out: out, ExpireAt: time.Now().Add(ttl)}
}

func (a *stubExecAgent) writeOK(w http.ResponseWriter, out contracts.ExecOutput) {
	respBytes, _ := json.Marshal(out)
	resp := contracts.A2ATaskResponse{Status: "ok", Output: respBytes}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
