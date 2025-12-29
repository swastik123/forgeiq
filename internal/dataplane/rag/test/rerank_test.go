package dataplanerag_test

import (
	"forgeiq/internal/dataplane/rag"
	"testing"
)

func TestWeightedReranker(t *testing.T) {
	in := []rag.RetrievalResource{
		{DocID: "1", Source: "internal", Score: 1.0},
		{DocID: "2", Source: "external", Score: 1.0},
	}
	r := &rag.WeightedReranker{Weights: map[string]float64{"internal": 0.5, "external": 2.0}}
	out, err := r.Rerank(nil, "q", in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out[0].DocID != "2" {
		t.Fatalf("expected external doc to rank first, got %s", out[0].DocID)
	}
}

func TestTruncateAndThreshold(t *testing.T) {
	in := []rag.RetrievalResource{
		{DocID: "1", Score: 0.9},
		{DocID: "2", Score: 0.1},
		{DocID: "3", Score: 0.8},
	}
	out := rag.TruncateAndThreshold(in, 2, 0.5)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0].DocID != "1" {
		t.Fatalf("expected doc 1 first")
	}
}
