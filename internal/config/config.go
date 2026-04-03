package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds application configuration
type Config struct {
	// Server configuration
	Server ServerConfig

	// Temporal configuration
	Temporal TemporalConfig

	// Agent URLs
	RuleAgentURL     string
	DecisionAgentURL string
	PRAgentURL       string
	MCPBaseURL       string

	// Authentication
	Auth AuthConfig

	// Observability
	Observability ObservabilityConfig

	// Storage
	Storage StorageConfig

	// Vector DB (pgvector on Postgres)
	Vector VectorConfig

	// External sources (Elasticsearch)
	Elastic ElasticConfig

	// Embeddings (for internal RAG retrieval)
	Embeddings EmbeddingsConfig

	// RAG (dataset registry / policy)
	RAG RAGConfig

	// Agent router (A2A discovery + routing)
	AgentRouter AgentRouterConfig

	// Runtime tool plugins (loaded at startup)
	Plugins PluginsConfig

	// Multi-tenancy / inbound auth for control plane HTTP APIs
	Tenancy TenancyConfig

	// Reasoning service configuration (optional)
	Reasoning ReasoningConfig
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port         string
	ReadTimeout  int // seconds
	WriteTimeout int // seconds
	IdleTimeout  int // seconds
}

// TemporalConfig holds Temporal configuration
type TemporalConfig struct {
	HostPort  string
	TaskQueue string
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	// API Keys for external services
	RuleAgentAPIKey     string
	DecisionAgentAPIKey string
	PRAgentAPIKey       string
	MCPAPIKey           string

	// OAuth tokens
	RuleAgentToken     string
	DecisionAgentToken string
	PRAgentToken       string
	MCPToken           string

	// Custom headers (key=value pairs, comma-separated)
	RuleAgentHeaders     string
	DecisionAgentHeaders string
	PRAgentHeaders       string
	MCPHeaders           string
}

// ObservabilityConfig holds observability configuration
type ObservabilityConfig struct {
	LogLevel       string
	JaegerEndpoint string
	MetricsEnabled bool
	TracingEnabled bool
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Type string // "postgres", "mongodb", "memory"
	DSN  string // Data source name/connection string
}

// VectorConfig holds pgvector configuration (runs on Postgres)
type VectorConfig struct {
	Enabled bool
	DSN     string
	Table   string
	Dim     int
}

// ElasticConfig holds Elasticsearch configuration
type ElasticConfig struct {
	URL      string
	Index    string
	APIKey   string
	Username string
	Password string
}

// EmbeddingsConfig controls how query embeddings are generated for internal retrieval.
type EmbeddingsConfig struct {
	Provider string // "hash" | "http"
	Dim      int

	// HTTP provider
	URL    string
	APIKey string
	Model  string
}

// RAGConfig holds RAG dataset registry and safety/policy knobs.
type RAGConfig struct {
	// JSON array of dataset configs.
	// Example:
	// [{"id":"internal_docs","type":"internal","name":"Internal Docs","description":"...","internal":{"namespace":"default"}}, ...]
	DatasetsJSON string

	// Comma-separated allowed dataset IDs. Empty means "allow all configured datasets".
	AllowedDatasets string

	// If false, any dataset with type=external is blocked.
	AllowExternal bool

	// Hard limits to avoid runaway cost.
	MaxTopK int
	MaxDocs int
}

// AgentRouterConfig controls dynamic routing to A2A agents (rule/decision/etc).
type AgentRouterConfig struct {
	Enabled bool

	// Routing mode:
	// - "simple": heuristic routing only
	// - "hybrid": heuristic pre-filter + LLM-assisted selection with fallback
	Mode string

	// Selection mode (heuristic router):
	// - "deterministic": always pick top-1 (with deterministic tie-break)
	// - "softmax": sample from top-N using softmax(score/temperature)
	Selection string

	// If true, break exact-score ties randomly (seeded by request).
	TieBreakRandom bool
	// Scores within epsilon of top are considered tied.
	TieEpsilon float64

	SoftmaxTopN        int
	SoftmaxTemperature float64

	// Runtime-learned weights (bandit-ish). Computed from eval_runs and injected into scoring.
	RuntimeWeightsEnabled        bool
	RuntimeWeightsMax            float64
	RuntimeWeightsMinRuns        int
	RuntimeWeightsWindowHours    int
	RuntimeWeightsRefreshSeconds int

	// JSON array of agent entries.
	// Example:
	// [{"id":"rule-local","type":"rule","base_url":"http://rule-agent:8081","tags":["rules"],"task_types":["policy_eval"]}]
	AgentsJSON string

	// Enable discovery refresh by fetching agent cards from base_url + DiscoveryPath.
	DiscoveryEnabled bool
	DiscoveryPath    string

	// If true, load agents from the Agent Registry (Postgres) instead of static JSON.
	RegistryEnabled bool

	// LLM-assisted routing config (used when Mode=="hybrid")
	LLM LLMRouterConfig
}

// PluginsConfig controls runtime plugin loading for MCP tools.
// Plugins are typically "HTTP tools" that proxy to external services (search, github, wolfram, image generators, etc.)
type PluginsConfig struct {
	Enabled bool
	// Path to plugins/manifest.yaml
	ManifestPath string
	// Comma-separated allowlist of hostnames/suffixes for HTTP tool endpoints.
	// Use "*" to allow all (not recommended for production).
	HTTPAllowlist string
	// Default timeout for HTTP tools, in milliseconds.
	DefaultHTTPTimeoutMS int
}

// TenancyConfig controls tenant authentication and isolation.
// This is used by the control-orchestrator HTTP API layer (inbound requests).
type TenancyConfig struct {
	Enabled bool
	// If true, requests must authenticate as a tenant (API key or JWT).
	RequireAuth bool

	// API key auth: comma-separated "tenant:key" pairs.
	// Example: TENANCY_API_KEYS="acme:abc123,globex:def456"
	APIKeys string

	// JWT auth configuration
	JWTIssuer      string // optional
	JWTAudience    string // optional
	JWTTenantClaim string // claim name for tenant id (default "tenant_id")

	// One of the following should be provided for signature verification:
	// - HS256: shared secret
	JWTHS256Secret string
	// - RS256: PEM-encoded public key
	JWTRS256PublicKeyPEM string
}

// ReasoningConfig controls which "reasoning backend" Go components use when they need model decisions.
// - Provider: "mcp" (default) uses MCP tool llm.chat
// - Provider: "dspy" calls an external DSPy reasoning service
type ReasoningConfig struct {
	Provider string
	DSPyURL  string
	DSPyKey  string
}

// LLMRouterConfig configures LLM-assisted agent routing.
type LLMRouterConfig struct {
	Enabled bool
	// Full URL to the LLM router endpoint (e.g., http://llm-router:8085/choose)
	URL string
	// Request timeout in milliseconds (e.g., 1500)
	TimeoutMS int
	// Max candidates to include in LLM prompt (top-N from heuristic)
	MaxCandidates int

	// Optional auth for the LLM router endpoint
	APIKey  string
	Token   string
	Headers string // comma-separated key=value
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         getEnv("PORT", "8080"),
			ReadTimeout:  getEnvInt("READ_TIMEOUT", 15),
			WriteTimeout: getEnvInt("WRITE_TIMEOUT", 15),
			IdleTimeout:  getEnvInt("IDLE_TIMEOUT", 60),
		},
		Temporal: TemporalConfig{
			HostPort:  getEnv("TEMPORAL_HOSTPORT", "localhost:7233"),
			TaskQueue: getEnv("TEMPORAL_TASK_QUEUE", "CONTROL_PLANE_TASK_QUEUE"),
		},
		RuleAgentURL:     getEnv("RULE_AGENT_URL", "http://localhost:8081"),
		DecisionAgentURL: getEnv("DECISION_AGENT_URL", "http://localhost:8082"),
		PRAgentURL:       getEnv("PR_AGENT_URL", "http://localhost:8086"),
		MCPBaseURL:       getEnv("MCP_BASE_URL", "http://localhost:8090"),
		Auth: AuthConfig{
			RuleAgentAPIKey:      getEnv("RULE_AGENT_API_KEY", ""),
			DecisionAgentAPIKey:  getEnv("DECISION_AGENT_API_KEY", ""),
			PRAgentAPIKey:        getEnv("PR_AGENT_API_KEY", ""),
			MCPAPIKey:            getEnv("MCP_API_KEY", ""),
			RuleAgentToken:       getEnv("RULE_AGENT_TOKEN", ""),
			DecisionAgentToken:   getEnv("DECISION_AGENT_TOKEN", ""),
			PRAgentToken:         getEnv("PR_AGENT_TOKEN", ""),
			MCPToken:             getEnv("MCP_TOKEN", ""),
			RuleAgentHeaders:     getEnv("RULE_AGENT_HEADERS", ""),
			DecisionAgentHeaders: getEnv("DECISION_AGENT_HEADERS", ""),
			PRAgentHeaders:       getEnv("PR_AGENT_HEADERS", ""),
			MCPHeaders:           getEnv("MCP_HEADERS", ""),
		},
		Observability: ObservabilityConfig{
			LogLevel:       getEnv("LOG_LEVEL", "info"),
			JaegerEndpoint: getEnv("JAEGER_ENDPOINT", ""),
			MetricsEnabled: getEnvBool("METRICS_ENABLED", true),
			TracingEnabled: getEnvBool("TRACING_ENABLED", false),
		},
		Storage: StorageConfig{
			Type: getEnv("STORAGE_TYPE", "memory"),
			DSN:  getEnv("STORAGE_DSN", ""),
		},
		Vector: VectorConfig{
			Enabled: getEnvBool("VECTOR_ENABLED", false),
			DSN:     getEnv("VECTOR_DSN", getEnv("STORAGE_DSN", "")),
			Table:   getEnv("VECTOR_TABLE", "mcp_vectors"),
			Dim:     getEnvInt("VECTOR_DIM", 1536),
		},
		Elastic: ElasticConfig{
			URL:      getEnv("ELASTIC_URL", ""),
			Index:    getEnv("ELASTIC_INDEX", "logs-*"),
			APIKey:   getEnv("ELASTIC_API_KEY", ""),
			Username: getEnv("ELASTIC_USERNAME", ""),
			Password: getEnv("ELASTIC_PASSWORD", ""),
		},
		Embeddings: EmbeddingsConfig{
			Provider: getEnv("EMBEDDINGS_PROVIDER", "hash"),
			Dim:      getEnvInt("EMBEDDINGS_DIM", getEnvInt("VECTOR_DIM", 1536)),
			URL:      getEnv("EMBEDDINGS_URL", ""),
			APIKey:   getEnv("EMBEDDINGS_API_KEY", ""),
			Model:    getEnv("EMBEDDINGS_MODEL", ""),
		},
		RAG: RAGConfig{
			DatasetsJSON:    getEnv("RAG_DATASETS_JSON", ""),
			AllowedDatasets: getEnv("RAG_ALLOWED_DATASETS", ""),
			AllowExternal:   getEnvBool("RAG_ALLOW_EXTERNAL", true),
			MaxTopK:         getEnvInt("RAG_MAX_TOP_K", 50),
			MaxDocs:         getEnvInt("RAG_MAX_DOCS", 200),
		},
		AgentRouter: AgentRouterConfig{
			Enabled:                      getEnvBool("AGENT_ROUTER_ENABLED", false),
			Mode:                         getEnv("AGENT_ROUTER_MODE", "simple"),
			Selection:                    getEnv("AGENT_ROUTER_SELECTION", "deterministic"),
			TieBreakRandom:               getEnvBool("AGENT_ROUTER_TIEBREAK_RANDOM", false),
			TieEpsilon:                   getEnvFloat("AGENT_ROUTER_TIE_EPSILON", 0.000001),
			SoftmaxTopN:                  getEnvInt("AGENT_ROUTER_SOFTMAX_TOP_N", 5),
			SoftmaxTemperature:           getEnvFloat("AGENT_ROUTER_SOFTMAX_TEMPERATURE", 1.0),
			RuntimeWeightsEnabled:        getEnvBool("AGENT_ROUTER_RUNTIME_WEIGHTS_ENABLED", false),
			RuntimeWeightsMax:            getEnvFloat("AGENT_ROUTER_RUNTIME_WEIGHTS_MAX", 5.0),
			RuntimeWeightsMinRuns:        getEnvInt("AGENT_ROUTER_RUNTIME_WEIGHTS_MIN_RUNS", 30),
			RuntimeWeightsWindowHours:    getEnvInt("AGENT_ROUTER_RUNTIME_WEIGHTS_WINDOW_HOURS", 168),
			RuntimeWeightsRefreshSeconds: getEnvInt("AGENT_ROUTER_RUNTIME_WEIGHTS_REFRESH_SECONDS", 120),
			AgentsJSON:                   getEnv("AGENT_ROUTER_AGENTS_JSON", ""),
			DiscoveryEnabled:             getEnvBool("AGENT_ROUTER_DISCOVERY_ENABLED", false),
			DiscoveryPath:                getEnv("AGENT_ROUTER_DISCOVERY_PATH", "/.well-known/agent.json"),
			RegistryEnabled:              getEnvBool("AGENT_ROUTER_REGISTRY_ENABLED", false),
			LLM: LLMRouterConfig{
				Enabled:       getEnvBool("AGENT_ROUTER_LLM_ENABLED", false),
				URL:           getEnv("AGENT_ROUTER_LLM_URL", ""),
				TimeoutMS:     getEnvInt("AGENT_ROUTER_LLM_TIMEOUT_MS", 1500),
				MaxCandidates: getEnvInt("AGENT_ROUTER_LLM_MAX_CANDIDATES", 20),
				APIKey:        getEnv("AGENT_ROUTER_LLM_API_KEY", ""),
				Token:         getEnv("AGENT_ROUTER_LLM_TOKEN", ""),
				Headers:       getEnv("AGENT_ROUTER_LLM_HEADERS", ""),
			},
		},
		Plugins: PluginsConfig{
			Enabled:              getEnvBool("PLUGINS_ENABLED", false),
			ManifestPath:         getEnv("PLUGINS_MANIFEST_PATH", "plugins/manifest.yaml"),
			HTTPAllowlist:        getEnv("PLUGINS_HTTP_ALLOWLIST", "localhost,127.0.0.1,::1"),
			DefaultHTTPTimeoutMS: getEnvInt("PLUGINS_HTTP_TIMEOUT_MS", 8000),
		},
		Tenancy: TenancyConfig{
			Enabled:              getEnvBool("TENANCY_ENABLED", false),
			RequireAuth:          getEnvBool("TENANCY_REQUIRE_AUTH", false),
			APIKeys:              getEnv("TENANCY_API_KEYS", ""),
			JWTIssuer:            getEnv("TENANCY_JWT_ISSUER", ""),
			JWTAudience:          getEnv("TENANCY_JWT_AUDIENCE", ""),
			JWTTenantClaim:       getEnv("TENANCY_JWT_TENANT_CLAIM", "tenant_id"),
			JWTHS256Secret:       getEnv("TENANCY_JWT_HS256_SECRET", ""),
			JWTRS256PublicKeyPEM: getEnv("TENANCY_JWT_RS256_PUBLIC_KEY_PEM", ""),
		},
		Reasoning: ReasoningConfig{
			Provider: getEnv("REASONING_PROVIDER", "mcp"),
			DSPyURL:  getEnv("DSPY_URL", "http://localhost:8099"),
			DSPyKey:  getEnv("DSPY_API_KEY", ""),
		},
	}
}

// ParseHeaders parses comma-separated header string into map
func (a *AuthConfig) ParseHeaders(headerStr string) map[string]string {
	headers := make(map[string]string)
	if headerStr == "" {
		return headers
	}

	pairs := strings.Split(headerStr, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return headers
}

// GetRuleAgentHeaders returns parsed headers for rule agent
func (a *AuthConfig) GetRuleAgentHeaders() map[string]string {
	return a.ParseHeaders(a.RuleAgentHeaders)
}

// GetDecisionAgentHeaders returns parsed headers for decision agent
func (a *AuthConfig) GetDecisionAgentHeaders() map[string]string {
	return a.ParseHeaders(a.DecisionAgentHeaders)
}

// GetPRAgentHeaders returns parsed headers for pr agent
func (a *AuthConfig) GetPRAgentHeaders() map[string]string {
	return a.ParseHeaders(a.PRAgentHeaders)
}

// GetMCPHeaders returns parsed headers for MCP server
func (a *AuthConfig) GetMCPHeaders() map[string]string {
	return a.ParseHeaders(a.MCPHeaders)
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		var result float64
		if _, err := fmt.Sscanf(value, "%f", &result); err == nil {
			return result
		}
	}
	return defaultValue
}
