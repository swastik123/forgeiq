package tools

import (
	"fmt"
	"strings"

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
}
