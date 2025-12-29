package plugin_test

import (
	"context"
	"testing"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
	"forgeiq/internal/plugin"
)

type agentP struct{}

func (a agentP) Name() string        { return "a" }
func (a agentP) Version() string     { return "v1" }
func (a agentP) TaskTypes() []string { return []string{"x"} }
func (a agentP) Execute(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
	return contracts.Artifact{TaskID: task.ID}, nil
}

type toolP struct{}

func (t toolP) Name() string              { return "t" }
func (t toolP) Version() string           { return "v1" }
func (t toolP) Info() interfaces.ToolInfo { return interfaces.ToolInfo{Name: "t", Version: "v1"} }
func (t toolP) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := plugin.NewRegistry()
	r.RegisterAgent(agentP{})
	r.RegisterTool(toolP{})

	if _, ok := r.GetAgent("a", "v1"); !ok {
		t.Fatalf("expected agent")
	}
	if _, ok := r.GetTool("t", "v1"); !ok {
		t.Fatalf("expected tool")
	}
	if len(r.ListAgents()) != 1 || len(r.ListTools()) != 1 {
		t.Fatalf("expected lists")
	}
}
