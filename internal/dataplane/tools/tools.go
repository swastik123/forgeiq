package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/interfaces"
	mcpserver "forgeiq/internal/dataplane/mcp"
	"forgeiq/internal/external/elasticsearch"
	"forgeiq/internal/observability"
	"forgeiq/internal/vector/pgvector"

	"go.uber.org/zap"
)

// RegisterAll registers all available tools with the MCP server
func RegisterAll(s *mcpserver.Server, cfg *config.Config, logger *observability.Logger) {
	if logger == nil {
		logger = observability.NewNopLogger()
	}
	var (
		vectorStore *pgvector.Store
		esClient    *elasticsearch.Client
	)

	if cfg != nil && cfg.Vector.Enabled {
		store, err := pgvector.New(cfg.Vector.DSN, cfg.Vector.Table, cfg.Vector.Dim)
		if err != nil {
			logger.Error("Failed to initialize pgvector store; vector tools will be unavailable", zap.Error(err))
		} else {
			vectorStore = store
			logger.Info("Initialized pgvector store", zap.String("table", cfg.Vector.Table), zap.Int("dim", cfg.Vector.Dim))
		}
	}

	if cfg != nil && strings.TrimSpace(cfg.Elastic.URL) != "" {
		c, err := elasticsearch.New(cfg.Elastic.URL, cfg.Elastic.Index, nil)
		if err != nil {
			logger.Error("Failed to initialize Elasticsearch client; logs.search will fall back to simulated results", zap.Error(err))
		} else {
			esClient = c.WithAuth(cfg.Elastic.APIKey, cfg.Elastic.Username, cfg.Elastic.Password)
			logger.Info("Initialized Elasticsearch client", zap.String("index", cfg.Elastic.Index), zap.String("url", cfg.Elastic.URL))
		}
	}

	// RAG tools (internal vs external retrieval based on dataset type)
	registerRAGTools(s, cfg, logger, vectorStore, esClient)

	// Memory tools (canonical + discovery)
	registerMemoryTools(s, cfg, logger, vectorStore)

	// Minimal LLM tools (demo)
	registerLLMTools(s)

	// Runtime plugins (optional): load additional tools from plugins/manifest.yaml
	RegisterPlugins(s, cfg, logger)

	// Register logs.search tool
	logsSearchHandler := func(args map[string]any) (map[string]any, error) {
		query, _ := args["query"].(string)
		limit, ok := args["limit"].(float64)
		if !ok {
			limit = 50
		}

		logger.Info("logs.search called", zap.String("query", query), zap.Int("limit", int(limit)))

		// If Elasticsearch is configured, query it.
		if esClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			hits, err := esClient.Search(ctx, query, int(limit))
			if err != nil {
				logger.Error("logs.search elastic query failed; falling back to simulated results", zap.Error(err))
			} else {
				logs := make([]map[string]any, 0, len(hits))
				for _, h := range hits {
					logs = append(logs, map[string]any{
						"_index":    h.Index,
						"_id":       h.ID,
						"_score":    h.Score,
						"timestamp": h.Source["@timestamp"],
						"level":     h.Source["level"],
						"message":   h.Source["message"],
						"service":   h.Source["service"],
						"source":    h.Source,
					})
				}
				return map[string]any{
					"logs":   logs,
					"count":  len(logs),
					"query":  query,
					"engine": "elasticsearch",
				}, nil
			}
		}

		// Simulate log search - in production, this would query actual log storage
		logs := []map[string]any{
			{
				"timestamp": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
				"level":     "ERROR",
				"message":   fmt.Sprintf("Error in %s: %s", query, "Connection timeout"),
				"service":   extractService(query),
			},
			{
				"timestamp": time.Now().Add(-3 * time.Minute).Format(time.RFC3339),
				"level":     "WARN",
				"message":   fmt.Sprintf("Warning in %s: %s", query, "High latency detected"),
				"service":   extractService(query),
			},
		}

		// Limit results
		if int(limit) < len(logs) {
			logs = logs[:int(limit)]
		}

		return map[string]any{
			"logs":  logs,
			"count": len(logs),
			"query": query,
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "logs.search",
		Version: "v1",
		Tags:    []string{"read", "logs"},
		Scopes:  []string{"logs:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "number", "default": 50},
			},
			"required": []string{"query"},
		},
		Meta: map[string]string{
			"description": "Search application logs by query string",
		},
	}, logsSearchHandler)

	// Optional: logs.ingest (for local testing with Elasticsearch)
	logsIngestHandler := func(args map[string]any) (map[string]any, error) {
		if esClient == nil {
			return nil, fmt.Errorf("elasticsearch is not configured (set ELASTIC_URL)")
		}
		index, _ := args["index"].(string)
		docAny, ok := args["doc"].(map[string]any)
		if !ok || docAny == nil {
			return nil, fmt.Errorf("doc is required and must be an object")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		id, err := esClient.IndexDoc(ctx, index, docAny)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "index": index}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "logs.ingest",
		Version: "v1",
		Tags:    []string{"write", "logs", "external"},
		Scopes:  []string{"logs:write"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index": map[string]any{"type": "string", "description": "Concrete index name (e.g. logs-agent-platform)"},
				"doc":   map[string]any{"type": "object"},
			},
			"required": []string{"index", "doc"},
		},
		Meta: map[string]string{
			"description": "Index a log document into Elasticsearch (useful for local testing)",
		},
	}, logsIngestHandler)

	// Register incidents.create_ticket tool
	createTicketHandler := func(args map[string]any) (map[string]any, error) {
		title, _ := args["title"].(string)
		description, _ := args["description"].(string)

		if title == "" {
			return nil, fmt.Errorf("title is required")
		}

		logger.Info("incidents.create_ticket called", zap.String("title", title))

		// Simulate ticket creation - in production, this would create a ticket in a ticketing system
		ticketID := fmt.Sprintf("INC-%d", time.Now().Unix())

		return map[string]any{
			"ticket_id":   ticketID,
			"title":       title,
			"description": description,
			"status":      "open",
			"created_at":  time.Now().Format(time.RFC3339),
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "incidents.create_ticket",
		Version: "v1",
		Tags:    []string{"write", "incidents"},
		Scopes:  []string{"incidents:write"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
			"required": []string{"title"},
		},
		Meta: map[string]string{
			"description": "Create a new incident ticket",
		},
	}, createTicketHandler)

	// Register remediation.restart_service tool
	restartServiceHandler := func(args map[string]any) (map[string]any, error) {
		service, _ := args["service"].(string)

		if service == "" {
			return nil, fmt.Errorf("service name is required")
		}

		logger.Info("remediation.restart_service called", zap.String("service", service))

		// Simulate service restart - in production, this would actually restart the service
		// This is a dangerous operation, so it should be gated by policy/approval

		// Simulate restart delay
		time.Sleep(100 * time.Millisecond)

		return map[string]any{
			"service":      service,
			"action":       "restarted",
			"status":       "success",
			"restarted_at": time.Now().Format(time.RFC3339),
			"message":      fmt.Sprintf("Service %s has been restarted successfully", service),
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "remediation.restart_service",
		Version: "v1",
		Tags:    []string{"write", "remediation"},
		Scopes:  []string{"remediation:write"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service": map[string]any{"type": "string"},
			},
			"required": []string{"service"},
		},
		Meta: map[string]string{
			"description": "Restart a service (requires approval)",
		},
	}, restartServiceHandler)

	// Register vectors.upsert + vectors.search tools (pgvector)
	vectorsUpsertHandler := func(args map[string]any) (map[string]any, error) {
		if vectorStore == nil {
			return nil, fmt.Errorf("vector store is not enabled (set VECTOR_ENABLED=true and VECTOR_DSN)")
		}
		id, _ := args["id"].(string)
		namespace, _ := args["namespace"].(string)
		content, _ := args["content"].(string)

		var metadata map[string]any
		if m, ok := args["metadata"].(map[string]any); ok {
			metadata = m
		}

		embAny, ok := args["embedding"].([]any)
		if !ok {
			return nil, fmt.Errorf("embedding is required and must be an array of numbers")
		}
		emb := make([]float64, 0, len(embAny))
		for _, v := range embAny {
			switch n := v.(type) {
			case float64:
				emb = append(emb, n)
			case int:
				emb = append(emb, float64(n))
			default:
				return nil, fmt.Errorf("embedding must contain only numbers")
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := vectorStore.Upsert(ctx, id, namespace, content, metadata, emb); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "id": id, "namespace": namespace}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "vectors.upsert",
		Version: "v1",
		Tags:    []string{"write", "vector"},
		Scopes:  []string{"vectors:write"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":        map[string]any{"type": "string"},
				"namespace": map[string]any{"type": "string", "default": "default"},
				"content":   map[string]any{"type": "string"},
				"metadata":  map[string]any{"type": "object"},
				"embedding": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
			},
			"required": []string{"id", "embedding"},
		},
		Meta: map[string]string{
			"description": "Upsert a vector record into pgvector-backed Postgres",
		},
	}, vectorsUpsertHandler)

	vectorsSearchHandler := func(args map[string]any) (map[string]any, error) {
		if vectorStore == nil {
			return nil, fmt.Errorf("vector store is not enabled (set VECTOR_ENABLED=true and VECTOR_DSN)")
		}
		namespace, _ := args["namespace"].(string)
		topK, ok := args["top_k"].(float64)
		if !ok {
			topK = 10
		}
		embAny, ok := args["query_embedding"].([]any)
		if !ok {
			return nil, fmt.Errorf("query_embedding is required and must be an array of numbers")
		}
		emb := make([]float64, 0, len(embAny))
		for _, v := range embAny {
			switch n := v.(type) {
			case float64:
				emb = append(emb, n)
			case int:
				emb = append(emb, float64(n))
			default:
				return nil, fmt.Errorf("query_embedding must contain only numbers")
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		results, err := vectorStore.Search(ctx, namespace, emb, int(topK))
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			out = append(out, map[string]any{
				"id":        r.ID,
				"namespace": r.Namespace,
				"content":   r.Content,
				"metadata":  r.Metadata,
				"distance":  r.Distance,
				"score":     r.Score,
			})
		}
		return map[string]any{
			"results": out,
			"count":   len(out),
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "vectors.search",
		Version: "v1",
		Tags:    []string{"read", "vector"},
		Scopes:  []string{"vectors:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace":       map[string]any{"type": "string", "default": "default"},
				"top_k":           map[string]any{"type": "number", "default": 10},
				"query_embedding": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
			},
			"required": []string{"query_embedding"},
		},
		Meta: map[string]string{
			"description": "Vector similarity search (cosine distance) over pgvector-backed Postgres",
		},
	}, vectorsSearchHandler)

	logger.Info("Registered MCP tools",
		zap.Strings("tools", []string{
			"logs.search:v1",
			"logs.ingest:v1",
			"incidents.create_ticket:v1",
			"remediation.restart_service:v1",
			"vectors.upsert:v1",
			"vectors.search:v1",
		}),
	)
}

// extractService extracts service name from query string (simple heuristic)
func extractService(query string) string {
	// Simple extraction - in production, use proper parsing
	words := []string{"payments", "orders", "users", "inventory", "shipping"}
	for _, word := range words {
		if len(query) >= len(word) {
			for i := 0; i <= len(query)-len(word); i++ {
				if query[i:i+len(word)] == word {
					return word
				}
			}
		}
	}
	return "unknown"
}
