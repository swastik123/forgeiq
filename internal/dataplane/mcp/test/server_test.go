package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/dataplane/mcp"
)

func TestServer_ToolsListAndCall(t *testing.T) {
	s := mcp.New()
	s.RegisterTool("echo", "v1", func(args map[string]any) (map[string]any, error) {
		return map[string]any{"args": args}, nil
	})

	h := s.Handler()

	// tools.list
	{
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"1","method":"tools.list"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["result"] == nil {
			t.Fatalf("expected result")
		}
	}

	// tools.call
	{
		body := `{"jsonrpc":"2.0","id":"2","method":"tools.call","params":{"name":"echo","version":"v1","args":{"x":1}}}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["error"] != nil {
			t.Fatalf("unexpected error: %v", resp["error"])
		}
		if resp["result"] == nil {
			t.Fatalf("expected result")
		}
	}
}

func TestServer_InvalidMethod(t *testing.T) {
	s := mcp.New()
	req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestServer_ParseError(t *testing.T) {
	s := mcp.New()
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with rpc error envelope, got %d", rr.Code)
	}
}
