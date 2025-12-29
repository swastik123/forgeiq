package registry_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"forgeiq/internal/registry"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresAgentStore_MigrateAndList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// migrate execs
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_registry").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_type").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_updated_at").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := registry.NewPostgresAgentStoreWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	rows := sqlmock.NewRows([]string{"id", "type", "base_url", "name", "version", "health_url", "capabilities", "status", "last_seen_at", "last_healthy_at"}).
		AddRow("a1", "rule", "http://x", "Rule", "0.1", nil, []byte(`{"task_types":["t"],"tags":["x"]}`), "healthy", nil, nil)
	mock.ExpectQuery("SELECT id,type,base_url").WillReturnRows(rows)

	out, err := s.ListAgents(context.Background(), registry.AgentFilter{Type: "", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 1 || out[0].ID != "a1" {
		t.Fatalf("unexpected: %+v", out)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPostgresAgentStore_UpsertGetTouch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// migrate execs
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_registry").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_type").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_agent_registry_updated_at").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := registry.NewPostgresAgentStoreWithDB(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	now := time.Now()
	a := registry.AgentRecord{
		ID:           "a1",
		Type:         "rule",
		BaseURL:      "http://x",
		Name:         "Rule",
		Version:      "0.1",
		Status:       "healthy",
		LastSeenAt:   &now,
		Capabilities: registry.AgentCapabilities{TaskTypes: []string{"t"}, Tags: []string{"tag"}},
	}
	mock.ExpectExec("INSERT INTO agent_registry").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.UpsertAgent(context.Background(), a); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// GetAgent
	row := sqlmock.NewRows([]string{"id", "type", "base_url", "name", "version", "health_url", "capabilities", "status", "last_seen_at", "last_healthy_at"}).
		AddRow("a1", "rule", "http://x", "Rule", "0.1", sql.NullString{}, []byte(`{"task_types":["t"],"tags":["tag"]}`), "healthy", sql.NullTime{}, sql.NullTime{})
	mock.ExpectQuery("SELECT id,type,base_url").WithArgs("a1").WillReturnRows(row)
	got, err := s.GetAgent(context.Background(), "a1")
	if err != nil || got.ID != "a1" {
		t.Fatalf("get: %v %+v", err, got)
	}

	// TouchAgent
	mock.ExpectExec("UPDATE agent_registry SET status").WithArgs("healthy", sqlmock.AnyArg(), sqlmock.AnyArg(), "a1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.TouchAgent(context.Background(), "a1", "healthy"); err != nil {
		t.Fatalf("touch: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
