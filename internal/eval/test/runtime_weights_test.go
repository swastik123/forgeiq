package eval_test

import (
	"context"
	"testing"

	"forgeiq/internal/eval"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestComputeRuntimeWeights_MinRunsAndClamp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// migrate execs
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS eval_runs").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_started_at").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_decision_agent").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := eval.NewPostgresStoreWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// Expect query for decision agents; windowHours passed as string
	rows := sqlmock.NewRows([]string{"task_type", "agent_id", "runs", "avg_reward"}).
		AddRow("incident_triage", "decision-a", 10, 1.0). // below minRuns => ignored
		AddRow("incident_triage", "decision-b", 40, 1.0). // weight => +maxAbs
		AddRow("incident_triage", "decision-c", 40, -1.0) // weight => -maxAbs
	mock.ExpectQuery("FROM eval_runs").WillReturnRows(rows)

	w, err := s.ComputeRuntimeWeights(context.Background(), "decision", 168, 30, 5.0)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if w["incident_triage"]["decision-b"] != 5.0 {
		t.Fatalf("expected clamp to +5, got %v", w["incident_triage"]["decision-b"])
	}
	if w["incident_triage"]["decision-c"] != -5.0 {
		t.Fatalf("expected clamp to -5, got %v", w["incident_triage"]["decision-c"])
	}
	if _, ok := w["incident_triage"]["decision-a"]; ok {
		t.Fatalf("expected decision-a to be ignored due to minRuns")
	}
}
