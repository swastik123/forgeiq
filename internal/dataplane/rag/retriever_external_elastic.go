package rag

import (
	"context"
	"fmt"
	"strings"

	"forgeiq/internal/external/elasticsearch"
)

type ElasticSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]elasticsearch.SearchHit, error)
}

type ElasticRetriever struct {
	Client ElasticSearcher
}

func (r *ElasticRetriever) Retrieve(ctx context.Context, ds DatasetConfig, query string, topK int, filters map[string]any) ([]RetrievalResource, error) {
	_ = filters // TODO: translate filters -> ES query DSL
	if r == nil || r.Client == nil {
		return nil, fmt.Errorf("external elastic retriever not configured (client missing)")
	}
	if ds.External == nil {
		return nil, fmt.Errorf("dataset %s missing external config", ds.ID)
	}
	if strings.ToLower(ds.External.Adapter) != "elastic" {
		return nil, fmt.Errorf("dataset %s adapter=%s not supported by elastic retriever", ds.ID, ds.External.Adapter)
	}

	// Dataset index override is supported only when Client is *elasticsearch.Client.
	// If you inject a custom client for testing, keep the index inside that client.
	if ec, ok := r.Client.(*elasticsearch.Client); ok {
		c := *ec
		if strings.TrimSpace(ds.External.Index) != "" {
			c.Index = strings.TrimSpace(ds.External.Index)
		}
		hits, err := c.Search(ctx, query, topK)
		if err != nil {
			return nil, err
		}
		return normalizeHits(ds, hits), nil
	}

	hits, err := r.Client.Search(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	return normalizeHits(ds, hits), nil
}

func normalizeHits(ds DatasetConfig, hits []elasticsearch.SearchHit) []RetrievalResource {
	out := make([]RetrievalResource, 0, len(hits))
	for _, h := range hits {
		title, _ := h.Source["title"].(string)
		if title == "" {
			// Common log fields
			if msg, ok := h.Source["message"].(string); ok && msg != "" {
				title = msg
			} else {
				title = h.ID
			}
		}
		content := ""
		if c, ok := h.Source["content"].(string); ok {
			content = c
		} else if msg, ok := h.Source["message"].(string); ok {
			content = msg
		}
		out = append(out, RetrievalResource{
			DocID:     h.ID,
			DatasetID: ds.ID,
			Source:    "external",
			Title:     title,
			Content:   content,
			Metadata:  h.Source,
			Score:     h.Score,
		})
	}
	return out
}
