package storage

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStorage implements Storage using in-memory storage (for testing/development)
type MemoryStorage struct {
	mu         sync.RWMutex
	workflows  map[string]*WorkflowRecord
	artifacts  map[string]*ArtifactRecord
	auditLogs  []*AuditLog
}

// NewMemoryStorage creates a new in-memory storage instance
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		workflows: make(map[string]*WorkflowRecord),
		artifacts: make(map[string]*ArtifactRecord),
		auditLogs: make([]*AuditLog, 0),
	}
}

func (m *MemoryStorage) SaveWorkflow(ctx context.Context, workflow *WorkflowRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workflows[workflow.ID] = workflow
	return nil
}

func (m *MemoryStorage) GetWorkflow(ctx context.Context, workflowID string) (*WorkflowRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workflows[workflowID]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}
	return w, nil
}

func (m *MemoryStorage) ListWorkflows(ctx context.Context, filter *WorkflowFilter) ([]*WorkflowRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*WorkflowRecord
	for _, w := range m.workflows {
		if filter != nil {
			if filter.TaskID != "" && w.TaskID != filter.TaskID {
				continue
			}
			if filter.Type != "" && w.Type != filter.Type {
				continue
			}
			if filter.Status != "" && w.Status != filter.Status {
				continue
			}
			if filter.From != nil && w.CreatedAt.Before(*filter.From) {
				continue
			}
			if filter.To != nil && w.CreatedAt.After(*filter.To) {
				continue
			}
		}
		results = append(results, w)
	}

	// Simple limit/offset
	if filter != nil {
		if filter.Offset > 0 && filter.Offset < len(results) {
			results = results[filter.Offset:]
		}
		if filter.Limit > 0 && filter.Limit < len(results) {
			results = results[:filter.Limit]
		}
	}

	return results, nil
}

func (m *MemoryStorage) UpdateWorkflowStatus(ctx context.Context, workflowID string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}
	w.Status = status
	w.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStorage) SaveArtifact(ctx context.Context, artifact *ArtifactRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts[artifact.ID] = artifact
	return nil
}

func (m *MemoryStorage) GetArtifact(ctx context.Context, artifactID string) (*ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.artifacts[artifactID]
	if !ok {
		return nil, fmt.Errorf("artifact not found: %s", artifactID)
	}
	return a, nil
}

func (m *MemoryStorage) ListArtifacts(ctx context.Context, workflowID string) ([]*ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*ArtifactRecord
	for _, a := range m.artifacts {
		if a.WorkflowID == workflowID {
			results = append(results, a)
		}
	}
	return results, nil
}

func (m *MemoryStorage) SaveAuditLog(ctx context.Context, log *AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditLogs = append(m.auditLogs, log)
	return nil
}

func (m *MemoryStorage) QueryAuditLogs(ctx context.Context, filter *AuditFilter) ([]*AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*AuditLog
	for _, log := range m.auditLogs {
		if filter != nil {
			if filter.WorkflowID != "" && log.WorkflowID != filter.WorkflowID {
				continue
			}
			if filter.Action != "" && log.Action != filter.Action {
				continue
			}
			if filter.From != nil && log.CreatedAt.Before(*filter.From) {
				continue
			}
			if filter.To != nil && log.CreatedAt.After(*filter.To) {
				continue
			}
		}
		results = append(results, log)
	}

	if filter != nil {
		if filter.Offset > 0 && filter.Offset < len(results) {
			results = results[filter.Offset:]
		}
		if filter.Limit > 0 && filter.Limit < len(results) {
			results = results[:filter.Limit]
		}
	}

	return results, nil
}

func (m *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryStorage) Close() error {
	return nil
}


