package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("memory store dsn is required")
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
	s := &PostgresStore{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS memory_records (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'default',
			record_type TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			evidence_refs JSONB,
			confidence DOUBLE PRECISION NOT NULL DEFAULT 0.0,
			sensitivity TEXT NOT NULL DEFAULT 'internal',
			expires_at TIMESTAMPTZ,
			version INT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS memory_records_tenant_type_idx ON memory_records(tenant_id, record_type)`,
		`CREATE INDEX IF NOT EXISTS memory_records_expires_idx ON memory_records(expires_at)`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("memory migrate failed: %w", err)
		}
	}
	return nil
}

type UpsertInput struct {
	ID           string
	TenantID     string
	RecordType   string
	Payload      map[string]any
	EvidenceRefs []string
	Confidence   float64
	Sensitivity  Sensitivity
	ExpiresAt    *time.Time
}

func (s *PostgresStore) Upsert(ctx context.Context, in UpsertInput) (Record, error) {
	if s == nil || s.db == nil {
		return Record{}, fmt.Errorf("memory store not configured")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "mem_" + randHex(12)
	}
	tenant := strings.TrimSpace(in.TenantID)
	if tenant == "" {
		tenant = "default"
	}
	rtype := strings.TrimSpace(in.RecordType)
	if rtype == "" {
		return Record{}, fmt.Errorf("record_type is required")
	}
	if in.Payload == nil {
		in.Payload = map[string]any{}
	}
	payloadJSON, _ := json.Marshal(in.Payload)
	refsJSON, _ := json.Marshal(in.EvidenceRefs)
	sens := strings.TrimSpace(string(in.Sensitivity))
	if sens == "" {
		sens = string(SensitivityInternal)
	}

	// Upsert with version increment on updates.
	q := `
		INSERT INTO memory_records (id, tenant_id, record_type, payload, evidence_refs, confidence, sensitivity, expires_at, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,1,NOW(),NOW())
		ON CONFLICT (id) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			record_type = EXCLUDED.record_type,
			payload = EXCLUDED.payload,
			evidence_refs = EXCLUDED.evidence_refs,
			confidence = EXCLUDED.confidence,
			sensitivity = EXCLUDED.sensitivity,
			expires_at = EXCLUDED.expires_at,
			version = memory_records.version + 1,
			updated_at = NOW()
		RETURNING id, tenant_id, record_type, payload, evidence_refs, confidence, sensitivity, expires_at, version, created_at, updated_at`

	var (
		out             Record
		payloadOutJSON  []byte
		evidenceOutJSON []byte
		sensOut         string
	)
	err := s.db.QueryRowContext(ctx, q, id, tenant, rtype, payloadJSON, refsJSON, in.Confidence, sens, in.ExpiresAt).Scan(
		&out.ID, &out.TenantID, &out.RecordType, &payloadOutJSON, &evidenceOutJSON, &out.Confidence, &sensOut, &out.ExpiresAt, &out.Version, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return Record{}, fmt.Errorf("memory upsert: %w", err)
	}
	out.Sensitivity = Sensitivity(sensOut)
	if len(payloadOutJSON) > 0 {
		_ = json.Unmarshal(payloadOutJSON, &out.Payload)
	}
	if len(evidenceOutJSON) > 0 {
		_ = json.Unmarshal(evidenceOutJSON, &out.EvidenceRef)
	}
	return out, nil
}

func (s *PostgresStore) Get(ctx context.Context, tenantID string, id string) (Record, bool, error) {
	if s == nil || s.db == nil {
		return Record{}, false, fmt.Errorf("memory store not configured")
	}
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		tenant = "default"
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Record{}, false, fmt.Errorf("record_id is required")
	}

	q := `SELECT id, tenant_id, record_type, payload, evidence_refs, confidence, sensitivity, expires_at, version, created_at, updated_at
		  FROM memory_records WHERE tenant_id=$1 AND id=$2`
	var (
		out             Record
		payloadJSON     []byte
		evidenceOutJSON []byte
		sensOut         string
	)
	err := s.db.QueryRowContext(ctx, q, tenant, id).Scan(
		&out.ID, &out.TenantID, &out.RecordType, &payloadJSON, &evidenceOutJSON, &out.Confidence, &sensOut, &out.ExpiresAt, &out.Version, &out.CreatedAt, &out.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("memory get: %w", err)
	}
	out.Sensitivity = Sensitivity(sensOut)
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &out.Payload)
	}
	if len(evidenceOutJSON) > 0 {
		_ = json.Unmarshal(evidenceOutJSON, &out.EvidenceRef)
	}
	return out, true, nil
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

