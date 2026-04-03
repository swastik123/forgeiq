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
	"forgeiq/internal/memory"
	"forgeiq/internal/observability"
	"forgeiq/internal/vector/pgvector"

	"go.uber.org/zap"
)

// registerMemoryTools registers:
// - memory.propose:v1 (write canonical + update discovery)
// - memory.search:v1  (IDs only)
// - memory.get:v1     (redacted)
func registerMemoryTools(s *mcpserver.Server, cfg *config.Config, logger *observability.Logger, vectorStore *pgvector.Store) {
	if logger == nil {
		logger = observability.NewNopLogger()
	}
	if cfg == nil {
		cfg = config.Load()
	}

	var store *memory.PostgresStore
	if strings.TrimSpace(cfg.Storage.DSN) != "" {
		ms, err := memory.NewPostgresStore(cfg.Storage.DSN)
		if err != nil {
			logger.Error("Failed to initialize canonical memory store; memory tools will be unavailable", zap.Error(err))
		} else {
			store = ms
			logger.Info("Initialized canonical memory store (postgres)")
		}
	} else {
		logger.Warn("STORAGE_DSN not set; canonical memory store is unavailable")
	}

	// Use the same embedder selection pattern as RAG, but ensure embedding dim matches vector dim.
	var embedder rag.Embedder
	if vectorStore != nil {
		dim := cfg.Vector.Dim
		if dim <= 0 {
			dim = cfg.Embeddings.Dim
		}
		provider := strings.ToLower(strings.TrimSpace(cfg.Embeddings.Provider))
		switch provider {
		case "", "hash":
			// Deterministic dev/test embedder only; NOT semantic.
			embedder = rag.NewHashEmbedder(dim)
			logger.Warn("Using HashEmbedder for memory search (deterministic but NOT semantic). For semantic LTM, set EMBEDDINGS_PROVIDER=http and configure EMBEDDINGS_URL/API key.", zap.Int("dim", dim))
		case "http":
			if strings.TrimSpace(cfg.Embeddings.URL) == "" {
				logger.Error("EMBEDDINGS_PROVIDER=http but EMBEDDINGS_URL is empty; memory semantic search is unavailable")
				embedder = nil
			} else if httpEmb, err := rag.NewHTTPEmbedder(cfg.Embeddings.URL, cfg.Embeddings.APIKey, cfg.Embeddings.Model, dim); err != nil {
				// Fail fast: do not silently fall back to hash when the operator asked for semantic embeddings.
				logger.Error("Failed to initialize HTTP embedder for memory tools; memory semantic search is unavailable", zap.Error(err))
				embedder = nil
			} else {
				embedder = httpEmb
				logger.Info("Initialized HTTP embedder for memory tools", zap.Int("dim", dim))
			}
		default:
			logger.Error("Unknown EMBEDDINGS_PROVIDER; memory semantic search is unavailable", zap.String("provider", provider))
			embedder = nil
		}
	}

	// 	 Workflow Engine: MemoryPlanStage, MemoryFetchStage, MemoryCommitStage
	// - Agent Runtime: stateless execution + delta output
	// - MCP Tools: external system calls (Bitbucket/scanners/k8s/logs/etc.)
	// - Memory Governance Layer: gate for writes (allow/deny + dedup/merge)
	// - Policy & Verification: rules/checks/approvals (v1 mostly rules + evidence checks)
	// - Canonical Memory Store: durable records + provenance + versioning
	// - Discovery Memory Index: fast retrieval (filters + vector/keyword)
	// - Embedding Service: embedding generation for queries + stored memories
	// - Audit Log: stores artifacts/hashes/receipts for replay & compliance

	// 	Workflow Engine: MemoryFetchStage (only if read_ltm=true)                 │
	// │    Query path:                                                               │
	// │      a) Discovery Memory Index (fast retrieval + filters)                    │
	// │      b) Embedding Service (embed query, similarity search)                   │
	// │      c) Canonical Memory Store (fetch full records by IDs if needed)         │
	// │    Output: MemoryBundle artifact → Audit Log

	// 	Workflow Engine: MemoryCommitStage                                        │
	// │    Send delta.memory_candidates[] to:                                         │
	// │      - Memory Governance Layer (gate)                                        │
	// │      - Policy & Verification Subsystems (rules/checks)                       │
	// │    Apply:                                                                    │
	// │      - allowlist (types/namespaces)                                          │
	// │      - secret/PII hard block                                                 │
	// │      - verification (evidence_refs or user_confirmed)                        │
	// │      - dedup/merge (namespace+type+title → new version or insert)            │
	// │    Output: verdicts + receipts → Audit Log

	// ───────────────────────────────────────────────────────────────────────────┐
	// │ 6) Persist approved memories                                                 │
	// │    a) Canonical Memory Store (source of truth, versioned records)            │
	// │    b) Embedding Service (embed new/updated memory content)                   │
	// │    c) Discovery Memory Index (upsert vector + metadata for retrieval)        │
	// │    Output: WriteReceipts + index_version → Audit Log
	// memory.propose
	proposeHandler := func(args map[string]any) (map[string]any, error) {
		if store == nil {
			return nil, fmt.Errorf("canonical memory store not configured (set STORAGE_DSN)")
		}
		tenantID, _ := args["tenant_id"].(string)
		recordID, _ := args["record_id"].(string)
		recordType, _ := args["record_type"].(string)
		payload, _ := args["payload"].(map[string]any)
		sensStr, _ := args["sensitivity"].(string)
		suggestedConf, _ := args["confidence"].(float64)
		expiresAtStr, _ := args["expires_at"].(string)
		sanitizedTextArg, _ := args["sanitized_text"].(string)

		var evidenceRefs []string
		if a, ok := args["evidence_refs"].([]any); ok {
			for _, v := range a {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					evidenceRefs = append(evidenceRefs, s)
				}
			}
		}

		var expiresAt *time.Time
		if strings.TrimSpace(expiresAtStr) != "" {
			t, err := time.Parse(time.RFC3339, expiresAtStr)
			if err != nil {
				return nil, fmt.Errorf("expires_at must be RFC3339: %w", err)
			}
			expiresAt = &t
		}

		// (1) Confidence: do NOT trust caller-provided confidence. Treat it as suggested and compute final confidence here.
		finalConf := clamp01(suggestedConf)
		if finalConf == 0 {
			// If caller omitted confidence, align with schema default.
			finalConf = 0.5
		}

		// (2) Evidence rule: if evidence_refs is empty, cap confidence and/or force short TTL.
		now := time.Now().UTC()
		if len(evidenceRefs) == 0 {
			if finalConf > 0.5 {
				finalConf = 0.5
			}
			if expiresAt == nil {
				t := now.Add(30 * 24 * time.Hour)
				expiresAt = &t
				expiresAtStr = t.Format(time.RFC3339)
			}
		}

		// Compute confidence using simple controller policy (extensible).
		finalConf = computeConfidence(recordType, evidenceRefs, "workflow", finalConf)

		out, err := store.Upsert(context.Background(), memory.UpsertInput{
			ID:           recordID,
			TenantID:     tenantID,
			RecordType:   recordType,
			Payload:      payload,
			EvidenceRefs: evidenceRefs,
			Confidence:   finalConf,
			Sensitivity:  memory.Sensitivity(strings.ToLower(strings.TrimSpace(sensStr))),
			ExpiresAt:    expiresAt,
		})
		if err != nil {
			return nil, err
		}

		// (4) Sanitized text: derive deterministically from canonical fields. Ignore caller-provided sanitized_text (poisoning risk).
		if strings.TrimSpace(sanitizedTextArg) != "" {
			logger.Warn("Ignoring caller-provided sanitized_text for memory.propose; derived server-side", zap.String("record_id", out.ID))
		}
		sanitizedText := renderSanitizedText(out.RecordType, out.Payload)

		indexed := false
		if vectorStore != nil && embedder != nil && strings.TrimSpace(sanitizedText) != "" {
			emb, eerr := embedder.Embed(context.Background(), sanitizedText)
			if eerr != nil {
				logger.Error("memory.propose embedding failed", zap.Error(eerr))
			} else {
				ns := "memory:" + strings.TrimSpace(out.TenantID)
				meta := map[string]any{
					"record_type": out.RecordType,
					"sensitivity": string(out.Sensitivity),
					"confidence":  out.Confidence,
					"expires_at":  expiresAtStr,
					"schema":      "canonical",
					"version":     out.Version,
					// (3) Recency: include updated_at for rerank / freshness logic.
					"updated_at": out.UpdatedAt.UTC().Format(time.RFC3339),
				}
				if err := vectorStore.Upsert(context.Background(), out.ID, ns, "", meta, emb); err != nil {
					logger.Error("memory.propose vector upsert failed", zap.Error(err))
				} else {
					indexed = true
				}
			}
		}

		return map[string]any{
			"ok":          true,
			"record_id":   out.ID,
			"tenant_id":   out.TenantID,
			"record_type": out.RecordType,
			"version":     out.Version,
			"indexed":     indexed,
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "memory.propose",
		Version: "v1",
		Tags:    []string{"write", "memory"},
		Scopes:  []string{"memory:write"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tenant_id":      map[string]any{"type": "string", "default": "default"},
				"record_id":      map[string]any{"type": "string", "description": "Optional stable ID; if omitted, one is generated."},
				"record_type":    map[string]any{"type": "string"},
				"payload":        map[string]any{"type": "object"},
				"evidence_refs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"confidence":     map[string]any{"type": "number", "default": 0.5},
				"expires_at":     map[string]any{"type": "string", "description": "RFC3339 timestamp (optional)"},
				"sensitivity":    map[string]any{"type": "string", "enum": []any{"public", "internal", "restricted", "secret"}, "default": "internal"},
				"sanitized_text": map[string]any{"type": "string", "description": "Sanitized text used only for discovery indexing; optional but recommended."},
			},
			"required": []string{"record_type", "payload"},
		},
		Meta: map[string]string{
			"description": "Submit a canonical memory proposal (workflow-owned). Writes to canonical store and optionally updates discovery index.",
		},
	}, proposeHandler)

	// memory.search (IDs only)
	searchHandler := func(args map[string]any) (map[string]any, error) {
		if vectorStore == nil || embedder == nil {
			return nil, fmt.Errorf("memory search not configured (need VECTOR_ENABLED/VECTOR_DSN and an embedder: EMBEDDINGS_PROVIDER=http with EMBEDDINGS_URL, or EMBEDDINGS_PROVIDER=hash for dev/test)")
		}
		tenantID, _ := args["tenant_id"].(string)
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		topKf, _ := args["top_k"].(float64)
		topK := int(topKf)
		if topK <= 0 {
			topK = 5
		}
		// Optional post-filtering knobs (controller-friendly).
		minConff, _ := args["min_confidence"].(float64)
		excludeExpired, _ := args["exclude_expired"].(bool)
		includeFiltered, _ := args["include_filtered"].(bool)
		maxSensStr, _ := args["max_sensitivity"].(string)
		maxSens := memory.Sensitivity(strings.ToLower(strings.TrimSpace(maxSensStr)))
		if maxSens == "" {
			maxSens = memory.SensitivityInternal
		}
		// Default behavior: exclude expired records (but keep sensitivity/confidence filtering opt-in via args).
		if _, ok := args["exclude_expired"]; !ok {
			excludeExpired = true
		}

		emb, err := embedder.Embed(context.Background(), query)
		if err != nil {
			return nil, err
		}
		ns := "memory:" + strings.TrimSpace(tenantID)
		if ns == "memory:" {
			ns = "memory:default"
		}
		recs, err := vectorStore.Search(context.Background(), ns, emb, topK)
		if err != nil {
			return nil, err
		}
		matches := make([]map[string]any, 0, len(recs))
		filtered := make([]map[string]any, 0)
		now := time.Now()
		for i, r := range recs {
			// IDs-only: do not return content.
			m := map[string]any{
				"record_id": r.ID,
				"score":     r.Score,
				"rank":      i + 1,
			}

			// Strictly safe metadata subset (no free-form text).
			meta := map[string]any{}
			var (
				confidence  float64
				expiresAt   string
				sensitivity string
				recordType  string
				version     any
			)
			if r.Metadata != nil {
				if v, ok := r.Metadata["confidence"].(float64); ok {
					confidence = v
				} else if v, ok := r.Metadata["confidence"].(int); ok {
					confidence = float64(v)
				}
				if v, ok := r.Metadata["expires_at"].(string); ok {
					expiresAt = v
				}
				if v, ok := r.Metadata["sensitivity"].(string); ok {
					sensitivity = v
				}
				if v, ok := r.Metadata["record_type"].(string); ok {
					recordType = v
				}
				if v, ok := r.Metadata["version"]; ok {
					version = v
				}
			}
			if recordType != "" {
				meta["record_type"] = recordType
			}
			if sensitivity != "" {
				meta["sensitivity"] = sensitivity
			}
			if expiresAt != "" {
				meta["expires_at"] = expiresAt
			}
			if version != nil {
				meta["version"] = version
			}
			// Always include confidence (controller can use it even if 0).
			meta["confidence"] = confidence
			m["metadata"] = meta

			// Optional post-filtering with reason codes.
			reasonCodes := make([]string, 0, 2)
			if minConff > 0 && confidence > 0 && confidence < minConff {
				reasonCodes = append(reasonCodes, "low_confidence_filtered")
			}
			if strings.TrimSpace(expiresAt) != "" {
				if t, err := time.Parse(time.RFC3339, expiresAt); err == nil && now.After(t) {
					if excludeExpired {
						reasonCodes = append(reasonCodes, "expired_filtered")
					}
				}
			}
			if strings.TrimSpace(sensitivity) != "" {
				if memory.SensitivityRank(memory.Sensitivity(strings.ToLower(strings.TrimSpace(sensitivity)))) > memory.SensitivityRank(maxSens) {
					reasonCodes = append(reasonCodes, "sensitivity_filtered")
				}
			}

			if len(reasonCodes) > 0 {
				m["reason_codes"] = reasonCodes
				if !includeFiltered {
					filtered = append(filtered, m)
					continue
				}
			}
			matches = append(matches, m)
		}
		return map[string]any{
			"ok":             true,
			"matches":        matches,
			"count":          len(matches),
			"filtered":       filtered,
			"filtered_count": len(filtered),
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "memory.search",
		Version: "v1",
		Tags:    []string{"read", "memory"},
		Scopes:  []string{"memory:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tenant_id":        map[string]any{"type": "string", "default": "default"},
				"query":            map[string]any{"type": "string"},
				"top_k":            map[string]any{"type": "number", "default": 5},
				"min_confidence":   map[string]any{"type": "number", "default": 0.0, "description": "Optional post-filter: drop results with confidence < min_confidence (if confidence metadata exists)."},
				"exclude_expired":  map[string]any{"type": "boolean", "default": true, "description": "Optional post-filter: mark/drop expired results based on metadata.expires_at."},
				"max_sensitivity":  map[string]any{"type": "string", "enum": []any{"public", "internal", "restricted", "secret"}, "default": "internal", "description": "Optional post-filter: mark/drop results exceeding this sensitivity."},
				"include_filtered": map[string]any{"type": "boolean", "default": false, "description": "If true, include filtered results in matches with reason_codes; otherwise they appear in filtered[]."},
			},
			"required": []string{"query"},
		},
		Meta: map[string]string{
			"description": "Semantic search over discovery index. Returns record identifiers only (no content), plus rank and safe metadata. Can optionally post-filter and emit reason codes.",
		},
	}, searchHandler)

	// memory.get (redacted)
	getHandler := func(args map[string]any) (map[string]any, error) {
		if store == nil {
			return nil, fmt.Errorf("canonical memory store not configured (set STORAGE_DSN)")
		}
		tenantID, _ := args["tenant_id"].(string)
		recordID, _ := args["record_id"].(string)
		maxSensStr, _ := args["max_sensitivity"].(string)
		includePayload := true
		if b, ok := args["include_payload"].(bool); ok {
			includePayload = b
		}
		rec, ok, err := store.Get(context.Background(), tenantID, recordID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return map[string]any{"ok": false, "found": false}, nil
		}

		// Expiry enforcement.
		expired := false
		if rec.ExpiresAt != nil && time.Now().After(*rec.ExpiresAt) {
			expired = true
			includePayload = false
		}

		maxSens := memory.Sensitivity(strings.ToLower(strings.TrimSpace(maxSensStr)))
		if maxSens == "" {
			maxSens = memory.SensitivityInternal
		}
		redacted := false
		if !includePayload || memory.SensitivityRank(rec.Sensitivity) > memory.SensitivityRank(maxSens) {
			redacted = true
			rec.Payload = nil
		}

		out := map[string]any{
			"id":            rec.ID,
			"tenant_id":     rec.TenantID,
			"record_type":   rec.RecordType,
			"evidence_refs": rec.EvidenceRef,
			"confidence":    rec.Confidence,
			"sensitivity":   string(rec.Sensitivity),
			"expires_at":    "",
			"version":       rec.Version,
			"created_at":    rec.CreatedAt.Format(time.RFC3339),
			"updated_at":    rec.UpdatedAt.Format(time.RFC3339),
		}
		if rec.ExpiresAt != nil {
			out["expires_at"] = rec.ExpiresAt.Format(time.RFC3339)
		}
		if rec.Payload != nil {
			out["payload"] = rec.Payload
		}

		return map[string]any{
			"ok":       true,
			"found":    true,
			"expired":  expired,
			"redacted": redacted,
			"record":   out,
		}, nil
	}

	s.RegisterToolWithInfo(interfaces.ToolInfo{
		Name:    "memory.get",
		Version: "v1",
		Tags:    []string{"read", "memory"},
		Scopes:  []string{"memory:read"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tenant_id":       map[string]any{"type": "string", "default": "default"},
				"record_id":       map[string]any{"type": "string"},
				"max_sensitivity": map[string]any{"type": "string", "enum": []any{"public", "internal", "restricted", "secret"}, "default": "internal"},
				"include_payload": map[string]any{"type": "boolean", "default": true},
			},
			"required": []string{"record_id"},
		},
		Meta: map[string]string{
			"description": "Fetch a canonical memory record. Payload is redacted if sensitivity exceeds max_sensitivity (or record is expired).",
		},
	}, getHandler)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// computeConfidence implements a controller-side confidence policy.
// It is intentionally conservative and should be extended as you add provenance signals.
func computeConfidence(recordType string, evidenceRefs []string, source string, suggested float64) float64 {
	_ = recordType
	_ = source
	conf := clamp01(suggested)
	// If evidence exists, enforce a modest minimum (still bounded by 1.0).
	if len(evidenceRefs) > 0 && conf < 0.6 {
		conf = 0.6
	}
	// If no evidence, cap confidence.
	if len(evidenceRefs) == 0 && conf > 0.5 {
		conf = 0.5
	}
	return conf
}

// renderSanitizedText deterministically renders canonical fields into a string suitable for embedding.
// Keep it compact, structured, and free of executable instructions.
func renderSanitizedText(recordType string, payload map[string]any) string {
	if payload == nil {
		payload = map[string]any{}
	}
	// encoding/json marshals map keys in sorted order, giving stable output.
	b, err := json.Marshal(payload)
	if err != nil {
		return "record_type:" + strings.TrimSpace(recordType)
	}
	s := "record_type:" + strings.TrimSpace(recordType) + "\n" + "payload:" + string(b)
	const maxLen = 4000
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
