package pgvector_test

import (
	"forgeiq/internal/vector/pgvector"
	"testing"
)

func TestNew_Validation(t *testing.T) {
	if _, err := pgvector.New("", "x", 3); err == nil {
		t.Fatalf("expected dsn required")
	}
	if _, err := pgvector.New("dsn", "bad-name", 3); err == nil {
		t.Fatalf("expected invalid table")
	}
	if _, err := pgvector.New("dsn", "tbl", 0); err == nil {
		t.Fatalf("expected invalid dim")
	}
}
