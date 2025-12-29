package dataplanerag_test

import (
	"context"
	"testing"

	"forgeiq/internal/dataplane/rag"
	"forgeiq/internal/external/elasticsearch"
	"forgeiq/internal/vector/pgvector"
)

type fakeVector struct {
	recs []pgvector.Record
}

func (f *fakeVector) Search(ctx context.Context, namespace string, queryEmbedding []float64, k int) ([]pgvector.Record, error) {
	_ = ctx
	_ = namespace
	_ = queryEmbedding
	if k > 0 && k < len(f.recs) {
		return f.recs[:k], nil
	}
	return f.recs, nil
}

type fakeElastic struct {
	hits []elasticsearch.SearchHit
}

func (f *fakeElastic) Search(ctx context.Context, query string, limit int) ([]elasticsearch.SearchHit, error) {
	_ = ctx
	_ = query
	if limit > 0 && limit < len(f.hits) {
		return f.hits[:limit], nil
	}
	return f.hits, nil
}

type fakeEmbedder struct{ dim int }

func (f fakeEmbedder) Dim() int { return f.dim }
func (f fakeEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	_ = ctx
	_ = text
	return make([]float64, f.dim), nil
}

func TestService_RetrieveSingle_Internal(t *testing.T) {
	reg, err := rag.LoadRegistryFromJSON(`[{"id":"d1","type":"internal","name":"D1","description":"", "internal":{"namespace":"ns"}}]`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	svc := &rag.Service{
		Registry: reg,
		Internal: &rag.InternalRetriever{
			Vector:   &fakeVector{recs: []pgvector.Record{{ID: "doc1", Content: "c", Score: 0.9}}},
			Embedder: fakeEmbedder{dim: 3},
		},
		ExternalElastic: &rag.ElasticRetriever{Client: &fakeElastic{}},
		RuleRouter:      &rag.RuleBasedRouter{},
	}

	res, debug, err := svc.RetrieveSingle(context.Background(), "anything", []string{"d1"}, rag.RouterRuleBased, 5, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if debug["selected_dataset_id"] != "d1" {
		t.Fatalf("expected selected_dataset_id=d1, got %v", debug["selected_dataset_id"])
	}
	if len(res) != 1 || res[0].Source != "internal" || res[0].DocID != "doc1" {
		t.Fatalf("unexpected resources: %+v", res)
	}
}

func TestService_RetrieveMultiple_Mixed(t *testing.T) {
	reg, err := rag.LoadRegistryFromJSON(`[
	  {"id":"i","type":"internal","name":"I","description":"", "internal":{"namespace":"ns"}},
	  {"id":"e","type":"external","name":"E","description":"", "external":{"adapter":"elastic","index":"x"}}
	]`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	svc := &rag.Service{
		Registry: reg,
		Internal: &rag.InternalRetriever{
			Vector:   &fakeVector{recs: []pgvector.Record{{ID: "doc1", Content: "c", Score: 0.9}}},
			Embedder: fakeEmbedder{dim: 3},
		},
		ExternalElastic: &rag.ElasticRetriever{Client: &fakeElastic{hits: []elasticsearch.SearchHit{{ID: "h1", Score: 1.0, Source: map[string]any{"message": "m"}}}}},
		RuleRouter:      &rag.RuleBasedRouter{},
	}

	res, debug, err := svc.RetrieveMultiple(context.Background(), "q", []string{"i", "e"}, 5, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if debug["datasets"].(int) != 2 {
		t.Fatalf("expected datasets=2")
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
}
