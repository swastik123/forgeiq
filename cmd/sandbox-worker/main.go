package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"forgeiq/internal/observability"
	"forgeiq/internal/sandbox"
	"forgeiq/internal/sandbox/scripts"

	"go.uber.org/zap"
)

func main() {
	logger, err := observability.NewLogger(os.Getenv("LOG_LEVEL") == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to init logger: %v", err))
	}
	defer logger.Sync()
	logger = logger.WithComponent("sandbox-worker")

	baseDir := strings.TrimSpace(os.Getenv("SANDBOX_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/var/lib/forgeiq-sbx"
	}
	slots := 2
	if v := strings.TrimSpace(os.Getenv("SANDBOX_SLOTS")); v != "" {
		fmt.Sscanf(v, "%d", &slots)
	}
	runscBin := strings.TrimSpace(os.Getenv("RUNSC_BIN"))
	if runscBin == "" {
		runscBin = "runsc"
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SANDBOX_MODE")))
	if mode == "" {
		mode = "runsc" // runsc | docker
	}
	dockerBin := strings.TrimSpace(os.Getenv("DOCKER_BIN"))
	if dockerBin == "" {
		dockerBin = "docker"
	}
	dockerRuntime := strings.TrimSpace(os.Getenv("DOCKER_RUNTIME"))
	if dockerRuntime == "" {
		dockerRuntime = "runsc"
	}
	dockerImageGo := strings.TrimSpace(os.Getenv("DOCKER_IMAGE_GO"))
	if dockerImageGo == "" {
		dockerImageGo = "golang:1.22-bookworm"
	}
	dockerImagePy := strings.TrimSpace(os.Getenv("DOCKER_IMAGE_PY"))
	if dockerImagePy == "" {
		dockerImagePy = "python:3.12-bookworm"
	}
	rootfsGo := strings.TrimSpace(os.Getenv("ROOTFS_GO"))
	rootfsPy := strings.TrimSpace(os.Getenv("ROOTFS_PY"))

	pool := sandbox.SlotPool{BaseDir: baseDir, Slots: slots}
	if err := pool.EnsureLayout(); err != nil {
		logger.Fatal("failed to init sandbox layout", zap.Error(err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req sandbox.VerifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.TimeoutSec <= 0 {
			req.TimeoutSec = 300
		}
		if req.RunID == "" {
			req.RunID = fmt.Sprintf("pr-%d-%d", req.PRID, time.Now().Unix())
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(req.TimeoutSec)*time.Second)
		defer cancel()

		slot, err := pool.Acquire(10 * time.Second)
		if err != nil {
			http.Error(w, "no free sandbox slots", http.StatusTooManyRequests)
			return
		}
		defer slot.Release()

		out, err := runOne(ctx, logger, *slot, mode, runscBin, dockerBin, dockerRuntime, dockerImageGo, dockerImagePy, rootfsGo, rootfsPy, req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	addr := ":" + strings.TrimSpace(os.Getenv("PORT"))
	if addr == ":" {
		addr = ":8091"
	}
	logger.Info("starting sandbox-worker", zap.String("addr", addr), zap.String("base_dir", baseDir), zap.Int("slots", slots), zap.String("mode", mode))
	_ = http.ListenAndServe(addr, mux)
}

func runOne(ctx context.Context, logger *observability.Logger, slot sandbox.Slot, mode string, runscBin string, dockerBin string, dockerRuntime string, dockerImageGo string, dockerImagePy string, rootfsGo string, rootfsPy string, req sandbox.VerifyRequest) (sandbox.VerifyResult, error) {
	res := sandbox.VerifyResult{
		RunID:    req.RunID,
		Profile: req.Profile,
		Started: time.Now(),
		Details: map[string]sandbox.CommandResult{},
	}
	// Prepare per-run dirs under slot
	runRoot := filepath.Join(slot.BaseDir, "out", "runs", req.RunID)
	workspace := filepath.Join(slot.BaseDir, "workspace", req.RunID)
	bundle := filepath.Join(slot.BaseDir, "bundle", req.RunID)
	state := filepath.Join(slot.BaseDir, "state", req.RunID)
	outDir := filepath.Join(runRoot, "out")
	for _, d := range []string{runRoot, workspace, bundle, state, outDir} {
		_ = os.MkdirAll(d, 0o755)
	}
	res.OutDir = outDir

	// 1) Populate workspace
	if err := populateWorkspace(ctx, workspace, req); err != nil {
		res.OK = false
		res.Error = err.Error()
		res.Finished = time.Now()
		return res, err
	}

	// 2) Write patch if provided
	if strings.TrimSpace(req.PatchDiff) != "" {
		_ = os.WriteFile(filepath.Join(outDir, "patch.diff"), []byte(req.PatchDiff), 0o644)
	}

	// 3) Detect profile
	profile := req.Profile
	if profile == "" || profile == sandbox.ProfileAuto {
		hasGo := fileExists(filepath.Join(workspace, "go.mod"))
		hasPy := fileExists(filepath.Join(workspace, "pyproject.toml")) || fileExists(filepath.Join(workspace, "requirements.txt"))
		switch {
		case hasGo:
			profile = sandbox.ProfileGo
		case hasPy:
			profile = sandbox.ProfilePython
		default:
			profile = sandbox.ProfileGo
		}
	}

	rootfs := ""
	verifyScript := ""
	switch profile {
	case sandbox.ProfileGo:
		rootfs = rootfsGo
		verifyScript = scripts.GoVerify
	case sandbox.ProfilePython:
		rootfs = rootfsPy
		verifyScript = scripts.PyVerify
	default:
		rootfs = rootfsGo
		verifyScript = scripts.GoVerify
	}
	if strings.TrimSpace(rootfs) == "" {
		err := fmt.Errorf("rootfs not configured for profile %s (set ROOTFS_GO/ROOTFS_PY)", profile)
		res.OK = false
		res.Error = err.Error()
		res.Finished = time.Now()
		return res, err
	}

	// 4) Write verify.sh into bundle and mount it
	verifyPath := filepath.Join(bundle, "verify.sh")
	_ = os.WriteFile(verifyPath, []byte(verifyScript), 0o755)

	var cr sandbox.CommandResult
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "docker":
		img := dockerImageGo
		if profile == sandbox.ProfilePython {
			img = dockerImagePy
		}
		cr = runDocker(ctx, dockerBin, dockerRuntime, img, workspace, outDir, verifyPath, time.Duration(req.TimeoutSec)*time.Second)
		res.Details["docker"] = cr
	default:
		if strings.TrimSpace(rootfs) == "" {
			err := fmt.Errorf("rootfs not configured for profile %s (set ROOTFS_GO/ROOTFS_PY) or use SANDBOX_MODE=docker", profile)
			res.OK = false
			res.Error = err.Error()
			res.Finished = time.Now()
			return res, err
		}
		// Write bundle config.json
		if err := sandbox.WriteBundle(sandbox.BundleSpec{
			BundleDir:  bundle,
			RootfsPath: rootfs,
			Workspace:  workspace,
			OutDir:     outDir,
			VerifySh:   verifyPath,
			Pids:       256,
			NoFile:     1024,
		}); err != nil {
			res.OK = false
			res.Error = err.Error()
			res.Finished = time.Now()
			return res, err
		}
		// Run runsc
		r := sandbox.Runsc{Bin: runscBin}
		sandboxID := "forgeiq-" + req.RunID
		cr = r.Run(ctx, sandboxID, bundle, state, time.Duration(req.TimeoutSec)*time.Second)
		res.Details["runsc"] = cr
	}

	res.OK = cr.OK
	res.Finished = time.Now()

	if !cr.OK {
		logger.Warn("sandbox run failed", zap.String("run_id", req.RunID), zap.String("err", cr.Error))
		return res, fmt.Errorf("sandbox failed: %s", cr.Error)
	}
	return res, nil
}

func populateWorkspace(ctx context.Context, workspace string, req sandbox.VerifyRequest) error {
	// Preferred for local dev: if RepoPath points to a mounted repo, populate via git archive (no host checkout)
	// and fall back to rsync. Otherwise, clone.
	srcPath := strings.TrimSpace(req.RepoPath)
	if srcPath != "" {
		if st, err := os.Stat(srcPath); err == nil && st.IsDir() {
			sha := strings.TrimSpace(req.CommitSHA)
			if sha == "" {
				sha = "HEAD"
			}
			// Try git archive (safe, doesn't modify source repo state)
			cmd := exec.CommandContext(ctx, "bash", "-lc", fmt.Sprintf("git -C %q archive %q | tar -x -C %q", srcPath, sha, workspace))
			if out, err := cmd.CombinedOutput(); err == nil {
				_ = out
				return nil
			}
			// Fallback: rsync a snapshot as-is
			cmd = exec.CommandContext(ctx, "rsync", "-a", "--delete", srcPath+string(os.PathSeparator), workspace+string(os.PathSeparator))
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("rsync failed: %v: %s", err, string(out))
			}
			return nil
		}
	}

	// Network clone fallback
	src := strings.TrimSpace(req.CloneURL)
	if src == "" {
		src = strings.TrimSpace(req.RepoPath)
	}
	if src == "" {
		return fmt.Errorf("repo_path or clone_url required")
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--no-checkout", src, workspace)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %v: %s", err, string(out))
	}
	sha := strings.TrimSpace(req.CommitSHA)
	if sha == "" {
		sha = "HEAD"
	}
	cmd = exec.CommandContext(ctx, "git", "-C", workspace, "checkout", sha)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %v: %s", err, string(out))
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runDocker(ctx context.Context, dockerBin string, runtime string, image string, workspace string, outDir string, verifyPath string, timeout time.Duration) sandbox.CommandResult {
	start := time.Now()
	if dockerBin == "" {
		dockerBin = "docker"
	}
	if runtime == "" {
		runtime = "runsc"
	}
	if image == "" {
		image = "golang:1.22-bookworm"
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	args := []string{
		"run", "--rm",
		"--runtime=" + runtime,
		"--network=none",
		"-v", workspace + ":/workspace",
		"-v", outDir + ":/out",
		"-v", verifyPath + ":/verify.sh:ro",
		"-w", "/workspace",
		image,
		"bash", "/verify.sh",
	}
	cmd := exec.CommandContext(ctx, dockerBin, args...)
	out, err := cmd.CombinedOutput()
	cr := sandbox.CommandResult{Duration: time.Since(start)}
	if err == nil {
		cr.OK = true
		return cr
	}
	cr.OK = false
	if ee, ok := err.(*exec.ExitError); ok {
		cr.ExitCode = ee.ExitCode()
	}
	cr.Error = fmt.Sprintf("%v: %s", err, string(out))
	return cr
}

