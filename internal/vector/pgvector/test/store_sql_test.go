package pgvector_test

import (
	"context"
	"testing"

	"forgeiq/internal/vector/pgvector"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestStore_MigrateAndUpsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS mcp_vectors").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS mcp_vectors_namespace_idx").WillReturnResult(sqlmock.NewResult(0, 0))

	s, err := pgvector.NewWithDB(db, "mcp_vectors", 3)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	mock.ExpectExec("INSERT INTO mcp_vectors").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Upsert(context.Background(), "id1", "default", "c", map[string]any{"title": "t"}, []float64{0, 0, 0}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
