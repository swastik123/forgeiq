package strategy

import (
	"context"
	"encoding/json"
	"fmt"

	"forgeiq/internal/controlplane/interfaces"
)

// MCPModelClient implements ModelClient by calling an MCP tool (llm.chat).
// This keeps model calls observable + policy-controllable like other tools.
type MCPModelClient struct {
	Tooler interfaces.ToolClient
	Tool   string
	Ver    string
}

func NewMCPModelClient(tooler interfaces.ToolClient) *MCPModelClient {
	return &MCPModelClient{Tooler: tooler, Tool: "llm.chat", Ver: "v1"}
}

func (c *MCPModelClient) Decide(ctx context.Context, req ModelRequest) (ModelDecision, error) {
	if c == nil || c.Tooler == nil {
		return ModelDecision{}, fmt.Errorf("mcp model client: tooler is nil")
	}
	args := map[string]any{
		"messages": req.State.Messages,
		"tools":    req.Tools,
		"model":    req.Model,
		"meta":     req.Meta,
	}
	out, err := c.Tooler.CallTool(ctx, c.Tool, c.Ver, args)
	if err != nil {
		return ModelDecision{}, err
	}
	// Normalize into ModelDecision
	var dec ModelDecision
	if v, ok := out["final_answer"].(string); ok {
		dec.FinalAnswer = v
	}
	if v, ok := out["summary"].(string); ok {
		dec.Summary = v
	}
	if tc, ok := out["tool_call"].(map[string]any); ok {
		b, _ := json.Marshal(tc)
		var call ToolCall
		_ = json.Unmarshal(b, &call)
		if call.Name != "" {
			dec.ToolCall = &call
		}
	}
	return dec, nil
}
