package observability_test

import (
	"forgeiq/internal/observability"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthChecker_Healthy(t *testing.T) {
	l := observability.NewNopLogger()
	hc := observability.NewHealthChecker(l)
	hc.RegisterCheck("ok", func() error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	hc.HealthHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHealthChecker_Unhealthy(t *testing.T) {
	l := observability.NewNopLogger()
	hc := observability.NewHealthChecker(l)
	hc.RegisterCheck("bad", func() error { return errTest("no") })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	hc.HealthHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestReadyHandler(t *testing.T) {
	l := observability.NewNopLogger()
	hc := observability.NewHealthChecker(l)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	hc.ReadyHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
