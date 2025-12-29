package eval_test

import (
	"context"
	"database/sql"
	"testing"

	"forgeiq/internal/eval"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvalStore_MigrateAndUpsertStart(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS eval_runs").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_started_at").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_decision_agent").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := eval.NewPostgresStoreWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	mock.ExpectExec("INSERT INTO eval_runs").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.UpsertStart(context.Background(), "t1", "w1", "r1", "IncidentWorkflow"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestEvalStore_RecordAndCompare(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS eval_runs").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_started_at").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_eval_runs_decision_agent").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := eval.NewPostgresStoreWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	mock.ExpectExec("UPDATE eval_runs SET decision_agent_id").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.RecordAgent(context.Background(), "t1", "decision", "decision-local", "0.2"); err != nil {
		t.Fatalf("record agent: %v", err)
	}

	mock.ExpectExec("UPDATE eval_runs SET plan_steps").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.RecordPlan(context.Background(), "t1", 3, 0.9, map[string]any{"x": "y"}); err != nil {
		t.Fatalf("record plan: %v", err)
	}

	rows := sqlmock.NewRows([]string{"decision_agent_id", "decision_agent_version", "runs", "completed", "denied", "failed", "avg_tool_calls", "avg_approvals", "avg_duration_ms"}).
		AddRow("decision-local", "0.1", 10, 8, 1, 1, 2.0, 0.5, sql.NullFloat64{Float64: 123.0, Valid: true})
	mock.ExpectQuery("SELECT decision_agent_id").WillReturnRows(rows)
	out, err := s.CompareDecisionAgentVersions(context.Background(), "decision-local", []string{"0.1", "0.2"})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(out) != 1 || out[0].DecisionAgentID != "decision-local" {
		t.Fatalf("unexpected: %+v", out)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
