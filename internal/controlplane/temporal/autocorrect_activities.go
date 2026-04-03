package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"forgeiq/internal/autocorrect"
	"forgeiq/internal/controlplane/contracts"
	mcpclient "forgeiq/internal/transport/mcp"

	"go.temporal.io/sdk/activity"
)

// EvaluateAndStoreCorrections runs a deterministic evaluator and stores resulting correction artifacts
// in governed canonical memory via MCP memory.propose:v1.
//
// This keeps agents stateless and makes “learning” workflow-controlled and auditable.
func (a *Activities) EvaluateAndStoreCorrections(ctx context.Context, task contracts.Task, phase string, agentType string, toolName string, outputs map[string]any, policy contracts.PolicyDecision) (autocorrect.EvaluateResult, error) {
	if a == nil || a.Config == nil {
		return autocorrect.EvaluateResult{}, fmt.Errorf("activities not configured")
	}
	if !policy.Allowed {
		return autocorrect.EvaluateResult{Verdict: autocorrect.VerdictStop}, nil
	}
	// Only store corrections if the workflow is permitted to write memory.
	if !contains(policy.Scopes, "memory:write") {
		return autocorrect.EvaluateResult{
			Verdict: autocorrect.VerdictProceed,
			Debug: map[string]any{
				"note": "memory:write scope missing; corrections not stored",
			},
		}, nil
	}

	req := autocorrect.EvaluateRequest{
		TenantID:  task.TenantID,
		TaskID:    task.ID,
		TaskType:  task.Type,
		Phase:     strings.TrimSpace(phase),
		AgentType: strings.TrimSpace(agentType),
		ToolName:  strings.TrimSpace(toolName),
		Outputs:   outputs,
	}
	res := autocorrect.Evaluate(req)

	if len(res.Corrections) == 0 {
		return res, nil
	}

	client := mcpclient.NewClientFromConfig(a.Config)
	stored := 0
	for _, c := range res.Corrections {
		payloadBytes, _ := json.Marshal(c)
		var payload map[string]any
		_ = json.Unmarshal(payloadBytes, &payload)

		// Use memory.propose to store governed correction artifacts. Handler will compute confidence/TTL safely.
		args := map[string]any{
			"tenant_id":     defaultTenant(task.TenantID),
			"record_type":   "correction_artifact",
			"payload":       payload,
			"evidence_refs": c.EvidenceRefs,
			"confidence":    c.SuggestedConfidence,
			// Sensitivity is controller-controlled; default internal.
			"sensitivity": "internal",
		}
		if c.ExpiresAt != nil {
			args["expires_at"] = c.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}

		_, err := client.CallTool(ctx, "memory.propose", "v1", args)
		if err != nil {
			activity.GetLogger(ctx).Warn("Failed to store correction artifact", "err", err)
			continue
		}
		stored++
	}
	if res.Debug == nil {
		res.Debug = map[string]any{}
	}
	res.Debug["stored"] = stored
	return res, nil
}

// FetchCorrections retrieves correction artifacts relevant to this task and phase.
// It performs Stage A (search IDs) and Stage B (get redacted records) using MCP memory tools.
func (a *Activities) FetchCorrections(ctx context.Context, task contracts.Task, query string, topK int, policy contracts.PolicyDecision) ([]map[string]any, error) {
	if a == nil || a.Config == nil {
		return nil, fmt.Errorf("activities not configured")
	}
	if !policy.Allowed {
		return nil, nil
	}
	if !contains(policy.Scopes, "memory:read") {
		return nil, nil
	}

	client := mcpclient.NewClientFromConfig(a.Config)
	searchArgs := map[string]any{
		"tenant_id": defaultTenant(task.TenantID),
		"query":     strings.TrimSpace(query),
		"top_k":     topK,
		// Conservative filters (controller can tune).
		"exclude_expired": true,
		"max_sensitivity": "internal",
	}
	searchOut, err := client.CallTool(ctx, "memory.search", "v1", searchArgs)
	if err != nil {
		return nil, err
	}

	rawMatches, _ := searchOut["matches"].([]any)
	out := make([]map[string]any, 0, len(rawMatches))
	for _, m := range rawMatches {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		id, _ := mm["record_id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		getArgs := map[string]any{
			"tenant_id":       defaultTenant(task.TenantID),
			"record_id":       id,
			"max_sensitivity": "internal",
			"include_payload": true,
		}
		getOut, gerr := client.CallTool(ctx, "memory.get", "v1", getArgs)
		if gerr != nil {
			continue
		}
		rec, ok := getOut["record"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func defaultTenant(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "default"
	}
	return t
}

