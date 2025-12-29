package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dim() int
}

// HashEmbedder is a deterministic, local embedder for dev/testing (NOT semantic).
// It produces a stable vector of length dim from the text bytes.
type HashEmbedder struct {
	dim int
}

func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 8
	}
	return &HashEmbedder{dim: dim}
}

func (h *HashEmbedder) Dim() int { return h.dim }

func (h *HashEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	_ = ctx
	sum := sha256.Sum256([]byte(text))
	// Expand sha256 into dim floats by re-hashing blocks.
	out := make([]float64, h.dim)
	seed := sum[:]
	for i := 0; i < h.dim; i++ {
		block := sha256.Sum256(append(seed, byte(i)))
		u := binary.LittleEndian.Uint64(block[:8])
		// Map uint64 -> [-1, 1]
		f := (float64(u) / float64(math.MaxUint64))*2.0 - 1.0
		out[i] = f
	}
	return out, nil
}

// HTTPEmbedder calls an external embedding service.
// Expected request: {"input":"...","model":"..."} (model optional)
// Expected response: {"embedding":[...]}.
type HTTPEmbedder struct {
	url    string
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

func NewHTTPEmbedder(url, apiKey, model string, dim int) (*HTTPEmbedder, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("embeddings url is required")
	}
	if dim <= 0 {
		return nil, fmt.Errorf("embeddings dim is required")
	}
	return &HTTPEmbedder{
		url:    url,
		apiKey: strings.TrimSpace(apiKey),
		model:  strings.TrimSpace(model),
		dim:    dim,
		http:   &http.Client{Timeout: 12 * time.Second},
	}, nil
}

func (h *HTTPEmbedder) Dim() int { return h.dim }

func (h *HTTPEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	body := map[string]any{
		"input": text,
	}
	if h.model != "" {
		body["model"] = h.model
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed http %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	var out struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, err
	}
	if len(out.Embedding) != h.dim {
		return nil, fmt.Errorf("embedding dim mismatch: got %d want %d", len(out.Embedding), h.dim)
	}
	return out.Embedding, nil
}


