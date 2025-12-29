package rag

import (
	"context"
	"sort"
)

type RerankingMode string

const (
	RerankModel        RerankingMode = "reranking_model"
	RerankWeighted     RerankingMode = "weighted_score"
	RerankNone         RerankingMode = "none"
)

// Reranker can reorder results.
type Reranker interface {
	Rerank(ctx context.Context, query string, in []RetrievalResource) ([]RetrievalResource, error)
}

// WeightedReranker applies a simple per-source weight to the current score.
// (You can extend this later to incorporate keyword_score, freshness, etc.)
type WeightedReranker struct {
	Weights map[string]float64 // "internal"->w, "external"->w
}

func (r *WeightedReranker) Rerank(ctx context.Context, query string, in []RetrievalResource) ([]RetrievalResource, error) {
	_ = ctx
	_ = query
	out := make([]RetrievalResource, 0, len(in))
	out = append(out, in...)
	for i := range out {
		w := 1.0
		if r != nil && r.Weights != nil {
			if ww, ok := r.Weights[out[i].Source]; ok {
				w = ww
			}
		}
		out[i].Score = out[i].Score * w
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func TruncateAndThreshold(in []RetrievalResource, topK int, scoreThreshold float64) []RetrievalResource {
	if scoreThreshold > 0 {
		tmp := make([]RetrievalResource, 0, len(in))
		for _, r := range in {
			if r.Score >= scoreThreshold {
				tmp = append(tmp, r)
			}
		}
		in = tmp
	}
	if topK > 0 && len(in) > topK {
		return in[:topK]
	}
	return in
}


