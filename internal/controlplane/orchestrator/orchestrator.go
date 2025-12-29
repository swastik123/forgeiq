package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	"forgeiq/internal/transport/a2a"
)

type Orchestrator struct {
	RuleAgentURL     string
	DecisionAgentURL string
	Tools            interfaces.ToolClient

	// Optional: agent router (if set, routes rule/decision calls dynamically)
	AgentRouter a2a.Router
	Config      *config.Config
}

func (o *Orchestrator) Run(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
	start := time.Now()

	// 1) Policy
	ruleClient := o.pickClient(ctx, "rule", o.RuleAgentURL, task.ID, task.Type)
	pArt, err := ruleClient.RunTask(ctx, task)
	if err != nil {
		return contracts.Artifact{}, err
	}
	policy, ok := pArt.Payload["policy"].(map[string]any)
	if !ok {
		return contracts.Artifact{}, errors.New("invalid policy artifact")
	}
	pd := decodePolicy(policy)

	if !pd.Allowed {
		return contracts.Artifact{
			TaskID: task.ID, Type: "Result",
			Payload: map[string]any{"status": "denied", "reasons": pd.Reasons},
			TS:      time.Now(),
		}, nil
	}

	// 2) Plan
	decisionClient := o.pickClient(ctx, "decision", o.DecisionAgentURL, task.ID, task.Type)
	planArt, err := decisionClient.RunTask(ctx, task)
	if err != nil {
		return contracts.Artifact{}, err
	}
	plan := decodePlan(planArt.Payload["plan"].(map[string]any))

	toolCalls := 0
	evidence := map[string]any{
		"policy": pd,
	}

	// 3) Execute steps with policy enforcement
	for _, step := range plan.Steps {
		toolCalls++
		if toolCalls > pd.Budgets.MaxToolCalls {
			return contracts.Artifact{}, fmt.Errorf("budget exceeded: tool calls")
		}
		if time.Since(start) > time.Duration(pd.Budgets.MaxSeconds)*time.Second {
			return contracts.Artifact{}, fmt.Errorf("budget exceeded: time")
		}

		// tag check
		if contains(step.Tags, "write") && !pd.AllowWrites {
			// stop and require approval
			return contracts.Artifact{
				TaskID: task.ID,
				Type:   "Result",
				Payload: map[string]any{
					"status":          "needs_approval",
					"blocked_step_id": step.StepID,
					"blocked_tool":    step.ToolName,
					"message":         "write step blocked by policy; re-run with metadata.approved=true",
				},
				TS: time.Now(),
			}, nil
		}
		if !tagsAllowed(pd.ToolTagAllow, step.Tags) {
			return contracts.Artifact{}, fmt.Errorf("policy blocks tags for tool %s", step.ToolName)
		}

		// scope check (simple)
		for _, rs := range step.Scopes {
			if !contains(pd.Scopes, rs) {
				return contracts.Artifact{}, fmt.Errorf("missing required scope %s", rs)
			}
		}

		out, err := o.Tools.CallTool(ctx, step.ToolName, step.ToolVersion, step.Args)
		if err != nil {
			if step.StopOnErr {
				return contracts.Artifact{}, err
			}
			evidence["step_error_"+step.StepID] = err.Error()
			continue
		}
		evidence["step_out_"+step.StepID] = out
	}

	return contracts.Artifact{
		TaskID: task.ID,
		Type:   "Result",
		Payload: map[string]any{
			"status":   "completed",
			"evidence": evidence,
		},
		TS: time.Now(),
	}, nil
}

func (o *Orchestrator) pickClient(ctx context.Context, agentType, fallbackURL, taskID, taskType string) *a2a.Client {
	if o != nil && o.AgentRouter != nil {
		res, err := o.AgentRouter.Route(ctx, a2a.RouteRequest{AgentType: agentType, TaskType: taskType, Tags: []string{agentType}, Goal: taskType, Seed: taskID})
		if err == nil {
			return a2a.NewClientForAgent(o.Config, res.Agent)
		}
	}
	return a2a.NewClient(fallbackURL)
}

// --- helpers (keep them simple; swap with strict decoding later)

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func tagsAllowed(allow []string, stepTags []string) bool {
	for _, t := range stepTags {
		// only enforce read/write here for brevity
		if t == "read" || t == "write" {
			if !contains(allow, t) {
				return false
			}
		}
	}
	return true
}

func decodePolicy(m map[string]any) contracts.PolicyDecision {
	// (Minimal decoding; replace with json marshal/unmarshal for strictness)
	p := contracts.PolicyDecision{Allowed: true}
	if v, ok := m["allowed"].(bool); ok {
		p.Allowed = v
	}
	if v, ok := m["allow_writes"].(bool); ok {
		p.AllowWrites = v
	}
	if v, ok := m["tool_tag_allow"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				p.ToolTagAllow = append(p.ToolTagAllow, s)
			}
		}
	}
	if v, ok := m["scopes"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				p.Scopes = append(p.Scopes, s)
			}
		}
	}
	if v, ok := m["budgets"].(map[string]any); ok {
		if mtc, ok := v["max_tool_calls"].(float64); ok {
			p.Budgets.MaxToolCalls = int(mtc)
		}
		if ms, ok := v["max_seconds"].(float64); ok {
			p.Budgets.MaxSeconds = int(ms)
		}
	}
	if v, ok := m["reasons"].(map[string]any); ok {
		p.Reasons = map[string]string{}
		for k, vv := range v {
			if s, ok := vv.(string); ok {
				p.Reasons[k] = s
			}
		}
	}
	return p
}

func decodePlan(m map[string]any) contracts.Plan {
	var p contracts.Plan
	if steps, ok := m["steps"].([]any); ok {
		for _, s := range steps {
			sm, _ := s.(map[string]any)
			var ps contracts.PlanStep
			ps.StepID, _ = sm["step_id"].(string)
			ps.Goal, _ = sm["goal"].(string)
			ps.ToolName, _ = sm["tool_name"].(string)
			ps.ToolVersion, _ = sm["tool_version"].(string)
			ps.StopOnErr, _ = sm["stop_on_err"].(bool)

			if tags, ok := sm["tags"].([]any); ok {
				for _, t := range tags {
					if ts, ok := t.(string); ok {
						ps.Tags = append(ps.Tags, ts)
					}
				}
			}
			if scopes, ok := sm["scopes"].([]any); ok {
				for _, t := range scopes {
					if ts, ok := t.(string); ok {
						ps.Scopes = append(ps.Scopes, ts)
					}
				}
			}
			if args, ok := sm["args"].(map[string]any); ok {
				ps.Args = args
			}
			p.Steps = append(p.Steps, ps)
		}
	}
	if c, ok := m["confidence"].(float64); ok {
		p.Confidence = c
	}
	return p
}
