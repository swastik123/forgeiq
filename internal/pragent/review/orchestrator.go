package review

import (
	"context"
	"fmt"
	"strings"

	"forgeiq/internal/pragent"
	"forgeiq/internal/pragent/diff"
	"forgeiq/internal/pragent/findings"
	"forgeiq/internal/pragent/lang"
)

type Input struct {
	PRTitle       string
	PRDescription string
	DiffText      string
	RepoSummary   string
	PolicyYAML    string
}

type Reviewer interface {
	ReviewDiff(ctx context.Context, in Input, patch diff.Patch) ([]pragent.Finding, string, error)
}

type ToolRunner interface {
	// Optional: for future local checkout based checks. Return findings or evidence summary.
	Run(ctx context.Context, repoDir string, language pragent.Language, patch diff.Patch) ([]pragent.Finding, map[string]any, error)
}

type Orchestrator struct {
	Reviewer Reviewer
	Runners  []ToolRunner
	Config   pragent.ReviewConfig
}

func (o *Orchestrator) Review(ctx context.Context, in Input) ([]pragent.Finding, map[string]any, error) {
	if o == nil || o.Reviewer == nil {
		return nil, nil, fmt.Errorf("review orchestrator: reviewer is nil")
	}
	patch, err := diff.ParseUnified(in.DiffText)
	if err != nil {
		return nil, nil, err
	}

	// Attach language hints per file (for filtering/prompting downstream)
	langs := map[string]pragent.Language{}
	for _, fp := range patch.Files {
		path := fp.NewPath
		if strings.TrimSpace(path) == "" {
			path = fp.OldPath
		}
		langs[path] = lang.Detect(path)
	}

	fs, summary, err := o.Reviewer.ReviewDiff(ctx, in, patch)
	if err != nil {
		return nil, nil, err
	}

	// Ensure fingerprints + IDs
	for i := range fs {
		if fs[i].Fingerprint == "" {
			fs[i].Fingerprint = findings.Fingerprint(fs[i])
		}
		if fs[i].ID == "" {
			fs[i].ID = "f-" + fs[i].Fingerprint[:12]
		}
		// If language missing, infer from path
		if fs[i].Refs == nil {
			fs[i].Refs = map[string]string{}
		}
		if fs[i].Path != "" && fs[i].Refs["language"] == "" {
			fs[i].Refs["language"] = string(langs[fs[i].Path])
		}
	}

	fs = findings.DedupeAndCap(fs, o.Config.MaxFindingsTotal, o.Config.MaxFindingsPerFile)

	meta := map[string]any{
		"summary":   summary,
		"file_lang": langs,
	}
	return fs, meta, nil
}
