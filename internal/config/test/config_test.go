package config_test

import (
	"testing"

	"forgeiq/internal/config"
)

func TestLoad_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("TEMPORAL_HOSTPORT", "t:7233")
	t.Setenv("VECTOR_ENABLED", "true")
	t.Setenv("VECTOR_DIM", "8")

	cfg := config.Load()
	if cfg.Server.Port != "9999" {
		t.Fatalf("expected port override, got %s", cfg.Server.Port)
	}
	if cfg.Temporal.HostPort != "t:7233" {
		t.Fatalf("expected temporal override")
	}
	if !cfg.Vector.Enabled || cfg.Vector.Dim != 8 {
		t.Fatalf("expected vector enabled + dim 8")
	}
}

func TestAuthConfig_ParseHeaders(t *testing.T) {
	a := &config.AuthConfig{MCPHeaders: "X-A=1, X-B = two"}
	h := a.GetMCPHeaders()
	if h["X-A"] != "1" || h["X-B"] != "two" {
		t.Fatalf("unexpected headers: %+v", h)
	}
}
