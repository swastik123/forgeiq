package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"forgeiq/internal/catalog"
	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/eval"
	"forgeiq/internal/transport/a2a"
	mcpclient "forgeiq/internal/transport/mcp"

	"go.temporal.io/sdk/activity"
)

// Activities contains the Temporal activities for the workflow
type Activities struct {
	Config  *config.Config
	Router  a2a.Router
	Catalog *catalog.Catalog
	Eval    *eval.Store
}

// EvalPolicy calls the Rule Agent to evaluate policy for a task
func (a *Activities) EvalPolicy(ctx context.Context, task contracts.Task) (contracts.PolicyDecision, error) {
	activity.GetLogger(ctx).Info("Evaluating policy", "task_id", task.ID)

	a.evalStart(ctx, task.ID)
	client, agentMeta, err := a.pickA2AClient(ctx, "rule", contracts.Task{ID: task.ID, Type: task.Type})
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	a.evalAgent(ctx, task.ID, "rule", agentMeta)
	artifact, err := client.RunTask(ctx, task)
	if err != nil {
		return contracts.PolicyDecision{}, fmt.Errorf("failed to call rule agent: %w", err)
	}

	// Extract policy decision from artifact payload
	policyRaw, ok := artifact.Payload["policy"]
	if !ok {
		return contracts.PolicyDecision{}, fmt.Errorf("policy not found in artifact payload")
	}

	// Convert to PolicyDecision
	policyBytes, err := json.Marshal(policyRaw)
	if err != nil {
		return contracts.PolicyDecision{}, fmt.Errorf("failed to marshal policy: %w", err)
	}

	var pd contracts.PolicyDecision
	if err := json.Unmarshal(policyBytes, &pd); err != nil {
		return contracts.PolicyDecision{}, fmt.Errorf("failed to unmarshal policy: %w", err)
	}

	return pd, nil
}

// GetPlan calls the Decision Agent to create an execution plan
func (a *Activities) GetPlan(ctx context.Context, task contracts.Task, policy contracts.PolicyDecision, evidence map[string]any) (contracts.Plan, error) {
	activity.GetLogger(ctx).Info("Getting plan", "task_id", task.ID)

	client, agentMeta, err := a.pickA2AClient(ctx, "decision", contracts.Task{ID: task.ID, Type: task.Type})
	if err != nil {
		return contracts.Plan{}, err
	}
	a.evalAgent(ctx, task.ID, "decision", agentMeta)
	artifact, err := client.RunTask(ctx, task)
	if err != nil {
		return contracts.Plan{}, fmt.Errorf("failed to call decision agent: %w", err)
	}

	// Extract plan from artifact payload
	planRaw, ok := artifact.Payload["plan"]
	if !ok {
		return contracts.Plan{}, fmt.Errorf("plan not found in artifact payload")
	}

	// Convert to Plan
	planBytes, err := json.Marshal(planRaw)
	if err != nil {
		return contracts.Plan{}, fmt.Errorf("failed to marshal plan: %w", err)
	}

	var plan contracts.Plan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return contracts.Plan{}, fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	// Tool Catalog validation: prevent runtime breakage early.
	if a.Catalog != nil {
		if err := a.Catalog.ValidatePlan(plan); err != nil {
			return contracts.Plan{}, fmt.Errorf("plan failed tool compatibility validation: %w", err)
		}
	}
	if a.Eval != nil {
		_ = a.Eval.RecordPlan(ctx, task.ID, len(plan.Steps), plan.Confidence, map[string]any{
			"validated_by_catalog": a.Catalog != nil,
			"task_type":            task.Type,
		})
	}

	return plan, nil
}

func (a *Activities) pickA2AClient(ctx context.Context, agentType string, task contracts.Task) (*a2a.Client, *a2a.Agent, error) {
	// Use Agent Router if enabled/configured; fallback to legacy single URL config.
	if a != nil && a.Config != nil && a.Config.AgentRouter.Enabled {
		// Allow callers to pass routing hints for planner selection via task metadata.
		// Example: task.Metadata["planner_tags"] = "sre,incident,payments"
		// Optional per-agent-type override: task.Metadata["planner_tags_decision"], task.Metadata["planner_tags_rule"]
		tags := []string{agentType}
		if strings.TrimSpace(task.TenantID) != "" {
			// Add a stable tenant tag so routers/registries can isolate by tenant if desired.
			tags = append(tags, "tenant:"+strings.TrimSpace(task.TenantID))
		}
		if task.Metadata != nil {
			if v := strings.TrimSpace(task.Metadata["planner_tags_"+agentType]); v != "" {
				tags = append(tags, parseCSVTags(v)...)
			} else if v := strings.TrimSpace(task.Metadata["planner_tags"]); v != "" {
				tags = append(tags, parseCSVTags(v)...)
			}
		}
		tags = uniqueStrings(tags)
		sort.Strings(tags)

		r := a.Router
		if r == nil {
			rr, err := a2a.NewAgentRouterFromConfig(a.Config)
			if err != nil {
				return nil, nil, fmt.Errorf("agent router config error: %w", err)
			}
			r, err = a2a.NewRouterFromConfig(a.Config, rr.Agents)
			if err != nil {
				return nil, nil, fmt.Errorf("agent router build error: %w", err)
			}
			// Best-effort discovery (optional)
			if a.Config.AgentRouter.DiscoveryEnabled {
				// discovery only supported by AgentRouter (best-effort)
				rr.Discover(ctx, a.Config.AgentRouter.DiscoveryPath)
			}
		}
		res, err := r.Route(ctx, a2a.RouteRequest{
			AgentType: agentType,
			TaskType:  task.Type,
			Tags:      tags,
			Goal:      task.Type,
			Seed:      task.ID,
		})
		if err != nil {
			return nil, nil, err
		}
		return a2a.NewClientForAgent(a.Config, res.Agent), &res.Agent, nil
	}

	client := a2a.NewClientFromConfig(a.Config, agentType)
	if client == nil {
		return nil, nil, fmt.Errorf("failed to create %s agent client", agentType)
	}
	return client, nil, nil
}

func (a *Activities) evalStart(ctx context.Context, taskID string) {
	if a == nil || a.Eval == nil {
		return
	}
	info := activity.GetInfo(ctx)
	_ = a.Eval.UpsertStart(ctx, taskID, info.WorkflowExecution.ID, info.WorkflowExecution.RunID, info.WorkflowType.Name)
}

func (a *Activities) evalAgent(ctx context.Context, taskID, agentType string, agent *a2a.Agent) {
	if a == nil || a.Eval == nil || agent == nil {
		return
	}
	_ = a.Eval.RecordAgent(ctx, taskID, agentType, agent.ID, agent.Version)
}

// CallTool calls the MCP server to execute a tool
func (a *Activities) CallTool(ctx context.Context, step contracts.PlanStep, policy contracts.PolicyDecision) (map[string]any, error) {
	activity.GetLogger(ctx).Info("Calling tool", "tool", step.ToolName, "step_id", step.StepID)

	// Tool Catalog validation (prevents runtime breakage on missing tool/schema mismatch)
	if a.Catalog != nil {
		if err := a.Catalog.ValidateToolCall(step.ToolName, step.ToolVersion, step.Args); err != nil {
			return nil, err
		}
	}

	// Validate policy allows this tool call
	if contains(step.Tags, "write") && !policy.AllowWrites {
		return nil, fmt.Errorf("write operation blocked by policy")
	}

	// Check tag permissions
	for _, tag := range step.Tags {
		if tag == "read" || tag == "write" {
			if !contains(policy.ToolTagAllow, tag) {
				return nil, fmt.Errorf("tag '%s' not allowed by policy", tag)
			}
		}
	}

	// Check scopes
	for _, requiredScope := range step.Scopes {
		if !contains(policy.Scopes, requiredScope) {
			return nil, fmt.Errorf("missing required scope: %s", requiredScope)
		}
	}

	// Call MCP server
	client := mcpclient.NewClientFromConfig(a.Config)
	if a.Eval != nil {
		_ = a.Eval.IncToolCalls(ctx, activity.GetInfo(ctx).WorkflowExecution.ID)
	}
	result, err := client.CallTool(ctx, step.ToolName, step.ToolVersion, step.Args)
	if err != nil {
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	return result, nil
}

// RecordApproval increments approval counters for eval loop.
func (a *Activities) RecordApproval(ctx context.Context, taskID string) error {
	if a == nil || a.Eval == nil {
		return nil
	}
	return a.Eval.IncApprovals(ctx, taskID)
}

// CompleteEval marks a run completed/failed/denied for eval loop.
func (a *Activities) CompleteEval(ctx context.Context, taskID string, status string, lastErr string) error {
	if a == nil || a.Eval == nil {
		return nil
	}
	return a.Eval.Complete(ctx, taskID, status, lastErr)
}

// Helper function to check if a string slice contains a value
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func parseCSVTags(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
