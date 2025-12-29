package orchestrator

import (
	"context"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/temporal"

	"go.temporal.io/sdk/client"
)

type TemporalAdapter struct {
	Temporal  client.Client
	TaskQueue string
}

func (t *TemporalAdapter) Run(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
	// start async, wait here for completion to keep same signature
	// (later you can change API to return workflow_id immediately)
	we, err := t.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        task.ID,
		TaskQueue: t.TaskQueue,
	}, temporal.IncidentWorkflow, task)
	if err != nil {
		return contracts.Artifact{}, err
	}

	var result contracts.Artifact
	if err := we.Get(ctx, &result); err != nil {
		return contracts.Artifact{}, err
	}
	return result, nil
}
