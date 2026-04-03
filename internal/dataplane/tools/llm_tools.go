package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"forgeiq/internal/controlplane/interfaces"
	mcpserver "forgeiq/internal/dataplane/mcp"
)

// registerLLMTools registers a minimal local LLM tool for strategy demos.
// This is NOT a real LLM: it makes deterministic choices based on keywords.
func registerLLMTools(s *mcpserver.Server) {
	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "llm.chat",
		Version: "v1",
		Tags:    []string{"read", "llm"},
		Scopes:  []string{"llm:chat"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"messages": map[string]any{"type": "array"},
				"tools":    map[string]any{"type": "array"},
			},
			"required": []any{"messages"},
		},
		Meta: map[string]string{"owner": "platform", "note": "demo-only deterministic LLM"},
	}, func(args map[string]any) (map[string]any, error) {
		// messages: [{role, content, name?, data?}]
		msgs, _ := args["messages"].([]any)
		last := ""
		for i := len(msgs) - 1; i >= 0; i-- {
			if m, ok := msgs[i].(map[string]any); ok {
				if c, ok := m["content"].(string); ok && strings.TrimSpace(c) != "" {
					last = c
					break
				}
			}
		}
		q := strings.ToLower(last)

		// If user asks to use logs, propose tool call logs.search
		if strings.Contains(q, "logs") {
			return map[string]any{
				"tool_call": map[string]any{
					"name":    "logs.search",
					"version": "v1",
					"args":    map[string]any{"query": last, "limit": 5},
				},
				"summary": "selected logs.search based on keyword match",
			}, nil
		}
		// If user asks "summarize", finalize directly.
		if strings.Contains(q, "summarize") || strings.Contains(q, "summary") {
			return map[string]any{
				"final_answer": fmt.Sprintf("Summary (demo): %s", last),
				"summary":      "returned final answer (demo)",
			}, nil
		}
		// Default final
		return map[string]any{
			"final_answer": fmt.Sprintf("Answer (demo): %s", last),
			"summary":      "returned final answer (demo default)",
		}, nil
	})

	// v2: typed result + (light) schema enforcement + retries.
	//
	// v2 supports optional real provider backends (LiteLLM/OpenAI/Anthropic) behind MCP,
	// with schema validation + retries. If not configured, it falls back to demo behavior.
	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "llm.chat",
		Version: "v2",
		Tags:    []string{"read", "llm"},
		Scopes:  []string{"llm:chat"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"messages": map[string]any{"type": "array"},
				"tools":    map[string]any{"type": "array"},
				"model":    map[string]any{"type": "object"},
				"meta":     map[string]any{"type": "object"},
				// Response formatting / schema enforcement
				"response_format": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{"type": "string"}, // "json_schema" | "json_object"
						"name": map[string]any{"type": "string"},
						"schema": map[string]any{
							"type": "object", // JSON Schema (subset)
						},
					},
				},
				"retries": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"max_attempts": map[string]any{"type": "number", "default": 2},
					},
				},
				"strict": map[string]any{"type": "boolean", "default": true},
			},
			"required": []any{"messages"},
		},
		Meta: map[string]string{"owner": "platform", "note": "typed LLM w/ schema validation + retries; provider-backed when configured"},
	}, func(args map[string]any) (map[string]any, error) {
		msgs, _ := args["messages"].([]any)
		normalizedMsgs := normalizeMessages(msgs)
		last := lastNonEmptyContent(normalizedMsgs)

		// Pull schema (optional)
		var schema map[string]any
		if rf, ok := args["response_format"].(map[string]any); ok && rf != nil {
			if s2, ok := rf["schema"].(map[string]any); ok && s2 != nil {
				schema = s2
			}
		}

		// Optional response_format passthrough for OpenAI-compatible providers.
		var responseFormat map[string]any
		if rf, ok := args["response_format"].(map[string]any); ok && rf != nil {
			responseFormat = rf
		}

		// Optional model name
		modelName := ""
		if m, ok := args["model"].(map[string]any); ok && m != nil {
			if v, ok := m["model"].(string); ok {
				modelName = strings.TrimSpace(v)
			}
		}

		maxAttempts := 2
		if r, ok := args["retries"].(map[string]any); ok && r != nil {
			if v, ok := r["max_attempts"].(float64); ok && int(v) > 0 {
				maxAttempts = int(v)
			}
		}

		// Demo fallback candidate: if user prompt looks like PR review schema, return an empty review.
		// Otherwise return a typed object with an "answer" field.
		buildCandidate := func() map[string]any {	
			l := strings.ToLower(last)
			if strings.Contains(l, "unified diff:") && strings.Contains(l, "reviewcomments") {
				return map[string]any{
					"reviewComments": []any{},
					"summary": map[string]any{
						"overall":       "Demo fallback (no provider output).",
						"risk":          "LOW",
						"areasReviewed": []any{},
						"areasSkipped":  []any{},
					},
				}
			}

			// OLD demo schema (keep if you still want it)
			if strings.Contains(l, "unified diff:") && strings.Contains(l, "\"findings\"") {
				return map[string]any{
					"summary":  "Demo review: parsed diff-scoped prompt; no deterministic findings.",
					"findings": []any{},
				}
			}

			return map[string]any{
				"answer": fmt.Sprintf("Answer (demo v2): %s", last),
			}
		}

		var candidate map[string]any
		var valErr error
		attempts := 0
		provCfg := llmConfigFromEnv()

		for attempts < maxAttempts {
			attempts++
			candidate = nil

			// 1) Try provider-backed call if configured
			if provCfg.Provider != provDemo {
				ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
				res, err := callLLMProvider(ctx, provCfg, modelName, normalizedMsgs, responseFormat)
				cancel()
				if err == nil {
					txt := strings.TrimSpace(res.RawText)
					if j := extractJSONObject(txt); j != "" {
						txt = j
					}
					var obj map[string]any
					if err := json.Unmarshal([]byte(txt), &obj); err == nil && len(obj) > 0 {
						candidate = obj
					}
				}
			}

			// 2) Fall back to demo candidate
			if candidate == nil {
				candidate = buildCandidate()
			}

			valErr = validateAgainstSchema(schema, candidate)
			if valErr == nil {
				break
			}
			// In a real implementation, we'd reprompt the provider with validation errors.
			// For now, attempt a minimal repair for the common PR review shape.
			// if schemaRequires(schema, "summary") && schemaRequires(schema, "findings") {
			// 	candidate["summary"] = "Demo review: repaired to match schema."
			// 	if _, ok := candidate["findings"]; !ok {
			// 		candidate["findings"] = []any{}
			// 	}
			// 	valErr = validateAgainstSchema(schema, candidate)
			// 	if valErr == nil {
			// 		break
			// 	}
			// }
			if schemaRequires(schema, "reviewComments") && schemaRequires(schema, "summary") {
				if _, ok := candidate["reviewComments"]; !ok {
					candidate["reviewComments"] = []any{}
				}
				if _, ok := candidate["summary"]; !ok {
					candidate["summary"] = map[string]any{
						"overall":       "Repaired to match schema.",
						"risk":          "LOW",
						"areasReviewed": []any{},
						"areasSkipped":  []any{},
					}
				}
				valErr = validateAgainstSchema(schema, candidate)
				if valErr == nil {
					break
				}
			}
		}

		if valErr != nil {
			return nil, valErr
		}

		return map[string]any{
			"status":   "ok",
			"attempts": attempts,
			"result":   candidate,
		}, nil
	})
}

func normalizeMessages(msgs []any) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, mm := range msgs {
		m, ok := mm.(map[string]any)
		if !ok || m == nil {
			continue
		}
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		role = strings.TrimSpace(role)
		content = strings.TrimSpace(content)
		if role == "" {
			continue
		}
		out = append(out, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	return out
}

func lastNonEmptyContent(msgs []map[string]any) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if c, ok := msgs[i]["content"].(string); ok && strings.TrimSpace(c) != "" {
			return c
		}
	}
	return ""
}

func extractJSONObject(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return ""
}

// validateAgainstSchema validates a tiny subset of JSON Schema: object.required and
// object.properties types for string/array/object/number/boolean.
// If schema is nil/empty, validation is skipped.
func validateAgainstSchema(schema map[string]any, result map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	typ, _ := schema["type"].(string)
	if typ != "" && typ != "object" {
		// For our purposes, only object schemas are supported in v2 demo.
		return fmt.Errorf("unsupported schema.type: %s", typ)
	}
	reqAny, _ := schema["required"].([]any)
	for _, r := range reqAny {
		k, _ := r.(string)
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, ok := result[k]; !ok {
			return fmt.Errorf("schema validation: missing required field %q", k)
		}
	}
	props, _ := schema["properties"].(map[string]any)
	for k, ps := range props {
		pm, ok := ps.(map[string]any)
		if !ok || pm == nil {
			continue
		}
		want, _ := pm["type"].(string)
		if want == "" {
			continue
		}
		v, ok := result[k]
		if !ok || v == nil {
			continue
		}
		if !typeMatches(want, v) {
			return fmt.Errorf("schema validation: field %q expected %s", k, want)
		}
	}
	return nil
}

func schemaRequires(schema map[string]any, key string) bool {
	if schema == nil {
		return false
	}
	reqAny, _ := schema["required"].([]any)
	for _, r := range reqAny {
		if s, ok := r.(string); ok && s == key {
			return true
		}
	}
	return false
}

func typeMatches(want string, v any) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "array":
		_, ok := v.([]any)
		if ok {
			return true
		}
		// tolerate []map
		_, ok2 := v.([]map[string]any)
		return ok2
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "number":
		_, ok := v.(float64)
		if ok {
			return true
		}
		_, ok = v.(int)
		if ok {
			return true
		}
		_, ok = v.(int64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	default:
		return false
	}
}
