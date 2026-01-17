package temporal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// A2AHTTPClient calls external agents over the shared A2A contract.
// Endpoints are configured via env:
// - RULE_AGENT_URL
// - RUNBOOK_AGENT_URL
// - OBS_AGENT_URL
// - EXEC_AGENT_URL
type A2AHTTPClient struct {
	httpClient *http.Client
	RuleURL    string
	RunbookURL string
	ObsURL     string
	ExecURL    string
}

func NewA2AHTTPClient() *A2AHTTPClient {
	return &A2AHTTPClient{
		httpClient: &http.Client{},
		RuleURL:    os.Getenv("RULE_AGENT_URL"),
		RunbookURL: os.Getenv("RUNBOOK_AGENT_URL"),
		ObsURL:     os.Getenv("OBS_AGENT_URL"),
		ExecURL:    os.Getenv("EXEC_AGENT_URL"),
	}
}

func (c *A2AHTTPClient) call(ctx context.Context, baseURL string, req *contracts.A2ATaskRequest, resp *contracts.A2ATaskResponse) error {
	if baseURL == "" {
		return fmt.Errorf("baseURL not configured")
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/task", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("agent returned status %d", httpResp.StatusCode)
	}
	if err := json.NewDecoder(httpResp.Body).Decode(resp); err != nil {
		return err
	}
	if resp.Status == "error" {
		return fmt.Errorf("agent error: %s", resp.Error)
	}
	return nil
}

// ---- Activities called by the workflow ----

func CallRuleAgentActivity(ctx context.Context, in RunbookAutomationInput) (contracts.RuleAgentOutput, error) {
	client := NewA2AHTTPClient()

	payload := contracts.RuleAgentInput{
		IncidentID: in.IncidentID,
		Service:    in.Service,
		Symptom:    in.Symptom,
		Metadata:   in.Metadata,
	}

	inputBytes, _ := json.Marshal(payload)
	req := contracts.A2ATaskRequest{
		Agent:    contracts.AgentKindRule,
		TaskType: "evaluate_rules",
		Input:    inputBytes,
		Context: map[string]any{
			"incident_id": in.IncidentID,
			"service":     in.Service,
			"symptom":     in.Symptom,
		},
	}

	var resp contracts.A2ATaskResponse
	if err := client.call(ctx, client.RuleURL, &req, &resp); err != nil {
		return contracts.RuleAgentOutput{}, err
	}

	var out contracts.RuleAgentOutput
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		return contracts.RuleAgentOutput{}, err
	}
	return out, nil
}

func CallRunbookAgentActivity(ctx context.Context, ruleOut contracts.RuleAgentOutput, in RunbookAutomationInput) (contracts.RunbookAgentOutput, error) {
	client := NewA2AHTTPClient()

	payload := contracts.RunbookAgentInput{
		RunbookID:  ruleOut.RunbookID,
		Service:    in.Service,
		IncidentID: in.IncidentID,
	}
	inputBytes, _ := json.Marshal(payload)

	req := contracts.A2ATaskRequest{
		Agent:    contracts.AgentKindRunbook,
		TaskType: "get_runbook",
		Input:    inputBytes,
		Context: map[string]any{
			"severity": ruleOut.Severity,
		},
	}

	var resp contracts.A2ATaskResponse
	if err := client.call(ctx, client.RunbookURL, &req, &resp); err != nil {
		return contracts.RunbookAgentOutput{}, err
	}
	var out contracts.RunbookAgentOutput
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		return contracts.RunbookAgentOutput{}, err
	}
	return out, nil
}

func CallObservabilityAgentActivity(ctx context.Context, step contracts.RunbookStep, in RunbookAutomationInput) (contracts.ObservabilityOutput, error) {
	client := NewA2AHTTPClient()

	params := map[string]string{}
	// Allow runbooks to pass params either nested under input.params or as top-level keys.
	if pm, ok := step.Input["params"].(map[string]any); ok && pm != nil {
		for k, v := range pm {
			params[k] = strings.TrimSpace(fmt.Sprint(v))
		}
	}
	if pm, ok := step.Input["params"].(map[string]string); ok && pm != nil {
		for k, v := range pm {
			params[k] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"start", "end", "step", "range", "range_seconds", "trend"} {
		if v, ok := step.Input[k]; ok && v != nil {
			params[k] = strings.TrimSpace(fmt.Sprint(v))
		}
	}

	obsIn := contracts.ObservabilityInput{
		QueryType: fmt.Sprint(step.Input["query_type"]),
		Query:     fmt.Sprint(step.Input["query"]),
		Params:    params,
	}
	obsBytes, _ := json.Marshal(obsIn)

	req := contracts.A2ATaskRequest{
		Agent:         contracts.AgentKindObservability,
		TaskType:      step.TaskType,
		Input:         obsBytes,
		CorrelationID: step.ID,
		Context: map[string]any{
			"incident_id": in.IncidentID,
			"service":     in.Service,
		},
	}

	var resp contracts.A2ATaskResponse
	if err := client.call(ctx, client.ObsURL, &req, &resp); err != nil {
		return contracts.ObservabilityOutput{}, err
	}
	var out contracts.ObservabilityOutput
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		return contracts.ObservabilityOutput{}, err
	}
	return out, nil
}

func CallExecAgentActivity(ctx context.Context, step contracts.RunbookStep, in RunbookAutomationInput) (contracts.ExecOutput, error) {
	client := NewA2AHTTPClient()

	execIn := contracts.ExecInput{
		ActionType: fmt.Sprint(step.Input["action_type"]),
		Params:     mapStringAnyToString(step.Input),
	}
	execBytes, _ := json.Marshal(execIn)

	workflowID := ""
	if in.Metadata != nil {
		workflowID = in.Metadata["workflow_id"]
	}
	idemKey := ""
	if workflowID != "" && step.ID != "" {
		idemKey = workflowID + ":" + step.ID
	}

	req := contracts.A2ATaskRequest{
		Agent:          contracts.AgentKindExec,
		TaskType:       step.TaskType,
		Input:          execBytes,
		CorrelationID:  step.ID,
		IdempotencyKey: idemKey,
		Context: map[string]any{
			"incident_id": in.IncidentID,
			"service":     in.Service,
			"workflow_id": workflowID,
			"step_id":     step.ID,
		},
	}

	var resp contracts.A2ATaskResponse
	if err := client.call(ctx, client.ExecURL, &req, &resp); err != nil {
		return contracts.ExecOutput{}, err
	}
	var out contracts.ExecOutput
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		return contracts.ExecOutput{}, err
	}
	return out, nil
}

func mapStringAnyToString(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = fmt.Sprint(v)
	}
	return out
}
