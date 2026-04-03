package pragent

// PRAgent is a Bitbucket-focused PR/code review agent.
// This package is intentionally split into small subpackages (diff/lang/bitbucket/etc).

type Language string

const (
	LangUnknown   Language = "unknown"
	LangGo        Language = "go"
	LangPython    Language = "python"
	LangJava      Language = "java"
	LangTerraform Language = "terraform"
)

type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityHigh    Severity = "high"
	SeverityMedium  Severity = "medium"
	SeverityLow     Severity = "low"
	SeverityNit     Severity = "nit"
)

type Finding struct {
	ID         string   `json:"id"`
	RuleID     string   `json:"rule_id"`
	Severity   Severity `json:"severity"`
	Category   string   `json:"category,omitempty"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"` // 1-based line in "new" file
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`

	// Stable fingerprint used to dedupe + update existing comments across reruns.
	Fingerprint string `json:"fingerprint"`

	// Optional: link to evidence/source.
	Refs map[string]string `json:"refs,omitempty"`
}

type ReviewConfig struct {
	// Publishing behavior
	PublishInline bool `json:"publish_inline"`
	PublishSummary bool `json:"publish_summary"`

	// Noise controls
	MaxFindingsPerFile int `json:"max_findings_per_file"`
	MaxFindingsTotal   int `json:"max_findings_total"`

	// Thresholds
	BlockOnSeverity Severity `json:"block_on_severity"` // e.g. "high" means block on high+blocker
}

