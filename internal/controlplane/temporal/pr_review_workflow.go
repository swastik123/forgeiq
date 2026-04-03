package temporal

import (
	"time"

	"forgeiq/internal/controlplane/contracts"

	"go.temporal.io/sdk/workflow"
)

// PRReviewWorkflow runs a PR review via the external PR Agent.
// This is intentionally orchestration-only: Bitbucket API calls + publishing happen inside the PR agent.
func PRReviewWorkflow(ctx workflow.Context, task contracts.Task) (contracts.Artifact, error) {
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
		StartToCloseTimeout: 3 * time.Minute,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Call PR agent. We use a dedicated task type for the agent itself: "review_pr".
	// The workflow type for /run remains "pr_review".
	var art contracts.Artifact
	if err := workflow.ExecuteActivity(ctx, "ReviewPR", task).Get(ctx, &art); err != nil {
		status.State = "failed"
		status.LastError = err.Error()
		status.UpdatedAtRFC = now()
		return contracts.Artifact{}, err
	}

	status.State = "completed"
	status.UpdatedAtRFC = now()
	status.Evidence["result_type"] = art.Type
	finalResult = art
	return finalResult, nil
}

