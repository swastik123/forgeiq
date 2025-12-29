package observability_test

import (
	"forgeiq/internal/observability"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPMiddleware_LoggingAndMetrics(t *testing.T) {
	logger := observability.NewNopLogger()
	metrics := observability.NewMetrics()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("ok"))
	})

	h := observability.HTTPLoggingMiddleware(logger, observability.HTTPMetricsMiddleware(metrics, next))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 201 {
		t.Fatalf("expected 201, got %d", rr.Code)
	}
}
