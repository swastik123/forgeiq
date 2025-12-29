package transporta2a_test

import (
	"context"
	"testing"

	"forgeiq/internal/config"
	"forgeiq/internal/transport/a2a"
)

func TestAgentRouter_TieBreakRandom_Reproducible(t *testing.T) {
	cfg := config.Load()
	cfg.AgentRouter.Selection = "deterministic"
	cfg.AgentRouter.TieBreakRandom = true
	cfg.AgentRouter.TieEpsilon = 0.000001

	r, err := a2a.NewAgentRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Override agents with same score (no tags/task types, same weight)
	r.Agents = []a2a.Agent{
		{ID: "a1", Type: "decision", BaseURL: "http://a1"},
		{ID: "a2", Type: "decision", BaseURL: "http://a2"},
	}

	seed := "task-123"
	res1, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Seed: seed})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// same seed should produce same choice repeatedly
	for i := 0; i < 10; i++ {
		res2, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Seed: seed})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res2.Agent.ID != res1.Agent.ID {
			t.Fatalf("expected reproducible choice, got %s then %s", res1.Agent.ID, res2.Agent.ID)
		}
	}
}

func TestAgentRouter_Softmax_Reproducible(t *testing.T) {
	cfg := config.Load()
	cfg.AgentRouter.Selection = "softmax"
	cfg.AgentRouter.SoftmaxTopN = 2
	cfg.AgentRouter.SoftmaxTemperature = 1.0

	r, err := a2a.NewAgentRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Force scores: a2 is slightly better due to weight
	r.Agents = []a2a.Agent{
		{ID: "a1", Type: "rule", BaseURL: "http://a1", Weight: 0},
		{ID: "a2", Type: "rule", BaseURL: "http://a2", Weight: 1},
	}

	seed := "task-abc"
	res1, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "rule", Seed: seed})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for i := 0; i < 10; i++ {
		res2, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "rule", Seed: seed})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res2.Agent.ID != res1.Agent.ID {
			t.Fatalf("expected reproducible softmax choice, got %s then %s", res1.Agent.ID, res2.Agent.ID)
		}
	}
}

func TestAgentRouter_InvalidSelection_FallsBackToDeterministic(t *testing.T) {
	cfg := config.Load()
	cfg.AgentRouter.Selection = "nonsense"

	r, err := a2a.NewAgentRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	r.Agents = []a2a.Agent{
		{ID: "a2", Type: "decision", BaseURL: "http://a2", Weight: 1},
		{ID: "a1", Type: "decision", BaseURL: "http://a1", Weight: 0},
	}
	res, err := r.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Seed: "x"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a2" {
		t.Fatalf("expected deterministic top score, got %s", res.Agent.ID)
	}
}
