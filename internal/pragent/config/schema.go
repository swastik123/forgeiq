package config

import (
	"fmt"
	"strings"

	"forgeiq/internal/pragent"
)

// Repo-level configuration file (optional).
// Supported locations (checked in this order):
// - .forgeiq/pr_reviewer.yaml
// - pr_reviewer.yaml

type File struct {
	Version  int                 `yaml:"version"`
	Profiles map[string]Profile   `yaml:"profiles,omitempty"`
	Ignore   IgnoreConfig         `yaml:"ignore,omitempty"`
	Limits   LimitsConfig         `yaml:"limits,omitempty"`
	Publish  PublishConfig        `yaml:"publish,omitempty"`
}

type Profile struct {
	Enabled bool `yaml:"enabled"`
	// Reserved knobs (not fully enforced in v1 agent yet)
	BlockOn string `yaml:"block_on,omitempty"`
	Rules   struct {
		Enable  []string `yaml:"enable,omitempty"`
		Disable []string `yaml:"disable,omitempty"`
	} `yaml:"rules,omitempty"`
}

type IgnoreConfig struct {
	Paths []string `yaml:"paths,omitempty"`
}

type LimitsConfig struct {
	MaxFindingsTotal   int `yaml:"max_findings_total,omitempty"`
	MaxFindingsPerFile int `yaml:"max_findings_per_file,omitempty"`
}

type PublishConfig struct {
	Inline  bool `yaml:"inline"`
	Summary bool `yaml:"summary"`
}

func (f *File) Validate() error {
	if f.Version == 0 {
		// treat missing as v1
		f.Version = 1
	}
	if f.Version != 1 {
		return fmt.Errorf("unsupported pr_reviewer.yaml version: %d", f.Version)
	}
	// basic sanity checks
	for _, p := range f.Ignore.Paths {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("ignore.paths contains empty entry")
		}
	}
	return nil
}

func (f *File) ToReviewConfigDefaults() pragent.ReviewConfig {
	cfg := pragent.ReviewConfig{
		PublishInline:      true,
		PublishSummary:     true,
		MaxFindingsPerFile: 8,
		MaxFindingsTotal:   50,
		BlockOnSeverity:    pragent.SeverityHigh,
	}
	// Apply publish
	if f.Publish.Inline || f.Publish.Summary {
		// If present, respect explicitly.
		cfg.PublishInline = f.Publish.Inline
		cfg.PublishSummary = f.Publish.Summary
	}
	// Apply limits
	if f.Limits.MaxFindingsTotal > 0 {
		cfg.MaxFindingsTotal = f.Limits.MaxFindingsTotal
	}
	if f.Limits.MaxFindingsPerFile > 0 {
		cfg.MaxFindingsPerFile = f.Limits.MaxFindingsPerFile
	}
	return cfg
}

