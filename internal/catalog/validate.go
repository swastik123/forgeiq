package catalog

import (
	"fmt"
)

// ValidateArgsAgainstSchema performs a minimal validation against the JSON-schema-like shape
// we already use in ToolInfo.Schema.
//
// Supported subset:
// - schema.type == "object"
// - schema.required == []string
// - schema.properties == map[string]any
// - properties[field].type in {"string","number","object","array","boolean"}
func ValidateArgsAgainstSchema(schema map[string]any, args map[string]any) error {
	if schema == nil {
		return nil
	}
	typ, _ := schema["type"].(string)
	if typ != "" && typ != "object" {
		// We only model object args in this framework
		return nil
	}

	// required fields
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			k, _ := r.(string)
			if k == "" {
				continue
			}
			if args == nil {
				return fmt.Errorf("missing required arg: %s", k)
			}
			if _, ok := args[k]; !ok {
				return fmt.Errorf("missing required arg: %s", k)
			}
		}
	} else if reqs, ok := schema["required"].([]string); ok {
		for _, k := range reqs {
			if k == "" {
				continue
			}
			if args == nil {
				return fmt.Errorf("missing required arg: %s", k)
			}
			if _, ok := args[k]; !ok {
				return fmt.Errorf("missing required arg: %s", k)
			}
		}
	}

	props, _ := schema["properties"].(map[string]any)
	for k, pv := range props {
		pm, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		pt, _ := pm["type"].(string)
		if pt == "" {
			continue
		}
		if args == nil {
			continue
		}
		val, ok := args[k]
		if !ok || val == nil {
			continue
		}
		if err := typeCheck(pt, val); err != nil {
			return fmt.Errorf("arg %s: %w", k, err)
		}
	}
	return nil
}

func typeCheck(pt string, v any) error {
	switch pt {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "number":
		switch v.(type) {
		case float64, float32, int, int64, int32, uint64, uint32, uint:
			return nil
		default:
			return fmt.Errorf("expected number")
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("expected object")
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("expected array")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	}
	return nil
}


