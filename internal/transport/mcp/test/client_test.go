package transportmcp_test

import (
	"context"
	"encoding/json"
	mcpclient "forgeiq/internal/transport/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ListToolsAndCallTool(t *testing.T) {
	var gotAuth string
	var gotAPIKey string
	var gotBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-API-Key")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		var req map[string]any
		_ = json.Unmarshal(b, &req)
		method, _ := req["method"].(string)
		switch method {
		case "tools.list":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "1", "result": []any{map[string]any{"name": "x", "version": "v1"}}})
		case "tools.call":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "1", "result": map[string]any{"ok": true}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "1", "error": map[string]any{"code": -32601, "message": "Method not found"}})
		}
	}))
	defer ts.Close()

	c := mcpclient.NewClientWithConfig(mcpclient.ClientConfig{BaseURL: ts.URL, APIKey: "k", Token: "t", Headers: map[string]string{"X-Custom": "v"}})

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "x" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if gotAPIKey != "k" || gotAuth != "Bearer t" {
		t.Fatalf("expected auth headers set, got apiKey=%q auth=%q", gotAPIKey, gotAuth)
	}
	if !strings.Contains(gotBody, `"method":"tools.list"`) {
		t.Fatalf("expected tools.list request, got %s", gotBody)
	}

	out, err := c.CallTool(context.Background(), "x", "v1", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("unexpected out: %+v", out)
	}
}
