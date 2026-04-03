package prompts

import (
	_ "embed"
	"fmt"
	"strings"
)

// Versioned prompt registry (embedded in the binary).
// Keep IDs stable and bump version directories to roll prompts safely.

//go:embed pr_review/v1/system.txt
var prReviewV1System string

//go:embed pr_review/v1/user_template.txt
var prReviewV1UserTemplate string

//go:embed pr_review/v2/system.txt
var prReviewV2System string

//go:embed pr_review/v2/user_template.txt
var prReviewV2UserTemplate string

type Prompt struct {
	ID           string
	Version      string
	System       string
	UserTemplate string
}

func Get(id string, version string) (Prompt, error) {
	id = strings.TrimSpace(id)
	version = strings.TrimSpace(version)
	if id == "" {
		id = "pr_review"
	}
	if version == "" {
		version = "v2"
	}

	switch id + ":" + version {
	case "pr_review:v2":
		return Prompt{
			ID:           "pr_review",
			Version:      "v2",
			System:       strings.TrimSpace(prReviewV2System),
			UserTemplate: strings.TrimSpace(prReviewV2UserTemplate),
		}, nil
	case "pr_review:v1":
		return Prompt{
			ID:           "pr_review",
			Version:      "v1",
			System:       strings.TrimSpace(prReviewV1System),
			UserTemplate: strings.TrimSpace(prReviewV1UserTemplate),
		}, nil
	default:
		return Prompt{}, fmt.Errorf("unknown prompt %q version %q", id, version)
	}
}

func RenderUser(tpl string, title string, desc string, diff string, repoSummary string, policyYAML string) string {
	s := tpl
	s = strings.ReplaceAll(s, "{{TITLE}}", title)
	s = strings.ReplaceAll(s, "{{DESCRIPTION}}", desc)
	s = strings.ReplaceAll(s, "{{DIFF}}", diff)
	s = strings.ReplaceAll(s, "{{REPO_SUMMARY}}", repoSummary)
	s = strings.ReplaceAll(s, "{{POLICY_YAML}}", policyYAML)
	return s
}
