package controlplanetemporal_test

import (
	"context"
	"testing"

	"forgeiq/internal/controlplane/contracts"

	"forgeiq/internal/controlplane/temporal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

type dummyActivities struct{}

func (d *dummyActivities) EvalPolicy(ctx context.Context, task contracts.Task) (contracts.PolicyDecision, error) {
	return contracts.PolicyDecision{}, nil
}
func (d *dummyActivities) GetPlan(ctx context.Context, task contracts.Task, pd contracts.PolicyDecision, evidence map[string]any) (contracts.Plan, error) {
	return contracts.Plan{}, nil
}
func (d *dummyActivities) CallTool(ctx context.Context, step contracts.PlanStep, pd contracts.PolicyDecision) (map[string]any, error) {
	return map[string]any{}, nil
}
func (d *dummyActivities) RecordApproval(ctx context.Context, taskID string) error { return nil }
func (d *dummyActivities) CompleteEval(ctx context.Context, taskID string, status string, lastErr string) error {
	return nil
}

func registerNamedActivities(env *testsuite.TestWorkflowEnvironment) {
	// Register an activity struct so method names map 1:1 to activity names used in workflow ("EvalPolicy", etc.)
	env.RegisterActivity(&dummyActivities{})
}

func TestIncidentWorkflow_Denied(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(temporal.IncidentWorkflow)
	registerNamedActivities(env)

	task := contracts.Task{ID: "t1", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}}

	env.OnActivity("EvalPolicy", mock.Anything, task).
		Return(contracts.PolicyDecision{Allowed: false, Reasons: map[string]string{"x": "y"}}, nil)
	env.OnActivity("CompleteEval", mock.Anything, task.ID, "denied", mock.Anything).
		Return(nil)

	env.ExecuteWorkflow(temporal.IncidentWorkflow, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out contracts.Artifact
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, "t1", out.TaskID)
	require.Equal(t, "Result", out.Type)
	require.Equal(t, "denied", out.Payload["status"])
}

func TestIncidentWorkflow_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(temporal.IncidentWorkflow)
	registerNamedActivities(env)

	task := contracts.Task{ID: "t2", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}}
	pd := contracts.PolicyDecision{
		Allowed:      true,
		AllowWrites:  true,
		ToolTagAllow: []string{"read", "write"},
		Scopes:       []string{"logs:read"},
		Budgets:      contracts.Budget{MaxToolCalls: 10, MaxSeconds: 60},
	}
	plan := contracts.Plan{
		Confidence: 0.9,
		Steps: []contracts.PlanStep{
			{StepID: "s1", ToolName: "logs.search", ToolVersion: "v1", Tags: []string{"read"}, Scopes: []string{"logs:read"}, Args: map[string]any{"query": "x"}, StopOnErr: true},
		},
	}

	env.OnActivity("EvalPolicy", mock.Anything, task).Return(pd, nil)
	env.OnActivity("GetPlan", mock.Anything, task, pd, mock.Anything).Return(plan, nil)
	env.OnActivity("CallTool", mock.Anything, plan.Steps[0], pd).Return(map[string]any{"ok": true}, nil)
	env.OnActivity("CompleteEval", mock.Anything, task.ID, "completed", "").Return(nil)

	env.ExecuteWorkflow(temporal.IncidentWorkflow, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out contracts.Artifact
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, "completed", out.Payload["status"])
}

func TestIncidentWorkflow_WriteGate_Approval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(temporal.IncidentWorkflow)
	registerNamedActivities(env)

	task := contracts.Task{ID: "t3", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}}
	pd := contracts.PolicyDecision{
		Allowed:      true,
		AllowWrites:  false,
		ToolTagAllow: []string{"read", "write"},
		Scopes:       []string{"incidents:write"},
		Budgets:      contracts.Budget{MaxToolCalls: 10, MaxSeconds: 60},
	}
	plan := contracts.Plan{
		Confidence: 0.9,
		Steps: []contracts.PlanStep{
			{StepID: "s1", ToolName: "incidents.create_ticket", ToolVersion: "v1", Tags: []string{"write"}, Scopes: []string{"incidents:write"}, Args: map[string]any{"title": "x"}, StopOnErr: true},
		},
	}

	env.OnActivity("EvalPolicy", mock.Anything, task).Return(pd, nil)
	env.OnActivity("GetPlan", mock.Anything, task, pd, mock.Anything).Return(plan, nil)
	env.OnActivity("RecordApproval", mock.Anything, task.ID).Return(nil)
	// After approval workflow mutates pd.AllowWrites=true before CallTool, so accept any policy arg
	env.OnActivity("CallTool", mock.Anything, plan.Steps[0], mock.Anything).Return(map[string]any{"ticket_id": "INC-1"}, nil)
	env.OnActivity("CompleteEval", mock.Anything, task.ID, "completed", "").Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(temporal.SignalApproval, true)
	}, 0)

	env.ExecuteWorkflow(temporal.IncidentWorkflow, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
