package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// PgDocStore implements store.DocStore backed by PostgreSQL (#11), mirroring
// SQLiteDocStore semantics so /v1/docs works identically in PG mode.
type PgDocStore struct {
	client *PostgresClient
}

// NewPgDocStore creates a PostgreSQL-backed documentation store.
func NewPgDocStore(client *PostgresClient) *PgDocStore {
	return &PgDocStore{client: client}
}

// Save inserts or replaces a documentation record, preserving created_at on
// update. If doc.ID is empty, a new UUID is generated.
func (s *PgDocStore) Save(ctx context.Context, doc store.DocRecord) error {
	if doc.Category == "" {
		return fmt.Errorf("%w: category is required", ErrInvalidInput)
	}
	if doc.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	if doc.Content == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidInput)
	}

	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}
	if doc.Metadata == nil {
		doc.Metadata = map[string]any{}
	}

	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	now := time.Now().UTC()

	_, err = s.client.db.ExecContext(ctx,
		`INSERT INTO documentation (id, session_id, category, title, content, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		 ON CONFLICT (id) DO UPDATE SET
		   session_id = EXCLUDED.session_id,
		   category = EXCLUDED.category,
		   title = EXCLUDED.title,
		   content = EXCLUDED.content,
		   metadata = EXCLUDED.metadata,
		   updated_at = EXCLUDED.updated_at`,
		doc.ID, nullString(doc.SessionID), doc.Category, doc.Title, doc.Content,
		string(metadataJSON), now,
	)
	if err != nil {
		return fmt.Errorf("failed to save doc: %w", err)
	}
	return nil
}

// Get retrieves a documentation record by ID.
func (s *PgDocStore) Get(ctx context.Context, id string) (*store.DocRecord, error) {
	row := s.client.db.QueryRowContext(ctx,
		`SELECT id, session_id, category, title, content, metadata, created_at, updated_at
		 FROM documentation WHERE id = $1`, id)
	return scanDocRecordPG(row)
}

// ListByCategory returns documentation records filtered by category, newest first.
func (s *PgDocStore) ListByCategory(ctx context.Context, category string, limit int) ([]*store.DocRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, session_id, category, title, content, metadata, created_at, updated_at
		 FROM documentation WHERE category = $1 ORDER BY updated_at DESC LIMIT $2`,
		category, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list docs by category: %w", err)
	}
	defer rows.Close()
	return scanDocRecordsPG(rows)
}

// ListBySession returns documentation records for a specific session.
func (s *PgDocStore) ListBySession(ctx context.Context, sessionID string) ([]*store.DocRecord, error) {
	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, session_id, category, title, content, metadata, created_at, updated_at
		 FROM documentation WHERE session_id = $1 ORDER BY created_at ASC`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list docs by session: %w", err)
	}
	defer rows.Close()
	return scanDocRecordsPG(rows)
}

// Search performs a basic text search across title and content.
func (s *PgDocStore) Search(ctx context.Context, query string, limit int) ([]*store.DocRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + query + "%"
	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, session_id, category, title, content, metadata, created_at, updated_at
		 FROM documentation WHERE title LIKE $1 OR content LIKE $1 ORDER BY updated_at DESC LIMIT $2`,
		pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search docs: %w", err)
	}
	defer rows.Close()
	return scanDocRecordsPG(rows)
}

func scanDocRecordPG(row *sql.Row) (*store.DocRecord, error) {
	var doc store.DocRecord
	var sessionID sql.NullString
	var metadataJSON []byte

	err := row.Scan(&doc.ID, &sessionID, &doc.Category, &doc.Title,
		&doc.Content, &metadataJSON, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan doc record: %w", err)
	}

	if sessionID.Valid {
		doc.SessionID = sessionID.String
	}
	if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
		doc.Metadata = map[string]any{}
	}
	return &doc, nil
}

func scanDocRecordsPG(rows *sql.Rows) ([]*store.DocRecord, error) {
	var docs []*store.DocRecord
	for rows.Next() {
		var doc store.DocRecord
		var sessionID sql.NullString
		var metadataJSON []byte

		err := rows.Scan(&doc.ID, &sessionID, &doc.Category, &doc.Title,
			&doc.Content, &metadataJSON, &doc.CreatedAt, &doc.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan doc record: %w", err)
		}

		if sessionID.Valid {
			doc.SessionID = sessionID.String
		}
		if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
			doc.Metadata = map[string]any{}
		}
		docs = append(docs, &doc)
	}
	return docs, rows.Err()
}
