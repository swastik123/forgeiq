package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"forgeiq/internal/config"
)

// Agent describes a routable A2A agent endpoint.
type Agent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`     // "rule" | "decision" | ...
	BaseURL string `json:"base_url"` // e.g. https://agent.example.com

	// Optional vendor metadata for adapter selection (e.g., "google", "adobe")
	Vendor string `json:"vendor,omitempty"`

	// Optional alternate endpoints (vendor agents). If empty, canonical A2A uses BaseURL + "/task".
	// Common key: "invoke"
	Endpoints map[string]string `json:"endpoints,omitempty"`

	// Optional metadata (can be discovered)
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	TaskTypes   []string `json:"task_types,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Version     string   `json:"version,omitempty"`

	// Optional weighting for tie-breaks (higher wins)
	Weight float64 `json:"weight,omitempty"`

	// Optional per-agent auth; if empty, falls back to cfg.Auth (rule/decision)
	APIKey  string            `json:"api_key,omitempty"`
	Token   string            `json:"token,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type RouteRequest struct {
	AgentType string   // "rule" | "decision" | ...
	TaskType  string   // optional
	Tags      []string // optional

	// Optional human-readable goal used by HybridRouter for semantic routing.
	Goal string

	// Optional seed for reproducible routing (e.g., taskID/workflowID).
	Seed string
}

type RouteResult struct {
	Agent Agent
	// Optional capability selection (used by HybridRouter when agents expose capabilities).
	CapabilityID string
	// "simple" | "llm" | "fallback"
	Reason string
	Debug  map[string]any
}

// Router selects an agent for a request.
type Router interface {
	Route(ctx context.Context, req RouteRequest) (*RouteResult, error)
}

// AgentRouter routes to the "best" agent from a registry.
type AgentRouter struct {
	Agents  []Agent
	Options RouterOptions
}

type RouterOptions struct {
	Selection          string // "deterministic" | "softmax"
	TieBreakRandom     bool
	TieEpsilon         float64
	SoftmaxTopN        int
	SoftmaxTemperature float64

	// Runtime weights: agentType -> taskType -> agentID -> weight
	RuntimeWeightsEnabled bool
	RuntimeWeightsMax     float64
	RuntimeWeights        map[string]map[string]map[string]float64
}

func optionsFromConfig(cfg *config.Config) RouterOptions {
	o := RouterOptions{
		Selection:             "deterministic",
		TieBreakRandom:        false,
		TieEpsilon:            0.000001,
		SoftmaxTopN:           5,
		SoftmaxTemperature:    1.0,
		RuntimeWeightsEnabled: false,
		RuntimeWeightsMax:     5.0,
		RuntimeWeights:        nil,
	}
	if cfg == nil {
		return o
	}
	if s := strings.ToLower(strings.TrimSpace(cfg.AgentRouter.Selection)); s != "" {
		o.Selection = s
	}
	if o.Selection != "deterministic" && o.Selection != "softmax" {
		o.Selection = "deterministic"
	}
	o.TieBreakRandom = cfg.AgentRouter.TieBreakRandom
	if cfg.AgentRouter.TieEpsilon > 0 {
		o.TieEpsilon = cfg.AgentRouter.TieEpsilon
	}
	if cfg.AgentRouter.SoftmaxTopN > 0 {
		o.SoftmaxTopN = cfg.AgentRouter.SoftmaxTopN
	}
	if cfg.AgentRouter.SoftmaxTemperature > 0 {
		o.SoftmaxTemperature = cfg.AgentRouter.SoftmaxTemperature
	}
	o.RuntimeWeightsEnabled = cfg.AgentRouter.RuntimeWeightsEnabled
	if cfg.AgentRouter.RuntimeWeightsMax > 0 {
		o.RuntimeWeightsMax = cfg.AgentRouter.RuntimeWeightsMax
	}
	return o
}

func NewAgentRouterFromConfig(cfg *config.Config) (*AgentRouter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	raw := strings.TrimSpace(cfg.AgentRouter.AgentsJSON)
	if raw == "" {
		return &AgentRouter{Agents: nil, Options: optionsFromConfig(cfg)}, nil
	}
	var agents []Agent
	if err := json.Unmarshal([]byte(raw), &agents); err != nil {
		return nil, fmt.Errorf("invalid AGENT_ROUTER_AGENTS_JSON: %w", err)
	}
	// Normalize
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		a.ID = strings.TrimSpace(a.ID)
		a.Type = strings.TrimSpace(a.Type)
		a.BaseURL = strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
		if a.ID == "" || a.Type == "" || a.BaseURL == "" {
			return nil, fmt.Errorf("agent requires id,type,base_url")
		}
		out = append(out, a)
	}
	return &AgentRouter{Agents: out, Options: optionsFromConfig(cfg)}, nil
}

// Discover updates agent metadata by fetching an AgentCard (best-effort).
func (r *AgentRouter) Discover(ctx context.Context, discoveryPath string) {
	if r == nil {
		return
	}
	if discoveryPath == "" {
		discoveryPath = "/.well-known/agent.json"
	}
	for i := range r.Agents {
		card, err := FetchAgentCard(ctx, r.Agents[i].BaseURL, discoveryPath)
		if err != nil {
			continue
		}
		if card.Name != "" {
			r.Agents[i].Name = card.Name
		}
		if card.Description != "" {
			r.Agents[i].Description = card.Description
		}
		if len(card.TaskTypes) > 0 {
			r.Agents[i].TaskTypes = card.TaskTypes
		}
		if card.Version != "" {
			r.Agents[i].Version = card.Version
		}
	}
}

func (r *AgentRouter) Route(ctx context.Context, req RouteRequest) (*RouteResult, error) {
	_ = ctx
	if r == nil {
		return nil, fmt.Errorf("agent router is nil")
	}
	req.AgentType = strings.TrimSpace(req.AgentType)
	if req.AgentType == "" {
		return nil, fmt.Errorf("agent_type is required")
	}

	type cand struct {
		a       Agent
		score   float64
		reasons []string
	}
	cands := make([]cand, 0, len(r.Agents))
	for _, a := range r.Agents {
		if a.Type != req.AgentType {
			continue
		}
		score, reasons := scoreAgentWithOptions(a, req, r.Options)
		cands = append(cands, cand{a: a, score: score, reasons: reasons})
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no agent found for type=%s", req.AgentType)
	}

	// Sort deterministically by score desc, then id asc.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score == cands[j].score {
			return cands[i].a.ID < cands[j].a.ID
		}
		return cands[i].score > cands[j].score
	})

	debug := map[string]any{
		"candidates":     len(cands),
		"selection_mode": r.Options.Selection,
		"seed":           req.Seed,
	}

	// Deterministic top-1 with optional random tie-break among equal-top (within epsilon).
	if r.Options.Selection != "softmax" {
		bestScore := cands[0].score
		tieEps := r.Options.TieEpsilon
		tied := []cand{cands[0]}
		for i := 1; i < len(cands); i++ {
			if math.Abs(cands[i].score-bestScore) <= tieEps {
				tied = append(tied, cands[i])
				continue
			}
			break
		}
		debug["top_score"] = bestScore
		debug["tied"] = len(tied)

		chosen := tied[0]
		if r.Options.TieBreakRandom && len(tied) > 1 {
			rng := rand.New(rand.NewSource(hashSeed(req)))
			chosen = tied[rng.Intn(len(tied))]
			debug["tiebreak"] = "random"
		} else {
			debug["tiebreak"] = "deterministic"
		}
		debug["selected_score"] = chosen.score
		debug["selected_reasons"] = chosen.reasons
		return &RouteResult{Agent: chosen.a, Reason: "simple", Debug: debug}, nil
	}

	// Softmax sampling among top-N
	topN := r.Options.SoftmaxTopN
	if topN <= 0 {
		topN = 5
	}
	if topN > len(cands) {
		topN = len(cands)
	}
	temp := r.Options.SoftmaxTemperature
	if temp <= 0 {
		temp = 1.0
	}
	debug["softmax_top_n"] = topN
	debug["softmax_temperature"] = temp
	maxScore := cands[0].score
	weights := make([]float64, 0, topN)
	sum := 0.0
	for i := 0; i < topN; i++ {
		w := math.Exp((cands[i].score - maxScore) / temp)
		weights = append(weights, w)
		sum += w
	}
	rng := rand.New(rand.NewSource(hashSeed(req)))
	x := rng.Float64() * sum
	acc := 0.0
	chosenIdx := 0
	for i, w := range weights {
		acc += w
		if x <= acc {
			chosenIdx = i
			break
		}
	}
	chosen := cands[chosenIdx]
	debug["selected_score"] = chosen.score
	debug["selected_reasons"] = chosen.reasons
	debug["softmax_pick_index"] = chosenIdx
	return &RouteResult{Agent: chosen.a, Reason: "simple", Debug: debug}, nil
}

func hashSeed(req RouteRequest) int64 {
	h := fnv.New64a()
	seed := strings.TrimSpace(req.Seed)
	if seed == "" {
		seed = req.AgentType + "|" + req.TaskType + "|" + strings.Join(req.Tags, ",") + "|" + req.Goal
	}
	_, _ = h.Write([]byte(seed))
	return int64(h.Sum64())
}

func scoreAgent(a Agent, req RouteRequest) (float64, []string) {
	score := 0.0
	var reasons []string

	// TaskType match
	if req.TaskType != "" && containsStr(a.TaskTypes, req.TaskType) {
		score += 50
		reasons = append(reasons, "task_type_match")
	}
	// Tag overlap
	for _, t := range req.Tags {
		if containsStr(a.Tags, t) {
			score += 10
			reasons = append(reasons, "tag:"+t)
		}
	}
	// Weight
	if a.Weight != 0 {
		score += a.Weight
		reasons = append(reasons, "weight")
	}
	// Small bias for discovered metadata completeness
	if a.Name != "" {
		score += 0.1
	}
	return score, reasons
}

func scoreAgentWithOptions(a Agent, req RouteRequest, opt RouterOptions) (float64, []string) {
	score, reasons := scoreAgent(a, req)
	if !opt.RuntimeWeightsEnabled || opt.RuntimeWeights == nil {
		return score, reasons
	}
	tt := strings.TrimSpace(req.TaskType)
	if tt == "" {
		return score, reasons
	}
	at := strings.TrimSpace(req.AgentType)
	byType := opt.RuntimeWeights[at]
	if byType == nil {
		return score, reasons
	}
	byTask := byType[tt]
	if byTask == nil {
		return score, reasons
	}
	w := byTask[a.ID]
	if w == 0 {
		return score, reasons
	}
	maxAbs := opt.RuntimeWeightsMax
	if maxAbs <= 0 {
		maxAbs = 5.0
	}
	if w > maxAbs {
		w = maxAbs
	}
	if w < -maxAbs {
		w = -maxAbs
	}
	score += w
	reasons = append(reasons, "runtime_weight")
	return score, reasons
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// NewClientForAgent builds an A2A client for a routed agent, applying auth rules.
func NewClientForAgent(cfg *config.Config, agent Agent) *Client {
	timeout := 30 * time.Second
	headers := agent.Headers
	apiKey := agent.APIKey
	token := agent.Token

	// fallback auth: use existing cfg.Auth for known agent types
	if cfg != nil {
		if apiKey == "" && token == "" && len(headers) == 0 {
			switch agent.Type {
			case "rule":
				apiKey = cfg.Auth.RuleAgentAPIKey
				token = cfg.Auth.RuleAgentToken
				headers = cfg.Auth.GetRuleAgentHeaders()
			case "decision":
				apiKey = cfg.Auth.DecisionAgentAPIKey
				token = cfg.Auth.DecisionAgentToken
				headers = cfg.Auth.GetDecisionAgentHeaders()
			}
		}
	}

	// Default adapter registry: supports canonical A2A + optional invoke endpoint.
	adapters := NewAdapterRegistry()

	return NewClientWithConfig(ClientConfig{
		BaseURL:  agent.BaseURL,
		Timeout:  timeout,
		APIKey:   apiKey,
		Token:    token,
		Headers:  headers,
		Agent:    &agent,
		Adapters: adapters,
	})
}
