package runbooks

import (
	"os"
	"path/filepath"
	"testing"

	"forgeiq/internal/controlplane/contracts"
)

func TestLoadDirAndValidateRunbook_OK(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "rb.yaml")
	if err := os.WriteFile(p, []byte(validRunbookYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	rbs, err := LoadDir(td)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, ok := rbs["rb-default-k8s"]; !ok {
		t.Fatalf("expected runbook id rb-default-k8s")
	}
}

func TestLoadDir_InvalidRunbook_Fails(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "bad.yaml")
	if err := os.WriteFile(p, []byte(invalidRunbookYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := LoadDir(td); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRender_SubstitutesStepInputs(t *testing.T) {
	rb := contracts.Runbook{
		ID:      "rb-default-k8s",
		Version: "v1",
		Name:    "x",
		Diagnostics: []contracts.RunbookStep{
			{
				ID:       "d1",
				Name:     "d1",
				Agent:    contracts.AgentKindObservability,
				TaskType: "promql_query",
				Input: map[string]any{
					"query_type": "promql",
					"query":      `sum(rate(http_requests_total{service="{{service}}"}[5m]))`,
				},
			},
		},
	}

	out := Render(rb, map[string]string{"service": "payments"})
	q := out.Diagnostics[0].Input["query"].(string)
	if q == "" || q == rb.Diagnostics[0].Input["query"].(string) {
		t.Fatalf("expected substitution, got: %q", q)
	}
}

func TestNormalizeIDs_PrefixesSteps(t *testing.T) {
	rb := contracts.Runbook{
		ID:      "rb-default-k8s",
		Version: "v1",
		Name:    "x",
		Diagnostics: []contracts.RunbookStep{
			{ID: "diag-1", Name: "d", Agent: contracts.AgentKindObservability, TaskType: "promql_query", Input: map[string]any{"query_type": "promql", "query": "up"}},
		},
		Actions: []contracts.RunbookStep{
			{ID: "act-1", Name: "a", Agent: contracts.AgentKindExec, TaskType: "k8s_restart", Input: map[string]any{"action_type": "k8s_restart"}},
		},
	}

	out := NormalizeIDs(rb)
	if out.Diagnostics[0].ID != "rb-default-k8s:v1:diag-1" {
		t.Fatalf("unexpected diagnostic id: %q", out.Diagnostics[0].ID)
	}
	if out.Actions[0].ID != "rb-default-k8s:v1:act-1" {
		t.Fatalf("unexpected action id: %q", out.Actions[0].ID)
	}
	// Idempotent: calling twice should not double-prefix.
	out2 := NormalizeIDs(out)
	if out2.Diagnostics[0].ID != out.Diagnostics[0].ID {
		t.Fatalf("expected idempotent normalization")
	}
}

const validRunbookYAML = `
id: rb-default-k8s
version: v1
name: Default K8s Restart Runbook
diagnostics:
  - id: diag-promql-5xx-rate
    name: Check error rate
    description: Query Prometheus for 5xx rate
    agent: observability
    task_type: promql_query
    input:
      query_type: promql
      query: 'sum(rate(http_requests_total{service="{{service}}",code=~"5.."}[5m]))'
      gate_signal: value
      gate_op: "<="
      gate_threshold: 0.1
    requires_approval: false
actions:
  - id: action-k8s-restart
    name: Restart deployment
    description: Rollout restart
    agent: exec
    task_type: k8s_restart
    input:
      action_type: k8s_restart
      namespace: default
      deployment: "{{service}}"
    requires_approval: true
`

const invalidRunbookYAML = `
id: rb-bad
version: v1
name: Bad runbook
diagnostics:
  - id: diag-1
    name: missing query
    agent: observability
    task_type: promql_query
    input:
      query_type: promql
`
