package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	"forgeiq/internal/controlplane/orchestrator"
)

type fakeTools struct {
	callCount int
}

func (f *fakeTools) ListTools(ctx context.Context) ([]interfaces.ToolInfo, error) { return nil, nil }
func (f *fakeTools) CallTool(ctx context.Context, name string, version string, args map[string]any) (map[string]any, error) {
	f.callCount++
	return map[string]any{"ok": true}, nil
}

func TestOrchestrator_Run_Denied(t *testing.T) {
	rule := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contracts.Artifact{
			TaskID: "t1",
			Type:   "PolicyDecision",
			Payload: map[string]any{"policy": map[string]any{
				"allowed": false,
				"reasons": map[string]any{"x": "y"},
			}},
			TS: time.Now(),
		})
	}))
	defer rule.Close()

	decision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("decision agent should not be called on denied policy")
	}))
	defer decision.Close()

	o := &orchestrator.Orchestrator{RuleAgentURL: rule.URL, DecisionAgentURL: decision.URL, Tools: &fakeTools{}}
	task := contracts.Task{ID: "t1", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}, Metadata: map[string]string{}}
	art, err := o.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if art.Payload["status"] != "denied" {
		t.Fatalf("expected denied, got %+v", art.Payload)
	}
}

func TestOrchestrator_Run_HappyPath(t *testing.T) {
	rule := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contracts.Artifact{
			TaskID: "t2",
			Type:   "PolicyDecision",
			Payload: map[string]any{"policy": map[string]any{
				"allowed":        true,
				"allow_writes":   true,
				"tool_tag_allow": []any{"read"},
				"scopes":         []any{"logs:read"},
				"budgets":        map[string]any{"max_tool_calls": float64(10), "max_seconds": float64(60)},
			}},
			TS: time.Now(),
		})
	}))
	defer rule.Close()

	decision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contracts.Artifact{
			TaskID: "t2",
			Type:   "Plan",
			Payload: map[string]any{"plan": map[string]any{
				"confidence": float64(0.7),
				"steps": []any{
					map[string]any{
						"step_id":      "s1",
						"tool_name":    "logs.search",
						"tool_version": "v1",
						"tags":         []any{"read", "logs"},
						"scopes":       []any{"logs:read"},
						"args":         map[string]any{"query": "x"},
						"stop_on_err":  true,
					},
				},
			}},
			TS: time.Now(),
		})
	}))
	defer decision.Close()

	tools := &fakeTools{}
	o := &orchestrator.Orchestrator{RuleAgentURL: rule.URL, DecisionAgentURL: decision.URL, Tools: tools}
	task := contracts.Task{ID: "t2", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}, Metadata: map[string]string{}}
	art, err := o.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if art.Payload["status"] != "completed" {
		t.Fatalf("expected completed, got %+v", art.Payload)
	}
	if tools.callCount != 1 {
		t.Fatalf("expected 1 tool call, got %d", tools.callCount)
	}
}
