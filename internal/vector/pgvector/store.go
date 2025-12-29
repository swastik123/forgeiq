package pgvector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Store is a minimal pgvector-backed vector store using database/sql (no extra deps).
type Store struct {
	db    *sql.DB
	table string
	dim   int
}

type Record struct {
	ID        string         `json:"id"`
	Namespace string         `json:"namespace"`
	Content   string         `json:"content,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Distance  float64        `json:"distance,omitempty"`
	Score     float64        `json:"score,omitempty"` // derived similarity (1 - cosine distance)
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func New(dsn, table string, dim int) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("vector dsn is required")
	}
	if table == "" {
		table = "mcp_vectors"
	}
	if !identRe.MatchString(table) {
		return nil, fmt.Errorf("invalid vector table name: %q", table)
	}
	if dim <= 0 {
		return nil, fmt.Errorf("invalid vector dim: %d", dim)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetConnMaxLifetime(10 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	s := &Store{db: db, table: table, dim: dim}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// NewWithDB is a test/helper constructor that uses an existing *sql.DB (no ping).
func NewWithDB(db *sql.DB, table string, dim int) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if table == "" {
		table = "mcp_vectors"
	}
	if !identRe.MatchString(table) {
		return nil, fmt.Errorf("invalid vector table name: %q", table)
	}
	if dim <= 0 {
		return nil, fmt.Errorf("invalid vector dim: %d", dim)
	}
	s := &Store{db: db, table: table, dim: dim}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	// pgvector extension + a single table for embeddings
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			namespace TEXT NOT NULL DEFAULT 'default',
			content TEXT,
			metadata JSONB,
			embedding VECTOR(%d) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.table, s.dim),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_namespace_idx ON %s(namespace)`, s.table, s.table),
		// IVFFLAT index is optional; it requires ANALYZE and training lists for best perf. Keep it simple by not creating by default.
	}

	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("vector migrate failed: %w", err)
		}
	}
	return nil
}

func (s *Store) Upsert(ctx context.Context, id, namespace, content string, metadata map[string]any, embedding []float64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if namespace == "" {
		namespace = "default"
	}
	if len(embedding) != s.dim {
		return fmt.Errorf("embedding dim mismatch: got %d want %d", len(embedding), s.dim)
	}
	embStr := formatVectorLiteral(embedding)

	var metaJSON []byte
	if metadata != nil {
		metaJSON, _ = json.Marshal(metadata)
	}

	q := fmt.Sprintf(
		`INSERT INTO %s (id, namespace, content, metadata, embedding, updated_at)
		 VALUES ($1, $2, $3, $4, $5::vector, NOW())
		 ON CONFLICT (id) DO UPDATE SET
			namespace = EXCLUDED.namespace,
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding,
			updated_at = NOW()`, s.table,
	)

	_, err := s.db.ExecContext(ctx, q, id, namespace, content, metaJSON, embStr)
	if err != nil {
		return fmt.Errorf("vector upsert: %w", err)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, namespace string, queryEmbedding []float64, k int) ([]Record, error) {
	if k <= 0 || k > 200 {
		k = 10
	}
	if len(queryEmbedding) != s.dim {
		return nil, fmt.Errorf("query_embedding dim mismatch: got %d want %d", len(queryEmbedding), s.dim)
	}
	if namespace == "" {
		namespace = "default"
	}
	embStr := formatVectorLiteral(queryEmbedding)

	// Use cosine distance operator <=> (lower is better)
	q := fmt.Sprintf(
		`SELECT id, namespace, content, metadata, (embedding <=> $1::vector) AS distance
		 FROM %s
		 WHERE namespace = $2
		 ORDER BY embedding <=> $1::vector
		 LIMIT $3`, s.table,
	)

	rows, err := s.db.QueryContext(ctx, q, embStr, namespace, k)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		var metaJSON []byte
		if err := rows.Scan(&r.ID, &r.Namespace, &r.Content, &metaJSON, &r.Distance); err != nil {
			return nil, fmt.Errorf("vector scan: %w", err)
		}
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &r.Metadata)
		}
		r.Score = 1.0 - r.Distance
		out = append(out, r)
	}
	return out, nil
}

func formatVectorLiteral(v []float64) string {
	// pgvector text format: [1,2,3]
	// Keep it compact; postgres will parse floats.
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(trimFloat(x))
	}
	b.WriteByte(']')
	return b.String()
}

func trimFloat(f float64) string {
	// Use %g for compact output.
	return fmt.Sprintf("%g", f)
}


