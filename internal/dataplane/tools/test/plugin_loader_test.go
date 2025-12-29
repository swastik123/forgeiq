package dataplanetools_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"forgeiq/internal/config"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/dataplane/tools"
	"forgeiq/internal/observability"
)

func TestPluginLoader_HTTPTool_RoundTripViaRPC(t *testing.T) {
	// External endpoint that the plugin tool will call
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args map[string]any
		_ = json.NewDecoder(r.Body).Decode(&args)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"echo": args["q"],
		})
	}))
	t.Cleanup(ext.Close)

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	toolsPath := filepath.Join(dir, "web.json")

	if err := os.WriteFile(manifestPath, []byte("plugins:\n  - id: test\n    enabled: true\n    tools_file: web.json\n"), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	toolJSON := `{
  "tools": [
    {
      "name": "web.search",
      "version": "v1",
      "tags": ["read"],
      "scopes": ["web:read"],
      "schema": {"type":"object","properties":{"q":{"type":"string"}},"required":["q"]},
      "type": "http",
      "http": {"url":"` + ext.URL + `","method":"POST","timeout_ms": 5000}
    }
  ]
}`
	if err := os.WriteFile(toolsPath, []byte(toolJSON), 0644); err != nil {
		t.Fatalf("write tools: %v", err)
	}

	cfg := config.Load()
	cfg.Plugins.Enabled = true
	cfg.Plugins.ManifestPath = manifestPath
	cfg.Plugins.HTTPAllowlist = "*"

	s := mcpserver.New()
	tools.RegisterPlugins(s, cfg, observability.NewNopLogger())

	// Call tool over MCP JSON-RPC
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "tools.call",
		"params": map[string]any{
			"name":    "web.search",
			"version": "v1",
			"args":    map[string]any{"q": "hello"},
		},
	}
	b, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(b))
	s.Handler().ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("rpc status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}
	if resp.Result["ok"] != true {
		t.Fatalf("expected ok=true, got %v", resp.Result["ok"])
	}
	if resp.Result["echo"] != "hello" {
		t.Fatalf("expected echo=hello, got %v", resp.Result["echo"])
	}
}
