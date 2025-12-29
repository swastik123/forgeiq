package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/interfaces"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/observability"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// RegisterPlugins loads tools from a manifest and registers them with the MCP server.
// This enables "out-of-box" tool packs without recompiling, as long as users provide endpoints/credentials.
func RegisterPlugins(s *mcpserver.Server, cfg *config.Config, logger *observability.Logger) {
	if s == nil || cfg == nil || !cfg.Plugins.Enabled {
		return
	}
	if logger == nil {
		logger = observability.NewNopLogger()
	}

	manifestPath := strings.TrimSpace(cfg.Plugins.ManifestPath)
	if manifestPath == "" {
		manifestPath = "plugins/manifest.yaml"
	}

	m, err := loadManifest(manifestPath)
	if err != nil {
		logger.Warn("plugin manifest load failed", zap.String("path", manifestPath), zap.Error(err))
		return
	}

	baseDir := filepath.Dir(manifestPath)
	allowlist := parseAllowlist(cfg.Plugins.HTTPAllowlist)
	timeout := time.Duration(cfg.Plugins.DefaultHTTPTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	loaded := 0
	skipped := 0
	for _, p := range m.Plugins {
		if !p.Enabled {
			continue
		}
		toolsFile := strings.TrimSpace(p.ToolsFile)
		if toolsFile == "" {
			continue
		}
		if !filepath.IsAbs(toolsFile) {
			toolsFile = filepath.Join(baseDir, toolsFile)
		}
		td, err := loadToolDefs(toolsFile)
		if err != nil {
			logger.Warn("plugin tools load failed", zap.String("plugin", p.ID), zap.String("file", toolsFile), zap.Error(err))
			continue
		}
		for _, t := range td.Tools {
			toolID := strings.TrimSpace(t.Name) + ":" + strings.TrimSpace(t.Version)
			if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Version) == "" {
				skipped++
				continue
			}
			if strings.ToLower(strings.TrimSpace(t.Type)) != "http" || t.HTTP == nil {
				logger.Warn("unsupported plugin tool type; skipping", zap.String("tool", toolID), zap.String("type", t.Type))
				skipped++
				continue
			}

			httpCfg := *t.HTTP
			httpCfg.URL = os.ExpandEnv(strings.TrimSpace(httpCfg.URL))
			if httpCfg.URL == "" {
				// Template tool: requires env var to enable.
				logger.Info("plugin tool skipped (no URL configured)", zap.String("tool", toolID), zap.String("plugin", p.ID))
				skipped++
				continue
			}

			if httpCfg.TimeoutMS <= 0 {
				httpCfg.TimeoutMS = int(timeout / time.Millisecond)
			}
			httpCfg.Method = strings.ToUpper(strings.TrimSpace(httpCfg.Method))
			if httpCfg.Method == "" {
				httpCfg.Method = http.MethodPost
			}
			for k, v := range httpCfg.Headers {
				httpCfg.Headers[k] = os.ExpandEnv(v)
			}

			if err := validateEndpoint(httpCfg.URL, allowlist); err != nil {
				logger.Warn("plugin tool endpoint not allowed; skipping", zap.String("tool", toolID), zap.Error(err))
				skipped++
				continue
			}

			handler := buildHTTPToolHandler(httpCfg)
			info := interfaces.ToolInfo{
				Name:    t.Name,
				Version: t.Version,
				Tags:    t.Tags,
				Scopes:  t.Scopes,
				Schema:  t.Schema,
				Meta:    t.Meta,
			}
			if info.Meta == nil {
				info.Meta = map[string]string{}
			}
			info.Meta["plugin_id"] = p.ID
			info.Meta["plugin_type"] = "http"
			s.RegisterToolWithInfo(info, handler)
			loaded++
		}
	}

	logger.Info("plugin tools loaded", zap.Int("loaded", loaded), zap.Int("skipped", skipped), zap.String("manifest", manifestPath))
}

type manifest struct {
	Plugins []manifestPlugin `yaml:"plugins"`
}

type manifestPlugin struct {
	ID        string `yaml:"id"`
	Enabled   bool   `yaml:"enabled"`
	ToolsFile string `yaml:"tools_file"`
}

func loadManifest(path string) (*manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type toolDefs struct {
	Tools []toolDef `json:"tools"`
}

type toolDef struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Tags    []string          `json:"tags,omitempty"`
	Scopes  []string          `json:"scopes,omitempty"`
	Schema  map[string]any    `json:"schema,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`

	Type string       `json:"type"` // "http"
	HTTP *httpToolDef `json:"http,omitempty"`
}

type httpToolDef struct {
	URL       string            `json:"url"`
	Method    string            `json:"method,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`

	// If method is GET, args are encoded into query parameters.
	// If method is POST/PUT/PATCH, args are sent as JSON body.
}

func loadToolDefs(path string) (*toolDefs, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var td toolDefs
	if err := json.Unmarshal(b, &td); err != nil {
		return nil, err
	}
	return &td, nil
}

func buildHTTPToolHandler(cfg httpToolDef) mcpserver.ToolHandler {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	return func(args map[string]any) (map[string]any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		method := strings.ToUpper(strings.TrimSpace(cfg.Method))
		if method == "" {
			method = http.MethodPost
		}

		endpoint := cfg.URL
		var body io.Reader

		if method == http.MethodGet {
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, fmt.Errorf("invalid url: %w", err)
			}
			q := u.Query()
			for k, v := range args {
				q.Set(k, fmt.Sprint(v))
			}
			u.RawQuery = q.Encode()
			endpoint = u.String()
		} else {
			b, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal args: %w", err)
			}
			body = bytes.NewReader(b)
		}

		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range cfg.Headers {
			if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
				req.Header.Set(k, v)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		rb, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("http tool returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
		}

		// Try decode JSON into map. If not an object, wrap.
		var out any
		if err := json.Unmarshal(rb, &out); err != nil {
			return map[string]any{"raw": string(rb)}, nil
		}
		if m, ok := out.(map[string]any); ok {
			return m, nil
		}
		return map[string]any{"result": out}, nil
	}
}

func parseAllowlist(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"localhost", "127.0.0.1", "::1"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateEndpoint(rawURL string, allowlist []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}

	for _, a := range allowlist {
		if a == "*" {
			return nil
		}
		if strings.EqualFold(host, a) {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(host), strings.ToLower(a)) {
			return nil
		}
		// Allow CIDR blocks
		if strings.Contains(a, "/") {
			if ip := net.ParseIP(host); ip != nil {
				if _, cidr, err := net.ParseCIDR(a); err == nil && cidr.Contains(ip) {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("host %q not in allowlist", host)
}
