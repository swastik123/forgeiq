package storage_test

import (
	"context"
	"forgeiq/internal/storage"
	"testing"
	"time"
)

func TestMemoryStorage_WorkflowCRUD(t *testing.T) {
	s := storage.NewMemoryStorage()
	ctx := context.Background()

	w := &storage.WorkflowRecord{
		ID:        "w1",
		TaskID:    "t1",
		Type:      "incident_triage",
		Status:    "running",
		Input:     map[string]any{"x": 1},
		Metadata:  map[string]string{"m": "v"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.SaveWorkflow(ctx, w); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetWorkflow(ctx, "w1")
	if err != nil || got.ID != "w1" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if err := s.UpdateWorkflowStatus(ctx, "w1", "completed"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetWorkflow(ctx, "w1")
	if got.Status != "completed" {
		t.Fatalf("expected completed, got %s", got.Status)
	}
}

func TestMemoryStorage_ArtifactsAndAudit(t *testing.T) {
	s := storage.NewMemoryStorage()
	ctx := context.Background()

	a := &storage.ArtifactRecord{ID: "a1", WorkflowID: "w1", TaskID: "t1", Type: "Evidence", Payload: map[string]any{"x": 1}, CreatedAt: time.Now()}
	if err := s.SaveArtifact(ctx, a); err != nil {
		t.Fatalf("save artifact: %v", err)
	}
	if _, err := s.GetArtifact(ctx, "a1"); err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	l := &storage.AuditLog{ID: "l1", WorkflowID: "w1", Action: "tool_called", Resource: "logs.search", CreatedAt: time.Now()}
	if err := s.SaveAuditLog(ctx, l); err != nil {
		t.Fatalf("save audit: %v", err)
	}
	logs, err := s.QueryAuditLogs(ctx, &storage.AuditFilter{WorkflowID: "w1"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("query audit: %v len=%d", err, len(logs))
	}
}
