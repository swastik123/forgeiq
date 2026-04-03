package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/interfaces"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/dataplane/rag"
	"forgeiq/internal/external/elasticsearch"
	"forgeiq/internal/observability"
	"forgeiq/internal/vector/pgvector"

	"go.uber.org/zap"
)

func registerRAGTools(s *mcpserver.Server, cfg *config.Config, logger *observability.Logger, vectorStore *pgvector.Store, esClient *elasticsearch.Client) {
	if logger == nil {
		logger = observability.NewNopLogger()
	}
	if cfg == nil {
		cfg = config.Load()
	}

	reg, err := rag.LoadRegistryFromJSON(cfg.RAG.DatasetsJSON)
	if err != nil {
		logger.Error("Failed to load RAG datasets; rag tools will be unavailable", zap.Error(err))
		return
	}

	var embedder rag.Embedder
	provider := strings.ToLower(strings.TrimSpace(cfg.Embeddings.Provider))
	switch provider {
	case "", "hash":
		embedder = rag.NewHashEmbedder(cfg.Embeddings.Dim)
		logger.Warn("Using HashEmbedder for RAG internal retrieval (deterministic but NOT semantic). For semantic retrieval, set EMBEDDINGS_PROVIDER=http and configure EMBEDDINGS_URL/API key.", zap.Int("dim", cfg.Embeddings.Dim))
	case "http":
		if strings.TrimSpace(cfg.Embeddings.URL) == "" {
			logger.Error("EMBEDDINGS_PROVIDER=http but EMBEDDINGS_URL is empty; internal semantic retrieval is unavailable")
			embedder = nil
		} else if httpEmb, err := rag.NewHTTPEmbedder(cfg.Embeddings.URL, cfg.Embeddings.APIKey, cfg.Embeddings.Model, cfg.Embeddings.Dim); err != nil {
			logger.Error("Failed to initialize HTTP embedder; internal semantic retrieval is unavailable", zap.Error(err))
			embedder = nil
		} else {
			embedder = httpEmb
			logger.Info("Initialized HTTP embedder for RAG internal retrieval", zap.Int("dim", cfg.Embeddings.Dim))
		}
	default:
		logger.Error("Unknown EMBEDDINGS_PROVIDER; internal semantic retrieval is unavailable", zap.String("provider", provider))
		embedder = nil
	}

	svc := &rag.Service{
		Registry:        reg,
		Internal:        &rag.InternalRetriever{Vector: vectorStore, Embedder: embedder},
		ExternalElastic: &rag.ElasticRetriever{Client: esClient},
		RuleRouter:      &rag.RuleBasedRouter{},
	}

	allowed := parseCSVSet(cfg.RAG.AllowedDatasets)

	isDatasetAllowed := func(ds rag.DatasetConfig) error {
		if len(allowed) > 0 {
			if !allowed[ds.ID] {
				return fmt.Errorf("dataset not allowed: %s", ds.ID)
			}
		}
		if ds.Type == rag.DatasetExternal && !cfg.RAG.AllowExternal {
			return fmt.Errorf("external datasets are disabled by policy")
		}
		return nil
	}

	clampTopK := func(k int) int {
		if k <= 0 {
			k = 5
		}
		if cfg.RAG.MaxTopK > 0 && k > cfg.RAG.MaxTopK {
			return cfg.RAG.MaxTopK
		}
		return k
	}

	// rag.single_retrieve
	singleHandler := func(args map[string]any) (map[string]any, error) {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		dsAny, ok := args["available_datasets"].([]any)
		if !ok || len(dsAny) == 0 {
			return nil, fmt.Errorf("available_datasets is required")
		}
		datasetIDs := make([]string, 0, len(dsAny))
		for _, v := range dsAny {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				datasetIDs = append(datasetIDs, s)
			}
		}
		if len(datasetIDs) == 0 {
			return nil, fmt.Errorf("available_datasets must contain strings")
		}

		modeStr, _ := args["router_mode"].(string)
		if modeStr == "" {
			modeStr = "rule_based"
		}
		topKf, _ := args["top_k"].(float64)
		topK := clampTopK(int(topKf))

		var filters map[string]any
		if f, ok := args["filters"].(map[string]any); ok {
			filters = f
		}

		// Policy gate: ensure datasets requested are allowed by config.
		for _, id := range datasetIDs {
			ds, ok := reg.Get(id)
			if !ok {
				return nil, fmt.Errorf("unknown dataset: %s", id)
			}
			if err := isDatasetAllowed(ds); err != nil {
				return nil, err
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		res, debug, err := svc.RetrieveSingle(ctx, query, datasetIDs, rag.RouterMode(modeStr), topK, filters)
		if err != nil {
			return nil, err
		}
		if cfg.RAG.MaxDocs > 0 && len(res) > cfg.RAG.MaxDocs {
			res = res[:cfg.RAG.MaxDocs]
		}
		return map[string]any{
			"resources": res,
			"count":     len(res),
			"debug":     debug,
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "rag.single_retrieve",
		Version: "v1",
		Tags:    []string{"read", "rag"},
		Scopes:  []string{"rag:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":              map[string]any{"type": "string"},
				"available_datasets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"router_mode":        map[string]any{"type": "string", "enum": []string{"llm_toolcall", "react", "rule_based"}, "default": "rule_based"},
				"top_k":              map[string]any{"type": "number", "default": 5},
				"filters":            map[string]any{"type": "object"},
			},
			"required": []string{"query", "available_datasets"},
		},
		Meta: map[string]string{
			"description": "Route to a single dataset (internal pgvector or external adapter) and retrieve normalized resources",
		},
	}, singleHandler)

	// rag.multiple_retrieve
	multiHandler := func(args map[string]any) (map[string]any, error) {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		dsAny, ok := args["available_datasets"].([]any)
		if !ok || len(dsAny) == 0 {
			return nil, fmt.Errorf("available_datasets is required")
		}
		datasetIDs := make([]string, 0, len(dsAny))
		for _, v := range dsAny {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				datasetIDs = append(datasetIDs, s)
			}
		}
		if len(datasetIDs) == 0 {
			return nil, fmt.Errorf("available_datasets must contain strings")
		}

		topKf, _ := args["top_k"].(float64)
		topK := clampTopK(int(topKf))
		perDataset := topK
		if len(datasetIDs) > 1 {
			// retrieve a smaller slice from each dataset before reranking
			perDataset = clampTopK(max(3, topK/len(datasetIDs)))
		}

		scoreThreshold, _ := args["score_threshold"].(float64)
		rerankMode, _ := args["reranking_mode"].(string)
		if rerankMode == "" {
			rerankMode = "none"
		}

		var filters map[string]any
		if f, ok := args["filters"].(map[string]any); ok {
			filters = f
		}

		for _, id := range datasetIDs {
			ds, ok := reg.Get(id)
			if !ok {
				return nil, fmt.Errorf("unknown dataset: %s", id)
			}
			if err := isDatasetAllowed(ds); err != nil {
				return nil, err
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		all, debug, err := svc.RetrieveMultiple(ctx, query, datasetIDs, perDataset, filters)
		if err != nil {
			return nil, err
		}

		// Rerank (cleanly pluggable; model tool is a stub for now)
		switch rag.RerankingMode(rerankMode) {
		case rag.RerankNone:
			// keep as-is
		case rag.RerankWeighted:
			weights := map[string]float64{"internal": 1.0, "external": 1.0}
			if w, ok := args["weights"].(map[string]any); ok {
				if vi, ok := w["internal"].(float64); ok {
					weights["internal"] = vi
				}
				if ve, ok := w["external"].(float64); ok {
					weights["external"] = ve
				}
			}
			rer := &rag.WeightedReranker{Weights: weights}
			all, err = rer.Rerank(ctx, query, all)
			if err != nil {
				return nil, err
			}
		case rag.RerankModel:
			// placeholder: stable sort by score (until a real cross-encoder tool is wired)
			rer := &rag.WeightedReranker{Weights: map[string]float64{"internal": 1.0, "external": 1.0}}
			all, err = rer.Rerank(ctx, query, all)
			if err != nil {
				return nil, err
			}
			debug["rerank_note"] = "reranking_model not wired; used score sort fallback"
		default:
			return nil, fmt.Errorf("invalid reranking_mode: %s", rerankMode)
		}

		all = rag.TruncateAndThreshold(all, topK, scoreThreshold)
		if cfg.RAG.MaxDocs > 0 && len(all) > cfg.RAG.MaxDocs {
			all = all[:cfg.RAG.MaxDocs]
		}

		return map[string]any{
			"resources": all,
			"count":     len(all),
			"debug":     debug,
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "rag.multiple_retrieve",
		Version: "v1",
		Tags:    []string{"read", "rag"},
		Scopes:  []string{"rag:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":              map[string]any{"type": "string"},
				"available_datasets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"top_k":              map[string]any{"type": "number", "default": 10},
				"score_threshold":    map[string]any{"type": "number", "default": 0},
				"reranking_mode":     map[string]any{"type": "string", "enum": []string{"reranking_model", "weighted_score", "none"}, "default": "none"},
				"weights":            map[string]any{"type": "object"},
				"filters":            map[string]any{"type": "object"},
			},
			"required": []string{"query", "available_datasets"},
		},
		Meta: map[string]string{
			"description": "Retrieve from multiple datasets (internal+external), then rerank/merge into normalized resources",
		},
	}, multiHandler)

	// Helper: llm.router (stub for now; kept as a tool so you can swap impl later)
	llmRouterHandler := func(args map[string]any) (map[string]any, error) {
		// For now: choose the first dataset, or by tag match.
		query, _ := args["query"].(string)
		dsAny, ok := args["datasets"].([]any)
		if !ok || len(dsAny) == 0 {
			return nil, fmt.Errorf("datasets is required")
		}
		// datasets: array of objects {id,name,description,tags}
		datasets := make([]rag.DatasetConfig, 0, len(dsAny))
		for _, v := range dsAny {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			id, _ := m["id"].(string)
			name, _ := m["name"].(string)
			desc, _ := m["description"].(string)
			var tags []string
			if ta, ok := m["tags"].([]any); ok {
				for _, tv := range ta {
					if ts, ok := tv.(string); ok {
						tags = append(tags, ts)
					}
				}
			}
			datasets = append(datasets, rag.DatasetConfig{ID: id, Name: name, Description: desc, Tags: tags, Type: rag.DatasetInternal})
		}
		rb := &rag.RuleBasedRouter{}
		id, dbg, err := rb.Route(context.Background(), query, datasets)
		if err != nil {
			return nil, err
		}
		return map[string]any{"dataset_id": id, "debug": dbg}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "llm.router",
		Version: "v1",
		Tags:    []string{"read", "rag", "router"},
		Scopes:  []string{"rag:route"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":    map[string]any{"type": "string"},
				"datasets": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			},
			"required": []string{"query", "datasets"},
		},
		Meta: map[string]string{
			"description": "Router tool (stub). Replace with real LLM tool-calling router later.",
		},
	}, llmRouterHandler)

	// Helper: rag.rerank_model (stub for now)
	rerankHandler := func(args map[string]any) (map[string]any, error) {
		// Expect resources as JSON array; return unchanged (placeholder).
		raw, ok := args["resources"]
		if !ok {
			return nil, fmt.Errorf("resources is required")
		}
		b, _ := json.Marshal(raw)
		var res []rag.RetrievalResource
		if err := json.Unmarshal(b, &res); err != nil {
			return nil, fmt.Errorf("resources must be a list of retrieval_resource objects")
		}
		return map[string]any{"resources": res, "count": len(res)}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "rag.rerank_model",
		Version: "v1",
		Tags:    []string{"read", "rag", "rerank"},
		Scopes:  []string{"rag:rerank"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string"},
				"resources": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
				"model":     map[string]any{"type": "string"},
			},
			"required": []string{"query", "resources"},
		},
		Meta: map[string]string{
			"description": "Cross-encoder reranker tool (stub). Wire a real model later.",
		},
	}, rerankHandler)
}

func parseCSVSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
