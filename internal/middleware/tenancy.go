package middleware

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"forgeiq/internal/config"
)

type tenantContextKey struct{}

// TenantContext is derived from inbound auth credentials.
type TenantContext struct {
	TenantID string
	Subject  string
	AuthType string // "api_key" | "jwt" | ""
}

func TenantFromContext(ctx context.Context) TenantContext {
	if ctx == nil {
		return TenantContext{}
	}
	if v := ctx.Value(tenantContextKey{}); v != nil {
		if tc, ok := v.(TenantContext); ok {
			return tc
		}
	}
	return TenantContext{}
}

func WithTenantContext(ctx context.Context, tc TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tc)
}

// TenantAuthMiddleware authenticates a request as a tenant using either:
// - X-API-Key (mapped to tenant via TENANCY_API_KEYS), or
// - Authorization: Bearer <JWT> (verified via HS256 or RS256).
//
// It sets TenantContext on the request context and optionally enforces tenant isolation
// for task-id based endpoints by inspecting the task_id prefix "tenant-<id>-task-...".
func TenantAuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg == nil || !cfg.Tenancy.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			tc, ok, err := authenticateTenant(cfg, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if !ok && cfg.Tenancy.RequireAuth {
				http.Error(w, "missing tenant authentication", http.StatusUnauthorized)
				return
			}

			// If authenticated, enforce that task_id paths match tenant prefix.
			if tc.TenantID != "" {
				if tid := extractTaskIDFromPath(r.URL.Path); tid != "" {
					if want, ok := tenantFromTaskID(tid); ok && want != tc.TenantID {
						http.Error(w, "forbidden (tenant mismatch)", http.StatusForbidden)
						return
					}
				}
			}

			next.ServeHTTP(w, r.WithContext(WithTenantContext(r.Context(), tc)))
		})
	}
}

func extractTaskIDFromPath(path string) string {
	// Supported endpoint prefixes that carry taskID as suffix.
	prefixes := []string{
		"/status/",
		"/result/",
		"/approve/",
		"/feedback/",
		"/refine/",
		"/stop/",
		"/events/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			id := strings.TrimPrefix(path, p)
			id = strings.TrimSpace(id)
			// defensive: do not allow slashes in id
			if id == "" || strings.Contains(id, "/") {
				return ""
			}
			return id
		}
	}
	return ""
}

func tenantFromTaskID(taskID string) (string, bool) {
	// taskID: tenant-<tenant>-task-<timestamp>
	if !strings.HasPrefix(taskID, "tenant-") {
		return "", false
	}
	rest := strings.TrimPrefix(taskID, "tenant-")
	parts := strings.SplitN(rest, "-task-", 2)
	if len(parts) != 2 {
		return "", false
	}
	tenant := strings.TrimSpace(parts[0])
	if tenant == "" {
		return "", false
	}
	return tenant, true
}

func authenticateTenant(cfg *config.Config, r *http.Request) (TenantContext, bool, error) {
	// Prefer JWT if present.
	if ah := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
		tok := strings.TrimSpace(ah[len("bearer "):])
		tc, err := verifyJWTAndExtractTenant(cfg, tok)
		if err != nil {
			return TenantContext{}, false, err
		}
		return tc, true, nil
	}

	// API key
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		return TenantContext{}, false, nil
	}
	tenant, ok := tenantForAPIKey(cfg.Tenancy.APIKeys, apiKey)
	if !ok {
		return TenantContext{}, false, fmt.Errorf("invalid api key")
	}
	return TenantContext{TenantID: tenant, Subject: "api_key", AuthType: "api_key"}, true, nil
}

func tenantForAPIKey(raw string, key string) (string, bool) {
	// raw: "tenant:key,tenant2:key2"
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		tenant := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if tenant != "" && val != "" && val == key {
			return tenant, true
		}
	}
	return "", false
}

func verifyJWTAndExtractTenant(cfg *config.Config, token string) (TenantContext, error) {
	headerB, payloadB, sigB, err := splitJWT(token)
	if err != nil {
		return TenantContext{}, err
	}

	var header map[string]any
	if err := json.Unmarshal(headerB, &header); err != nil {
		return TenantContext{}, fmt.Errorf("jwt header decode: %w", err)
	}
	alg, _ := header["alg"].(string)
	alg = strings.TrimSpace(alg)

	// Verify signature
	signed := jwtSigningInput(token)
	switch alg {
	case "HS256":
		sec := cfg.Tenancy.JWTHS256Secret
		if strings.TrimSpace(sec) == "" {
			return TenantContext{}, fmt.Errorf("jwt hs256 secret not configured")
		}
		mac := hmac.New(sha256.New, []byte(sec))
		_, _ = mac.Write([]byte(signed))
		want := mac.Sum(nil)
		if !hmac.Equal(want, sigB) {
			return TenantContext{}, fmt.Errorf("jwt signature invalid")
		}
	case "RS256":
		pemStr := strings.TrimSpace(cfg.Tenancy.JWTRS256PublicKeyPEM)
		if pemStr == "" {
			return TenantContext{}, fmt.Errorf("jwt rs256 public key not configured")
		}
		pub, err := parseRSAPublicKeyPEM(pemStr)
		if err != nil {
			return TenantContext{}, err
		}
		h := sha256.Sum256([]byte(signed))
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sigB); err != nil {
			return TenantContext{}, fmt.Errorf("jwt signature invalid")
		}
	default:
		return TenantContext{}, fmt.Errorf("unsupported jwt alg: %s", alg)
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadB, &claims); err != nil {
		return TenantContext{}, fmt.Errorf("jwt claims decode: %w", err)
	}

	// Standard validations (best-effort, but enforce exp/nbf if present)
	if iss := strings.TrimSpace(cfg.Tenancy.JWTIssuer); iss != "" {
		if ciss, _ := claims["iss"].(string); strings.TrimSpace(ciss) != iss {
			return TenantContext{}, fmt.Errorf("jwt issuer mismatch")
		}
	}
	if aud := strings.TrimSpace(cfg.Tenancy.JWTAudience); aud != "" {
		if !jwtAudienceMatches(claims["aud"], aud) {
			return TenantContext{}, fmt.Errorf("jwt audience mismatch")
		}
	}
	now := time.Now().Unix()
	if exp, ok := claimInt64(claims["exp"]); ok && now >= exp {
		return TenantContext{}, fmt.Errorf("jwt expired")
	}
	if nbf, ok := claimInt64(claims["nbf"]); ok && now < nbf {
		return TenantContext{}, fmt.Errorf("jwt not yet valid")
	}

	tenantClaim := strings.TrimSpace(cfg.Tenancy.JWTTenantClaim)
	if tenantClaim == "" {
		tenantClaim = "tenant_id"
	}
	tenant, _ := claims[tenantClaim].(string)
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		// fallback common claim
		if t, _ := claims["tid"].(string); strings.TrimSpace(t) != "" {
			tenant = strings.TrimSpace(t)
		}
	}
	if tenant == "" {
		return TenantContext{}, fmt.Errorf("jwt missing tenant claim")
	}
	sub, _ := claims["sub"].(string)
	return TenantContext{TenantID: tenant, Subject: strings.TrimSpace(sub), AuthType: "jwt"}, nil
}

func splitJWT(tok string) ([]byte, []byte, []byte, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, nil, nil, fmt.Errorf("invalid jwt format")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("jwt header b64: %w", err)
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("jwt payload b64: %w", err)
	}
	sb, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("jwt signature b64: %w", err)
	}
	return hb, pb, sb, nil
}

func jwtSigningInput(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return tok
	}
	return parts[0] + "." + parts[1]
}

func parseRSAPublicKeyPEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("invalid rsa public key pem")
	}
	// Try PKIX first
	if pubAny, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if pub, ok := pubAny.(*rsa.PublicKey); ok {
			return pub, nil
		}
	}
	// Try PKCS1
	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	// Try cert
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pub, nil
		}
	}
	return nil, fmt.Errorf("failed to parse rsa public key pem")
}

func jwtAudienceMatches(raw any, want string) bool {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) == want
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && strings.TrimSpace(s) == want {
				return true
			}
		}
	}
	return false
}

func claimInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
