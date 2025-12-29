package externalelasticsearch_test

import (
	"context"
	"encoding/json"
	"forgeiq/internal/external/elasticsearch"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Search_SendsQuery(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": map[string]any{
				"hits": []any{
					map[string]any{"_index": "idx", "_id": "1", "_score": 1.0, "_source": map[string]any{"message": "m", "@timestamp": "t"}},
				},
			},
		})
	}))
	defer ts.Close()

	c, err := elasticsearch.New(ts.URL, "logs-agent-platform", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	c.WithAuth("abc", "", "")

	hits, err := c.Search(context.Background(), "payments", 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotPath != "/logs-agent-platform/_search" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "ApiKey abc" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"query":"payments"`) {
		t.Fatalf("expected query in body, got %s", gotBody)
	}
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestClient_IndexDoc_RequiresConcreteIndex(t *testing.T) {
	c, err := elasticsearch.New("http://example", "logs-*", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, err := c.IndexDoc(context.Background(), "", map[string]any{"x": 1}); err == nil {
		t.Fatalf("expected error for wildcard index")
	}
}
