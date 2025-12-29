package temporal

import (
	"fmt"
	"time"

	"forgeiq/internal/controlplane/contracts"

	"go.temporal.io/sdk/workflow"
)

// IncidentWorkflowWithAgenticLoop is an enhanced workflow with agentic loop capabilities
func IncidentWorkflowWithAgenticLoop(ctx workflow.Context, task contracts.Task) (contracts.Artifact, error) {
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
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// 1) Policy evaluation
	var pd contracts.PolicyDecision
	if err := workflow.ExecuteActivity(ctx, "EvalPolicy", task).Get(ctx, &pd); err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		status.UpdatedAtRFC = now()
		return contracts.Artifact{}, err
	}
	status.Evidence["policy"] = pd
	status.UpdatedAtRFC = now()

	if !pd.Allowed {
		status.State = "denied"
		finalResult = contracts.Artifact{
			TaskID: task.ID,
			Type:   "Result",
			Payload: map[string]any{
				"status":  "denied",
				"reasons": pd.Reasons,
			},
			TS: workflow.Now(ctx),
		}
		return finalResult, nil
	}

	// 2) Agentic Loop Configuration
	loopConfig := AgenticLoopConfig{
		MaxIterations:    5,               // Max 5 iterations
		IterationTimeout: 5 * time.Minute, // 5 min per iteration
		EnableFeedback:   true,            // Enable human feedback
		AutoRefine:       true,            // Auto-refine plans
	}

	// 3) Execute agentic loop
	loopResult, err := AgenticLoop(ctx, task, loopConfig, pd)
	if err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		status.UpdatedAtRFC = now()
		return contracts.Artifact{}, err
	}

	status.State = "completed"
	status.UpdatedAtRFC = now()
	finalResult = loopResult

	return finalResult, nil
}

// Enhanced workflow with iterative refinement
func IncidentWorkflowIterative(ctx workflow.Context, task contracts.Task) (contracts.Artifact, error) {
	now := func() string { return workflow.Now(ctx).Format(time.RFC3339) }

	status := WorkflowStatus{
		State:        "running",
		TaskID:       task.ID,
		UpdatedAtRFC: now(),
		Evidence:     map[string]any{},
	}

	_ = workflow.SetQueryHandler(ctx, QueryStatus, func() (WorkflowStatus, error) {
		return status, nil
	})

	var finalResult contracts.Artifact
	_ = workflow.SetQueryHandler(ctx, QueryResult, func() (contracts.Artifact, error) {
		return finalResult, nil
	})

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Policy evaluation
	var pd contracts.PolicyDecision
	if err := workflow.ExecuteActivity(ctx, "EvalPolicy", task).Get(ctx, &pd); err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		return contracts.Artifact{}, err
	}

	if !pd.Allowed {
		status.State = "denied"
		return contracts.Artifact{
			TaskID:  task.ID,
			Type:    "Result",
			Payload: map[string]any{"status": "denied", "reasons": pd.Reasons},
			TS:      workflow.Now(ctx),
		}, nil
	}

	// Iterative planning and execution
	maxIterations := 3
	evidence := map[string]any{"policy": pd}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		status.Evidence[fmt.Sprintf("iteration_%d", iteration)] = iteration
		status.UpdatedAtRFC = now()

		// Get plan (may be refined based on previous evidence)
		var plan contracts.Plan
		if err := workflow.ExecuteActivity(ctx, "GetPlan", task, pd, evidence).Get(ctx, &plan); err != nil {
			status.State = "failed"
			status.LastError = err.Error()
			return contracts.Artifact{}, err
		}

		status.Evidence[fmt.Sprintf("plan_%d", iteration)] = plan

		// Execute plan steps
		allStepsSucceeded := true
		for _, step := range plan.Steps {
			// Write gate
			if has(step.Tags, "write") && !pd.AllowWrites {
				status.State = "needs_approval"
				status.BlockedStep = step.StepID
				status.BlockedTool = step.ToolName

				var approved bool
				workflow.GetSignalChannel(ctx, SignalApproval).Receive(ctx, &approved)
				if !approved {
					status.State = "denied"
					return contracts.Artifact{
						TaskID:  task.ID,
						Type:    "Result",
						Payload: map[string]any{"status": "rejected"},
						TS:      workflow.Now(ctx),
					}, nil
				}
				pd.AllowWrites = true
			}

			var out map[string]any
			err := workflow.ExecuteActivity(ctx, "CallTool", step, pd).Get(ctx, &out)
			if err != nil {
				if step.StopOnErr {
					status.State = "failed"
					status.LastError = err.Error()
					return contracts.Artifact{}, err
				}
				evidence[fmt.Sprintf("step_error_%s_iter_%d", step.StepID, iteration)] = err.Error()
				allStepsSucceeded = false
				continue
			}

			evidence[fmt.Sprintf("step_result_%s_iter_%d", step.StepID, iteration)] = out
		}

		// If all steps succeeded, we're done
		if allStepsSucceeded {
			status.State = "completed"
			finalResult = contracts.Artifact{
				TaskID: task.ID,
				Type:   "Result",
				Payload: map[string]any{
					"status":     "completed",
					"iterations": iteration,
					"evidence":   evidence,
				},
				TS: workflow.Now(ctx),
			}
			return finalResult, nil
		}

		// Check if we should continue or wait for refinement signal
		if iteration < maxIterations {
			// Wait for refinement signal or continue automatically after delay
			selector := workflow.NewSelector(ctx)
			refineChan := workflow.GetSignalChannel(ctx, SignalRefine)
			selector.AddReceive(refineChan, func(c workflow.ReceiveChannel, more bool) {
				// User requested refinement, continue to next iteration
			})
			selector.AddDefault(func() {
				// Auto-continue after short delay
			})

			// Wait up to 10 seconds for refinement signal, otherwise auto-continue
			workflow.AwaitWithTimeout(ctx, 10*time.Second, func() bool {
				selector.Select(ctx)
				return false
			})
		}
	}

	// Max iterations reached
	status.State = "completed"
	finalResult = contracts.Artifact{
		TaskID: task.ID,
		Type:   "Result",
		Payload: map[string]any{
			"status":     "completed_with_retries",
			"iterations": maxIterations,
			"evidence":   evidence,
		},
		TS: workflow.Now(ctx),
	}
	return finalResult, nil
}
