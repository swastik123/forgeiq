package rag

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Registry         *Registry
	Internal         *InternalRetriever
	ExternalElastic  *ElasticRetriever
	RuleRouter       *RuleBasedRouter
}

func (s *Service) RetrieveSingle(ctx context.Context, query string, datasetIDs []string, routerMode RouterMode, topK int, filters map[string]any) ([]RetrievalResource, map[string]any, error) {
	if s == nil || s.Registry == nil {
		return nil, nil, fmt.Errorf("rag service not configured (registry missing)")
	}
	datasets, err := s.Registry.List(datasetIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(datasets) == 0 {
		return nil, nil, fmt.Errorf("available_datasets is empty")
	}

	var router Router
	switch routerMode {
	case RouterRuleBased, "":
		router = s.RuleRouter
	case RouterLLMToolcall, RouterReAct:
		// Clean extension point: wire LLM/ReAct router later.
		// For now we fall back to rule-based but expose debug info.
		router = s.RuleRouter
	default:
		return nil, nil, fmt.Errorf("invalid router_mode: %s", routerMode)
	}
	if router == nil {
		router = &RuleBasedRouter{}
	}

	dsID, debug, err := router.Route(ctx, query, datasets)
	if err != nil {
		return nil, nil, err
	}
	ds, ok := s.Registry.Get(dsID)
	if !ok {
		return nil, nil, fmt.Errorf("router selected unknown dataset: %s", dsID)
	}
	if debug == nil {
		debug = map[string]any{}
	}
	debug["router_mode"] = string(routerMode)
	if routerMode == RouterLLMToolcall || routerMode == RouterReAct {
		debug["router_note"] = "llm/react router not wired; used rule_based fallback"
	}

	res, err := s.retrieveDataset(ctx, ds, query, topK, filters)
	if err != nil {
		return nil, debug, err
	}
	debug["selected_dataset_id"] = dsID
	return res, debug, nil
}

func (s *Service) RetrieveMultiple(ctx context.Context, query string, datasetIDs []string, perDatasetTopK int, filters map[string]any) ([]RetrievalResource, map[string]any, error) {
	if s == nil || s.Registry == nil {
		return nil, nil, fmt.Errorf("rag service not configured (registry missing)")
	}
	datasets, err := s.Registry.List(datasetIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(datasets) == 0 {
		return nil, nil, fmt.Errorf("available_datasets is empty")
	}

	all := make([]RetrievalResource, 0, len(datasets)*perDatasetTopK)
	debug := map[string]any{
		"datasets": len(datasets),
	}
	for _, ds := range datasets {
		res, err := s.retrieveDataset(ctx, ds, query, perDatasetTopK, filters)
		if err != nil {
			// Keep going; partial failure is often acceptable for multi-retrieve.
			key := "error_" + ds.ID
			debug[key] = err.Error()
			continue
		}
		all = append(all, res...)
	}
	return all, debug, nil
}

func (s *Service) retrieveDataset(ctx context.Context, ds DatasetConfig, query string, topK int, filters map[string]any) ([]RetrievalResource, error) {
	// Tight per-dataset timeout (data-plane safety)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	switch ds.Type {
	case DatasetInternal:
		if s.Internal == nil {
			return nil, fmt.Errorf("internal retriever not configured")
		}
		return s.Internal.Retrieve(ctx, ds, query, topK, filters)
	case DatasetExternal:
		if ds.External == nil {
			return nil, fmt.Errorf("dataset %s missing external config", ds.ID)
		}
		switch strings.ToLower(ds.External.Adapter) {
		case "elastic":
			if s.ExternalElastic == nil {
				return nil, fmt.Errorf("external elastic retriever not configured")
			}
			return s.ExternalElastic.Retrieve(ctx, ds, query, topK, filters)
		default:
			return nil, fmt.Errorf("unsupported external adapter: %s", ds.External.Adapter)
		}
	default:
		return nil, fmt.Errorf("invalid dataset type: %s", ds.Type)
	}
}


