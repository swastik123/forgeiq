package rag

import (
	"context"
	"fmt"

	"forgeiq/internal/vector/pgvector"
)

type VectorSearcher interface {
	Search(ctx context.Context, namespace string, queryEmbedding []float64, k int) ([]pgvector.Record, error)
}

type InternalRetriever struct {
	Vector   VectorSearcher
	Embedder Embedder
}

func (r *InternalRetriever) Retrieve(ctx context.Context, ds DatasetConfig, query string, topK int, filters map[string]any) ([]RetrievalResource, error) {
	_ = filters // TODO: pushdown filters/metadata conditions
	if r == nil || r.Vector == nil {
		return nil, fmt.Errorf("internal retriever not configured (vector store missing)")
	}
	if r.Embedder == nil {
		return nil, fmt.Errorf("internal retriever not configured (embedder missing)")
	}
	if ds.Internal == nil {
		return nil, fmt.Errorf("dataset %s missing internal config", ds.ID)
	}

	emb, err := r.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	recs, err := r.Vector.Search(ctx, ds.Internal.Namespace, emb, topK)
	if err != nil {
		return nil, err
	}

	out := make([]RetrievalResource, 0, len(recs))
	for _, rec := range recs {
		title := rec.ID
		if rec.Metadata != nil {
			if t, ok := rec.Metadata["title"].(string); ok && t != "" {
				title = t
			}
		}
		out = append(out, RetrievalResource{
			DocID:     rec.ID,
			DatasetID: ds.ID,
			Source:    "internal",
			Title:     title,
			Content:   rec.Content,
			Metadata:  rec.Metadata,
			Score:     rec.Score,
		})
	}
	return out, nil
}
