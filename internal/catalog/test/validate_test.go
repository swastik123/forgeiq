package catalog_test

import (
	"testing"

	"forgeiq/internal/catalog"
)

func TestValidateArgsAgainstSchema_RequiredFields(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "number"},
		},
		"required": []any{"query"},
	}

	if err := catalog.ValidateArgsAgainstSchema(schema, map[string]any{"query": "x"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := catalog.ValidateArgsAgainstSchema(schema, map[string]any{"limit": 1}); err == nil {
		t.Fatalf("expected missing required arg error")
	}
}

func TestValidateArgsAgainstSchema_TypeChecks(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"s": map[string]any{"type": "string"},
			"n": map[string]any{"type": "number"},
			"o": map[string]any{"type": "object"},
			"a": map[string]any{"type": "array"},
			"b": map[string]any{"type": "boolean"},
		},
	}

	okArgs := map[string]any{
		"s": "hi",
		"n": 1.0,
		"o": map[string]any{"x": 1},
		"a": []any{"x"},
		"b": true,
	}
	if err := catalog.ValidateArgsAgainstSchema(schema, okArgs); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	bad := map[string]any{"s": 123}
	if err := catalog.ValidateArgsAgainstSchema(schema, bad); err == nil {
		t.Fatalf("expected type error")
	}
}
