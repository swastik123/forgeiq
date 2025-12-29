package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type PostgresAgentStore struct {
	db *sql.DB
}

func NewPostgresAgentStore(dsn string) (*PostgresAgentStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("registry dsn is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &PostgresAgentStore{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// NewPostgresAgentStoreWithDB is a test/helper constructor that uses an existing *sql.DB.
func NewPostgresAgentStoreWithDB(db *sql.DB) (*PostgresAgentStore, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	s := &PostgresAgentStore{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PostgresAgentStore) Close() error { return s.db.Close() }

func (s *PostgresAgentStore) migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS agent_registry (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			base_url TEXT NOT NULL,
			name TEXT NOT NULL,
			version TEXT NOT NULL,
			health_url TEXT,
			capabilities JSONB,
			status TEXT NOT NULL DEFAULT 'unknown',
			last_seen_at TIMESTAMPTZ,
			last_healthy_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_registry_type ON agent_registry(type)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_registry_status ON agent_registry(status)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_registry_updated_at ON agent_registry(updated_at)`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("registry migrate: %w", err)
		}
	}
	return nil
}

func (s *PostgresAgentStore) UpsertAgent(ctx context.Context, a AgentRecord) error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Type) == "" || strings.TrimSpace(a.BaseURL) == "" {
		return fmt.Errorf("agent requires id,type,base_url")
	}
	if strings.TrimSpace(a.Name) == "" {
		a.Name = a.ID
	}
	if strings.TrimSpace(a.Version) == "" {
		a.Version = "unknown"
	}
	capsJSON, _ := json.Marshal(a.Capabilities)

	q := `INSERT INTO agent_registry (id, type, base_url, name, version, health_url, capabilities, status, last_seen_at, last_healthy_at, updated_at)
	      VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8,'unknown'),$9,$10,NOW())
	      ON CONFLICT (id) DO UPDATE SET
	        type=EXCLUDED.type,
	        base_url=EXCLUDED.base_url,
	        name=EXCLUDED.name,
	        version=EXCLUDED.version,
	        health_url=EXCLUDED.health_url,
	        capabilities=EXCLUDED.capabilities,
	        status=EXCLUDED.status,
	        last_seen_at=EXCLUDED.last_seen_at,
	        last_healthy_at=EXCLUDED.last_healthy_at,
	        updated_at=NOW()`
	_, err := s.db.ExecContext(ctx, q, a.ID, a.Type, a.BaseURL, a.Name, a.Version, nullIfEmpty(a.HealthURL), capsJSON, a.Status, a.LastSeenAt, a.LastHealthyAt)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}
	return nil
}

func (s *PostgresAgentStore) GetAgent(ctx context.Context, id string) (*AgentRecord, error) {
	var a AgentRecord
	var capsJSON []byte
	var healthURL sql.NullString
	var lastSeen, lastHealthy sql.NullTime
	q := `SELECT id,type,base_url,name,version,health_url,capabilities,status,last_seen_at,last_healthy_at
	      FROM agent_registry WHERE id=$1`
	err := s.db.QueryRowContext(ctx, q, id).Scan(&a.ID, &a.Type, &a.BaseURL, &a.Name, &a.Version, &healthURL, &capsJSON, &a.Status, &lastSeen, &lastHealthy)
	if err != nil {
		return nil, err
	}
	if healthURL.Valid {
		a.HealthURL = healthURL.String
	}
	if len(capsJSON) > 0 {
		_ = json.Unmarshal(capsJSON, &a.Capabilities)
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		a.LastSeenAt = &t
	}
	if lastHealthy.Valid {
		t := lastHealthy.Time
		a.LastHealthyAt = &t
	}
	return &a, nil
}

func (s *PostgresAgentStore) ListAgents(ctx context.Context, f AgentFilter) ([]AgentRecord, error) {
	q := `SELECT id,type,base_url,name,version,health_url,capabilities,status,last_seen_at,last_healthy_at
	      FROM agent_registry WHERE 1=1`
	args := []any{}
	i := 1
	if strings.TrimSpace(f.Type) != "" {
		q += fmt.Sprintf(" AND type=$%d", i)
		args = append(args, f.Type)
		i++
	}
	if strings.TrimSpace(f.Status) != "" {
		q += fmt.Sprintf(" AND status=$%d", i)
		args = append(args, f.Status)
		i++
	}
	q += " ORDER BY updated_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", i)
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentRecord
	for rows.Next() {
		var a AgentRecord
		var capsJSON []byte
		var healthURL sql.NullString
		var lastSeen, lastHealthy sql.NullTime
		if err := rows.Scan(&a.ID, &a.Type, &a.BaseURL, &a.Name, &a.Version, &healthURL, &capsJSON, &a.Status, &lastSeen, &lastHealthy); err != nil {
			return nil, err
		}
		if healthURL.Valid {
			a.HealthURL = healthURL.String
		}
		if len(capsJSON) > 0 {
			_ = json.Unmarshal(capsJSON, &a.Capabilities)
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			a.LastSeenAt = &t
		}
		if lastHealthy.Valid {
			t := lastHealthy.Time
			a.LastHealthyAt = &t
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *PostgresAgentStore) TouchAgent(ctx context.Context, id string, status string) error {
	now := time.Now().UTC()
	lastHealthy := (*time.Time)(nil)
	if status == "healthy" {
		lastHealthy = &now
	}
	q := `UPDATE agent_registry SET status=$1, last_seen_at=$2, last_healthy_at=COALESCE($3,last_healthy_at), updated_at=NOW() WHERE id=$4`
	_, err := s.db.ExecContext(ctx, q, status, now, lastHealthy, id)
	return err
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}


