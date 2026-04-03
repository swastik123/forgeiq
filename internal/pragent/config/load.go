package config

import (
	"context"
	"fmt"

	"forgeiq/internal/pragent/bitbucket"

	"gopkg.in/yaml.v3"
)

type LoadResult struct {
	File      File   // effective config
	Source    string // "repo:.forgeiq/pr_reviewer.yaml" | "repo:pr_reviewer.yaml" | "default"
	RawSHA256 string // reserved for future
}

func LoadFromBitbucket(ctx context.Context, bb *bitbucket.Client, workspace, repoSlug, ref string) (LoadResult, error) {
	def, err := DefaultFile()
	if err != nil {
		return LoadResult{}, err
	}
	if bb == nil {
		return LoadResult{File: def, Source: "default"}, nil
	}

	paths := []string{".forgeiq/pr_reviewer.yaml", "pr_reviewer.yaml"}
	for _, p := range paths {
		b, err := bb.GetFile(ctx, workspace, repoSlug, ref, p)
		if err != nil {
			if bitbucket.IsNotFound(err) {
				continue
			}
			// Any other error: fail open to default but surface in Source.
			return LoadResult{File: def, Source: "default"}, nil
		}
		var f File
		if err := yaml.Unmarshal(b, &f); err != nil {
			// invalid yaml => fallback
			return LoadResult{File: def, Source: "default"}, nil
		}
		if err := f.Validate(); err != nil {
			return LoadResult{File: def, Source: "default"}, nil
		}
		return LoadResult{File: f, Source: "repo:" + p}, nil
	}

	return LoadResult{File: def, Source: "default"}, nil
}

func (r LoadResult) String() string {
	if r.Source == "" {
		return "default"
	}
	return fmt.Sprintf("%s (v%d)", r.Source, r.File.Version)
}

