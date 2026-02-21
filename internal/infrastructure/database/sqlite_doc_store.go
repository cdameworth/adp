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

// SQLiteDocStore implements store.DocStore backed by SQLite.
type SQLiteDocStore struct {
	client *SQLiteClient
}

// NewSQLiteDocStore creates a new SQLite-backed documentation store.
func NewSQLiteDocStore(client *SQLiteClient) *SQLiteDocStore {
	return &SQLiteDocStore{client: client}
}

// Save inserts or replaces a documentation record.
// If doc.ID is empty, a new UUID is generated.
func (s *SQLiteDocStore) Save(ctx context.Context, doc store.DocRecord) error {
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

	now := time.Now().UTC().Format(time.RFC3339Nano)

	query := `INSERT OR REPLACE INTO documentation (
		id, session_id, category, title, content, metadata, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, COALESCE((SELECT created_at FROM documentation WHERE id = ?), ?), ?)`

	_, err = s.client.DB().ExecContext(ctx, query,
		doc.ID,
		doc.SessionID,
		doc.Category,
		doc.Title,
		doc.Content,
		string(metadataJSON),
		doc.ID, // for COALESCE subquery
		now,    // fallback created_at for new records
		now,    // updated_at always set to now
	)
	if err != nil {
		return fmt.Errorf("failed to save doc: %w", err)
	}

	return nil
}

// Get retrieves a documentation record by ID.
func (s *SQLiteDocStore) Get(ctx context.Context, id string) (*store.DocRecord, error) {
	query := `SELECT id, session_id, category, title, content, metadata, created_at, updated_at
	FROM documentation WHERE id = ?`

	return scanDocRecord(s.client.DB().QueryRowContext(ctx, query, id))
}

// ListByCategory returns documentation records filtered by category, newest first.
func (s *SQLiteDocStore) ListByCategory(ctx context.Context, category string, limit int) ([]*store.DocRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, session_id, category, title, content, metadata, created_at, updated_at
	FROM documentation WHERE category = ? ORDER BY updated_at DESC LIMIT ?`

	rows, err := s.client.DB().QueryContext(ctx, query, category, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list docs by category: %w", err)
	}
	defer rows.Close()

	return scanDocRecords(rows)
}

// ListBySession returns documentation records for a specific session.
func (s *SQLiteDocStore) ListBySession(ctx context.Context, sessionID string) ([]*store.DocRecord, error) {
	query := `SELECT id, session_id, category, title, content, metadata, created_at, updated_at
	FROM documentation WHERE session_id = ? ORDER BY created_at ASC`

	rows, err := s.client.DB().QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list docs by session: %w", err)
	}
	defer rows.Close()

	return scanDocRecords(rows)
}

// Search performs a basic text search across title and content.
func (s *SQLiteDocStore) Search(ctx context.Context, query string, limit int) ([]*store.DocRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	pattern := "%" + query + "%"

	sqlQuery := `SELECT id, session_id, category, title, content, metadata, created_at, updated_at
	FROM documentation WHERE title LIKE ? OR content LIKE ? ORDER BY updated_at DESC LIMIT ?`

	rows, err := s.client.DB().QueryContext(ctx, sqlQuery, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search docs: %w", err)
	}
	defer rows.Close()

	return scanDocRecords(rows)
}

func scanDocRecord(row *sql.Row) (*store.DocRecord, error) {
	var doc store.DocRecord
	var sessionID sql.NullString
	var metadataJSON string
	var createdAtStr, updatedAtStr string

	err := row.Scan(
		&doc.ID, &sessionID, &doc.Category, &doc.Title,
		&doc.Content, &metadataJSON, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan doc record: %w", err)
	}

	if sessionID.Valid {
		doc.SessionID = sessionID.String
	}
	if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
		doc.Metadata = map[string]any{}
	}
	doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	doc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)

	return &doc, nil
}

func scanDocRecords(rows *sql.Rows) ([]*store.DocRecord, error) {
	var docs []*store.DocRecord
	for rows.Next() {
		var doc store.DocRecord
		var sessionID sql.NullString
		var metadataJSON string
		var createdAtStr, updatedAtStr string

		err := rows.Scan(
			&doc.ID, &sessionID, &doc.Category, &doc.Title,
			&doc.Content, &metadataJSON, &createdAtStr, &updatedAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan doc record: %w", err)
		}

		if sessionID.Valid {
			doc.SessionID = sessionID.String
		}
		if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
			doc.Metadata = map[string]any{}
		}
		doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		doc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)

		docs = append(docs, &doc)
	}
	return docs, rows.Err()
}
