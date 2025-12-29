package registry

import (
	"context"
	"time"
)

// AgentRecord is the persisted agent registry entry.
// This is what the control plane/router uses instead of hardcoding URLs.
type AgentRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`     // "rule" | "decision" | ...
	BaseURL string `json:"base_url"` // base URL for /task, /.well-known/agent.json, /health

	Name    string `json:"name"`
	Version string `json:"version"`

	HealthURL string `json:"health_url,omitempty"` // optional override

	Capabilities AgentCapabilities `json:"capabilities"`

	Status       string     `json:"status"` // "unknown" | "healthy" | "unhealthy"
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	LastHealthyAt *time.Time `json:"last_healthy_at,omitempty"`
}

type AgentCapabilities struct {
	TaskTypes []string          `json:"task_types,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type AgentFilter struct {
	Type   string
	Status string
	Limit  int
}

type AgentStore interface {
	UpsertAgent(ctx context.Context, a AgentRecord) error
	GetAgent(ctx context.Context, id string) (*AgentRecord, error)
	ListAgents(ctx context.Context, f AgentFilter) ([]AgentRecord, error)
	TouchAgent(ctx context.Context, id string, status string) error
	Close() error
}


