package temporal

import (
	"fmt"
	"time"

	"forgeiq/internal/controlplane/contracts"

	"go.temporal.io/sdk/workflow"
)

// AgenticLoopConfig configures the agentic loop behavior
type AgenticLoopConfig struct {
	MaxIterations    int           // Maximum number of iterations
	IterationTimeout time.Duration // Timeout per iteration
	EnableFeedback   bool          // Enable human feedback
	AutoRefine       bool          // Automatically refine plans based on results
}

// AgenticLoopState tracks the state of the agentic loop
type AgenticLoopState struct {
	Iteration      int               `json:"iteration"`
	CurrentPlan    contracts.Plan    `json:"current_plan"`
	Evidence       map[string]any    `json:"evidence"`
	Feedback       []FeedbackEntry   `json:"feedback"`
	Refinements    []RefinementEntry `json:"refinements"`
	LastError      string            `json:"last_error,omitempty"`
	NeedsApproval  bool              `json:"needs_approval"`
	ApprovalReason string            `json:"approval_reason,omitempty"`
}

// FeedbackEntry represents user feedback
type FeedbackEntry struct {
	Iteration int       `json:"iteration"`
	StepID    string    `json:"step_id,omitempty"`
	Feedback  string    `json:"feedback"`
	Action    string    `json:"action"` // "approve", "reject", "modify", "continue"
	Timestamp time.Time `json:"timestamp"`
}

// RefinementEntry tracks plan refinements
type RefinementEntry struct {
	Iteration    int            `json:"iteration"`
	Reason       string         `json:"reason"`
	OriginalPlan contracts.Plan `json:"original_plan"`
	RefinedPlan  contracts.Plan `json:"refined_plan"`
	Timestamp    time.Time      `json:"timestamp"`
}

// Signals for agentic loop are defined in workflow.go

// AgenticLoop implements an iterative agentic loop pattern
// This allows the workflow to:
// 1. Execute a plan
// 2. Collect results/evidence
// 3. Optionally refine the plan based on results
// 4. Get user feedback if needed
// 5. Continue iterating until goal is met or max iterations reached
func AgenticLoop(ctx workflow.Context, task contracts.Task, config AgenticLoopConfig, policy contracts.PolicyDecision, status *WorkflowStatus) (contracts.Artifact, error) {
	nowRFC := func() string { return workflow.Now(ctx).Format(time.RFC3339) }
	state := AgenticLoopState{
		Iteration:   0,
		Evidence:    make(map[string]any),
		Feedback:    make([]FeedbackEntry, 0),
		Refinements: make([]RefinementEntry, 0),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Setup signal channels for user interaction
	feedbackChan := workflow.GetSignalChannel(ctx, SignalFeedback)
	refineChan := workflow.GetSignalChannel(ctx, SignalRefine)
	stopChan := workflow.GetSignalChannel(ctx, SignalStop)

	// Initial plan
	var plan contracts.Plan
	if err := workflow.ExecuteActivity(ctx, "GetPlan", task, policy, state.Evidence).Get(ctx, &plan); err != nil {
		return contracts.Artifact{}, err
	}
	state.CurrentPlan = plan
	state.Evidence["initial_plan"] = plan
	if status != nil {
		status.Evidence["initial_plan"] = plan
		status.UpdatedAtRFC = nowRFC()
	}

	for state.Iteration < config.MaxIterations {
		state.Iteration++

		// Check for stop signal (non-blocking)
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(stopChan, func(c workflow.ReceiveChannel, more bool) {
			var stop bool
			c.Receive(ctx, &stop)
			if stop {
				state.Evidence["stopped_by_user"] = true
			}
		})
		selector.AddDefault(func() {})

		// Wait briefly for signals (non-blocking check)
		workflow.AwaitWithTimeout(ctx, 100*time.Millisecond, func() bool {
			selector.Select(ctx)
			return false
		})

		// Execute current plan iteration
		iterationResult, err := executePlanIteration(ctx, state.CurrentPlan, policy, state.Evidence)
		if err != nil {
			state.LastError = err.Error()
			state.Evidence["iteration_error"] = err.Error()

			// If auto-refine is enabled, try to refine the plan
			if config.AutoRefine && state.Iteration < config.MaxIterations {
				refinedPlan, refineErr := refinePlan(ctx, task, policy, state.Evidence, err)
				if refineErr == nil && len(refinedPlan.Steps) > 0 {
					state.Refinements = append(state.Refinements, RefinementEntry{
						Iteration:    state.Iteration,
						Reason:       "Error recovery: " + err.Error(),
						OriginalPlan: state.CurrentPlan,
						RefinedPlan:  refinedPlan,
						Timestamp:    workflow.Now(ctx),
					})
					state.CurrentPlan = refinedPlan
					continue // Retry with refined plan
				}
			}

			// If we can't recover, check for user feedback
			if config.EnableFeedback {
				state.NeedsApproval = true
				state.ApprovalReason = "Iteration failed: " + err.Error()

				// Update workflow-visible status so clients can see we're waiting.
				if status != nil {
					status.State = "waiting_for_feedback"
					status.NeedsHumanInput = true
					status.WaitingOnSignal = SignalFeedback
					status.WaitingSinceRFC = nowRFC()
					status.Prompt = state.ApprovalReason
					status.UpdatedAtRFC = nowRFC()
				}
				// Also store in evidence for debugging.
				state.Evidence["waiting"] = map[string]any{
					"needs_human_input": true,
					"waiting_on_signal": SignalFeedback,
					"prompt":            state.ApprovalReason,
					"waiting_since_rfc": nowRFC(),
				}

				// Wait for user feedback
				var feedback FeedbackEntry
				feedbackChan.Receive(ctx, &feedback)
				state.Feedback = append(state.Feedback, feedback)
				if status != nil {
					status.State = "running"
					status.NeedsHumanInput = false
					status.WaitingOnSignal = ""
					status.WaitingSinceRFC = ""
					status.Prompt = ""
					status.UpdatedAtRFC = nowRFC()
				}

				if feedback.Action == "stop" {
					break
				} else if feedback.Action == "continue" {
					continue
				} else if feedback.Action == "modify" {
					// User wants to modify the plan
					// In a real implementation, you'd get the modified plan from the signal
					continue
				}
			}

			// If no recovery possible, return error
			if state.Iteration >= config.MaxIterations {
				return contracts.Artifact{
					TaskID: task.ID,
					Type:   "Result",
					Payload: map[string]any{
						"status":     "failed",
						"iterations": state.Iteration,
						"error":      err.Error(),
						"evidence":   state.Evidence,
					},
					TS: workflow.Now(ctx),
				}, nil
			}
		}

		// Merge iteration results into evidence
		for k, v := range iterationResult {
			state.Evidence[fmt.Sprintf("iteration_%d_%s", state.Iteration, k)] = v
		}

		// Check if goal is achieved
		if isGoalAchieved(state.Evidence, task) {
			return contracts.Artifact{
				TaskID: task.ID,
				Type:   "Result",
				Payload: map[string]any{
					"status":      "completed",
					"iterations":  state.Iteration,
					"evidence":    state.Evidence,
					"refinements": len(state.Refinements),
					"feedback":    len(state.Feedback),
				},
				TS: workflow.Now(ctx),
			}, nil
		}

		// Check for refinement request
		selector = workflow.NewSelector(ctx)
		selector.AddReceive(refineChan, func(c workflow.ReceiveChannel, more bool) {
			var refine bool
			c.Receive(ctx, &refine)
			if refine {
				refinedPlan, refineErr := refinePlan(ctx, task, policy, state.Evidence, nil)
				if refineErr == nil {
					state.Refinements = append(state.Refinements, RefinementEntry{
						Iteration:    state.Iteration,
						Reason:       "User requested refinement",
						OriginalPlan: state.CurrentPlan,
						RefinedPlan:  refinedPlan,
						Timestamp:    workflow.Now(ctx),
					})
					state.CurrentPlan = refinedPlan
				}
			}
		})
		selector.AddDefault(func() {})

		workflow.AwaitWithTimeout(ctx, 100*time.Millisecond, func() bool {
			selector.Select(ctx)
			return false
		})

		// If auto-refine is enabled and we have new evidence, consider refining
		if config.AutoRefine && state.Iteration < config.MaxIterations {
			shouldRefine := shouldRefinePlan(state.Evidence, state.CurrentPlan)
			if shouldRefine {
				refinedPlan, refineErr := refinePlan(ctx, task, policy, state.Evidence, nil)
				if refineErr == nil && len(refinedPlan.Steps) > 0 {
					state.Refinements = append(state.Refinements, RefinementEntry{
						Iteration:    state.Iteration,
						Reason:       "Auto-refinement based on evidence",
						OriginalPlan: state.CurrentPlan,
						RefinedPlan:  refinedPlan,
						Timestamp:    workflow.Now(ctx),
					})
					state.CurrentPlan = refinedPlan
				}
			}
		}

		// Small delay between iterations
		workflow.Sleep(ctx, 1*time.Second)
	}

	// Max iterations reached
	return contracts.Artifact{
		TaskID: task.ID,
		Type:   "Result",
		Payload: map[string]any{
			"status":      "max_iterations_reached",
			"iterations":  state.Iteration,
			"evidence":    state.Evidence,
			"refinements": len(state.Refinements),
			"feedback":    len(state.Feedback),
		},
		TS: workflow.Now(ctx),
	}, nil
}

// executePlanIteration executes one iteration of the plan
func executePlanIteration(ctx workflow.Context, plan contracts.Plan, policy contracts.PolicyDecision, evidence map[string]any) (map[string]any, error) {
	results := make(map[string]any)

	for _, step := range plan.Steps {
		// Check for write operations requiring approval
		if has(step.Tags, "write") && !policy.AllowWrites {
			return nil, fmt.Errorf("write operation requires approval")
		}

		var stepResult map[string]any
		err := workflow.ExecuteActivity(ctx, "CallTool", step, policy).Get(ctx, &stepResult)
		if err != nil {
			if step.StopOnErr {
				return nil, err
			}
			results["step_error_"+step.StepID] = err.Error()
			continue
		}

		results["step_result_"+step.StepID] = stepResult
	}

	return results, nil
}

// refinePlan attempts to refine the plan based on evidence
func refinePlan(ctx workflow.Context, task contracts.Task, policy contracts.PolicyDecision, evidence map[string]any, lastErr error) (contracts.Plan, error) {
	// Call decision agent with updated evidence to get refined plan
	var refinedPlan contracts.Plan
	err := workflow.ExecuteActivity(ctx, "GetPlan", task, policy, evidence).Get(ctx, &refinedPlan)
	return refinedPlan, err
}

// shouldRefinePlan determines if the plan should be refined based on evidence
func shouldRefinePlan(evidence map[string]any, currentPlan contracts.Plan) bool {
	// Simple heuristic: refine if we have errors or unexpected results
	for k := range evidence {
		if containsString(k, "error") || containsString(k, "unexpected") {
			return true
		}
	}
	return false
}

// isGoalAchieved checks if the workflow goal has been achieved
func isGoalAchieved(evidence map[string]any, task contracts.Task) bool {
	// Check for completion indicators in evidence
	if status, ok := evidence["status"].(string); ok && status == "completed" {
		return true
	}
	if completed, ok := evidence["goal_achieved"].(bool); ok && completed {
		return true
	}
	return false
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
