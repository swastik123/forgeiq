package temporal

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"forgeiq/internal/controlplane/contracts"

	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	RunbookWorkflowName = "RunbookAutomationWorkflow"
	ApprovalSignalName  = "approval"
)

// Workflow input – you can align this with your existing /run payload.
type RunbookAutomationInput struct {
	IncidentID string            `json:"incident_id"`
	Service    string            `json:"service"`
	Symptom    string            `json:"symptom"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Final state we return (and also persist in workflow history).
type RunbookAutomationResult struct {
	IncidentID    string                                   `json:"incident_id"`
	Service       string                                   `json:"service"`
	Symptom       string                                   `json:"symptom"`
	RuleDecision  contracts.RuleAgentOutput                `json:"rule_decision"`
	Runbook       contracts.Runbook                        `json:"runbook"`
	Diagnostics   map[string]contracts.ObservabilityOutput `json:"diagnostics"`
	Actions       map[string]contracts.ExecOutput          `json:"actions"`
	CompletedAt   time.Time                                `json:"completed_at"`
	Failed        bool                                     `json:"failed"`
	FailureReason string                                   `json:"failure_reason,omitempty"`
}

// Signal payload for approvals
type ApprovalSignal struct {
	StepID   string `json:"step_id"`
	Approved bool   `json:"approved"`
	Approver string `json:"approver"`
	Comment  string `json:"comment,omitempty"`
}

// RunbookAutomationWorkflow orchestrates the end-to-end flow.
//
// IMPORTANT: This workflow is started by `control-orchestrator` `/run`, which always passes `contracts.Task`.
// We deterministically map the task into a strongly-typed `RunbookAutomationInput` without JSON-marshal/unmarshal
// (map iteration order would be nondeterministic inside a Temporal workflow).
func RunbookAutomationWorkflow(ctx workflow.Context, task contracts.Task) (*RunbookAutomationResult, error) {
	logger := workflow.GetLogger(ctx)

	in := RunbookAutomationInput{
		IncidentID: stringFromAny(task.Input["incident_id"]),
		Service:    stringFromAny(task.Input["service"]),
		Symptom:    stringFromAny(task.Input["symptom"]),
		Metadata:   copyStringMap(task.Metadata),
	}

	// Propagate a stable workflow/task identifier into runbook input metadata so downstream agents
	// can build idempotency keys (task_id + step_id).
	info := workflow.GetInfo(ctx)
	if in.Metadata == nil {
		in.Metadata = map[string]string{}
	}
	if in.Metadata["workflow_id"] == "" {
		in.Metadata["workflow_id"] = info.WorkflowExecution.ID
	}

	result := &RunbookAutomationResult{
		IncidentID:  in.IncidentID,
		Service:     in.Service,
		Symptom:     in.Symptom,
		Diagnostics: make(map[string]contracts.ObservabilityOutput),
		Actions:     make(map[string]contracts.ExecOutput),
	}

	// Default activity options
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// 1. Call Rule Agent
	var ruleOut contracts.RuleAgentOutput
	if err := workflow.ExecuteActivity(ctx, CallRuleAgentActivity, in).Get(ctx, &ruleOut); err != nil {
		logger.Error("CallRuleAgentActivity failed", "error", err)
		result.Failed = true
		result.FailureReason = "rule_agent_error"
		return result, err
	}
	result.RuleDecision = ruleOut

	// 2. Call Runbook Agent
	var rbOut contracts.RunbookAgentOutput
	if err := workflow.ExecuteActivity(ctx, CallRunbookAgentActivity, ruleOut, in).Get(ctx, &rbOut); err != nil {
		logger.Error("CallRunbookAgentActivity failed", "error", err)
		result.Failed = true
		result.FailureReason = "runbook_agent_error"
		return result, err
	}
	result.Runbook = rbOut.Runbook

	// 3. Run diagnostics (Observability Agent)
	for _, step := range rbOut.Runbook.Diagnostics {
		logger.Info("Running diagnostic step", "step_id", step.ID, "name", step.Name)

		var obsOut contracts.ObservabilityOutput
		if err := workflow.ExecuteActivity(ctx, CallObservabilityAgentActivity, step, in).Get(ctx, &obsOut); err != nil {
			logger.Error("CallObservabilityAgentActivity failed", "step_id", step.ID, "error", err)
			// we don't necessarily fail the entire workflow here
			continue
		}
		result.Diagnostics[step.ID] = obsOut

		// Optional gate: if a diagnostic step defines a threshold, the workflow can stop early
		// (e.g., "only proceed if error rate is below X").
		if ok, why := evalDiagnosticGate(step, obsOut); !ok {
			logger.Error("Diagnostic gate failed", "step_id", step.ID, "reason", why)
			result.Failed = true
			result.FailureReason = "diagnostic_gate_failed: " + why
			result.CompletedAt = workflow.Now(ctx)
			return result, nil
		}
	}

	// 4. Execute actions (Exec Agent) with approval when required
	approvalCh := workflow.GetSignalChannel(ctx, ApprovalSignalName)

	for _, step := range rbOut.Runbook.Actions {
		logger.Info("Processing action step", "step_id", step.ID, "name", step.Name)

		skip := false
		if step.RequiresApproval || ruleOut.RequiresHumanApproval {
			logger.Info("Waiting for approval", "step_id", step.ID)

			var approved bool
			for !approved {
				var sig ApprovalSignal
				approvalCh.Receive(ctx, &sig)
				if sig.StepID != step.ID {
					logger.Info("Received approval for different step, ignoring", "expected", step.ID, "got", sig.StepID)
					continue
				}
				if !sig.Approved {
					logger.Info("Action not approved", "step_id", step.ID, "approver", sig.Approver)
					// skip this step but continue workflow
					skip = true
					break
				}
				logger.Info("Action approved", "step_id", step.ID, "approver", sig.Approver)
				approved = true
			}
		}
		if skip {
			continue
		}

		// Call Exec Agent
		var execOut contracts.ExecOutput
		if err := workflow.ExecuteActivity(ctx, CallExecAgentActivity, step, in).Get(ctx, &execOut); err != nil {
			logger.Error("CallExecAgentActivity failed", "step_id", step.ID, "error", err)
			result.Actions[step.ID] = contracts.ExecOutput{
				Success: false,
				Details: err.Error(),
			}
			continue
		}
		result.Actions[step.ID] = execOut
	}

	result.CompletedAt = workflow.Now(ctx)
	return result, nil
}

func evalDiagnosticGate(step contracts.RunbookStep, out contracts.ObservabilityOutput) (bool, string) {
	// Step.Input supports (all optional):
	// - gate_signal: "value" (key in out.Signals)
	// - gate_op: "<" | "<=" | ">" | ">=" | "==" | "!="
	// - gate_threshold: number (float) or string parseable as float
	gateSignal := ""
	if v, ok := step.Input["gate_signal"]; ok && v != nil {
		gateSignal = strings.TrimSpace(fmt.Sprint(v))
	}
	gateOp := ""
	if v, ok := step.Input["gate_op"]; ok && v != nil {
		gateOp = strings.TrimSpace(fmt.Sprint(v))
	}
	if gateSignal == "" || gateOp == "" {
		return true, ""
	}

	rawThr, ok := step.Input["gate_threshold"]
	if !ok {
		return false, "gate_threshold_missing"
	}
	thr, err := parseFloatAny(rawThr)
	if err != nil {
		return false, "gate_threshold_invalid"
	}
	if out.Degraded {
		return false, "observability_degraded"
	}
	if out.Signals == nil {
		return false, "signals_missing"
	}
	v, ok := out.Signals[gateSignal]
	if !ok {
		return false, "signal_not_found:" + gateSignal
	}
	if compareFloat(v, gateOp, thr) {
		return true, ""
	}
	return false, fmt.Sprintf("%s %s %v (got=%v)", gateSignal, gateOp, thr, v)
}

func parseFloatAny(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	return s
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func compareFloat(v float64, op string, thr float64) bool {
	switch op {
	case "<":
		return v < thr
	case "<=":
		return v <= thr
	case ">":
		return v > thr
	case ">=":
		return v >= thr
	case "==":
		return v == thr
	case "!=":
		return v != thr
	default:
		// Unknown op => fail closed
		return false
	}
}
