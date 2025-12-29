package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LLMChoice is the parsed selection returned by an LLM router.
type LLMChoice struct {
	AgentID      string  `json:"agent_id"`
	CapabilityID string  `json:"capability_id,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	Raw          string  `json:"-"`

	// Ranked is an optional top-K ranked list. If present, router should prefer it.
	Ranked []LLMRankedChoice `json:"ranked,omitempty"`
}

type LLMRankedChoice struct {
	AgentID      string  `json:"agent_id"`
	CapabilityID string  `json:"capability_id,omitempty"`
	Score        float64 `json:"score,omitempty"`
}

// LLMClient chooses an agent from a prompt.
// It must only return IDs; the router must still validate against candidates.
type LLMClient interface {
	Choose(ctx context.Context, prompt string) (LLMChoice, error)
}

// HTTPLLMClient calls an HTTP endpoint that returns JSON like:
// {"agent_id":"...","capability_id":"..."}.
type HTTPLLMClient struct {
	URL     string
	Timeout time.Duration

	APIKey  string
	Token   string
	Headers map[string]string

	HTTP *http.Client
}

func NewHTTPLLMClient(url string) *HTTPLLMClient {
	return &HTTPLLMClient{
		URL:     strings.TrimSpace(url),
		Timeout: 1500 * time.Millisecond,
	}
}

func (c *HTTPLLMClient) Choose(ctx context.Context, prompt string) (LLMChoice, error) {
	if c == nil {
		return LLMChoice{}, fmt.Errorf("llm client is nil")
	}
	if strings.TrimSpace(c.URL) == "" {
		return LLMChoice{}, fmt.Errorf("llm url is empty")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: c.Timeout}
	}
	body, err := json.Marshal(map[string]any{"prompt": prompt})
	if err != nil {
		return LLMChoice{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return LLMChoice{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return LLMChoice{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LLMChoice{}, fmt.Errorf("llm router http %d", resp.StatusCode)
	}
	var raw json.RawMessage
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return LLMChoice{}, err
	}
	var out LLMChoice
	if err := json.Unmarshal(raw, &out); err != nil {
		// allow wrappers like {"choice":{...}}
		var wrapper struct {
			Choice LLMChoice `json:"choice"`
		}
		if err2 := json.Unmarshal(raw, &wrapper); err2 != nil {
			return LLMChoice{}, err
		}
		out = wrapper.Choice
	}
	out.Raw = string(raw)
	out.AgentID = strings.TrimSpace(out.AgentID)
	out.CapabilityID = strings.TrimSpace(out.CapabilityID)
	for i := range out.Ranked {
		out.Ranked[i].AgentID = strings.TrimSpace(out.Ranked[i].AgentID)
		out.Ranked[i].CapabilityID = strings.TrimSpace(out.Ranked[i].CapabilityID)
	}
	return out, nil
}


