package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresStorage implements Storage using PostgreSQL
type PostgresStorage struct {
	db *sql.DB
}

// NewPostgresStorage creates a new PostgreSQL storage instance
func NewPostgresStorage(dsn string) (*PostgresStorage, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &PostgresStorage{db: db}
	if err := storage.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return storage, nil
}

// NewPostgresStorageWithDB is a test/helper constructor that uses an existing *sql.DB.
func NewPostgresStorageWithDB(db *sql.DB) (*PostgresStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	storage := &PostgresStorage{db: db}
	if err := storage.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return storage, nil
}

// migrate creates necessary tables
func (p *PostgresStorage) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS workflows (
			id VARCHAR(255) PRIMARY KEY,
			task_id VARCHAR(255) NOT NULL,
			type VARCHAR(100) NOT NULL,
			status VARCHAR(50) NOT NULL,
			input JSONB NOT NULL,
			output JSONB,
			metadata JSONB,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMP,
			error TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workflows_task_id ON workflows(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status)`,
		`CREATE INDEX IF NOT EXISTS idx_workflows_created_at ON workflows(created_at)`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id VARCHAR(255) PRIMARY KEY,
			workflow_id VARCHAR(255) NOT NULL,
			task_id VARCHAR(255) NOT NULL,
			type VARCHAR(100) NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_id ON artifacts(workflow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_task_id ON artifacts(task_id)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(255) PRIMARY KEY,
			workflow_id VARCHAR(255),
			action VARCHAR(100) NOT NULL,
			actor VARCHAR(255),
			resource VARCHAR(255) NOT NULL,
			details JSONB,
			ip_address VARCHAR(50),
			user_agent TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_workflow_id ON audit_logs(workflow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_logs(created_at)`,
	}

	for _, query := range queries {
		if _, err := p.db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute migration: %w", err)
		}
	}

	return nil
}

func (p *PostgresStorage) SaveWorkflow(ctx context.Context, workflow *WorkflowRecord) error {
	inputJSON, _ := json.Marshal(workflow.Input)
	outputJSON, _ := json.Marshal(workflow.Output)
	metadataJSON, _ := json.Marshal(workflow.Metadata)

	query := `INSERT INTO workflows (id, task_id, type, status, input, output, metadata, created_at, updated_at, completed_at, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			output = EXCLUDED.output,
			updated_at = EXCLUDED.updated_at,
			completed_at = EXCLUDED.completed_at,
			error = EXCLUDED.error`

	_, err := p.db.ExecContext(ctx, query,
		workflow.ID, workflow.TaskID, workflow.Type, workflow.Status,
		inputJSON, outputJSON, metadataJSON,
		workflow.CreatedAt, workflow.UpdatedAt, workflow.CompletedAt, workflow.Error)
	return err
}

func (p *PostgresStorage) GetWorkflow(ctx context.Context, workflowID string) (*WorkflowRecord, error) {
	var w WorkflowRecord
	var inputJSON, outputJSON, metadataJSON []byte
	var completedAt sql.NullTime

	query := `SELECT id, task_id, type, status, input, output, metadata, created_at, updated_at, completed_at, error
		FROM workflows WHERE id = $1`
	
	err := p.db.QueryRowContext(ctx, query, workflowID).Scan(
		&w.ID, &w.TaskID, &w.Type, &w.Status,
		&inputJSON, &outputJSON, &metadataJSON,
		&w.CreatedAt, &w.UpdatedAt, &completedAt, &w.Error)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(inputJSON, &w.Input)
	json.Unmarshal(outputJSON, &w.Output)
	json.Unmarshal(metadataJSON, &w.Metadata)
	if completedAt.Valid {
		w.CompletedAt = &completedAt.Time
	}

	return &w, nil
}

func (p *PostgresStorage) ListWorkflows(ctx context.Context, filter *WorkflowFilter) ([]*WorkflowRecord, error) {
	query := `SELECT id, task_id, type, status, input, output, metadata, created_at, updated_at, completed_at, error
		FROM workflows WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	if filter != nil {
		if filter.TaskID != "" {
			query += fmt.Sprintf(" AND task_id = $%d", argPos)
			args = append(args, filter.TaskID)
			argPos++
		}
		if filter.Type != "" {
			query += fmt.Sprintf(" AND type = $%d", argPos)
			args = append(args, filter.Type)
			argPos++
		}
		if filter.Status != "" {
			query += fmt.Sprintf(" AND status = $%d", argPos)
			args = append(args, filter.Status)
			argPos++
		}
		if filter.From != nil {
			query += fmt.Sprintf(" AND created_at >= $%d", argPos)
			args = append(args, *filter.From)
			argPos++
		}
		if filter.To != nil {
			query += fmt.Sprintf(" AND created_at <= $%d", argPos)
			args = append(args, *filter.To)
			argPos++
		}
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", argPos)
			args = append(args, filter.Limit)
			argPos++
		}
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argPos)
			args = append(args, filter.Offset)
			argPos++
		}
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []*WorkflowRecord
	for rows.Next() {
		var w WorkflowRecord
		var inputJSON, outputJSON, metadataJSON []byte
		var completedAt sql.NullTime

		err := rows.Scan(&w.ID, &w.TaskID, &w.Type, &w.Status,
			&inputJSON, &outputJSON, &metadataJSON,
			&w.CreatedAt, &w.UpdatedAt, &completedAt, &w.Error)
		if err != nil {
			return nil, err
		}

		json.Unmarshal(inputJSON, &w.Input)
		json.Unmarshal(outputJSON, &w.Output)
		json.Unmarshal(metadataJSON, &w.Metadata)
		if completedAt.Valid {
			w.CompletedAt = &completedAt.Time
		}

		workflows = append(workflows, &w)
	}

	return workflows, nil
}

func (p *PostgresStorage) UpdateWorkflowStatus(ctx context.Context, workflowID string, status string) error {
	query := `UPDATE workflows SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := p.db.ExecContext(ctx, query, status, workflowID)
	return err
}

func (p *PostgresStorage) SaveArtifact(ctx context.Context, artifact *ArtifactRecord) error {
	payloadJSON, _ := json.Marshal(artifact.Payload)

	query := `INSERT INTO artifacts (id, workflow_id, task_id, type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload`

	_, err := p.db.ExecContext(ctx, query,
		artifact.ID, artifact.WorkflowID, artifact.TaskID, artifact.Type,
		payloadJSON, artifact.CreatedAt)
	return err
}

func (p *PostgresStorage) GetArtifact(ctx context.Context, artifactID string) (*ArtifactRecord, error) {
	var a ArtifactRecord
	var payloadJSON []byte

	query := `SELECT id, workflow_id, task_id, type, payload, created_at
		FROM artifacts WHERE id = $1`
	
	err := p.db.QueryRowContext(ctx, query, artifactID).Scan(
		&a.ID, &a.WorkflowID, &a.TaskID, &a.Type, &payloadJSON, &a.CreatedAt)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(payloadJSON, &a.Payload)
	return &a, nil
}

func (p *PostgresStorage) ListArtifacts(ctx context.Context, workflowID string) ([]*ArtifactRecord, error) {
	query := `SELECT id, workflow_id, task_id, type, payload, created_at
		FROM artifacts WHERE workflow_id = $1 ORDER BY created_at DESC`

	rows, err := p.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []*ArtifactRecord
	for rows.Next() {
		var a ArtifactRecord
		var payloadJSON []byte

		err := rows.Scan(&a.ID, &a.WorkflowID, &a.TaskID, &a.Type, &payloadJSON, &a.CreatedAt)
		if err != nil {
			return nil, err
		}

		json.Unmarshal(payloadJSON, &a.Payload)
		artifacts = append(artifacts, &a)
	}

	return artifacts, nil
}

func (p *PostgresStorage) SaveAuditLog(ctx context.Context, log *AuditLog) error {
	detailsJSON, _ := json.Marshal(log.Details)

	query := `INSERT INTO audit_logs (id, workflow_id, action, actor, resource, details, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := p.db.ExecContext(ctx, query,
		log.ID, log.WorkflowID, log.Action, log.Actor, log.Resource,
		detailsJSON, log.IPAddress, log.UserAgent, log.CreatedAt)
	return err
}

func (p *PostgresStorage) QueryAuditLogs(ctx context.Context, filter *AuditFilter) ([]*AuditLog, error) {
	query := `SELECT id, workflow_id, action, actor, resource, details, ip_address, user_agent, created_at
		FROM audit_logs WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	if filter != nil {
		if filter.WorkflowID != "" {
			query += fmt.Sprintf(" AND workflow_id = $%d", argPos)
			args = append(args, filter.WorkflowID)
			argPos++
		}
		if filter.Action != "" {
			query += fmt.Sprintf(" AND action = $%d", argPos)
			args = append(args, filter.Action)
			argPos++
		}
		if filter.From != nil {
			query += fmt.Sprintf(" AND created_at >= $%d", argPos)
			args = append(args, *filter.From)
			argPos++
		}
		if filter.To != nil {
			query += fmt.Sprintf(" AND created_at <= $%d", argPos)
			args = append(args, *filter.To)
			argPos++
		}
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", argPos)
			args = append(args, filter.Limit)
			argPos++
		}
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argPos)
			args = append(args, filter.Offset)
			argPos++
		}
	}

	query += " ORDER BY created_at DESC"

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		var detailsJSON []byte

		err := rows.Scan(&log.ID, &log.WorkflowID, &log.Action, &log.Actor, &log.Resource,
			&detailsJSON, &log.IPAddress, &log.UserAgent, &log.CreatedAt)
		if err != nil {
			return nil, err
		}

		json.Unmarshal(detailsJSON, &log.Details)
		logs = append(logs, &log)
	}

	return logs, nil
}

func (p *PostgresStorage) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PostgresStorage) Close() error {
	return p.db.Close()
}

