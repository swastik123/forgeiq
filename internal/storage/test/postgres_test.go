package storage_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"forgeiq/internal/storage"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresStorage_MigrateAndSaveWorkflow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// migrate execs (match prefix to avoid full string match)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS workflows").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_task_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_created_at").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS artifacts").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_artifacts_task_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS audit_logs").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_workflow_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_action").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_created_at").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := storage.NewPostgresStorageWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	mock.ExpectExec("INSERT INTO workflows").WillReturnResult(sqlmock.NewResult(0, 1))
	now := time.Now()
	w := &storage.WorkflowRecord{
		ID:        "w1",
		TaskID:    "t1",
		Type:      "incident_triage",
		Status:    "running",
		Input:     map[string]any{"x": 1},
		Output:    map[string]any{},
		Metadata:  map[string]string{"m": "v"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.SaveWorkflow(context.Background(), w); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPostgresStorage_GetWorkflow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// migrate execs
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS workflows").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_task_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_workflows_created_at").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS artifacts").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_artifacts_task_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS audit_logs").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_workflow_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_action").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_audit_created_at").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := storage.NewPostgresStorageWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	now := time.Now()
	row := sqlmock.NewRows([]string{"id", "task_id", "type", "status", "input", "output", "metadata", "created_at", "updated_at", "completed_at", "error"}).
		AddRow("w1", "t1", "incident_triage", "completed", []byte(`{"x":1}`), []byte(`{"y":2}`), []byte(`{"m":"v"}`), now, now, sql.NullTime{}, "")
	mock.ExpectQuery("SELECT id, task_id, type").WithArgs("w1").WillReturnRows(row)
	got, err := s.GetWorkflow(context.Background(), "w1")
	if err != nil || got.ID != "w1" {
		t.Fatalf("get: %v %+v", err, got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
