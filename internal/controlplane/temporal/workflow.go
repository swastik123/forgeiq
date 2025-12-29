// package orchestrator

// import (
// 	"time"

// 	"forgeiq/internal/controlplane/contracts"

// 	"go.temporal.io/sdk/workflow"
// )

// func IncidentWorkflow(ctx workflow.Context, task contracts.Task) (contracts.Artifact, error) {
// 	ao := workflow.ActivityOptions{
// 		StartToCloseTimeout: 30 * time.Second,
// 		RetryPolicy: &workflow.RetryPolicy{
// 			InitialInterval: 1 * time.Second,
// 			MaximumInterval: 10 * time.Second,
// 			MaximumAttempts: 5,
// 		},
// 	}
// 	ctx = workflow.WithActivityOptions(ctx, ao)

// 	var pd contracts.PolicyDecision
// 	if err := workflow.ExecuteActivity(ctx, "EvalPolicy", task).Get(ctx, &pd); err != nil {
// 		return contracts.Artifact{}, err
// 	}
// 	if !pd.Allowed {
// 		return contracts.Artifact{TaskID: task.ID, Type: "Result",
// 			Payload: map[string]any{"status": "denied", "reasons": pd.Reasons},
// 			TS:      workflow.Now(ctx),
// 		}, nil
// 	}

// 	evidence := map[string]any{"policy": pd}

// 	var plan contracts.Plan
// 	if err := workflow.ExecuteActivity(ctx, "GetPlan", task, pd, evidence).Get(ctx, &plan); err != nil {
// 		return contracts.Artifact{}, err
// 	}

// 	for _, step := range plan.Steps {
// 		// write gate -> wait for approval signal
// 		if has(step.Tags, "write") && !pd.AllowWrites {
// 			var approval bool
// 			workflow.GetSignalChannel(ctx, "approval").Receive(ctx, &approval)
// 			if !approval {
// 				return contracts.Artifact{TaskID: task.ID, Type: "Result",
// 					Payload: map[string]any{"status": "rejected"},
// 					TS:      workflow.Now(ctx),
// 				}, nil
// 			}
// 			// once approved, allow writes (or re-eval policy via activity)
// 			pd.AllowWrites = true
// 		}

// 		var out map[string]any
// 		if err := workflow.ExecuteActivity(ctx, "CallTool", step).Get(ctx, &out); err != nil {
// 			if step.StopOnErr {
// 				return contracts.Artifact{}, err
// 			}
// 			evidence["step_error_"+step.StepID] = err.Error()
// 			continue
// 		}
// 		evidence["step_out_"+step.StepID] = out
// 	}

// 	return contracts.Artifact{
// 		TaskID: task.ID,
// 		Type:   "Result",
// 		Payload: map[string]any{
// 			"status":   "completed",
// 			"evidence": evidence,
// 		},
// 		TS: workflow.Now(ctx),
// 	}, nil
// }

// func has(xs []string, x string) bool {
// 	for _, v := range xs {
// 		if v == x {
// 			return true
// 		}
// 	}
// 	return false
// }

package temporal

import (
	"time"

	"forgeiq/internal/controlplane/contracts"

	"go.temporal.io/sdk/workflow"
)

const (
	TaskQueue = "CONTROL_PLANE_TASK_QUEUE"
)

// Signals
const (
	SignalApproval = "approval" // bool
	SignalFeedback = "feedback" // FeedbackEntry
	SignalRefine   = "refine"   // bool
	SignalContinue = "continue" // bool
	SignalStop     = "stop"     // bool
)

// Queries
const (
	QueryStatus = "status"
	QueryResult = "result"
)

type WorkflowStatus struct {
	State        string         `json:"state"` // running | needs_approval | completed | failed | denied
	TaskID       string         `json:"task_id"`
	BlockedStep  string         `json:"blocked_step,omitempty"`
	BlockedTool  string         `json:"blocked_tool,omitempty"`
	LastError    string         `json:"last_error,omitempty"`
	ToolCalls    int            `json:"tool_calls"`
	UpdatedAtRFC string         `json:"updated_at_rfc"`
	Evidence     map[string]any `json:"evidence,omitempty"`
}

func IncidentWorkflow(ctx workflow.Context, task contracts.Task) (contracts.Artifact, error) {
	now := func() string { return workflow.Now(ctx).Format(time.RFC3339) }

	status := WorkflowStatus{
		State:        "running",
		TaskID:       task.ID,
		UpdatedAtRFC: now(),
		Evidence:     map[string]any{},
	}

	// Expose status + result as queries
	_ = workflow.SetQueryHandler(ctx, QueryStatus, func() (WorkflowStatus, error) {
		return status, nil
	})

	var finalResult contracts.Artifact
	_ = workflow.SetQueryHandler(ctx, QueryResult, func() (contracts.Artifact, error) {
		return finalResult, nil
	})

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		// RetryPolicy will use default retry behavior
		// Custom retry policy can be set via workflow.WithActivityOptions if needed
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// 1) Policy via Rule Agent (A2A)
	var pd contracts.PolicyDecision
	if err := workflow.ExecuteActivity(ctx, "EvalPolicy", task).Get(ctx, &pd); err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		status.UpdatedAtRFC = now()
		_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
		return contracts.Artifact{}, err
	}
	status.Evidence["policy"] = pd
	status.UpdatedAtRFC = now()

	if !pd.Allowed {
		status.State = "denied"
		status.UpdatedAtRFC = now()

		finalResult = contracts.Artifact{
			TaskID: task.ID,
			Type:   "Result",
			Payload: map[string]any{
				"status":  "denied",
				"reasons": pd.Reasons,
			},
			TS: workflow.Now(ctx),
		}
		_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
		return finalResult, nil
	}

	// 2) Plan via Decision Agent (A2A)
	var plan contracts.Plan
	if err := workflow.ExecuteActivity(ctx, "GetPlan", task, pd, status.Evidence).Get(ctx, &plan); err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		status.UpdatedAtRFC = now()
		_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
		return contracts.Artifact{}, err
	}
	status.Evidence["plan"] = plan
	status.UpdatedAtRFC = now()

	toolCalls := 0

	// 3) Execute steps via MCP
	for _, step := range plan.Steps {
		toolCalls++
		status.ToolCalls = toolCalls
		status.UpdatedAtRFC = now()

		// Budgets
		if pd.Budgets.MaxToolCalls > 0 && toolCalls > pd.Budgets.MaxToolCalls {
			status.State = "failed"
			status.LastError = "budget exceeded: tool calls"
			status.UpdatedAtRFC = now()
			return contracts.Artifact{}, workflow.NewContinueAsNewError(ctx, IncidentWorkflow, task)
		}

		// Write gate: if step needs write but policy disallows -> wait approval signal
		if has(step.Tags, "write") && !pd.AllowWrites {
			status.State = "needs_approval"
			status.BlockedStep = step.StepID
			status.BlockedTool = step.ToolName
			status.UpdatedAtRFC = now()

			var approved bool
			workflow.GetSignalChannel(ctx, SignalApproval).Receive(ctx, &approved)
			if !approved {
				status.State = "denied"
				status.UpdatedAtRFC = now()
				finalResult = contracts.Artifact{
					TaskID: task.ID,
					Type:   "Result",
					Payload: map[string]any{
						"status": "rejected",
					},
					TS: workflow.Now(ctx),
				}
				_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
				return finalResult, nil
			}

			// After approval, allow writes for remainder (or you can re-evaluate policy again)
			pd.AllowWrites = true
			_ = workflow.ExecuteActivity(ctx, "RecordApproval", task.ID).Get(ctx, nil)
			status.Evidence["approval"] = map[string]any{"approved": true, "ts": now()}
			status.State = "running"
			status.BlockedStep = ""
			status.BlockedTool = ""
			status.UpdatedAtRFC = now()
		}

		var out map[string]any
		err := workflow.ExecuteActivity(ctx, "CallTool", step, pd).Get(ctx, &out)
		if err != nil {
			if step.StopOnErr {
				status.State = "failed"
				status.LastError = err.Error()
				status.UpdatedAtRFC = now()
				_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
				return contracts.Artifact{}, err
			}
			status.Evidence["step_error_"+step.StepID] = err.Error()
			status.UpdatedAtRFC = now()
			continue
		}
		status.Evidence["step_out_"+step.StepID] = out
		status.UpdatedAtRFC = now()
	}

	status.State = "completed"
	status.UpdatedAtRFC = now()

	finalResult = contracts.Artifact{
		TaskID: task.ID,
		Type:   "Result",
		Payload: map[string]any{
			"status":   "completed",
			"evidence": status.Evidence,
		},
		TS: workflow.Now(ctx),
	}
	_ = workflow.ExecuteActivity(ctx, "CompleteEval", task.ID, status.State, status.LastError).Get(ctx, nil)
	return finalResult, nil
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
