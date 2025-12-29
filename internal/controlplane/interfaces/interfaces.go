package interfaces

import (
	"context"

	"forgeiq/internal/controlplane/contracts"
)

type PolicyEvaluator interface {
	Evaluate(ctx context.Context, task contracts.Task) (contracts.PolicyDecision, error)
}

type Planner interface {
	Plan(ctx context.Context, task contracts.Task, policy contracts.PolicyDecision, evidence map[string]any) (contracts.Plan, error)
	Replan(ctx context.Context, task contracts.Task, policy contracts.PolicyDecision, evidence map[string]any, lastErr error) (contracts.Plan, error)
}

type ToolClient interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, version string, args map[string]any) (map[string]any, error)
}

type ToolInfo struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Tags    []string          `json:"tags"`
	Scopes  []string          `json:"scopes"`
	Schema  map[string]any    `json:"schema"` // JSON-schema-like
	Meta    map[string]string `json:"meta"`
}
