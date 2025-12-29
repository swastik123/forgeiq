package catalog_test

import (
	"testing"

	"forgeiq/internal/catalog"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
)

func TestCatalog_ValidateToolCall_UnknownTool(t *testing.T) {
	c := catalog.New()
	if err := c.ValidateToolCall("x", "v1", map[string]any{}); err == nil {
		t.Fatalf("expected error for unknown tool")
	}
}

func TestCatalog_ValidatePlan(t *testing.T) {
	c := catalog.New()
	c.Upsert(catalog.ToolDescriptor{
		Info: interfaces.ToolInfo{
			Name:    "logs.search",
			Version: "v1",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []any{"query"},
			},
			Meta: map[string]string{"owner": "platform"},
		},
	})

	plan := contracts.Plan{
		Steps: []contracts.PlanStep{
			{StepID: "s1", ToolName: "logs.search", ToolVersion: "v1", Args: map[string]any{"query": "x"}},
		},
	}
	if err := c.ValidatePlan(plan); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	plan2 := contracts.Plan{
		Steps: []contracts.PlanStep{
			{StepID: "s1", ToolName: "logs.search", ToolVersion: "v1", Args: map[string]any{}},
		},
	}
	if err := c.ValidatePlan(plan2); err == nil {
		t.Fatalf("expected error (missing required arg)")
	}
}
