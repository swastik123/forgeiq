package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("eval dsn is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// NewPostgresStoreWithDB is a test/helper constructor that uses an existing *sql.DB.
func NewPostgresStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	s := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS eval_runs (
			task_id TEXT PRIMARY KEY,
			workflow_id TEXT,
			run_id TEXT,
			workflow_type TEXT,
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ,
			status TEXT NOT NULL DEFAULT 'running',
			rule_agent_id TEXT,
			rule_agent_version TEXT,
			decision_agent_id TEXT,
			decision_agent_version TEXT,
			plan_steps INT,
			plan_confidence DOUBLE PRECISION,
			tool_calls INT NOT NULL DEFAULT 0,
			approvals INT NOT NULL DEFAULT 0,
			last_error TEXT,
			meta JSONB
		)`,
		`CREATE INDEX IF NOT EXISTS idx_eval_runs_started_at ON eval_runs(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_eval_runs_decision_agent ON eval_runs(decision_agent_id, decision_agent_version)`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("eval migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertStart(ctx context.Context, taskID, workflowID, runID, workflowType string) error {
	q := `INSERT INTO eval_runs(task_id, workflow_id, run_id, workflow_type, started_at, status)
	      VALUES ($1,$2,$3,$4,NOW(),'running')
	      ON CONFLICT (task_id) DO UPDATE SET workflow_id=EXCLUDED.workflow_id, run_id=EXCLUDED.run_id, workflow_type=EXCLUDED.workflow_type`
	_, err := s.db.ExecContext(ctx, q, taskID, workflowID, runID, workflowType)
	return err
}

func (s *Store) RecordAgent(ctx context.Context, taskID, agentType, agentID, agentVersion string) error {
	colID := "rule_agent_id"
	colVer := "rule_agent_version"
	if agentType == "decision" {
		colID = "decision_agent_id"
		colVer = "decision_agent_version"
	}
	q := fmt.Sprintf(`UPDATE eval_runs SET %s=$1, %s=$2 WHERE task_id=$3`, colID, colVer)
	_, err := s.db.ExecContext(ctx, q, agentID, agentVersion, taskID)
	return err
}

func (s *Store) RecordPlan(ctx context.Context, taskID string, steps int, confidence float64, meta map[string]any) error {
	metaJSON, _ := json.Marshal(meta)
	q := `UPDATE eval_runs SET plan_steps=$1, plan_confidence=$2, meta=COALESCE(meta,'{}'::jsonb) || $3::jsonb WHERE task_id=$4`
	_, err := s.db.ExecContext(ctx, q, steps, confidence, metaJSON, taskID)
	return err
}

func (s *Store) IncToolCalls(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE eval_runs SET tool_calls=tool_calls+1 WHERE task_id=$1`, taskID)
	return err
}

func (s *Store) IncApprovals(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE eval_runs SET approvals=approvals+1 WHERE task_id=$1`, taskID)
	return err
}

func (s *Store) Complete(ctx context.Context, taskID, status, lastErr string) error {
	q := `UPDATE eval_runs SET status=$1, last_error=$2, completed_at=NOW() WHERE task_id=$3`
	_, err := s.db.ExecContext(ctx, q, status, nullIfEmpty(lastErr), taskID)
	return err
}

type CompareResult struct {
	DecisionAgentID string `json:"decision_agent_id"`
	Version         string `json:"version"`
	Runs            int    `json:"runs"`
	Completed       int    `json:"completed"`
	Denied          int    `json:"denied"`
	Failed          int    `json:"failed"`
	AvgToolCalls    float64 `json:"avg_tool_calls"`
	AvgApprovals    float64 `json:"avg_approvals"`
	AvgDurationMs   float64 `json:"avg_duration_ms"`
}

// CompareDecisionAgentVersions aggregates metrics for decision agent versions.
func (s *Store) CompareDecisionAgentVersions(ctx context.Context, decisionAgentID string, versions []string) ([]CompareResult, error) {
	if s == nil {
		return nil, fmt.Errorf("eval store is nil")
	}
	if strings.TrimSpace(decisionAgentID) == "" || len(versions) == 0 {
		return nil, fmt.Errorf("decisionAgentID and versions are required")
	}

	q := `SELECT decision_agent_id,
	             decision_agent_version,
	             COUNT(*) as runs,
	             SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END) as completed,
	             SUM(CASE WHEN status='denied' THEN 1 ELSE 0 END) as denied,
	             SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END) as failed,
	             AVG(tool_calls) as avg_tool_calls,
	             AVG(approvals) as avg_approvals,
	             AVG(EXTRACT(EPOCH FROM (completed_at - started_at))*1000) as avg_duration_ms
	      FROM eval_runs
	      WHERE decision_agent_id = $1 AND decision_agent_version = ANY($2)
	      GROUP BY decision_agent_id, decision_agent_version
	      ORDER BY decision_agent_version`

	rows, err := s.db.QueryContext(ctx, q, decisionAgentID, pqStringArray(versions))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CompareResult
	for rows.Next() {
		var r CompareResult
		var avgDur sql.NullFloat64
		if err := rows.Scan(&r.DecisionAgentID, &r.Version, &r.Runs, &r.Completed, &r.Denied, &r.Failed, &r.AvgToolCalls, &r.AvgApprovals, &avgDur); err != nil {
			return nil, err
		}
		if avgDur.Valid {
			r.AvgDurationMs = avgDur.Float64
		}
		out = append(out, r)
	}
	return out, nil
}

func pqStringArray(xs []string) any { return pq.Array(xs) }

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}


