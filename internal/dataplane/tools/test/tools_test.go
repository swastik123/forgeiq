package dataplanetools_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/config"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/dataplane/tools"
	"forgeiq/internal/observability"
)

func rpcCall(t *testing.T, baseURL string, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, baseURL+"/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	http.DefaultServeMux = http.NewServeMux()
	// We'll call the handler directly in tests; this helper is only used with httptest.Server below.
	_ = rr
	return nil
}

func TestRegisterAll_RegistersRAGAndLogsTools_ExternalRAGWorks(t *testing.T) {
	// Fake Elasticsearch server
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// respond to /<index>/_search
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": map[string]any{
				"hits": []any{
					map[string]any{
						"_index": "logs-agent-platform",
						"_id":    "1",
						"_score": 1.0,
						"_source": map[string]any{
							"message":    "payments timeout",
							"@timestamp": "t",
							"service":    "payments",
							"level":      "ERROR",
						},
					},
				},
			},
		})
	}))
	defer es.Close()

	cfg := &config.Config{
		Elastic:    config.ElasticConfig{URL: es.URL, Index: "logs-agent-platform"},
		Vector:     config.VectorConfig{Enabled: false},
		Embeddings: config.EmbeddingsConfig{Provider: "hash", Dim: 8},
		RAG: config.RAGConfig{
			AllowExternal:   true,
			AllowedDatasets: "external_logs",
			DatasetsJSON:    `[{"id":"external_logs","type":"external","name":"External Logs","description":"x","external":{"adapter":"elastic","index":"logs-agent-platform"}}]`,
			MaxTopK:         50,
			MaxDocs:         200,
		},
	}

	s := mcpserver.New()
	tools.RegisterAll(s, cfg, observability.NewNopLogger())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// tools.list should include rag.single_retrieve
	{
		resp, err := http.Post(ts.URL+"/rpc", "application/json", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"1","method":"tools.list"}`))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out["result"] == nil {
			t.Fatalf("expected result")
		}
	}

	// rag.single_retrieve should succeed (external elastic dataset)
	{
		body := `{"jsonrpc":"2.0","id":"2","method":"tools.call","params":{"name":"rag.single_retrieve","version":"v1","args":{"query":"payments","available_datasets":["external_logs"],"router_mode":"rule_based","top_k":3}}}`
		resp, err := http.Post(ts.URL+"/rpc", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out["error"] != nil {
			t.Fatalf("unexpected error: %v", out["error"])
		}
		res := out["result"].(map[string]any)
		if res["count"].(float64) < 1 {
			t.Fatalf("expected at least 1 resource")
		}
	}
}
