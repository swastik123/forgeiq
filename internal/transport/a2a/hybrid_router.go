package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"forgeiq/internal/config"
)

// HybridRouter routes using:
// 1) hard filters + heuristic scoring (same as AgentRouter)
// 2) optional LLM selection among top-N candidates
// 3) strict validation + fallback
type HybridRouter struct {
	Agents   []Agent
	LLM      LLMClient
	Fallback Router

	MaxCandidates int
	LLMTimeout    time.Duration

	Options RouterOptions
}

func NewHybridRouter(agents []Agent, llm LLMClient, fallback Router) *HybridRouter {
	if fallback == nil {
		fallback = &AgentRouter{Agents: agents}
	}
	return &HybridRouter{
		Agents:        agents,
		LLM:           llm,
		Fallback:      fallback,
		MaxCandidates: 20,
		LLMTimeout:    1500 * time.Millisecond,
	}
}

// NewRouterFromConfig builds either a simple or hybrid router based on config.
// Pass in the candidate agents that were loaded (from config JSON or from registry store).
func NewRouterFromConfig(cfg *config.Config, agents []Agent) (Router, error) {

	//
	base := &AgentRouter{Agents: agents, Options: optionsFromConfig(cfg)}
	if cfg == nil || !cfg.AgentRouter.Enabled {
		return base, nil
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.AgentRouter.Mode))
	if mode == "" {
		mode = "simple"
	}
	if mode != "hybrid" {
		return base, nil
	}
	if !cfg.AgentRouter.LLM.Enabled || strings.TrimSpace(cfg.AgentRouter.LLM.URL) == "" {
		return base, nil
	}

	llm := NewHTTPLLMClient(cfg.AgentRouter.LLM.URL)
	llm.Timeout = time.Duration(cfg.AgentRouter.LLM.TimeoutMS) * time.Millisecond
	llm.APIKey = cfg.AgentRouter.LLM.APIKey
	llm.Token = cfg.AgentRouter.LLM.Token
	llm.Headers = cfg.Auth.ParseHeaders(cfg.AgentRouter.LLM.Headers)

	hr := NewHybridRouter(agents, llm, base)
	hr.Options = base.Options
	if cfg.AgentRouter.LLM.MaxCandidates > 0 {
		hr.MaxCandidates = cfg.AgentRouter.LLM.MaxCandidates
	}
	return hr, nil
}

func (r *HybridRouter) Route(ctx context.Context, req RouteRequest) (*RouteResult, error) {
	if r == nil {
		return nil, fmt.Errorf("hybrid router is nil")
	}
	// Build candidate list using same heuristic scoring, but keep top-N.
	cands, scored := r.topCandidates(req)
	if len(cands) == 0 {
		return nil, fmt.Errorf("no agent found for type=%s", strings.TrimSpace(req.AgentType))
	}
	debug := map[string]any{
		"mode":           "hybrid",
		"candidates":     len(cands),
		"candidates_all": scored,
	}

	if r.LLM == nil {
		res, err := r.Fallback.Route(ctx, req)
		if res != nil && res.Debug != nil {
			for k, v := range res.Debug {
				debug["fallback_"+k] = v
			}
			res.Debug = debug
			res.Reason = "fallback"
		}
		return res, err
	}

	prompt := buildPrompt(req, cands)
	llmCtx := ctx
	if r.LLMTimeout > 0 {
		var cancel context.CancelFunc
		llmCtx, cancel = context.WithTimeout(ctx, r.LLMTimeout)
		defer cancel()
	}
	choice, err := r.LLM.Choose(llmCtx, prompt)
	debug["llm_raw"] = choice.Raw
	debug["llm_err"] = ""
	if err != nil {
		debug["llm_err"] = err.Error()
		res, ferr := r.Fallback.Route(ctx, req)
		if res != nil {
			if res.Debug == nil {
				res.Debug = map[string]any{}
			}
			for k, v := range debug {
				res.Debug[k] = v
			}
			res.Reason = "fallback"
		}
		if ferr != nil {
			return nil, ferr
		}
		return res, nil
	}

	// Validate: choose first valid from ranked list (if present), else single choice.
	selectedAgentID := ""
	selectedCapID := ""
	if len(choice.Ranked) > 0 {
		debug["llm_ranked_count"] = len(choice.Ranked)
		for _, rc := range choice.Ranked {
			if rc.AgentID == "" {
				continue
			}
			if _, ok := findAgentByID(cands, rc.AgentID); ok {
				selectedAgentID = rc.AgentID
				selectedCapID = rc.CapabilityID
				break
			}
		}
	} else {
		selectedAgentID = choice.AgentID
		selectedCapID = choice.CapabilityID
	}

	agent, ok := findAgentByID(cands, selectedAgentID)
	if !ok {
		debug["llm_invalid_agent"] = selectedAgentID
		res, ferr := r.Fallback.Route(ctx, req)
		if res != nil {
			if res.Debug == nil {
				res.Debug = map[string]any{}
			}
			for k, v := range debug {
				res.Debug[k] = v
			}
			res.Reason = "fallback"
		}
		if ferr != nil {
			return nil, ferr
		}
		return res, nil
	}

	return &RouteResult{
		Agent:        agent,
		CapabilityID: selectedCapID,
		Reason:       "llm",
		Debug:        debug,
	}, nil
}

type scoredAgent struct {
	Agent   Agent
	Score   float64
	Reasons []string
}

func (r *HybridRouter) topCandidates(req RouteRequest) ([]Agent, []map[string]any) {
	req.AgentType = strings.TrimSpace(req.AgentType)
	if req.AgentType == "" {
		return nil, nil
	}
	all := make([]scoredAgent, 0, len(r.Agents))
	for _, a := range r.Agents {
		if strings.TrimSpace(a.Type) != req.AgentType {
			continue
		}
		s, reasons := scoreAgentWithOptions(a, req, r.Options)
		all = append(all, scoredAgent{Agent: a, Score: s, Reasons: reasons})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })

	n := len(all)
	if r.MaxCandidates > 0 && n > r.MaxCandidates {
		n = r.MaxCandidates
	}
	out := make([]Agent, 0, n)
	debug := make([]map[string]any, 0, len(all))
	for idx, sa := range all {
		debug = append(debug, map[string]any{
			"rank":     idx + 1,
			"id":       sa.Agent.ID,
			"base_url": sa.Agent.BaseURL,
			"score":    sa.Score,
			"reasons":  sa.Reasons,
		})
		if idx < n {
			out = append(out, sa.Agent)
		}
	}
	return out, debug
}

func findAgentByID(xs []Agent, id string) (Agent, bool) {
	id = strings.TrimSpace(id)
	for _, a := range xs {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

func buildPrompt(req RouteRequest, cands []Agent) string {
	var b strings.Builder
	b.WriteString("You are an agent router.\n")
	b.WriteString("You must choose the best agent from the candidates below.\n")
	b.WriteString("Rules:\n")
	b.WriteString("1. Only choose an agent_id that exists in the candidates list.\n")
	b.WriteString("2. If no agent is appropriate, return {\"agent_id\":\"\",\"capability_id\":\"\"}.\n")
	b.WriteString("3. Respond ONLY as raw JSON. No extra text, no markdown.\n")
	b.WriteString("4. Prefer returning a ranked shortlist when possible.\n")
	b.WriteString("\n")
	b.WriteString("Valid response shapes:\n")
	b.WriteString("{\"agent_id\":\"...\",\"capability_id\":\"...\"}\n")
	b.WriteString("{\"ranked\":[{\"agent_id\":\"...\",\"capability_id\":\"...\",\"score\":0.82},{\"agent_id\":\"...\",\"capability_id\":\"...\",\"score\":0.61}]}\n\n")

	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		goal = strings.TrimSpace(req.TaskType)
	}
	b.WriteString("Task / goal:\n")
	b.WriteString(goal)
	b.WriteString("\n\n")

	// Emit candidates as JSON so the model has a clean, machine-readable list.
	type cand struct {
		AgentID     string   `json:"agent_id"`
		Name        string   `json:"name,omitempty"`
		TaskTypes   []string `json:"task_types,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		Description string   `json:"description,omitempty"`
		Vendor      string   `json:"vendor,omitempty"`
	}

	out := make([]cand, 0, len(cands))
	for _, a := range cands {
		out = append(out, cand{
			AgentID:     a.ID,
			Name:        a.Name,
			TaskTypes:   a.TaskTypes,
			Tags:        a.Tags,
			Description: a.Description,
			Vendor:      a.Vendor,
		})
	}

	b.WriteString("Candidates (JSON array):\n")
	if bb, err := json.Marshal(out); err == nil {
		b.Write(bb)
	} else {
		// Fallback: if marshal fails for some reason, provide minimal list.
		b.WriteString("[")
		for i, a := range cands {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(fmt.Sprintf("{\"agent_id\":\"%s\"}", a.ID))
		}
		b.WriteString("]")
	}

	b.WriteString("\n\nReturn JSON only.\n")
	b.WriteString("{\"agent_id\":\"jira-change-ticket-agent\",\"capability_id\":\"\"}\n")
	return b.String()
}
