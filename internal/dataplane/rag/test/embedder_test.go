package dataplanerag_test

import (
	"context"
	"encoding/json"
	"forgeiq/internal/dataplane/rag"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHashEmbedder_Deterministic(t *testing.T) {
	e := rag.NewHashEmbedder(8)
	v1, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	v2, _ := e.Embed(context.Background(), "hello")
	if len(v1) != 8 || len(v2) != 8 {
		t.Fatalf("expected dim 8")
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("expected deterministic output")
		}
	}
}

func TestHTTPEmbedder(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["input"] == "" {
			http.Error(w, "missing input", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float64{1, 2, 3},
		})
	}))
	defer ts.Close()

	e, err := rag.NewHTTPEmbedder(ts.URL, "", "", 3)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	emb, err := e.Embed(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(emb) != 3 {
		t.Fatalf("expected dim 3")
	}
}
