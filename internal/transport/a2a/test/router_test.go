package transporta2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/config"
	"forgeiq/internal/transport/a2a"
)

func TestAgentRouter_RouteScoresTaskType(t *testing.T) {
	r := &a2a.AgentRouter{Agents: []a2a.Agent{
		{ID: "a", Type: "decision", BaseURL: "http://a", TaskTypes: []string{"x"}},
		{ID: "b", Type: "decision", BaseURL: "http://b", TaskTypes: []string{"incident_triage"}},
	}}
	res, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", TaskType: "incident_triage"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "b" {
		t.Fatalf("expected b, got %s", res.Agent.ID)
	}
}

func TestNewAgentRouterFromConfig_ParsesJSON(t *testing.T) {
	cfg := &config.Config{}
	cfg.AgentRouter.Enabled = true
	cfg.AgentRouter.AgentsJSON = `[{"id":"r1","type":"rule","base_url":"http://x"}]`
	r, err := a2a.NewAgentRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(r.Agents) != 1 || r.Agents[0].ID != "r1" {
		t.Fatalf("unexpected agents: %+v", r.Agents)
	}
}

func TestDiscover_FetchesCard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(a2a.AgentCard{Name: "n", Version: "1.2.3", TaskTypes: []string{"t"}})
	}))
	defer ts.Close()

	r := &a2a.AgentRouter{Agents: []a2a.Agent{{ID: "a", Type: "rule", BaseURL: ts.URL}}}
	r.Discover(context.Background(), "/.well-known/agent.json")
	if r.Agents[0].Version != "1.2.3" {
		t.Fatalf("expected version from card")
	}
}
