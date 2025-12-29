package validation_test

import (
	"testing"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/validation"
)

func TestValidateTask(t *testing.T) {
	task := contracts.Task{ID: "t1", Type: "incident_triage", Input: map[string]any{"service": "payments", "symptom": "err"}}
	if err := validation.ValidateTask(task); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	task.Input = map[string]any{"service": "x"}
	if err := validation.ValidateTask(task); err == nil {
		t.Fatalf("expected missing symptom error")
	}
}

func TestValidatePlanStep(t *testing.T) {
	step := contracts.PlanStep{StepID: "s1", ToolName: "logs.search", ToolVersion: "v1", Tags: []string{"read"}, Scopes: []string{"logs:read"}}
	if err := validation.ValidatePlanStep(step); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	step.Tags = []string{"badtag"}
	if err := validation.ValidatePlanStep(step); err == nil {
		t.Fatalf("expected invalid tag error")
	}
}

func TestSanitizeInput(t *testing.T) {
	in := map[string]any{
		" a\x00\r\n ": " v\r\n ",
		"nested":      map[string]any{"x\n": "y\x00"},
		"arr":         []any{" a\nb ", map[string]any{"k\r": "v"}},
	}
	out := validation.SanitizeInput(in)
	if _, ok := out["a"]; !ok {
		t.Fatalf("expected sanitized key")
	}
}
