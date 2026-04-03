package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type llmProvider string

const (
	provDemo     llmProvider = "demo"
	provLiteLLM  llmProvider = "litellm"  // OpenAI-compatible chat/completions
	provOpenAI   llmProvider = "openai"   // direct OpenAI chat/completions
	provAnthropic llmProvider = "anthropic" // direct Anthropic messages
)

type llmConfig struct {
	Provider llmProvider

	// LiteLLM
	LiteLLMBaseURL string
	LiteLLMAPIKey  string

	// OpenAI
	OpenAIBaseURL string
	OpenAIAPIKey  string

	// Anthropic
	AnthropicBaseURL string
	AnthropicAPIKey  string
	AnthropicVersion string
}

func llmConfigFromEnv() llmConfig {
	p := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	cfg := llmConfig{
		Provider:        provDemo,
		LiteLLMBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("LITELLM_BASE_URL")), "/"),
		LiteLLMAPIKey:   strings.TrimSpace(os.Getenv("LITELLM_API_KEY")),
		OpenAIBaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/"),
		OpenAIAPIKey:    strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		AnthropicBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")), "/"),
		AnthropicAPIKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicVersion: strings.TrimSpace(os.Getenv("ANTHROPIC_VERSION")),
	}
	if cfg.OpenAIBaseURL == "" {
		cfg.OpenAIBaseURL = "https://api.openai.com/v1"
	}
	if cfg.AnthropicBaseURL == "" {
		cfg.AnthropicBaseURL = "https://api.anthropic.com"
	}
	if cfg.AnthropicVersion == "" {
		cfg.AnthropicVersion = "2023-06-01"
	}

	switch p {
	case "litellm":
		cfg.Provider = provLiteLLM
	case "openai":
		cfg.Provider = provOpenAI
	case "anthropic":
		cfg.Provider = provAnthropic
	case "", "demo":
		cfg.Provider = provDemo
	default:
		// unknown -> demo
		cfg.Provider = provDemo
	}
	return cfg
}

type llmCallResult struct {
	RawText string
}

func callLLMProvider(ctx context.Context, cfg llmConfig, model string, messages []map[string]any, responseFormat map[string]any) (llmCallResult, error) {
	switch cfg.Provider {
	case provLiteLLM:
		return callOpenAICompatible(ctx, cfg.LiteLLMBaseURL, cfg.LiteLLMAPIKey, model, messages, responseFormat)
	case provOpenAI:
		return callOpenAICompatible(ctx, cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, model, messages, responseFormat)
	case provAnthropic:
		return callAnthropic(ctx, cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, cfg.AnthropicVersion, model, messages, responseFormat)
	default:
		return llmCallResult{}, fmt.Errorf("provider not configured")
	}
}

func callOpenAICompatible(ctx context.Context, baseURL, apiKey, model string, messages []map[string]any, responseFormat map[string]any) (llmCallResult, error) {
	if strings.TrimSpace(baseURL) == "" {
		return llmCallResult{}, fmt.Errorf("base url not configured")
	}
	if strings.TrimSpace(model) == "" {
		model = "gpt-4o-mini"
	}
	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
	}
	// If caller provided json_schema response_format, try to pass it through for providers that support it.
	if responseFormat != nil {
		// LiteLLM generally accepts OpenAI-like response_format for supported backends.
		reqBody["response_format"] = responseFormat
	}

	b, _ := json.Marshal(reqBody)
	u := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return llmCallResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	httpc := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return llmCallResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llmCallResult{}, fmt.Errorf("provider status %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llmCallResult{}, fmt.Errorf("decode openai-compatible: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return llmCallResult{}, fmt.Errorf("no choices returned")
	}
	return llmCallResult{RawText: parsed.Choices[0].Message.Content}, nil
}

func callAnthropic(ctx context.Context, baseURL, apiKey, version, model string, messages []map[string]any, responseFormat map[string]any) (llmCallResult, error) {
	if strings.TrimSpace(apiKey) == "" {
		return llmCallResult{}, fmt.Errorf("anthropic api key not configured")
	}
	if strings.TrimSpace(model) == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	// Convert OpenAI-like messages to Anthropic format:
	// - system: separate "system" string
	// - user/assistant: content array/text
	sys := ""
	msgs := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		if role == "system" {
			if strings.TrimSpace(content) != "" {
				sys = content
			}
			continue
		}
		msgs = append(msgs, map[string]any{
			"role":    role,
			"content": content,
		})
	}

	reqBody := map[string]any{
		"model":      model,
		"max_tokens": 2000,
		"messages":   msgs,
	}
	if sys != "" {
		reqBody["system"] = sys
	}
	// Anthropic doesn't share OpenAI response_format; we rely on schema validation/retry at MCP layer.
	_ = responseFormat

	b, _ := json.Marshal(reqBody)
	u := strings.TrimRight(baseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return llmCallResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", version)

	httpc := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return llmCallResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llmCallResult{}, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llmCallResult{}, fmt.Errorf("decode anthropic: %w", err)
	}
	out := strings.Builder{}
	for _, c := range parsed.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			out.WriteString(c.Text)
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return llmCallResult{}, fmt.Errorf("anthropic returned empty content")
	}
	return llmCallResult{RawText: out.String()}, nil
}

