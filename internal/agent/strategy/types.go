package strategy

import (
	"time"

	"forgeiq/internal/controlplane/interfaces"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role           `json:"role"`
	Content string         `json:"content"`
	Name    string         `json:"name,omitempty"` // tool name
	Data    map[string]any `json:"data,omitempty"` // optional structured payload
	TS      time.Time      `json:"ts,omitempty"`
}

type ConversationState struct {
	ThreadID string    `json:"thread_id,omitempty"`
	Messages []Message `json:"messages"`
}

type Budget struct {
	MaxIterations int           `json:"max_iterations"`
	MaxToolCalls  int           `json:"max_tool_calls"`
	MaxWallTime   time.Duration `json:"max_wall_time"`
}

type ModelConfig struct {
	Provider    string  `json:"provider,omitempty"` // "mcp" | "openai" | "anthropic" | ...
	Model       string  `json:"model,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ToolCatalog struct {
	Tools []interfaces.ToolInfo `json:"tools"`
}

type ToolCall struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Args    map[string]any `json:"args"`
}

type IterationTrace struct {
	Iteration   int            `json:"iteration"`
	StopReason  string         `json:"stop_reason,omitempty"`  // "final" | "max_iterations" | "max_tool_calls" | "error"
	ModelOutput map[string]any `json:"model_output,omitempty"` // safe summary only
	ToolCall    *ToolCall      `json:"tool_call,omitempty"`
	ToolResult  map[string]any `json:"tool_result,omitempty"`
	Error       string         `json:"error,omitempty"`
	LatencyMS   int64          `json:"latency_ms,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

type Trace struct {
	Strategy   string           `json:"strategy"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Iterations []IterationTrace `json:"iterations"`
}

type Result struct {
	FinalAnswer string        `json:"final_answer"`
	ToolOutputs []ToolCallOut `json:"tool_outputs,omitempty"`
	Trace       Trace         `json:"trace"`
}

type ToolCallOut struct {
	Tool ToolCall       `json:"tool"`
	Out  map[string]any `json:"out,omitempty"`
	Err  string         `json:"err,omitempty"`
}
