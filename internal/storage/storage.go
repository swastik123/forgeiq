package storage

import (
	"context"
	"time"
)

// Storage defines the interface for persistent storage
type Storage interface {
	// Workflow operations
	SaveWorkflow(ctx context.Context, workflow *WorkflowRecord) error
	GetWorkflow(ctx context.Context, workflowID string) (*WorkflowRecord, error)
	ListWorkflows(ctx context.Context, filter *WorkflowFilter) ([]*WorkflowRecord, error)
	UpdateWorkflowStatus(ctx context.Context, workflowID string, status string) error

	// Artifact operations
	SaveArtifact(ctx context.Context, artifact *ArtifactRecord) error
	GetArtifact(ctx context.Context, artifactID string) (*ArtifactRecord, error)
	ListArtifacts(ctx context.Context, workflowID string) ([]*ArtifactRecord, error)

	// Audit operations
	SaveAuditLog(ctx context.Context, log *AuditLog) error
	QueryAuditLogs(ctx context.Context, filter *AuditFilter) ([]*AuditLog, error)

	// Health check
	Ping(ctx context.Context) error
	Close() error
}

// WorkflowRecord represents a stored workflow
type WorkflowRecord struct {
	ID          string                 `json:"id"`
	TaskID      string                 `json:"task_id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Input       map[string]any          `json:"input"`
	Output      map[string]any          `json:"output,omitempty"`
	Metadata    map[string]string       `json:"metadata"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
	Error       string                  `json:"error,omitempty"`
}

// ArtifactRecord represents a stored artifact
type ArtifactRecord struct {
	ID         string                 `json:"id"`
	WorkflowID string                 `json:"workflow_id"`
	TaskID     string                 `json:"task_id"`
	Type       string                 `json:"type"`
	Payload    map[string]any          `json:"payload"`
	CreatedAt  time.Time              `json:"created_at"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID          string                 `json:"id"`
	WorkflowID  string                 `json:"workflow_id,omitempty"`
	Action      string                 `json:"action"` // "workflow_started", "workflow_completed", "tool_called", etc.
	Actor       string                 `json:"actor,omitempty"`
	Resource    string                 `json:"resource"`
	Details     map[string]any          `json:"details,omitempty"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// WorkflowFilter for querying workflows
type WorkflowFilter struct {
	TaskID   string
	Type     string
	Status   string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

// AuditFilter for querying audit logs
type AuditFilter struct {
	WorkflowID string
	Action     string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

