package a2a

import (
	"forgeiq/internal/config"
)

// NewClientFromConfig creates an A2A client from configuration
func NewClientFromConfig(cfg *config.Config, agentType string) *Client {
	var baseURL string
	var apiKey string
	var token string
	var headers map[string]string

	switch agentType {
	case "rule":
		baseURL = cfg.RuleAgentURL
		apiKey = cfg.Auth.RuleAgentAPIKey
		token = cfg.Auth.RuleAgentToken
		headers = cfg.Auth.GetRuleAgentHeaders()
	case "decision":
		baseURL = cfg.DecisionAgentURL
		apiKey = cfg.Auth.DecisionAgentAPIKey
		token = cfg.Auth.DecisionAgentToken
		headers = cfg.Auth.GetDecisionAgentHeaders()
	default:
		return nil
	}

	return NewClientWithConfig(ClientConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Token:   token,
		Headers: headers,
	})
}
