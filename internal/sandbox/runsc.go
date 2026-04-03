package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Runsc struct {
	Bin string // e.g. /usr/local/bin/runsc
}

func (r Runsc) Run(ctx context.Context, sandboxID string, bundleDir string, stateRoot string, timeout time.Duration) CommandResult {
	start := time.Now()
	if r.Bin == "" {
		r.Bin = "runsc"
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, r.Bin, "run", sandboxID, "--bundle", bundleDir, "--root", stateRoot)
	out, err := cmd.CombinedOutput()
	res := CommandResult{Duration: time.Since(start)}
	if err == nil {
		res.OK = true
		return res
	}
	res.OK = false
	res.Error = fmt.Sprintf("%v: %s", err, string(out))
	// best-effort exit code
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	}
	return res
}

