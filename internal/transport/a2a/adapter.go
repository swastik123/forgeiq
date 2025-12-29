package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// VendorAdapter adapts request/response shapes for agents that don't speak our canonical A2A.
// Canonical A2A is: POST {BaseURL}/task with contracts.Task -> contracts.Artifact.
type VendorAdapter interface {
	BuildRequest(ctx context.Context, agent Agent, task contracts.Task) (method string, invokeURL string, payload any, err error)
	ParseResponse(ctx context.Context, agent Agent, task contracts.Task, httpStatus int, body []byte) (contracts.Artifact, error)
}

// CanonicalAdapter assumes the agent speaks our canonical A2A (/task, contracts.Task, contracts.Artifact).
type CanonicalAdapter struct{}

func (a *CanonicalAdapter) BuildRequest(ctx context.Context, agent Agent, task contracts.Task) (string, string, any, error) {
	_ = ctx
	base := strings.TrimRight(strings.TrimSpace(agent.BaseURL), "/")
	if base == "" {
		return "", "", nil, fmt.Errorf("agent base_url is empty")
	}
	return http.MethodPost, base + "/task", task, nil
}

func (a *CanonicalAdapter) ParseResponse(ctx context.Context, agent Agent, task contracts.Task, httpStatus int, body []byte) (contracts.Artifact, error) {
	_ = ctx
	_ = agent
	if httpStatus != http.StatusOK {
		return contracts.Artifact{}, fmt.Errorf("agent returned status %d", httpStatus)
	}
	var art contracts.Artifact
	if err := json.Unmarshal(body, &art); err != nil {
		return contracts.Artifact{}, fmt.Errorf("decode artifact: %w", err)
	}
	if art.TaskID == "" {
		art.TaskID = task.ID
	}
	return art, nil
}

// InvokeAdapter is a generic vendor adapter that calls agent.Endpoints["invoke"] with a common payload:
// {"task_id": "...", "capability_id":"...", "input": {...}, "metadata": {...}}.
//
// capability_id is taken from task.Metadata["capability_id"] if present; otherwise task.Type is used.
// Response shape expected:
// {"task_id":"...","status":"success|error","output":{...},"error":{...},"metadata":{...}}
// This is NOT used for internal agents unless you configure endpoints.invoke.
type InvokeAdapter struct{}

func (a *InvokeAdapter) BuildRequest(ctx context.Context, agent Agent, task contracts.Task) (string, string, any, error) {
	_ = ctx
	invoke := ""
	if agent.Endpoints != nil {
		invoke = strings.TrimSpace(agent.Endpoints["invoke"])
	}
	if invoke == "" {
		return "", "", nil, fmt.Errorf("invoke endpoint missing")
	}
	// sanity: must be a valid URL
	if _, err := url.Parse(invoke); err != nil {
		return "", "", nil, fmt.Errorf("invalid invoke url: %w", err)
	}
	capID := task.Type
	if task.Metadata != nil {
		if s, ok := task.Metadata["capability_id"]; ok && strings.TrimSpace(s) != "" {
			capID = strings.TrimSpace(s)
		}
	}
	payload := map[string]any{
		"task_id":       task.ID,
		"capability_id": capID,
		"input":         task.Input,
		"metadata":      task.Metadata,
	}
	return http.MethodPost, invoke, payload, nil
}

func (a *InvokeAdapter) ParseResponse(ctx context.Context, agent Agent, task contracts.Task, httpStatus int, body []byte) (contracts.Artifact, error) {
	_ = ctx
	_ = agent
	if httpStatus < 200 || httpStatus >= 300 {
		return contracts.Artifact{}, fmt.Errorf("agent returned status %d", httpStatus)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return contracts.Artifact{}, fmt.Errorf("decode invoke response: %w", err)
	}
	out := contracts.Artifact{
		TaskID: task.ID,
		Type:   "VendorResult",
		Payload: map[string]any{
			"status":   raw["status"],
			"output":   raw["output"],
			"error":    raw["error"],
			"metadata": raw["metadata"],
		},
	}
	// If vendor returns canonical artifact, allow passthrough
	if _, ok := raw["type"].(string); ok && raw["payload"] != nil {
		var art contracts.Artifact
		if err := json.Unmarshal(body, &art); err == nil && art.Type != "" {
			if art.TaskID == "" {
				art.TaskID = task.ID
			}
			return art, nil
		}
	}
	return out, nil
}

// AdapterRegistry chooses adapter based on host/domain or agent metadata.
type AdapterRegistry struct {
	byDomain map[string]VendorAdapter
	byVendor map[string]VendorAdapter
	def      VendorAdapter
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		byDomain: make(map[string]VendorAdapter),
		byVendor: make(map[string]VendorAdapter),
		def:      &CanonicalAdapter{},
	}
}

func (r *AdapterRegistry) RegisterForDomain(domainSuffix string, adapter VendorAdapter) {
	r.byDomain[strings.TrimSpace(domainSuffix)] = adapter
}

func (r *AdapterRegistry) RegisterForVendor(vendor string, adapter VendorAdapter) {
	r.byVendor[strings.ToLower(strings.TrimSpace(vendor))] = adapter
}

func (r *AdapterRegistry) ForAgent(agent Agent) VendorAdapter {
	if r == nil {
		return &CanonicalAdapter{}
	}
	// If explicit endpoints.invoke exists, prefer InvokeAdapter unless overridden
	if agent.Endpoints != nil && strings.TrimSpace(agent.Endpoints["invoke"]) != "" {
		// allow vendor override
		if v := strings.ToLower(strings.TrimSpace(agent.Vendor)); v != "" {
			if ad, ok := r.byVendor[v]; ok {
				return ad
			}
		}
		return &InvokeAdapter{}
	}
	// vendor override
	if v := strings.ToLower(strings.TrimSpace(agent.Vendor)); v != "" {
		if ad, ok := r.byVendor[v]; ok {
			return ad
		}
	}
	// domain-based selection
	base := strings.TrimRight(strings.TrimSpace(agent.BaseURL), "/")
	u, err := url.Parse(base)
	if err == nil {
		host := u.Hostname()
		best := ""
		var chosen VendorAdapter
		for suffix, ad := range r.byDomain {
			if suffix == "" {
				continue
			}
			if strings.HasSuffix(host, suffix) && len(suffix) > len(best) {
				best = suffix
				chosen = ad
			}
		}
		if chosen != nil {
			return chosen
		}
	}
	if r.def != nil {
		return r.def
	}
	return &CanonicalAdapter{}
}

func doAdapterCall(ctx context.Context, httpc *http.Client, agent Agent, task contracts.Task, adapter VendorAdapter, headers map[string]string) (contracts.Artifact, error) {
	method, invokeURL, payload, err := adapter.BuildRequest(ctx, agent, task)
	if err != nil {
		return contracts.Artifact{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return contracts.Artifact{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, invokeURL, bytes.NewReader(body))
	if err != nil {
		return contracts.Artifact{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return contracts.Artifact{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return adapter.ParseResponse(ctx, agent, task, resp.StatusCode, respBody)
}
