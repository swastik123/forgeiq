package examples

import (
	"context"
	"time"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	"forgeiq/internal/plugin"
)

// Example: Custom Agent Plugin
type CustomAgent struct{}

func (a *CustomAgent) Name() string {
	return "custom-agent"
}

func (a *CustomAgent) Version() string {
	return "1.0.0"
}

func (a *CustomAgent) TaskTypes() []string {
	return []string{"custom_task", "data_processing"}
}

func (a *CustomAgent) Execute(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
	// Your custom agent logic here
	result := map[string]any{
		"processed": true,
		"task_id":   task.ID,
		"timestamp": time.Now().Unix(),
	}

	return contracts.Artifact{
		TaskID:  task.ID,
		Type:    "CustomResult",
		Payload: result,
		TS:      time.Now(),
	}, nil
}

// Example: Custom Tool Plugin
type CustomTool struct{}

func (t *CustomTool) Name() string {
	return "custom.tool"
}

func (t *CustomTool) Version() string {
	return "v1"
}

func (t *CustomTool) Info() interfaces.ToolInfo {
	return interfaces.ToolInfo{
		Name:    "custom.tool",
		Version: "v1",
		Tags:    []string{"read", "custom"},
		Scopes:  []string{"custom:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"param1": map[string]any{"type": "string"},
				"param2": map[string]any{"type": "number"},
			},
		},
		Meta: map[string]string{
			"description": "A custom tool example",
		},
	}
}

func (t *CustomTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	// Your custom tool logic here
	param1, _ := args["param1"].(string)
	param2, _ := args["param2"].(float64)

	return map[string]any{
		"result":      "success",
		"param1":      param1,
		"param2":      param2,
		"executed_at": time.Now().Format(time.RFC3339),
	}, nil
}

// Example: Registering plugins
func ExamplePluginRegistration() {
	registry := plugin.NewRegistry()

	// Register custom agent
	registry.RegisterAgent(&CustomAgent{})

	// Register custom tool
	registry.RegisterTool(&CustomTool{})

	// Use plugins
	agent, _ := registry.GetAgent("custom-agent", "1.0.0")
	tool, _ := registry.GetTool("custom.tool", "v1")

	// Execute
	ctx := context.Background()
	task := contracts.Task{
		ID:    "test-task",
		Type:  "custom_task",
		Input: map[string]any{"data": "test"},
	}

	artifact, _ := agent.Execute(ctx, task)
	_ = artifact

	result, _ := tool.Execute(ctx, map[string]any{"param1": "value", "param2": 42})
	_ = result
}
