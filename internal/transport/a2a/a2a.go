package a2a

import (
	"context"
	"net/http"
	"time"

	"forgeiq/internal/controlplane/contracts"
)

// AgentCard represents the agent discovery card
type AgentCard struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	TaskTypes   []string `json:"task_types"`
	Endpoint    string   `json:"endpoint"`
	Version     string   `json:"version"`
}

// ClientConfig holds configuration for A2A client
type ClientConfig struct {
	BaseURL string
	Timeout time.Duration
	// Authentication
	APIKey string
	Token  string
	// Custom headers
	Headers map[string]string

	// Optional: enable vendor adapters by providing agent metadata + adapter registry.
	Agent    *Agent
	Adapters *AdapterRegistry
}

// Client is an A2A protocol client for communicating with agents
type Client struct {
	BaseURL  string
	HTTP     *http.Client
	config   ClientConfig
	agent    *Agent
	adapters *AdapterRegistry
}

// NewClient creates a new A2A client with default configuration
func NewClient(baseURL string) *Client {
	return NewClientWithConfig(ClientConfig{
		BaseURL: baseURL,
		Timeout: 30 * time.Second,
	})
}

// NewClientWithConfig creates a new A2A client with custom configuration
func NewClientWithConfig(config ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &Client{
		BaseURL: config.BaseURL,
		HTTP: &http.Client{
			Timeout: config.Timeout,
		},
		config:   config,
		agent:    config.Agent,
		adapters: config.Adapters,
	}
}

// RunTask sends a task to an agent and returns the artifact
func (c *Client) RunTask(ctx context.Context, task contracts.Task) (contracts.Artifact, error) {
	// Default to canonical adapter. If agent metadata + adapter registry are present, use it.
	agent := Agent{BaseURL: c.BaseURL}
	if c.agent != nil {
		agent = *c.agent
		if agent.BaseURL == "" {
			agent.BaseURL = c.BaseURL
		}
	}
	adapters := c.adapters
	if adapters == nil {
		adapters = &AdapterRegistry{def: &CanonicalAdapter{}}
	}
	adapter := adapters.ForAgent(agent)

	// Compose headers (auth + custom)
	headers := map[string]string{}
	if c.config.APIKey != "" {
		headers["X-API-Key"] = c.config.APIKey
	}
	if c.config.Token != "" {
		headers["Authorization"] = "Bearer " + c.config.Token
	}
	for k, v := range c.config.Headers {
		headers[k] = v
	}
	return doAdapterCall(ctx, c.HTTP, agent, task, adapter, headers)
}
