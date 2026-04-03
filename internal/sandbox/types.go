package sandbox

import "time"

type Profile string

const (
	ProfileAuto   Profile = "auto"
	ProfileGo     Profile = "go"
	ProfilePython Profile = "python"
)

type VerifyRequest struct {
	RepoPath   string  `json:"repo_path,omitempty"`   // local path accessible to worker (e.g. /host/forgeiq)
	CloneURL   string  `json:"clone_url,omitempty"`   // optional: https clone url
	CommitSHA  string  `json:"commit_sha,omitempty"`  // checkout target
	PRID       int     `json:"pr_id,omitempty"`
	RunID      string  `json:"run_id,omitempty"`
	Profile    Profile `json:"profile,omitempty"`     // auto|go|python
	PatchDiff  string  `json:"patch_diff,omitempty"`  // unified diff to apply (optional)
	TimeoutSec int     `json:"timeout_sec,omitempty"` // default 300
}

type CommandResult struct {
	OK       bool          `json:"ok"`
	ExitCode int           `json:"exit_code,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type VerifyResult struct {
	OK       bool                   `json:"ok"`
	RunID    string                 `json:"run_id"`
	Profile  Profile                `json:"profile"`
	Started  time.Time              `json:"started"`
	Finished time.Time              `json:"finished"`
	OutDir   string                 `json:"out_dir,omitempty"`
	Details  map[string]CommandResult `json:"details,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

