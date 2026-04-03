		package autocorrect

		import (
			"fmt"
			"strings"
			"time"
		)

		// Evaluate is a small, deterministic evaluator that turns violations into structured correction artifacts.
		// This is intentionally conservative and is meant as an extension point (swap in a richer evaluator later).
		func Evaluate(req EvaluateRequest) EvaluateResult {
			res := EvaluateResult{
				Verdict: VerdictProceed,
				Debug: map[string]any{
					"phase": req.Phase,
				},
			}

			// Detect obvious failures in outputs.
			// Convention: workflow stores tool errors as strings under keys like "step_error_<id>".
			for k, v := range req.Outputs {
				if !strings.Contains(k, "error") {
					continue
				}
				msg, ok := v.(string)
				if !ok || strings.TrimSpace(msg) == "" {
					continue
				}

				// Produce a correction artifact: “don’t repeat this error; adjust plan/tool args/policy”.
				title := "Tool execution error"
				if req.ToolName != "" {
					title = "Tool error: " + req.ToolName
				}
				ttl := time.Now().UTC().Add(30 * 24 * time.Hour)

				res.Corrections = append(res.Corrections, CorrectionArtifact{
					Kind:          "guardrail",
					Title:         title,
					Violation:     fmt.Sprintf("%s: %s", k, strings.TrimSpace(msg)),
					Recommendation: "Avoid repeating this failure. If it is a permissions/scope error, request approval or adjust the plan to use allowed tools/scopes. If it is a schema error, fix the tool call arguments to match the tool schema.",
					Applicability: Applicability{
						TenantID:  req.TenantID,
						TaskTypes: nonEmpty([]string{req.TaskType}),
						AgentType: req.AgentType,
						ToolName:  req.ToolName,
					},
					EvidenceRefs: nonEmpty([]string{
						"task:" + strings.TrimSpace(req.TaskID),
						"phase:" + strings.TrimSpace(req.Phase),
					}),
					ExpiresAt:           &ttl,
					SuggestedConfidence: 0.6,
				})

				// If errors occurred, suggest replan (workflow can ignore if it handled the error).
				res.Verdict = VerdictReplan
			}

			// If the workflow has a low-confidence plan signal, add a correction.
			if v, ok := req.Outputs["plan_confidence"].(float64); ok && v > 0 && v < 0.35 {
				ttl := time.Now().UTC().Add(14 * 24 * time.Hour)
				res.Corrections = append(res.Corrections, CorrectionArtifact{
					Kind:          "contract",
					Title:         "Low plan confidence",
					Violation:     fmt.Sprintf("plan_confidence too low: %.3f", v),
					Recommendation: "Replan with narrower scope and stronger evidence requirements. Prefer read-only verification steps before any write actions.",
					Applicability: Applicability{
						TenantID:  req.TenantID,
						TaskTypes: nonEmpty([]string{req.TaskType}),
						AgentType: "decision",
					},
					EvidenceRefs: nonEmpty([]string{
						"task:" + strings.TrimSpace(req.TaskID),
						"phase:" + strings.TrimSpace(req.Phase),
					}),
					ExpiresAt:           &ttl,
					SuggestedConfidence: 0.55,
				})
			}

			return res
		}

		func nonEmpty(in []string) []string {
			out := make([]string, 0, len(in))
			for _, s := range in {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
			return out
		}

