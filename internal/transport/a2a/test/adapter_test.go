package transporta2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/transport/a2a"
)

func TestClient_DefaultAdapter_CallsTaskEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(contracts.Artifact{TaskID: "t1", Type: "ok", Payload: map[string]any{"x": 1}})
	}))
	defer srv.Close()

	agent := a2a.Agent{ID: "a1", Type: "rule", BaseURL: srv.URL}
	c := a2a.NewClientForAgent(nil, agent)
	_, err := c.RunTask(context.Background(), contracts.Task{ID: "t1", Type: "incident_triage", Input: map[string]any{"x": 1}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotPath != "/task" {
		t.Fatalf("expected /task, got %s", gotPath)
	}
}

func TestClient_InvokeAdapter_CallsInvokeEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": "t2",
			"status":  "success",
			"output":  map[string]any{"ok": true},
		})
	}))
	defer srv.Close()

	agent := a2a.Agent{
		ID:      "v1",
		Type:    "decision",
		BaseURL: "http://ignored",
		Endpoints: map[string]string{
			"invoke": srv.URL + "/invoke",
		},
	}
	c := a2a.NewClientForAgent(nil, agent)
	art, err := c.RunTask(context.Background(), contracts.Task{ID: "t2", Type: "summarize", Input: map[string]any{"doc": "x"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotPath != "/invoke" {
		t.Fatalf("expected /invoke, got %s", gotPath)
	}
	if art.Type == "" || art.Payload == nil {
		t.Fatalf("expected artifact wrapper, got %+v", art)
	}
}
