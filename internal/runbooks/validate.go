package runbooks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// ValidateRunbook performs structural validation so invalid runbooks fail fast (and in CI).
func ValidateRunbook(rb contracts.Runbook) error {
	if strings.TrimSpace(rb.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(rb.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(rb.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(rb.Diagnostics) == 0 && len(rb.Actions) == 0 {
		return fmt.Errorf("must have at least one step")
	}

	seen := map[string]struct{}{}
	addStep := func(s contracts.RunbookStep, where string) error {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("%s step.id is required", where)
		}
		if _, ok := seen[s.ID]; ok {
			return fmt.Errorf("duplicate step id: %s", s.ID)
		}
		seen[s.ID] = struct{}{}

		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("step %s: name is required", s.ID)
		}
		if strings.TrimSpace(s.TaskType) == "" {
			return fmt.Errorf("step %s: task_type is required", s.ID)
		}
		if s.Input == nil {
			return fmt.Errorf("step %s: input is required", s.ID)
		}

		switch s.Agent {
		case contracts.AgentKindObservability:
			if mapString(s.Input, "query_type") == "" {
				return fmt.Errorf("step %s: input.query_type is required for observability", s.ID)
			}
			if mapString(s.Input, "query") == "" {
				return fmt.Errorf("step %s: input.query is required for observability", s.ID)
			}
		case contracts.AgentKindExec:
			if mapString(s.Input, "action_type") == "" {
				return fmt.Errorf("step %s: input.action_type is required for exec", s.ID)
			}
		default:
			return fmt.Errorf("step %s: unsupported agent: %q", s.ID, string(s.Agent))
		}

		// Optional gate validation
		gateSignal := ""
		if v, ok := s.Input["gate_signal"]; ok && v != nil {
			gateSignal = strings.TrimSpace(fmt.Sprint(v))
		}
		gateOp := ""
		if v, ok := s.Input["gate_op"]; ok && v != nil {
			gateOp = strings.TrimSpace(fmt.Sprint(v))
		}
		if gateSignal != "" || gateOp != "" {
			if gateSignal == "" || gateOp == "" {
				return fmt.Errorf("step %s: gate_signal and gate_op must be set together", s.ID)
			}
			switch gateOp {
			case "<", "<=", ">", ">=", "==", "!=":
			default:
				return fmt.Errorf("step %s: invalid gate_op: %q", s.ID, gateOp)
			}
			if _, ok := s.Input["gate_threshold"]; !ok {
				return fmt.Errorf("step %s: gate_threshold is required when gate_* is used", s.ID)
			}
			if _, err := parseFloatAny(s.Input["gate_threshold"]); err != nil {
				return fmt.Errorf("step %s: gate_threshold must be a number", s.ID)
			}
		}

		return nil
	}

	for _, s := range rb.Diagnostics {
		if err := addStep(s, "diagnostics"); err != nil {
			return err
		}
	}
	for _, s := range rb.Actions {
		if err := addStep(s, "actions"); err != nil {
			return err
		}
	}

	return nil
}

func parseFloatAny(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
	}
}

func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// StableFirstID returns the lexicographically smallest runbook ID, to avoid
// nondeterminism when choosing a default from a map.
func StableFirstID(m map[string]contracts.Runbook) string {
	if len(m) == 0 {
		return ""
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids[0]
}
