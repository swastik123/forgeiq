package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forgeiq/internal/middleware"
	"go.uber.org/zap"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := middleware.NewRateLimiter(2, 1*time.Second, zap.NewNop())
	if !rl.Allow("a") || !rl.Allow("a") {
		t.Fatalf("first two should be allowed")
	}
	if rl.Allow("a") {
		t.Fatalf("third should be blocked")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 1*time.Minute, zap.NewNop())
	h := middleware.RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5"

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req)
	if rr1.Code != 200 {
		t.Fatalf("expected 200")
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr2.Code)
	}
}
