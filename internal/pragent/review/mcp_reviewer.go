package review

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"forgeiq/internal/agent/strategy"
	"forgeiq/internal/config"
	"forgeiq/internal/pragent"
	"forgeiq/internal/pragent/diff"
	"forgeiq/internal/pragent/prompts"
	mcpclient "forgeiq/internal/transport/mcp"
)

// MCPReviewer uses MCP tool `llm.chat:v1` to perform a strictly diff-scoped review.
// It does NOT attempt to fetch full repo context.

type MCPReviewer struct {
	MCPBaseURL string
	Model      strategy.ModelConfig
	Meta       map[string]any
}

func (r *MCPReviewer) ReviewDiff(ctx context.Context, in Input, patch diff.Patch) ([]pragent.Finding, string, error) {
	tooler := mcpclient.NewClientWithConfig(mcpclient.ClientConfig{
		BaseURL: strings.TrimRight(strings.TrimSpace(r.MCPBaseURL), "/"),
		Timeout: 40 * time.Second,
	})
	if tooler.BaseURL == "" {
		// Allow config fallback if caller didn't pass a base URL.
		cfg := config.Load()
		tooler = mcpclient.NewClientFromConfig(cfg)
	}

	model := r.Model
	if strings.TrimSpace(model.Provider) == "" {
		model.Provider = "mcp"
	}
	if strings.TrimSpace(model.Model) == "" {
		model.Model = "gpt-4.1-mini"
	}

	prPrompt, err := prompts.Get("pr_review", "v2")
	if err != nil {
		return nil, "", err
	}
	sys := prPrompt.System
	user := prompts.RenderUser(prPrompt.UserTemplate, in.PRTitle, in.PRDescription, in.DiffText, in.RepoSummary, in.PolicyYAML)

	// Prefer llm.chat:v2 (typed result + schema enforcement). Fall back to v1.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reviewComments": map[string]any{"type": "array"},
			"summary":        map[string]any{"type": "object"},
		},
		"required": []any{"reviewComments", "summary"},
	}

	argsV2 := map[string]any{
		"messages": []strategy.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		"tools": []any{},
		"model": model,
		"meta": mergeMeta(r.Meta, map[string]any{
			"prompt_id":      prPrompt.ID,
			"prompt_version": prPrompt.Version,
		}),
		"response_format": map[string]any{
			"type":   "json_schema",
			"name":   "pr_review",
			"schema": schema,
		},
		"retries": map[string]any{
			"max_attempts": 2,
		},
		"strict": true,
	}

	out, err := tooler.CallTool(ctx, "llm.chat", "v2", argsV2)
	b, _ := json.Marshal(out)
	log.Printf("llm.chat v2 raw: %s", string(b))
	if err == nil {
		if res, ok := out["result"].(map[string]any); ok && res != nil {
			// Parse typed result into struct
			b, _ := json.Marshal(res)
			var parsed prReviewResponse
			if err := json.Unmarshal(b, &parsed); err == nil {
				fs, sum := mapPRReviewResponse(parsed)
				return fs, sum, nil
			}
		}
	}

	// Fallback: v1 string JSON output
	argsV1 := map[string]any{
		"messages": []strategy.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		"tools": []any{},
		"model": model,
		"meta": mergeMeta(r.Meta, map[string]any{
			"prompt_id":      prPrompt.ID,
			"prompt_version": prPrompt.Version,
		}),
	}

	out1, err1 := tooler.CallTool(ctx, "llm.chat", "v1", argsV1)
	if err1 != nil {
		// Return original v2 error if v2 existed but parsing failed; else v1 error.
		if err != nil {
			return nil, "", err
		}
		return nil, "", err1
	}
	raw, _ := out1["final_answer"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", fmt.Errorf("llm returned empty final_answer")
	}

	var parsed prReviewResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		j := extractJSONObject(raw)
		if j == "" {
			return nil, "", fmt.Errorf("failed to parse llm JSON: %w", err)
		}
		if err2 := json.Unmarshal([]byte(j), &parsed); err2 != nil {
			return nil, "", fmt.Errorf("failed to parse llm JSON (extracted): %w", err2)
		}
	}
	fs, sum := mapPRReviewResponse(parsed)
	return fs, sum, nil
}

func extractJSONObject(s string) string {
	// Very small helper: find first '{' and last '}'.
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return ""
}

// ---- PR review v2 schema ----

type prReviewResponse struct {
	ReviewComments []struct {
		FilePath       string `json:"filePath"`
		LineNumber     int    `json:"lineNumber"`
		FileType       string `json:"fileType"` // DESTINATION|SOURCE
		Severity       string `json:"severity"` // BLOCKER|HIGH|MEDIUM|LOW|NIT
		Category       string `json:"category"` // SECURITY|PERFORMANCE|...
		Title          string `json:"title"`
		Comment        string `json:"comment"`
		SuggestedPatch string `json:"suggestedPatch,omitempty"`
	} `json:"reviewComments"`
	Summary struct {
		Overall       string   `json:"overall"`
		Risk          string   `json:"risk"` // LOW|MEDIUM|HIGH
		AreasReviewed []string `json:"areasReviewed"`
		AreasSkipped  []string `json:"areasSkipped"`
	} `json:"summary"`
}

func mapPRReviewResponse(r prReviewResponse) ([]pragent.Finding, string) {
	fs := make([]pragent.Finding, 0, len(r.ReviewComments))
	for _, c := range r.ReviewComments {
		f := pragent.Finding{
			RuleID:   strings.TrimSpace(c.Category),
			Severity: mapSeverity(c.Severity),
			Category: strings.ToLower(strings.TrimSpace(c.Category)),
			Path:     strings.TrimSpace(c.FilePath),
			Line:     c.LineNumber,
			Message:  strings.TrimSpace(c.Title + ": " + c.Comment),
		}
		if strings.TrimSpace(c.SuggestedPatch) != "" {
			f.Suggestion = strings.TrimSpace(c.SuggestedPatch)
		}
		if f.Refs == nil {
			f.Refs = map[string]string{}
		}
		if strings.TrimSpace(c.FileType) != "" {
			f.Refs["file_type"] = strings.TrimSpace(strings.ToUpper(c.FileType))
		}
		fs = append(fs, f)
	}
	sum := strings.TrimSpace(r.Summary.Overall)
	if strings.TrimSpace(r.Summary.Risk) != "" {
		sum = strings.TrimSpace(sum + " (risk=" + strings.TrimSpace(strings.ToUpper(r.Summary.Risk)) + ")")
	}
	return fs, sum
}

func mapSeverity(s string) pragent.Severity {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BLOCKER":
		return pragent.SeverityBlocker
	case "HIGH":
		return pragent.SeverityHigh
	case "MEDIUM":
		return pragent.SeverityMedium
	case "LOW":
		return pragent.SeverityLow
	case "NIT":
		return pragent.SeverityNit
	default:
		return pragent.SeverityLow
	}
}

func mergeMeta(a map[string]any, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
