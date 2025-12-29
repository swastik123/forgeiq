package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"forgeiq/internal/controlplane/interfaces"
)

// ClientConfig holds configuration for MCP client
type ClientConfig struct {
	BaseURL string
	Timeout time.Duration
	// Authentication
	APIKey string
	Token  string
	// Custom headers
	Headers map[string]string
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
	config  ClientConfig
}

// NewClient creates a new MCP client with default configuration
func NewClient(baseURL string) *Client {
	return NewClientWithConfig(ClientConfig{
		BaseURL: baseURL,
		Timeout: 20 * time.Second,
	})
}

// NewClientWithConfig creates a new MCP client with custom configuration
func NewClientWithConfig(config ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 20 * time.Second
	}

	return &Client{
		BaseURL: config.BaseURL,
		HTTP: &http.Client{
			Timeout: config.Timeout,
		},
		config: config,
	}
}

type rpcReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method string, params any, out any) error {
	reqBody, err := json.Marshal(rpcReq{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/rpc", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers
	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	if c.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
	}

	// Add custom headers
	for k, v := range c.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var r rpcResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	if r.Error != nil {
		return fmt.Errorf("rpc error %d: %s", r.Error.Code, r.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}
	return nil
}

func (c *Client) ListTools(ctx context.Context) ([]interfaces.ToolInfo, error) {
	var tools []interfaces.ToolInfo
	err := c.do(ctx, "tools.list", nil, &tools)
	return tools, err
}

func (c *Client) CallTool(ctx context.Context, name string, version string, args map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, "tools.call", map[string]any{
		"name":    name,
		"version": version,
		"args":    args,
	}, &out)
	return out, err
}
