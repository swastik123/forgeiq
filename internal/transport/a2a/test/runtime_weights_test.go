package transporta2a_test

import (
	"context"
	"testing"

	"forgeiq/internal/config"
	"forgeiq/internal/transport/a2a"
)

func TestAgentRouter_RuntimeWeights_ShiftsSelection(t *testing.T) {
	cfg := config.Load()
	cfg.AgentRouter.Selection = "deterministic"
	cfg.AgentRouter.RuntimeWeightsEnabled = true
	cfg.AgentRouter.RuntimeWeightsMax = 5.0

	r, err := a2a.NewAgentRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	r.Agents = []a2a.Agent{
		{ID: "a1", Type: "decision", BaseURL: "http://a1", Weight: 0},
		{ID: "a2", Type: "decision", BaseURL: "http://a2", Weight: 0},
	}
	r.Options.RuntimeWeights = map[string]map[string]map[string]float64{
		"decision": {
			"incident_triage": {
				"a2": 3.0,
			},
		},
	}

	res, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", TaskType: "incident_triage", Seed: "t1"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a2" {
		t.Fatalf("expected a2 due to runtime weight, got %s", res.Agent.ID)
	}
}
