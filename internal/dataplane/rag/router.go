package rag

import (
	"context"
	"fmt"
	"strings"
)

type RouterMode string

const (
	RouterLLMToolcall RouterMode = "llm_toolcall"
	RouterReAct       RouterMode = "react"
	RouterRuleBased   RouterMode = "rule_based"
)

type Router interface {
	Route(ctx context.Context, query string, datasets []DatasetConfig) (datasetID string, debug map[string]any, err error)
}

// RuleBasedRouter routes by matching dataset tags/name keywords in the query.
type RuleBasedRouter struct{}

func (r *RuleBasedRouter) Route(ctx context.Context, query string, datasets []DatasetConfig) (string, map[string]any, error) {
	_ = ctx
	q := strings.ToLower(query)
	for _, ds := range datasets {
		for _, t := range ds.Tags {
			if t != "" && strings.Contains(q, strings.ToLower(t)) {
				return ds.ID, map[string]any{"matched": "tag", "value": t}, nil
			}
		}
		if ds.Name != "" && strings.Contains(q, strings.ToLower(ds.Name)) {
			return ds.ID, map[string]any{"matched": "name", "value": ds.Name}, nil
		}
		if ds.Description != "" && strings.Contains(q, strings.ToLower(firstWord(ds.Description))) {
			return ds.ID, map[string]any{"matched": "desc_hint"}, nil
		}
	}
	if len(datasets) == 0 {
		return "", nil, fmt.Errorf("no datasets available")
	}
	return datasets[0].ID, map[string]any{"matched": "fallback_first"}, nil
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}


