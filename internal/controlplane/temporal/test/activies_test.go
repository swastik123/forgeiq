package controlplanetemporal_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/catalog"
	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	mcpserver "forgeiq/internal/dataplane/mcp"

	"forgeiq/internal/controlplane/temporal"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestActivities_EvalPolicy_GetPlan_CallTool(t *testing.T) {
	// Rule agent A2A server
	ruleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var task contracts.Task
		_ = json.NewDecoder(r.Body).Decode(&task)
		pd := contracts.PolicyDecision{
			Allowed:      true,
			AllowWrites:  true,
			ToolTagAllow: []string{"read", "write"},
			Scopes:       []string{"logs:read"},
			Budgets:      contracts.Budget{MaxToolCalls: 10, MaxSeconds: 60},
		}
		_ = json.NewEncoder(w).Encode(contracts.Artifact{TaskID: task.ID, Type: "PolicyDecision", Payload: map[string]any{"policy": pd}})
	}))
	defer ruleSrv.Close()

	// Decision agent A2A server
	decisionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var task contracts.Task
		_ = json.NewDecoder(r.Body).Decode(&task)
		plan := contracts.Plan{
			Confidence: 0.8,
			Steps: []contracts.PlanStep{
				{StepID: "s1", ToolName: "logs.search", ToolVersion: "v1", Tags: []string{"read"}, Scopes: []string{"logs:read"}, Args: map[string]any{"query": "x"}, StopOnErr: true},
			},
		}
		_ = json.NewEncoder(w).Encode(contracts.Artifact{TaskID: task.ID, Type: "Plan", Payload: map[string]any{"plan": plan}})
	}))
	defer decisionSrv.Close()

	// MCP server
	mcp := mcpserver.New()
	mcp.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "logs.search",
		Version: "v1",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
	}, func(args map[string]any) (map[string]any, error) {
		return map[string]any{"echo": args["query"]}, nil
	})
	mcpSrv := httptest.NewServer(mcp.Handler())
	defer mcpSrv.Close()

	cfg := &config.Config{
		RuleAgentURL:     ruleSrv.URL,
		DecisionAgentURL: decisionSrv.URL,
		MCPBaseURL:       mcpSrv.URL,
		AgentRouter:      config.AgentRouterConfig{Enabled: false},
	}

	cat := catalog.New()
	cat.Upsert(catalog.ToolDescriptor{Info: interfaces.ToolInfo{
		Name:    "logs.search",
		Version: "v1",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required":   []any{"query"},
		},
	}})

	acts := &temporal.Activities{Config: cfg, Catalog: cat}

	var ts testsuite.WorkflowTestSuite
	actEnv := ts.NewTestActivityEnvironment()
	actEnv.RegisterActivity(acts)

	task := contracts.Task{ID: "t1", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}, Metadata: map[string]string{}}

	// EvalPolicy
	res1, err := actEnv.ExecuteActivity(acts.EvalPolicy, task)
	require.NoError(t, err)
	var pd contracts.PolicyDecision
	require.NoError(t, res1.Get(&pd))
	require.True(t, pd.Allowed)

	// GetPlan (catalog validates)
	res2, err := actEnv.ExecuteActivity(acts.GetPlan, task, pd, map[string]any{})
	require.NoError(t, err)
	var plan contracts.Plan
	require.NoError(t, res2.Get(&plan))
	require.Len(t, plan.Steps, 1)

	// CallTool
	res3, err := actEnv.ExecuteActivity(acts.CallTool, plan.Steps[0], pd)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, res3.Get(&out))
	require.Equal(t, "x", out["echo"])
}
