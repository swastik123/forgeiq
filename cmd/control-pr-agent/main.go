package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"forgeiq/internal/agent/strategy"
	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/observability"
	"forgeiq/internal/pragent"
	"forgeiq/internal/pragent/bitbucket"
	prcfg "forgeiq/internal/pragent/config"
	"forgeiq/internal/pragent/diff"
	"forgeiq/internal/pragent/findings"
	"forgeiq/internal/pragent/lang"
	"forgeiq/internal/pragent/review"
	"forgeiq/internal/transport/a2a"
	"forgeiq/internal/sandbox"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func main() {
	logger, err := observability.NewLogger(os.Getenv("LOG_LEVEL") == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	defer logger.Sync()
	logger = logger.WithComponent("pr-agent")

	metrics := observability.NewMetrics()
	healthChecker := observability.NewHealthChecker(logger)

	port := os.Getenv("PR_AGENT_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8086"
	}

	card := a2a.AgentCard{
		Name:        "pr-agent",
		Description: "Bitbucket PR/code review agent (diff-scoped), with idempotent inline + summary publishing.",
		TaskTypes:   []string{"review_pr"},
		Endpoint:    "http://localhost:" + port + "/task",
		Version:     "0.1",
	}

	// Optional self-registration into control-plane Agent Registry
	if regURL := os.Getenv("AGENT_REGISTRY_URL"); regURL != "" {
		baseURL := os.Getenv("AGENT_BASE_URL")
		if baseURL == "" {
			baseURL = "http://pr-agent:" + port
		}
		payload := map[string]any{
			"id":       os.Getenv("AGENT_ID"),
			"type":     "pr",
			"base_url": baseURL,
			"name":     card.Name,
			"version":  card.Version,
			"capabilities": map[string]any{
				"task_types": card.TaskTypes,
				"tags":       []string{"pr", "review", "bitbucket"},
			},
			"status": "unknown",
		}
		if payload["id"] == "" {
			payload["id"] = "pr-local"
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(regURL, "/")+"/registry/agents", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		_, _ = http.DefaultClient.Do(req) // best-effort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthChecker.HealthHandler())
	mux.HandleFunc("/ready", healthChecker.ReadyHandler())
	mux.Handle("/metrics", observability.PrometheusHandler())
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})

	agent := &PRAgent{Logger: logger}

	mux.HandleFunc("/task", agent.handleTask)

	handler := observability.HTTPLoggingMiddleware(logger, mux)
	handler = observability.HTTPMetricsMiddleware(metrics, handler)

	srv := observability.NewServer(":"+port, handler)
	logger.Info("Starting pr-agent", zap.String("addr", ":"+port))
	if err := observability.StartServer(logger, srv, "pr-agent"); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}

type PRAgent struct {
	Logger *observability.Logger
}

func (a *PRAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var t contracts.Task
	// if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
	// 	http.Error(w, "invalid json", http.StatusBadRequest)
	// 	return
	// }

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&t); err != nil {
		if err == io.EOF {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(t.Type) != "review_pr" {
		http.Error(w, "unsupported task type", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	workspace := strings.TrimSpace(fmt.Sprint(t.Input["workspace"]))
	repoSlug := strings.TrimSpace(fmt.Sprint(t.Input["repo_slug"]))
	prID, err := bitbucket.ParsePRID(t.Input["pr_id"])
	if err != nil {
		http.Error(w, "invalid pr_id", http.StatusBadRequest)
		return
	}
	publish := boolFromAny(t.Input["publish"], true)
	publishInline := boolFromAny(t.Input["publish_inline"], true)
	publishSummary := boolFromAny(t.Input["publish_summary"], true)
	verifySandbox := boolFromAny(t.Input["verify_sandbox"], false)

	cfg := pragent.ReviewConfig{
		PublishInline:      publishInline,
		PublishSummary:     publishSummary,
		MaxFindingsPerFile: intFromAny(t.Input["max_findings_per_file"], 8),
		MaxFindingsTotal:   intFromAny(t.Input["max_findings_total"], 50),
		BlockOnSeverity:    pragent.SeverityHigh,
	}

	// Review (diff-scoped) using MCP LLM if configured
	mcpBase := strings.TrimSpace(os.Getenv("MCP_BASE_URL"))
	model := strategy.ModelConfig{
		Provider: "mcp",
		Model:    strings.TrimSpace(os.Getenv("PR_AGENT_MODEL")),
	}
	reviewer := &review.MCPReviewer{
		MCPBaseURL: mcpBase,
		Model:      model,
		Meta: map[string]any{
			"task_id": t.ID,
			"agent":   "pr-agent",
		},
	}
	orch := &review.Orchestrator{Reviewer: reviewer, Config: cfg}

	bb := bitbucket.NewFromEnv()
	pr, err := bb.GetPR(ctx, workspace, repoSlug, prID)
	if err != nil {
		http.Error(w, "bitbucket get pr: "+err.Error(), http.StatusBadGateway)
		return
	}
	diffText, err := bb.GetDiff(ctx, workspace, repoSlug, prID)
	if err != nil {
		http.Error(w, "bitbucket get diff: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Optional repo config (fallback to ForgeIQ defaults if missing)
	ref := strings.TrimSpace(pr.Source.Commit.Hash)
	cfgRes, _ := prcfg.LoadFromBitbucket(ctx, bb, workspace, repoSlug, ref)
	ignore := prcfg.BuildIgnoreMatcher(cfgRes.File.Ignore.Paths)
	diffText = diff.FilterUnifiedText(diffText, func(path string) bool { return !ignore(path) })

	// Apply config overrides
	cfg = cfgRes.File.ToReviewConfigDefaults()
	orch.Config = cfg
	publishInline = cfg.PublishInline
	publishSummary = cfg.PublishSummary

	// Build policy yaml string (effective config)
	policyBytes, _ := yaml.Marshal(cfgRes.File)
	policyYAML := string(policyBytes)

	// Build a lightweight repo summary from the diff (no checkout needed)
	repoSummary := ""
	if p, err := diff.ParseUnified(diffText); err == nil {
		sb := strings.Builder{}
		sb.WriteString(fmt.Sprintf("PR %s/%s#%d @ %s\n", workspace, repoSlug, prID, ref))
		sb.WriteString("Changed files:\n")
		for _, fp := range p.Files {
			path := fp.NewPath
			if strings.TrimSpace(path) == "" {
				path = fp.OldPath
			}
			if path == "" || ignore(path) {
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (lang=%s)\n", path, lang.Detect(path)))
		}
		repoSummary = strings.TrimSpace(sb.String())
	}

	fs, meta, err := orch.Review(ctx, review.Input{
		PRTitle:       pr.Title,
		PRDescription: pr.Description,
		DiffText:      diffText,
		RepoSummary:   repoSummary,
		PolicyYAML:    policyYAML,
	})
	if err != nil {
		http.Error(w, "review failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Final dedupe + ensure fingerprints
	for i := range fs {
		if fs[i].Fingerprint == "" {
			fs[i].Fingerprint = findings.Fingerprint(fs[i])
		}
	}
	// Drop findings for ignored paths (defense-in-depth)
	filtered := make([]pragent.Finding, 0, len(fs))
	for _, f := range fs {
		if f.Path != "" && ignore(f.Path) {
			continue
		}
		filtered = append(filtered, f)
	}
	fs = filtered
	fs = findings.DedupeAndCap(fs, cfg.MaxFindingsTotal, cfg.MaxFindingsPerFile)

	// Optional: sandbox verification (local/dev). Requires external sandbox-worker service.
	if verifySandbox {
		if meta == nil {
			meta = map[string]any{}
		}
		swURL := strings.TrimSpace(os.Getenv("SANDBOX_WORKER_URL"))
		if swURL == "" {
			meta["sandbox"] = map[string]any{"enabled": true, "error": "SANDBOX_WORKER_URL not set"}
		} else {
				repoPath := strings.TrimSpace(os.Getenv("SANDBOX_REPO_PATH"))
			vr, verr := callSandboxWorker(ctx, swURL, sandbox.VerifyRequest{
					RepoPath:   repoPath,
					CloneURL:   fmt.Sprintf("https://bitbucket.org/%s/%s.git", workspace, repoSlug),
				CommitSHA:  ref,
				PRID:       prID,
				RunID:      fmt.Sprintf("pr-%d-%s", prID, ref),
				Profile:    sandbox.ProfileAuto,
				TimeoutSec: 300,
			})
			if verr != nil {
				meta["sandbox"] = map[string]any{"enabled": true, "error": verr.Error(), "result": vr}
				// Add a high severity finding so the review is evidence-driven.
				fs = append([]pragent.Finding{{
					RuleID:    "SANDBOX_VERIFY",
					Severity:  pragent.SeverityHigh,
					Category:  "reliability",
					Path:      "",
					Line:      -1,
					Message:   "Sandbox verification failed: " + verr.Error(),
					Suggestion: "",
					Refs:      map[string]string{"run_id": vr.RunID},
				}}, fs...)
			} else {
				meta["sandbox"] = map[string]any{"enabled": true, "result": vr}
				if !vr.OK {
					fs = append([]pragent.Finding{{
						RuleID:    "SANDBOX_VERIFY",
						Severity:  pragent.SeverityHigh,
						Category:  "reliability",
						Path:      "",
						Line:      -1,
						Message:   "Sandbox verification failed (see logs/artifacts).",
						Refs:      map[string]string{"run_id": vr.RunID},
					}}, fs...)
				}
			}
		}
	}

	pubRes := any(nil)
	if publish {
		summary, _ := meta["summary"].(string)
		prc := bitbucket.PublishConfig{
			PublishInline:  cfg.PublishInline,
			PublishSummary: cfg.PublishSummary,
			MaxInline:      cfg.MaxFindingsTotal,
		}
		if r, err := bitbucket.Publish(ctx, bb, workspace, repoSlug, prID, fs, summary, prc); err == nil {
			pubRes = r
		} else {
			meta["publish_error"] = err.Error()
		}
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["config_source"] = cfgRes.Source
	meta["config_ref"] = ref

	art := contracts.Artifact{
		TaskID: t.ID,
		Type:   "Result",
		Payload: map[string]any{
			"status":        "ok",
			"pr_id":         prID,
			"repo":          repoSlug,
			"workspace":     workspace,
			"pr_title":      pr.Title,
			"pr_state":      pr.State,
			"findings":      fs,
			"finding_count": len(fs),
			"meta":          meta,
			"published":     pubRes,
		},
		TS: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(art)
}

func callSandboxWorker(ctx context.Context, baseURL string, req sandbox.VerifyRequest) (sandbox.VerifyResult, error) {
	b, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/verify", bytes.NewReader(b))
	if err != nil {
		return sandbox.VerifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return sandbox.VerifyResult{}, err
	}
	defer resp.Body.Close()
	var out sandbox.VerifyResult
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error != "" {
			return out, fmt.Errorf("%s", out.Error)
		}
		return out, fmt.Errorf("sandbox worker status %d", resp.StatusCode)
	}
	if !out.OK && out.Error != "" {
		return out, fmt.Errorf("%s", out.Error)
	}
	return out, nil
}

func boolFromAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "true" || s == "1" || s == "yes" {
			return true
		}
		if s == "false" || s == "0" || s == "no" {
			return false
		}
		return def
	default:
		return def
	}
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	default:
		n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		if err != nil {
			return def
		}
		return n
	}
}
