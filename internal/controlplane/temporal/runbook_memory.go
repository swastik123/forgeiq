package temporal

import (
	"fmt"
	"time"

	"forgeiq/internal/controlplane/contracts"
)

// RunbookAutomation "memory" is defined as CURRENT WORKFLOW STATE (not chat history).
// It is workflow-owned and updated only via deltas applied by the workflow.

type MemoryBucket string

const (
	BucketIntent     MemoryBucket = "IntentState"
	BucketPlan       MemoryBucket = "PlanState"
	BucketEvidence   MemoryBucket = "EvidenceState"
	BucketPolicyRisk MemoryBucket = "PolicyRiskState"
	BucketProgress   MemoryBucket = "ProgressState"
	BucketPointers   MemoryBucket = "PointersState"
)

type IntentState struct {
	IncidentID string            `json:"incident_id"`
	Service    string            `json:"service"`
	Symptom    string            `json:"symptom"`
	Goal       string            `json:"goal,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type PlanState struct {
	// For runbook automation, the plan is the selected runbook + a cursor through its steps.
	Runbook      contracts.Runbook `json:"runbook"`
	Phase        string            `json:"phase,omitempty"`          // "diagnostics" | "actions"
	CurrentStep  string            `json:"current_step,omitempty"`   // step.id
	CurrentIndex int               `json:"current_index,omitempty"`  // index within Phase slice
	Branch       map[string]string `json:"branch,omitempty"`         // reserved for conditions
	Constraints  map[string]any    `json:"constraints,omitempty"`    // reserved for token/safety budgets, etc
}

type EvidenceState struct {
	// Normalized, machine-usable facts from tools/agents.
	Diagnostics map[string]contracts.ObservabilityOutput `json:"diagnostics,omitempty"`
	Actions     map[string]contracts.ExecOutput          `json:"actions,omitempty"`
	Facts       map[string]any                           `json:"facts,omitempty"`
}

type PolicyRiskState struct {
	// Policy/approvals/safety budgets. (For runbook automation, rule agent can force approvals.)
	RequiresHumanApproval bool              `json:"requires_human_approval"`
	ApprovalReasons       []string          `json:"approval_reasons,omitempty"`
	Budgets               map[string]any    `json:"budgets,omitempty"`
	ToolScopes            []string          `json:"tool_scopes,omitempty"`
	Approvals             []ApprovalSignal  `json:"approvals,omitempty"`
}

type ProgressState struct {
	CompletedSteps []string         `json:"completed_steps,omitempty"`
	Retries        map[string]int   `json:"retries,omitempty"`
	Rollback       map[string]any   `json:"rollback,omitempty"`
	Failed         bool             `json:"failed"`
	FailureReason  string           `json:"failure_reason,omitempty"`
	Notes          map[string]string `json:"notes,omitempty"`
}

type PointersState struct {
	SelectedRunbookID string            `json:"selected_runbook_id,omitempty"`
	Artifacts         map[string]string `json:"artifacts,omitempty"` // e.g. "logs_snapshot" -> artifact_id/uri
}

type DeltaEntry struct {
	At         time.Time          `json:"at"`
	Bucket     MemoryBucket       `json:"bucket"`
	Action     string             `json:"action"` // small semantic action name (stable, auditable)
	Patch      map[string]any     `json:"patch,omitempty"`
	Actor      string             `json:"actor,omitempty"` // agent/tool/workflow component
	Rationale  string             `json:"rationale,omitempty"`
	Refs       map[string]string  `json:"refs,omitempty"` // artifact ids, correlation ids
	PrevVer    int64              `json:"prev_ver"`
	NextVer    int64              `json:"next_ver"`
}

type RunbookMemory struct {
	Intent     IntentState     `json:"intent"`
	Plan       PlanState       `json:"plan"`
	Evidence   EvidenceState   `json:"evidence"`
	PolicyRisk PolicyRiskState `json:"policy_risk"`
	Progress   ProgressState   `json:"progress"`
	Pointers   PointersState   `json:"pointers"`

	// Per-bucket versioning so we can compare/apply safely.
	Versions map[MemoryBucket]int64 `json:"versions"`
	// Delta log for audit/debug/replay.
	Deltas []DeltaEntry `json:"deltas"`
}

func NewRunbookMemory(intent IntentState) RunbookMemory {
	return RunbookMemory{
		Intent: intent,
		Evidence: EvidenceState{
			Diagnostics: map[string]contracts.ObservabilityOutput{},
			Actions:     map[string]contracts.ExecOutput{},
			Facts:       map[string]any{},
		},
		PolicyRisk: PolicyRiskState{
			Budgets: map[string]any{},
		},
		Progress: ProgressState{
			Retries: map[string]int{},
			Notes:   map[string]string{},
		},
		Pointers: PointersState{
			Artifacts: map[string]string{},
		},
		Versions: map[MemoryBucket]int64{
			BucketIntent:     1,
			BucketPlan:       0,
			BucketEvidence:   0,
			BucketPolicyRisk: 0,
			BucketProgress:   0,
			BucketPointers:   0,
		},
		Deltas: []DeltaEntry{},
	}
}

type Delta struct {
	Bucket    MemoryBucket
	Action    string
	Patch     map[string]any
	Actor     string
	Rationale string
	Refs      map[string]string
	// If non-zero, workflow can enforce optimistic concurrency per bucket.
	ExpectedVer int64
}

func (m *RunbookMemory) bucketVer(b MemoryBucket) int64 {
	if m.Versions == nil {
		m.Versions = map[MemoryBucket]int64{}
	}
	return m.Versions[b]
}

func (m *RunbookMemory) bump(b MemoryBucket) (prev, next int64) {
	prev = m.bucketVer(b)
	next = prev + 1
	m.Versions[b] = next
	return prev, next
}

func (m *RunbookMemory) Apply(now time.Time, d Delta) error {
	if d.Bucket == "" {
		return fmt.Errorf("delta bucket is required")
	}
	if d.Action == "" {
		return fmt.Errorf("delta action is required")
	}
	if d.ExpectedVer > 0 && m.bucketVer(d.Bucket) != d.ExpectedVer {
		return fmt.Errorf("version mismatch for %s: expected=%d got=%d", d.Bucket, d.ExpectedVer, m.bucketVer(d.Bucket))
	}

	// Apply delta (small set of workflow-known mutations; avoid generic reflective patching).
	switch d.Bucket {
	case BucketPolicyRisk:
		switch d.Action {
		case "set_requires_human_approval":
			if v, ok := d.Patch["requires_human_approval"].(bool); ok {
				m.PolicyRisk.RequiresHumanApproval = v
			}
			if r, ok := d.Patch["reason"].(string); ok && r != "" {
				m.PolicyRisk.ApprovalReasons = append(m.PolicyRisk.ApprovalReasons, r)
			}
		case "append_approval":
			ap, ok := d.Patch["approval"].(ApprovalSignal)
			if !ok {
				return fmt.Errorf("policy.append_approval expects ApprovalSignal")
			}
			m.PolicyRisk.Approvals = append(m.PolicyRisk.Approvals, ap)
			if r, ok := d.Patch["reason"].(string); ok && r != "" {
				m.PolicyRisk.ApprovalReasons = append(m.PolicyRisk.ApprovalReasons, r)
			}
		default:
			return fmt.Errorf("unsupported policy delta action: %s", d.Action)
		}

	case BucketPointers:
		switch d.Action {
		case "set_selected_runbook_id":
			if v, ok := d.Patch["selected_runbook_id"].(string); ok {
				m.Pointers.SelectedRunbookID = v
			}
		case "set_artifact_pointer":
			k, _ := d.Patch["key"].(string)
			v, _ := d.Patch["value"].(string)
			if k != "" && v != "" {
				if m.Pointers.Artifacts == nil {
					m.Pointers.Artifacts = map[string]string{}
				}
				m.Pointers.Artifacts[k] = v
			}
		default:
			return fmt.Errorf("unsupported pointers delta action: %s", d.Action)
		}

	case BucketPlan:
		switch d.Action {
		case "set_runbook":
			rb, ok := d.Patch["runbook"].(contracts.Runbook)
			if !ok {
				return fmt.Errorf("plan.set_runbook expects contracts.Runbook patch")
			}
			m.Plan.Runbook = rb
		case "set_cursor":
			if v, ok := d.Patch["phase"].(string); ok && v != "" {
				m.Plan.Phase = v
			}
			if v, ok := d.Patch["current_step"].(string); ok {
				m.Plan.CurrentStep = v
			}
			if v, ok := d.Patch["current_index"].(int); ok {
				m.Plan.CurrentIndex = v
			}
		default:
			return fmt.Errorf("unsupported plan delta action: %s", d.Action)
		}

	case BucketEvidence:
		switch d.Action {
		case "record_diagnostic":
			stepID, _ := d.Patch["step_id"].(string)
			out, ok := d.Patch["output"].(contracts.ObservabilityOutput)
			if stepID == "" || !ok {
				return fmt.Errorf("evidence.record_diagnostic expects step_id + output")
			}
			if m.Evidence.Diagnostics == nil {
				m.Evidence.Diagnostics = map[string]contracts.ObservabilityOutput{}
			}
			m.Evidence.Diagnostics[stepID] = out
		case "record_action":
			stepID, _ := d.Patch["step_id"].(string)
			out, ok := d.Patch["output"].(contracts.ExecOutput)
			if stepID == "" || !ok {
				return fmt.Errorf("evidence.record_action expects step_id + output")
			}
			if m.Evidence.Actions == nil {
				m.Evidence.Actions = map[string]contracts.ExecOutput{}
			}
			m.Evidence.Actions[stepID] = out
		case "set_fact":
			k, _ := d.Patch["key"].(string)
			if k == "" {
				return fmt.Errorf("evidence.set_fact expects key")
			}
			if m.Evidence.Facts == nil {
				m.Evidence.Facts = map[string]any{}
			}
			m.Evidence.Facts[k] = d.Patch["value"]
		default:
			return fmt.Errorf("unsupported evidence delta action: %s", d.Action)
		}

	case BucketProgress:
		switch d.Action {
		case "mark_step_completed":
			stepID, _ := d.Patch["step_id"].(string)
			if stepID == "" {
				return fmt.Errorf("progress.mark_step_completed expects step_id")
			}
			m.Progress.CompletedSteps = append(m.Progress.CompletedSteps, stepID)
		case "mark_failed":
			m.Progress.Failed = true
			if v, ok := d.Patch["reason"].(string); ok {
				m.Progress.FailureReason = v
			}
		default:
			return fmt.Errorf("unsupported progress delta action: %s", d.Action)
		}

	case BucketIntent:
		// Intent is typically fixed for a run; allow only non-breaking metadata/goal tweaks.
		switch d.Action {
		case "set_goal":
			if v, ok := d.Patch["goal"].(string); ok {
				m.Intent.Goal = v
			}
		default:
			return fmt.Errorf("unsupported intent delta action: %s", d.Action)
		}

	default:
		return fmt.Errorf("unknown bucket: %s", d.Bucket)
	}

	prev, next := m.bump(d.Bucket)
	m.Deltas = append(m.Deltas, DeltaEntry{
		At:        now,
		Bucket:    d.Bucket,
		Action:    d.Action,
		Patch:     d.Patch,
		Actor:     d.Actor,
		Rationale: d.Rationale,
		Refs:      d.Refs,
		PrevVer:   prev,
		NextVer:   next,
	})
	return nil
}

// ---- Handoff packets (agent-specific minimal views of state) ----

type RuleHandoffPacket struct {
	Intent IntentState `json:"intent"`
}

type RunbookHandoffPacket struct {
	Intent    IntentState     `json:"intent"`
	Pointers  PointersState   `json:"pointers"`
	Policy    PolicyRiskState `json:"policy_risk"`
	GoalHints map[string]any  `json:"goal_hints,omitempty"`
}

type ObservabilityHandoffPacket struct {
	Intent      IntentState         `json:"intent"`
	Policy      PolicyRiskState     `json:"policy_risk"`
	PlanCursor  map[string]any      `json:"plan_cursor,omitempty"`
	Step        contracts.RunbookStep `json:"step"`
	Constraints map[string]any      `json:"constraints,omitempty"`
}

type ExecHandoffPacket struct {
	Intent      IntentState           `json:"intent"`
	Policy      PolicyRiskState       `json:"policy_risk"`
	Step        contracts.RunbookStep `json:"step"`
	Constraints map[string]any        `json:"constraints,omitempty"`
}

func BuildRulePacket(m RunbookMemory) RuleHandoffPacket {
	return RuleHandoffPacket{Intent: m.Intent}
}

func BuildRunbookPacket(m RunbookMemory) RunbookHandoffPacket {
	return RunbookHandoffPacket{
		Intent:   m.Intent,
		Pointers: m.Pointers,
		Policy:   m.PolicyRisk,
	}
}

func BuildObservabilityPacket(m RunbookMemory, step contracts.RunbookStep) ObservabilityHandoffPacket {
	return ObservabilityHandoffPacket{
		Intent: m.Intent,
		Policy: m.PolicyRisk,
		PlanCursor: map[string]any{
			"phase":         m.Plan.Phase,
			"current_step":  m.Plan.CurrentStep,
			"current_index": m.Plan.CurrentIndex,
		},
		Step:        step,
		Constraints: m.Plan.Constraints,
	}
}

func BuildExecPacket(m RunbookMemory, step contracts.RunbookStep) ExecHandoffPacket {
	return ExecHandoffPacket{
		Intent:      m.Intent,
		Policy:      m.PolicyRisk,
		Step:        step,
		Constraints: m.Plan.Constraints,
	}
}

