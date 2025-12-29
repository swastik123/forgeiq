package strategy_test

import (
	"context"
	"testing"

	"forgeiq/internal/agent/strategy"
	"forgeiq/internal/controlplane/interfaces"
)

type fakeToolClient struct {
	calls []strategy.ToolCall
}

func (f *fakeToolClient) ListTools(ctx context.Context) ([]interfaces.ToolInfo, error) {
	_ = ctx
	return []interfaces.ToolInfo{{Name: "logs.search", Version: "v1"}}, nil
}

func (f *fakeToolClient) CallTool(ctx context.Context, name string, version string, args map[string]any) (map[string]any, error) {
	_ = ctx
	f.calls = append(f.calls, strategy.ToolCall{Name: name, Version: version, Args: args})
	return map[string]any{"ok": true}, nil
}

type fakeModel struct {
	step int
}

func (m *fakeModel) Decide(ctx context.Context, req strategy.ModelRequest) (strategy.ModelDecision, error) {
	_ = ctx
	_ = req
	m.step++
	if m.step == 1 {
		return strategy.ModelDecision{
			Summary:  "call logs.search",
			ToolCall: &strategy.ToolCall{Name: "logs.search", Version: "v1", Args: map[string]any{"query": "x"}},
		}, nil
	}
	return strategy.ModelDecision{FinalAnswer: "done", Summary: "final"}, nil
}

func TestFunctionCallingStrategy_ToolThenFinal(t *testing.T) {
	tooler := &fakeToolClient{}
	model := &fakeModel{}
	s := &strategy.FunctionCallingStrategy{}
	out, err := s.Run(context.Background(), strategy.Input{
		State:   strategy.ConversationState{Messages: []strategy.Message{{Role: strategy.RoleUser, Content: "get logs"}}},
		Tools:   strategy.ToolCatalog{Tools: []interfaces.ToolInfo{{Name: "logs.search", Version: "v1"}}},
		Budget:  strategy.Budget{MaxIterations: 5, MaxToolCalls: 5},
		Tooler:  tooler,
		Modeler: model,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.FinalAnswer != "done" {
		t.Fatalf("expected final answer, got %q", out.FinalAnswer)
	}
	if len(tooler.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tooler.calls))
	}
	if out.Trace.Strategy != "function_calling" {
		t.Fatalf("expected strategy trace")
	}
}
