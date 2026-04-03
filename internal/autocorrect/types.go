package autocorrect

import "time"

// Verdict is the evaluator decision returned to the workflow.
// The workflow remains the only writer of state and decides what to do next.
type Verdict string

const (
	VerdictProceed     Verdict = "proceed"
	VerdictReplan      Verdict = "replan"
	VerdictAskApproval Verdict = "ask_human_approval"
	VerdictStop        Verdict = "stop"
)

// Applicability scopes a correction artifact so reinjection can be selective.
// Keep this small and explicit to avoid over-injecting irrelevant guidance.
type Applicability struct {
	TenantID  string   `json:"tenant_id,omitempty"`
	TaskTypes []string `json:"task_types,omitempty"`
	AgentType string   `json:"agent_type,omitempty"` // "decision" | "rule" | "exec" | etc
	ToolName  string   `json:"tool_name,omitempty"`
	// Optional path/language scopes for code workflows; kept generic.
	Languages []string `json:"languages,omitempty"`
	PathGlobs []string `json:"path_globs,omitempty"`
}

// CorrectionArtifact is a governed, structured “fix hint” produced by evaluation.
// It is intended to be stored as a canonical memory record payload (not free text).
type CorrectionArtifact struct {
	Kind         string                 `json:"kind"` // e.g. "guardrail" | "suppression" | "contract"
	Title        string                 `json:"title"`
	Violation    string                 `json:"violation"`
	Recommendation string               `json:"recommendation"`
	Applicability Applicability         `json:"applicability"`
	EvidenceRefs []string               `json:"evidence_refs,omitempty"`
	Metadata     map[string]any         `json:"metadata,omitempty"`
	ExpiresAt    *time.Time             `json:"expires_at,omitempty"`
	SuggestedConfidence float64         `json:"suggested_confidence,omitempty"`
}

type EvaluateRequest struct {
	TenantID string
	TaskID   string
	TaskType string
	Phase    string // e.g. "policy" | "plan" | "step"

	AgentType string
	ToolName  string

	// Signals from the workflow / activity outputs (structured).
	Outputs map[string]any
}

type EvaluateResult struct {
	Verdict     Verdict              `json:"verdict"`
	Corrections []CorrectionArtifact `json:"corrections,omitempty"`
	Debug       map[string]any       `json:"debug,omitempty"`
}

