package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FetchAgentCard fetches an agent discovery card from baseURL + discoveryPath.
// discoveryPath defaults to "/.well-known/agent.json".
func FetchAgentCard(ctx context.Context, baseURL, discoveryPath string) (*AgentCard, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	if discoveryPath == "" {
		discoveryPath = "/.well-known/agent.json"
	}
	if !strings.HasPrefix(discoveryPath, "/") {
		discoveryPath = "/" + discoveryPath
	}

	c := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+discoveryPath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent card http %d", resp.StatusCode)
	}
	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, err
	}
	return &card, nil
}


