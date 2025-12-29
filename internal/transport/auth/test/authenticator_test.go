package transportauth_test

import (
	"forgeiq/internal/transport/auth"
	"net/http"
	"testing"
)

func TestAPIKeyAuth_DefaultAuthorizationBearer(t *testing.T) {
	a := auth.NewAPIKeyAuth("k", "")
	req, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	_ = a.Authenticate(req)
	if req.Header.Get("Authorization") != "Bearer k" {
		t.Fatalf("expected bearer auth, got %q", req.Header.Get("Authorization"))
	}
}

func TestAPIKeyAuth_CustomHeader(t *testing.T) {
	a := auth.NewAPIKeyAuth("k", "X-API-Key")
	req, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	_ = a.Authenticate(req)
	if req.Header.Get("X-API-Key") != "k" {
		t.Fatalf("expected custom header")
	}
}

func TestCreateAuthenticator_Errors(t *testing.T) {
	if _, err := auth.CreateAuthenticator("api_key", "", "", "", "", "", ""); err == nil {
		t.Fatalf("expected api key required error")
	}
	if _, err := auth.CreateAuthenticator("oauth", "", "", "", "", "", ""); err == nil {
		t.Fatalf("expected oauth token required error")
	}
	if _, err := auth.CreateAuthenticator("mtls", "", "", "", "", "", ""); err == nil {
		t.Fatalf("expected mtls cert+key required error")
	}
}
