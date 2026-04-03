package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"forgeiq/internal/pragent"
)

const (
	summaryMarker = "<!-- forgeiq-pr-agent:summary -->"
	fpMarkerPref  = "<!-- forgeiq-pr-agent:fingerprint="
)

type PublishConfig struct {
	PublishInline  bool
	PublishSummary bool
	MaxInline      int
}

type PublishResult struct {
	SummaryCommentID int            `json:"summary_comment_id,omitempty"`
	InlineCommentIDs map[string]int `json:"inline_comment_ids,omitempty"` // fingerprint -> comment id
	Updated          int            `json:"updated"`
	Created          int            `json:"created"`
}

func Publish(ctx context.Context, c *Client, workspace, repoSlug string, prID int, findings []pragent.Finding, summary string, cfg PublishConfig) (PublishResult, error) {
	if c == nil {
		return PublishResult{}, fmt.Errorf("bitbucket publisher: client is nil")
	}

	existing, err := c.ListComments(ctx, workspace, repoSlug, prID, 100)
	if err != nil {
		return PublishResult{}, err
	}

	// Index existing comments by fingerprint + summary marker.
	fpToCommentID := map[string]int{}
	summaryID := 0
	for _, cm := range existing {
		raw := cm.Content.Raw
		if strings.Contains(raw, summaryMarker) {
			summaryID = cm.ID
		}
		fp := extractFingerprint(raw)
		if fp != "" {
			fpToCommentID[fp] = cm.ID
		}
	}

	res := PublishResult{InlineCommentIDs: map[string]int{}}

	if cfg.PublishSummary {
		body := renderSummary(summary, findings)
		if summaryID > 0 {
			upd, err := c.UpdateComment(ctx, workspace, repoSlug, prID, summaryID, body)
			if err != nil {
				return res, err
			}
			res.SummaryCommentID = upd.ID
			res.Updated++
		} else {
			created, err := c.CreateComment(ctx, workspace, repoSlug, prID, body, "", 0, 0)
			if err != nil {
				return res, err
			}
			res.SummaryCommentID = created.ID
			res.Created++
		}
	}

	if cfg.PublishInline {
		max := cfg.MaxInline
		if max <= 0 {
			max = 50
		}
		n := 0
		for _, f := range findings {
			if n >= max {
				break
			}
			if strings.TrimSpace(f.Path) == "" || f.Line <= 0 {
				continue
			}
			fp := f.Fingerprint
			if fp == "" {
				continue
			}
			fileType := ""
			if f.Refs != nil {
				fileType = strings.ToUpper(strings.TrimSpace(f.Refs["file_type"]))
			}
			inlineTo := 0
			inlineFrom := 0
			if fileType == "SOURCE" {
				inlineFrom = f.Line
			} else {
				inlineTo = f.Line
			}
			body := renderInline(f)
			if id, ok := fpToCommentID[fp]; ok && id > 0 {
				upd, err := c.UpdateComment(ctx, workspace, repoSlug, prID, id, body)
				if err != nil {
					return res, err
				}
				res.InlineCommentIDs[fp] = upd.ID
				res.Updated++
			} else {
				created, err := c.CreateComment(ctx, workspace, repoSlug, prID, body, f.Path, inlineTo, inlineFrom)
				if err != nil {
					// If inline placement fails (diff drift), skip to keep agent robust.
					continue
				}
				res.InlineCommentIDs[fp] = created.ID
				res.Created++
			}
			n++
		}
	}

	return res, nil
}

func renderSummary(summary string, fs []pragent.Finding) string {
	sb := strings.Builder{}
	sb.WriteString(summaryMarker + "\n")
	sb.WriteString("## ForgeIQ PR Review\n\n")
	if strings.TrimSpace(summary) != "" {
		sb.WriteString("**Summary:** " + strings.TrimSpace(summary) + "\n\n")
	}
	if len(fs) == 0 {
		sb.WriteString("No findings.\n")
		return sb.String()
	}
	sb.WriteString("### Findings (" + fmt.Sprint(len(fs)) + ")\n\n")
	for _, f := range fs {
		loc := ""
		if f.Path != "" && f.Line > 0 {
			loc = fmt.Sprintf(" (`%s:%d`)", f.Path, f.Line)
		} else if f.Path != "" {
			loc = fmt.Sprintf(" (`%s`)", f.Path)
		}
		sb.WriteString(fmt.Sprintf("- **%s** %s%s: %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, loc, strings.TrimSpace(f.Message)))
	}
	return sb.String()
}

func renderInline(f pragent.Finding) string {
	sb := strings.Builder{}
	sb.WriteString(fpMarkerPref + f.Fingerprint + " -->\n")
	ft := ""
	if f.Refs != nil {
		ft = strings.ToUpper(strings.TrimSpace(f.Refs["file_type"]))
	}
	if ft != "" {
		sb.WriteString(fmt.Sprintf("**%s** (%s) [%s]\n\n", strings.ToUpper(string(f.Severity)), f.RuleID, ft))
	} else {
		sb.WriteString(fmt.Sprintf("**%s** (%s)\n\n", strings.ToUpper(string(f.Severity)), f.RuleID))
	}
	sb.WriteString(strings.TrimSpace(f.Message))
	if strings.TrimSpace(f.Suggestion) != "" {
		sb.WriteString("\n\n**Suggestion:** " + strings.TrimSpace(f.Suggestion))
	}
	return sb.String()
}

func extractFingerprint(raw string) string {
	i := strings.Index(raw, fpMarkerPref)
	if i < 0 {
		return ""
	}
	s := raw[i+len(fpMarkerPref):]
	j := strings.Index(s, " -->")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(s[:j])
}
