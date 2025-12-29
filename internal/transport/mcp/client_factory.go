package mcp

import (
	"forgeiq/internal/config"
)

// NewClientFromConfig creates an MCP client from configuration
func NewClientFromConfig(cfg *config.Config) *Client {
	return NewClientWithConfig(ClientConfig{
		BaseURL: cfg.MCPBaseURL,
		APIKey:  cfg.Auth.MCPAPIKey,
		Token:   cfg.Auth.MCPToken,
		Headers: cfg.Auth.GetMCPHeaders(),
	})
}
