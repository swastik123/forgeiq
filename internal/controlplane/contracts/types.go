package contracts

import "time"

type Task struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id,omitempty"` // logical tenant boundary (optional in dev)
	Type      string            `json:"type"`                // e.g. "incident_triage"
	Input     map[string]any    `json:"input"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
}

type Artifact struct {
	TaskID  string         `json:"task_id"`
	Type    string         `json:"type"` // "PolicyDecision", "Plan", "Evidence", "Result"
	Payload map[string]any `json:"payload"`
	TS      time.Time      `json:"ts"`
}

type PolicyDecision struct {
	Allowed      bool              `json:"allowed"`
	AllowWrites  bool              `json:"allow_writes"`
	Scopes       []string          `json:"scopes"`
	ToolTagAllow []string          `json:"tool_tag_allow"` // e.g. ["read"], or ["read","write"]
	Budgets      Budget            `json:"budgets"`
	Reasons      map[string]string `json:"reasons"`
}

type Budget struct {
	MaxToolCalls int `json:"max_tool_calls"`
	MaxSeconds   int `json:"max_seconds"`
}

type Plan struct {
	Steps      []PlanStep `json:"steps"`
	Confidence float64    `json:"confidence"`
	Fallbacks  []PlanStep `json:"fallbacks"`
}

type PlanStep struct {
	StepID      string         `json:"step_id"`
	Goal        string         `json:"goal"`
	ToolName    string         `json:"tool_name"` // MCP tool name
	ToolVersion string         `json:"tool_version"`
	Tags        []string       `json:"tags"`   // must include read/write
	Scopes      []string       `json:"scopes"` // required scopes
	Args        map[string]any `json:"args"`
	StopOnErr   bool           `json:"stop_on_err"`
}
