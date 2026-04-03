package bitbucket

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Bitbucket Cloud client (API 2.0) with env-based auth.
//
// Auth:
// - Prefer OAuth bearer: BITBUCKET_TOKEN
// - Else basic: BITBUCKET_USERNAME + BITBUCKET_APP_PASSWORD
//
// Base URL:
// - BITBUCKET_BASE_URL (default: https://api.bitbucket.org)

type Client struct {
	BaseURL string
	HTTP    *http.Client

	token   string
	basic64 string
}

func NewFromEnv() *Client {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("BITBUCKET_BASE_URL")), "/")
	if base == "" {
		base = "https://api.bitbucket.org"
	}
	c := &Client{
		BaseURL: base,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
		},
		token: strings.TrimSpace(os.Getenv("BITBUCKET_TOKEN")),
	}
	if c.token == "" {
		u := strings.TrimSpace(os.Getenv("BITBUCKET_USERNAME"))
		p := strings.TrimSpace(os.Getenv("BITBUCKET_APP_PASSWORD"))
		if u != "" && p != "" {
			c.basic64 = base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
		}
	}
	return c
}

type PullRequest struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	Author      struct {
		DisplayName string `json:"display_name"`
	} `json:"author"`
	Source struct {
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
}

type NotFoundError struct {
	Message string
}

func (e NotFoundError) Error() string { return e.Message }

func IsNotFound(err error) bool {
	_, ok := err.(NotFoundError)
	return ok
}

// GetFile fetches a file at a specific ref (commit hash, branch name, tag).
// Bitbucket Cloud: GET /2.0/repositories/{workspace}/{repo}/src/{ref}/{path}
func (c *Client) GetFile(ctx context.Context, workspace, repoSlug, ref, path string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	path = strings.TrimLeft(strings.TrimSpace(path), "/")
	if ref == "" || path == "" {
		return nil, fmt.Errorf("ref and path are required")
	}

	base := strings.TrimRight(c.BaseURL, "/")
	u := fmt.Sprintf("%s/2.0/repositories/%s/%s/src/%s/%s", base, esc(workspace), esc(repoSlug), url.PathEscape(ref), url.PathEscape(path))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, NotFoundError{Message: "file not found: " + path}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bitbucket get file status %d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

type Comment struct {
	ID      int `json:"id"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	Inline *struct {
		Path string `json:"path"`
		To   int    `json:"to,omitempty"`
		From int    `json:"from,omitempty"`
	} `json:"inline,omitempty"`
}

func (c *Client) GetPR(ctx context.Context, workspace, repoSlug string, prID int) (PullRequest, error) {
	var out PullRequest
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d", esc(workspace), esc(repoSlug), prID), nil, &out)
	return out, err
}

func (c *Client) GetDiff(ctx context.Context, workspace, repoSlug string, prID int) (string, error) {
	// Bitbucket Cloud returns raw unified diff as text.
	u := fmt.Sprintf("%s/2.0/repositories/%s/%s/pullrequests/%d/diff", strings.TrimRight(c.BaseURL, "/"), esc(workspace), esc(repoSlug), prID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("bitbucket diff status %d: %s", resp.StatusCode, string(b))
	}
	return string(b), nil
}

func (c *Client) ListComments(ctx context.Context, workspace, repoSlug string, prID int, limit int) ([]Comment, error) {
	if limit <= 0 {
		limit = 100
	}
	path := fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d/comments?pagelen=%d", esc(workspace), esc(repoSlug), prID, limit)
	var all []Comment
	next := path
	for next != "" {
		var page struct {
			Values []Comment `json:"values"`
			Next   string    `json:"next"`
		}
		if err := c.doJSON(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		if page.Next == "" {
			break
		}
		// "next" is an absolute URL; convert to path for doJSON.
		if strings.HasPrefix(page.Next, c.BaseURL) {
			next = strings.TrimPrefix(page.Next, c.BaseURL)
		} else {
			next = page.Next
		}
	}
	return all, nil
}

func (c *Client) CreateComment(ctx context.Context, workspace, repoSlug string, prID int, raw string, inlinePath string, inlineTo int, inlineFrom int) (Comment, error) {
	payload := map[string]any{
		"content": map[string]any{"raw": raw},
	}
	if strings.TrimSpace(inlinePath) != "" && (inlineTo > 0 || inlineFrom > 0) {
		inl := map[string]any{"path": inlinePath}
		if inlineTo > 0 {
			inl["to"] = inlineTo
		}
		if inlineFrom > 0 {
			inl["from"] = inlineFrom
		}
		payload["inline"] = inl
	}
	var out Comment
	err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d/comments", esc(workspace), esc(repoSlug), prID), payload, &out)
	return out, err
}

func (c *Client) UpdateComment(ctx context.Context, workspace, repoSlug string, prID int, commentID int, raw string) (Comment, error) {
	payload := map[string]any{"content": map[string]any{"raw": raw}}
	var out Comment
	err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d/comments/%d", esc(workspace), esc(repoSlug), prID, commentID), payload, &out)
	return out, err
}

func (c *Client) doJSON(ctx context.Context, method string, pathOrURL string, payload any, out any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	full := ""
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		full = pathOrURL
	} else {
		if !strings.HasPrefix(pathOrURL, "/") {
			pathOrURL = "/" + pathOrURL
		}
		full = base + pathOrURL
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bitbucket api status %d: %s", resp.StatusCode, string(b))
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) applyAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		return
	}
	if c.basic64 != "" {
		req.Header.Set("Authorization", "Basic "+c.basic64)
		return
	}
}

func esc(s string) string {
	return url.PathEscape(strings.TrimSpace(s))
}

func ParsePRID(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(t))
	default:
		return strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
	}
}
