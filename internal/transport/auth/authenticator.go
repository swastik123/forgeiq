package auth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

// Authenticator handles authentication for external services
type Authenticator interface {
	Authenticate(req *http.Request) error
	GetHTTPClient() (*http.Client, error)
}

// NoneAuth provides no authentication
type NoneAuth struct{}

func (a *NoneAuth) Authenticate(req *http.Request) error {
	return nil
}

func (a *NoneAuth) GetHTTPClient() (*http.Client, error) {
	return &http.Client{}, nil
}

// APIKeyAuth provides API key authentication
type APIKeyAuth struct {
	APIKey      string
	HeaderName  string // e.g., "X-API-Key", "Authorization"
	HeaderValue string // e.g., "Bearer {key}" or just "{key}"
}

func NewAPIKeyAuth(apiKey, headerName string) *APIKeyAuth {
	auth := &APIKeyAuth{
		APIKey:     apiKey,
		HeaderName: headerName,
	}
	
	// Default to Authorization header with Bearer prefix if not specified
	if headerName == "" {
		auth.HeaderName = "Authorization"
		auth.HeaderValue = fmt.Sprintf("Bearer %s", apiKey)
	} else if headerName == "Authorization" {
		auth.HeaderValue = fmt.Sprintf("Bearer %s", apiKey)
	} else {
		auth.HeaderValue = apiKey
	}
	
	return auth
}

func (a *APIKeyAuth) Authenticate(req *http.Request) error {
	req.Header.Set(a.HeaderName, a.HeaderValue)
	return nil
}

func (a *APIKeyAuth) GetHTTPClient() (*http.Client, error) {
	return &http.Client{}, nil
}

// OAuthAuth provides OAuth token authentication
type OAuthAuth struct {
	Token string
}

func NewOAuthAuth(token string) *OAuthAuth {
	return &OAuthAuth{Token: token}
}

func (a *OAuthAuth) Authenticate(req *http.Request) error {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.Token))
	return nil
}

func (a *OAuthAuth) GetHTTPClient() (*http.Client, error) {
	return &http.Client{}, nil
}

// MTLSAuth provides mTLS authentication
type MTLSAuth struct {
	CertPath string
	KeyPath  string
	CAPath   string
}

func NewMTLSAuth(certPath, keyPath, caPath string) *MTLSAuth {
	return &MTLSAuth{
		CertPath: certPath,
		KeyPath:  keyPath,
		CAPath:   caPath,
	}
}

func (a *MTLSAuth) Authenticate(req *http.Request) error {
	// mTLS is handled at the HTTP client level, not in headers
	return nil
}

func (a *MTLSAuth) GetHTTPClient() (*http.Client, error) {
	// Load client certificate
	cert, err := tls.LoadX509KeyPair(a.CertPath, a.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate if provided
	var caCertPool *x509.CertPool
	if a.CAPath != "" {
		caCert, err := ioutil.ReadFile(a.CAPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		caCertPool = x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
	} else {
		caCertPool, err = x509.SystemCertPool()
		if err != nil {
			caCertPool = x509.NewCertPool()
		}
	}

	// Create TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}

	// Create HTTP client with mTLS
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return &http.Client{Transport: transport}, nil
}

// CreateAuthenticator creates an authenticator based on auth type
func CreateAuthenticator(authType, apiKey, oauthToken, mtlsCert, mtlsKey, mtlsCA, headerName string) (Authenticator, error) {
	switch authType {
	case "none", "":
		return &NoneAuth{}, nil
	case "api_key":
		if apiKey == "" {
			return nil, fmt.Errorf("API key is required for api_key auth type")
		}
		return NewAPIKeyAuth(apiKey, headerName), nil
	case "oauth":
		if oauthToken == "" {
			return nil, fmt.Errorf("OAuth token is required for oauth auth type")
		}
		return NewOAuthAuth(oauthToken), nil
	case "mtls":
		if mtlsCert == "" || mtlsKey == "" {
			return nil, fmt.Errorf("mTLS certificate and key are required for mtls auth type")
		}
		return NewMTLSAuth(mtlsCert, mtlsKey, mtlsCA), nil
	default:
		return nil, fmt.Errorf("unknown auth type: %s", authType)
	}
}

// LoadFromEnv creates an authenticator from environment variables with prefix
func LoadFromEnv(prefix string) (Authenticator, error) {
	authType := os.Getenv(prefix + "_AUTH_TYPE")
	if authType == "" {
		authType = "none"
	}

	return CreateAuthenticator(
		authType,
		os.Getenv(prefix+"_API_KEY"),
		os.Getenv(prefix+"_OAUTH_TOKEN"),
		os.Getenv(prefix+"_MTLS_CERT"),
		os.Getenv(prefix+"_MTLS_KEY"),
		os.Getenv(prefix+"_MTLS_CA"),
		os.Getenv(prefix+"_API_KEY_HEADER"), // e.g., "X-API-Key" for custom headers
	)
}

