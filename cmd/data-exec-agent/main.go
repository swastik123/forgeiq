//go:build k8s
// +build k8s

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"forgeiq/internal/controlplane/contracts"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ExecAgent struct {
	k8s *kubernetes.Clientset
}

func main() {
	agent, err := NewExecAgent()
	if err != nil {
		log.Fatalf("failed to init exec agent: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", agent.handleDiscovery)
	mux.HandleFunc("/task", agent.handleTask)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := ":8085" // or whatever
	log.Printf("Exec Agent listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func NewExecAgent() (*ExecAgent, error) {
	// Use in-cluster config if available, else fall back to KUBECONFIG
	var cfg *rest.Config
	var err error

	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kc)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ExecAgent{k8s: clientset}, nil
}

func (a *ExecAgent) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	meta := map[string]any{
		"name":    "exec-agent",
		"version": "v1",
		"kind":    "exec",
		"tasks": []map[string]string{
			{"name": "k8s_scale", "description": "Scale a Kubernetes deployment"},
			{"name": "k8s_restart", "description": "Restart a Kubernetes deployment"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (a *ExecAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	var req contracts.A2ATaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var in contracts.ExecInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var out contracts.ExecOutput
	var err error

	switch in.ActionType {
	case "k8s_scale":
		err = a.handleK8sScale(r.Context(), req.IdempotencyKey, &in, &out)
	case "k8s_restart":
		err = a.handleK8sRestart(r.Context(), req.IdempotencyKey, &in, &out)
	default:
		err = a.handleUnknown(r.Context(), &in, &out)
	}

	resp := contracts.A2ATaskResponse{
		Status: "ok",
	}
	if err != nil {
		resp.Status = "error"
		resp.Error = err.Error()
	} else {
		bytes, _ := json.Marshal(out)
		resp.Output = bytes
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *ExecAgent) handleK8sScale(ctx context.Context, idemKey string, in *contracts.ExecInput, out *contracts.ExecOutput) error {
	ns := in.Params["namespace"]
	depl := in.Params["deployment"]
	replicasStr := in.Params["replicas"]

	// Already-applied check: if current replicas already match desired, treat as no-op.
	desired, err := strconv.Atoi(replicasStr)
	if err != nil {
		out.Success = false
		out.Details = "invalid replicas: " + replicasStr
		return err
	}
	d, err := a.k8s.AppsV1().Deployments(ns).Get(ctx, depl, metav1.GetOptions{})
	if err != nil {
		out.Success = false
		out.Details = err.Error()
		return err
	}
	cur := int32(0)
	if d.Spec.Replicas != nil {
		cur = *d.Spec.Replicas
	}
	if cur == int32(desired) {
		out.Success = true
		out.Details = "noop: deployment " + ns + "/" + depl + " already at " + replicasStr + " replicas"
		return nil
	}

	// Apply: set desired replicas.
	rep := int32(desired)
	d2 := d.DeepCopy()
	d2.Spec.Replicas = &rep
	if _, err := a.k8s.AppsV1().Deployments(ns).Update(ctx, d2, metav1.UpdateOptions{}); err != nil {
		out.Success = false
		out.Details = err.Error()
		return err
	}

	out.Success = true
	out.Details = "scaled deployment " + ns + "/" + depl + " to " + replicasStr + " replicas (idempotency_key=" + idemKey + ")"
	return nil
}

func (a *ExecAgent) handleK8sRestart(ctx context.Context, idemKey string, in *contracts.ExecInput, out *contracts.ExecOutput) error {
	ns := in.Params["namespace"]
	depl := in.Params["deployment"]

	d, err := a.k8s.AppsV1().Deployments(ns).Get(ctx, depl, metav1.GetOptions{})
	if err != nil {
		out.Success = false
		out.Details = err.Error()
		return err
	}

	// Already-applied check via pod-template annotation.
	annKey := "forgeiq.io/idempotency-key"
	if d.Spec.Template.Annotations != nil && idemKey != "" && d.Spec.Template.Annotations[annKey] == idemKey {
		out.Success = true
		out.Details = "noop: restart already applied for idempotency_key=" + idemKey
		return nil
	}

	d2 := d.DeepCopy()
	if d2.Spec.Template.Annotations == nil {
		d2.Spec.Template.Annotations = map[string]string{}
	}
	if idemKey != "" {
		d2.Spec.Template.Annotations[annKey] = idemKey
	}
	// bump annotation to trigger rollout restart semantics
	d2.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = metav1.Now().Format(time.RFC3339)
	_, err = a.k8s.AppsV1().Deployments(ns).Update(ctx, d2, metav1.UpdateOptions{})
	if err != nil {
		out.Success = false
		out.Details = err.Error()
		return err
	}

	out.Success = true
	out.Details = "restarted deployment " + ns + "/" + depl
	return nil
}

func (a *ExecAgent) handleUnknown(ctx context.Context, in *contracts.ExecInput, out *contracts.ExecOutput) error {
	out.Success = false
	out.Details = "unknown action type: " + in.ActionType
	return nil
}
