package contracts

import "encoding/json"

type AgentKind string

const (
	AgentKindRule          AgentKind = "rule"
	AgentKindRunbook       AgentKind = "runbook"
	AgentKindObservability AgentKind = "observability"
	AgentKindExec          AgentKind = "exec"
)

// A2ATaskRequest is the common envelope for all agent calls.
type A2ATaskRequest struct {
	Agent         AgentKind       `json:"agent"`                    // rule, runbook, observability, exec
	TaskType      string          `json:"task_type"`                // e.g. "evaluate_rules", "get_runbook"
	Input         json.RawMessage `json:"input"`                    // agent-specific payload
	Context       map[string]any  `json:"context,omitempty"`        // shared context (incident id, service, etc.)
	CorrelationID string          `json:"correlation_id,omitempty"` // maps back to workflow step
	// IdempotencyKey allows the caller to safely retry side-effecting requests.
	// Recommended format: "<workflow_or_task_id>:<step_id>".
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// A2ATaskResponse is the generic agent response.
type A2ATaskResponse struct {
	Status   string          `json:"status"` // "ok", "error", "needs_approval"
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Approval *ApprovalSpec   `json:"approval,omitempty"`
}

type ApprovalSpec struct {
	Required bool   `json:"required"`
	Reason   string `json:"reason,omitempty"`
}

// ---- Specific payloads per agent ----

// Input to Rule Agent
type RuleAgentInput struct {
	IncidentID string            `json:"incident_id"`
	Service    string            `json:"service"`
	Symptom    string            `json:"symptom"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Output from Rule Agent
type RuleAgentOutput struct {
	RunbookID             string `json:"runbook_id"`
	Severity              string `json:"severity"`
	RequiresHumanApproval bool   `json:"requires_human_approval"`
}

// Input to Runbook Agent
type RunbookAgentInput struct {
	RunbookID  string `json:"runbook_id"`
	Service    string `json:"service"`
	IncidentID string `json:"incident_id"`
}

// A runbook is described as diagnostics + actions.
type RunbookAgentOutput struct {
	Runbook Runbook `json:"runbook"`
}

type Runbook struct {
	ID           string        `json:"id" yaml:"id"`
	Version      string        `json:"version,omitempty" yaml:"version,omitempty"`
	Name         string        `json:"name" yaml:"name"`
	Diagnostics  []RunbookStep `json:"diagnostics" yaml:"diagnostics"`
	Actions      []RunbookStep `json:"actions" yaml:"actions"`
	DefaultOwner string        `json:"default_owner,omitempty" yaml:"default_owner,omitempty"`
}

type RunbookStep struct {
	ID               string         `json:"id" yaml:"id"`
	Name             string         `json:"name" yaml:"name"`
	Description      string         `json:"description" yaml:"description"`
	Agent            AgentKind      `json:"agent" yaml:"agent"` // observability or exec
	TaskType         string         `json:"task_type" yaml:"task_type"`
	Input            map[string]any `json:"input" yaml:"input"`
	RequiresApproval bool           `json:"requires_approval" yaml:"requires_approval"`
}

// Observability Agent input/output
type ObservabilityInput struct {
	QueryType string            `json:"query_type"` // "promql", "logs", "k8s_status", etc.
	Query     string            `json:"query"`
	Params    map[string]string `json:"params,omitempty"`
}

type ObservabilityOutput struct {
	// Human-readable summary for operators.
	Summary string `json:"summary"`
	// Machine-readable signals extracted from Raw (safe defaults for workflows).
	Signals map[string]float64 `json:"signals,omitempty"`
	// Source engine that produced the data (prometheus | loki | stub).
	Engine string `json:"engine,omitempty"`
	// Whether this result is degraded (e.g., backend not configured).
	Degraded bool `json:"degraded,omitempty"`
	// Optional error string (if degraded but still returning ok).
	Error string `json:"error,omitempty"`
	// Raw response payload (provider-specific).
	Raw any `json:"raw,omitempty"`
}

// Exec Agent input/output
type ExecInput struct {
	ActionType string            `json:"action_type"` // "k8s_scale", "k8s_restart", etc.
	Params     map[string]string `json:"params"`
}

type ExecOutput struct {
	Success bool   `json:"success"`
	Details string `json:"details"`
}
