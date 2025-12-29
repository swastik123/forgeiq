package elasticsearch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal Elasticsearch HTTP client (no external deps).
type Client struct {
	BaseURL string
	Index   string

	APIKey   string
	Username string
	Password string

	HTTP *http.Client
}

type SearchHit struct {
	Index  string                 `json:"_index"`
	ID     string                 `json:"_id"`
	Score  float64                `json:"_score"`
	Source map[string]any         `json:"_source"`
	Fields map[string]any         `json:"fields,omitempty"`
	Raw    map[string]json.RawMessage `json:"-"`
}

func New(baseURL, index string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("elastic baseURL is required")
	}
	if index == "" {
		index = "logs-*"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		BaseURL: baseURL,
		Index:   index,
		HTTP:    httpClient,
	}, nil
}

func (c *Client) WithAuth(apiKey, username, password string) *Client {
	c.APIKey = apiKey
	c.Username = username
	c.Password = password
	return c
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Auth (prefer API key if present)
	if strings.TrimSpace(c.APIKey) != "" {
		// ES supports either "ApiKey <base64(id:key)>" or encoded key string; we accept the raw string and pass through.
		req.Header.Set("Authorization", "ApiKey "+strings.TrimSpace(c.APIKey))
	} else if strings.TrimSpace(c.Username) != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.Password))
		req.Header.Set("Authorization", "Basic "+creds)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("elastic http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// Search runs a simple query_string search over the configured index.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	body := map[string]any{
		"size": limit,
		"query": map[string]any{
			"query_string": map[string]any{
				"query": query,
			},
		},
		"sort": []any{
			map[string]any{"@timestamp": map[string]any{"order": "desc"}},
		},
	}

	var resp struct {
		Hits struct {
			Hits []SearchHit `json:"hits"`
		} `json:"hits"`
	}

	path := fmt.Sprintf("/%s/_search", urlPathEscapeIndex(c.Index))
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp.Hits.Hits, nil
}

// IndexDoc indexes a document into a concrete index (or the configured Index if it's not a wildcard).
func (c *Client) IndexDoc(ctx context.Context, index string, doc map[string]any) (string, error) {
	if index == "" {
		index = c.Index
	}
	// If user configured wildcard like logs-*, require explicit target index.
	if strings.ContainsAny(index, "*?,") {
		return "", fmt.Errorf("index must be a concrete index name (got %q)", index)
	}
	var resp struct {
		ID string `json:"_id"`
	} 
	path := fmt.Sprintf("/%s/_doc", urlPathEscapeIndex(index))
	if err := c.do(ctx, http.MethodPost, path, doc, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

func urlPathEscapeIndex(index string) string {
	// Minimal escaping: path segments should not include spaces; replace with %20.
	return strings.ReplaceAll(index, " ", "%20")
}


