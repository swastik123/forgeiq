package transporta2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forgeiq/internal/transport/a2a"
)

type fakeFallbackRouter struct {
	chosen a2a.Agent
}

func (f *fakeFallbackRouter) Route(ctx context.Context, req a2a.RouteRequest) (*a2a.RouteResult, error) {
	_ = ctx
	_ = req
	return &a2a.RouteResult{Agent: f.chosen, Reason: "simple", Debug: map[string]any{"fallback": true}}, nil
}

func TestHybridRouter_UsesLLMWhenValid(t *testing.T) {
	// Fake LLM returns a valid candidate
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"agent_id": "a2"})
	}))
	defer llm.Close()

	agents := []a2a.Agent{
		{ID: "a1", Type: "decision", BaseURL: "http://a1", Tags: []string{"summarization"}},
		{ID: "a2", Type: "decision", BaseURL: "http://a2", Tags: []string{"summarization", "high-quality"}, Weight: 1},
	}

	h := a2a.NewHybridRouter(agents, &a2a.HTTPLLMClient{URL: llm.URL, Timeout: 500 * time.Millisecond}, &fakeFallbackRouter{chosen: agents[0]})
	h.MaxCandidates = 10

	res, err := h.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Goal: "Summarize this 20-page policy doc."})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a2" {
		t.Fatalf("expected a2, got %s", res.Agent.ID)
	}
	if res.Reason != "llm" {
		t.Fatalf("expected reason llm, got %s", res.Reason)
	}
}

func TestHybridRouter_FallbackWhenLLMReturnsUnknownAgent(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"agent_id": "not-a-candidate"})
	}))
	defer llm.Close()

	agents := []a2a.Agent{
		{ID: "a1", Type: "rule", BaseURL: "http://a1"},
		{ID: "a2", Type: "rule", BaseURL: "http://a2"},
	}
	fb := &fakeFallbackRouter{chosen: agents[0]}
	h := a2a.NewHybridRouter(agents, &a2a.HTTPLLMClient{URL: llm.URL, Timeout: 500 * time.Millisecond}, fb)

	res, err := h.Route(context.Background(), a2a.RouteRequest{AgentType: "rule", Goal: "policy"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a1" {
		t.Fatalf("expected fallback a1, got %s", res.Agent.ID)
	}
	if res.Reason != "fallback" {
		t.Fatalf("expected fallback reason, got %s", res.Reason)
	}
}

func TestHybridRouter_FallbackWhenLLMErrors(t *testing.T) {
	// Server returns 500
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer llm.Close()

	agents := []a2a.Agent{
		{ID: "a1", Type: "rule", BaseURL: "http://a1"},
	}
	fb := &fakeFallbackRouter{chosen: agents[0]}
	h := a2a.NewHybridRouter(agents, &a2a.HTTPLLMClient{URL: llm.URL, Timeout: 500 * time.Millisecond}, fb)

	res, err := h.Route(context.Background(), a2a.RouteRequest{AgentType: "rule", Goal: "policy"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a1" {
		t.Fatalf("expected fallback a1, got %s", res.Agent.ID)
	}
	if res.Reason != "fallback" {
		t.Fatalf("expected fallback reason, got %s", res.Reason)
	}
}

func TestHybridRouter_RankedList_PicksFirstValid(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ranked": []any{
				map[string]any{"agent_id": "a2", "capability_id": "c1", "score": 0.82},
				map[string]any{"agent_id": "a1", "capability_id": "c9", "score": 0.61},
			},
		})
	}))
	defer llm.Close()

	agents := []a2a.Agent{
		{ID: "a1", Type: "decision", BaseURL: "http://a1"},
		{ID: "a2", Type: "decision", BaseURL: "http://a2"},
	}
	h := a2a.NewHybridRouter(agents, &a2a.HTTPLLMClient{URL: llm.URL, Timeout: 500 * time.Millisecond}, &fakeFallbackRouter{chosen: agents[0]})

	res, err := h.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Goal: "summarize"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a2" {
		t.Fatalf("expected a2, got %s", res.Agent.ID)
	}
	if res.CapabilityID != "c1" {
		t.Fatalf("expected cap c1, got %s", res.CapabilityID)
	}
	if res.Reason != "llm" {
		t.Fatalf("expected reason llm, got %s", res.Reason)
	}
}

func TestHybridRouter_RankedList_SkipsInvalidThenPicksValid(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ranked": []any{
				map[string]any{"agent_id": "not-in-candidates", "score": 0.9},
				map[string]any{"agent_id": "a1", "capability_id": "c9", "score": 0.6},
			},
		})
	}))
	defer llm.Close()

	agents := []a2a.Agent{
		{ID: "a1", Type: "decision", BaseURL: "http://a1"},
	}
	h := a2a.NewHybridRouter(agents, &a2a.HTTPLLMClient{URL: llm.URL, Timeout: 500 * time.Millisecond}, &fakeFallbackRouter{chosen: agents[0]})

	res, err := h.Route(context.Background(), a2a.RouteRequest{AgentType: "decision", Goal: "summarize"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Agent.ID != "a1" {
		t.Fatalf("expected a1, got %s", res.Agent.ID)
	}
	if res.CapabilityID != "c9" {
		t.Fatalf("expected cap c9, got %s", res.CapabilityID)
	}
	if res.Reason != "llm" {
		t.Fatalf("expected reason llm, got %s", res.Reason)
	}
}
