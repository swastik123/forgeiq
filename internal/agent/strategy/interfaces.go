package strategy

import (
	"context"

	"forgeiq/internal/controlplane/interfaces"
)

type AgentStrategy interface {
	Name() string
	Run(ctx context.Context, in Input) (Result, error)
}

type Input struct {
	State   ConversationState     `json:"state"`
	Tools   ToolCatalog           `json:"tools"`
	Model   ModelConfig           `json:"model"`
	Budget  Budget                `json:"budget"`
	Meta    map[string]any        `json:"meta,omitempty"`
	Tooler  interfaces.ToolClient `json:"-"`
	Modeler ModelClient           `json:"-"`
}

// ModelClient is the strategy-facing model abstraction.
// It returns either a final answer or a tool call request.
type ModelClient interface {
	Decide(ctx context.Context, req ModelRequest) (ModelDecision, error)
}

type ModelRequest struct {
	State ConversationState     `json:"state"`
	Tools []interfaces.ToolInfo `json:"tools"`
	Model ModelConfig           `json:"model"`
	Meta  map[string]any        `json:"meta,omitempty"`
}

type ModelDecision struct {
	FinalAnswer string    `json:"final_answer,omitempty"`
	ToolCall    *ToolCall `json:"tool_call,omitempty"`
	// Safe debug summary (NOT chain-of-thought)
	Summary string `json:"summary,omitempty"`
}
