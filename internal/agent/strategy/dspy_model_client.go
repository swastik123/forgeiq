package strategy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DSPyModelClient implements ModelClient by calling an external "reasoning service"
// (typically a Python DSPy service).
//
// The service is expected to return a JSON response compatible with ModelDecision:
// - final_answer (string) OR tool_call ({name,version,args})
// - summary (string)
type DSPyModelClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewDSPyModelClient(baseURL string) *DSPyModelClient {
	return &DSPyModelClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *DSPyModelClient) Decide(ctx context.Context, req ModelRequest) (ModelDecision, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return ModelDecision{}, fmt.Errorf("dspy model client: base url is empty")
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}

	// This payload is intentionally "strategy-native" (messages/tools/model/meta),
	// so the DSPy service can implement tool calling if desired.
	payload := map[string]any{
		"state": req.State,
		"tools": req.Tools,
		"model": req.Model,
		"meta":  req.Meta,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ModelDecision{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/decide", bytes.NewReader(b))
	if err != nil {
		return ModelDecision{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return ModelDecision{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ModelDecision{}, fmt.Errorf("dspy reasoning service returned http %d", resp.StatusCode)
	}

	var dec ModelDecision
	if err := json.NewDecoder(resp.Body).Decode(&dec); err != nil {
		return ModelDecision{}, err
	}
	if dec.FinalAnswer == "" && dec.ToolCall == nil {
		return ModelDecision{}, fmt.Errorf("dspy reasoning service returned empty decision")
	}
	return dec, nil
}



