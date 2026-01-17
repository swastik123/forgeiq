package runbooks

import (
	"fmt"
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// Render performs simple string substitution over step input fields.
// Supported placeholder format: {{key}} (e.g., {{service}}, {{incident_id}}).
func Render(rb contracts.Runbook, vars map[string]string) contracts.Runbook {
	repl := buildReplacer(vars)

	out := rb
	out.Diagnostics = make([]contracts.RunbookStep, len(rb.Diagnostics))
	for i, s := range rb.Diagnostics {
		out.Diagnostics[i] = renderStep(s, repl)
	}
	out.Actions = make([]contracts.RunbookStep, len(rb.Actions))
	for i, s := range rb.Actions {
		out.Actions[i] = renderStep(s, repl)
	}
	return out
}

func renderStep(s contracts.RunbookStep, repl *strings.Replacer) contracts.RunbookStep {
	out := s
	out.Input = renderAnyMap(s.Input, repl)
	return out
}

func renderAnyMap(in map[string]any, repl *strings.Replacer) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = renderAny(v, repl)
	}
	return out
}

func renderAny(v any, repl *strings.Replacer) any {
	switch t := v.(type) {
	case string:
		return repl.Replace(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = renderAny(t[i], repl)
		}
		return out
	case map[string]any:
		return renderAnyMap(t, repl)
	default:
		return v
	}
}

func buildReplacer(vars map[string]string) *strings.Replacer {
	if len(vars) == 0 {
		return strings.NewReplacer()
	}
	parts := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		parts = append(parts, fmt.Sprintf("{{%s}}", k), v)
	}
	return strings.NewReplacer(parts...)
}



