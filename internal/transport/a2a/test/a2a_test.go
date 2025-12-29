package transporta2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/transport/a2a"
)

func TestClient_RunTask_SetsAuthHeaders(t *testing.T) {
	var gotAPIKey string
	var gotAuth string
	var gotCustom string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		_ = json.NewEncoder(w).Encode(contracts.Artifact{TaskID: "t1", Type: "ok", Payload: map[string]any{"x": 1}})
	}))
	defer ts.Close()

	c := a2a.NewClientWithConfig(a2a.ClientConfig{
		BaseURL: ts.URL,
		APIKey:  "k",
		Token:   "tok",
		Headers: map[string]string{"X-Custom": "v"},
	})

	_, err := c.RunTask(context.Background(), contracts.Task{ID: "t1"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotAPIKey != "k" {
		t.Fatalf("expected X-API-Key, got %q", gotAPIKey)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("expected Authorization, got %q", gotAuth)
	}
	if gotCustom != "v" {
		t.Fatalf("expected custom header, got %q", gotCustom)
	}
}
