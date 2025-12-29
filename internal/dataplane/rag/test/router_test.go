package dataplanerag_test

import (
	"forgeiq/internal/dataplane/rag"
	"testing"
)

func TestRuleBasedRouter_TagMatch(t *testing.T) {
	r := &rag.RuleBasedRouter{}
	ds := []rag.DatasetConfig{
		{ID: "a", Name: "A", Tags: []string{"payments"}},
		{ID: "b", Name: "B", Tags: []string{"orders"}},
	}
	id, dbg, err := r.Route(nil, "payments timeout", ds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != "a" {
		t.Fatalf("expected a, got %s (dbg=%v)", id, dbg)
	}
}

func TestRuleBasedRouter_FallbackFirst(t *testing.T) {
	r := &rag.RuleBasedRouter{}
	ds := []rag.DatasetConfig{{ID: "a"}, {ID: "b"}}
	id, dbg, err := r.Route(nil, "no match", ds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != "a" {
		t.Fatalf("expected a, got %s (dbg=%v)", id, dbg)
	}
}
