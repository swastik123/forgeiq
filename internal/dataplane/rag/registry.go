package rag

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Registry struct {
	byID map[string]DatasetConfig
}

func LoadRegistryFromJSON(datasetsJSON string) (*Registry, error) {
	datasetsJSON = strings.TrimSpace(datasetsJSON)
	if datasetsJSON == "" {
		return &Registry{byID: map[string]DatasetConfig{}}, nil
	}

	var list []DatasetConfig
	if err := json.Unmarshal([]byte(datasetsJSON), &list); err != nil {
		return nil, fmt.Errorf("invalid RAG_DATASETS_JSON: %w", err)
	}

	r := &Registry{byID: make(map[string]DatasetConfig, len(list))}
	for _, ds := range list {
		ds.ID = strings.TrimSpace(ds.ID)
		if ds.ID == "" {
			return nil, fmt.Errorf("dataset id is required")
		}
		if _, exists := r.byID[ds.ID]; exists {
			return nil, fmt.Errorf("duplicate dataset id: %s", ds.ID)
		}
		switch ds.Type {
		case DatasetInternal:
			if ds.Internal == nil {
				return nil, fmt.Errorf("dataset %s is internal but missing internal config", ds.ID)
			}
			if strings.TrimSpace(ds.Internal.Namespace) == "" {
				ds.Internal.Namespace = "default"
			}
		case DatasetExternal:
			if ds.External == nil {
				return nil, fmt.Errorf("dataset %s is external but missing external config", ds.ID)
			}
			if strings.TrimSpace(ds.External.Adapter) == "" {
				return nil, fmt.Errorf("dataset %s external.adapter is required", ds.ID)
			}
		default:
			return nil, fmt.Errorf("dataset %s has invalid type: %q", ds.ID, ds.Type)
		}

		r.byID[ds.ID] = ds
	}
	return r, nil
}

func (r *Registry) Get(id string) (DatasetConfig, bool) {
	ds, ok := r.byID[id]
	return ds, ok
}

func (r *Registry) List(ids []string) ([]DatasetConfig, error) {
	out := make([]DatasetConfig, 0, len(ids))
	for _, id := range ids {
		ds, ok := r.byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown dataset: %s", id)
		}
		out = append(out, ds)
	}
	return out, nil
}

func (r *Registry) All() []DatasetConfig {
	out := make([]DatasetConfig, 0, len(r.byID))
	for _, ds := range r.byID {
		out = append(out, ds)
	}
	return out
}


