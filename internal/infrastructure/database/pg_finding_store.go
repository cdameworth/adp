package database

import (
	"context"
	"log"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
	"github.com/google/uuid"
)

// PgFindingStore persists reconciliation findings in PostgreSQL (#10).
// See SQLiteFindingStore for the error-handling contract: the interface is
// synchronous and error-free; failures are logged and degrade gracefully.
type PgFindingStore struct {
	client *PostgresClient
}

// NewPgFindingStore creates a PostgreSQL-backed finding store.
func NewPgFindingStore(client *PostgresClient) *PgFindingStore {
	return &PgFindingStore{client: client}
}

// Upsert inserts a finding or, if one exists for (Type, Reference), refreshes
// repo/ref/author/updated_at while preserving ID, status, and detected_at.
func (s *PgFindingStore) Upsert(f enforcement.Finding) enforcement.Finding {
	if f.ID == "" {
		f.ID = newID("find_")
	}
	now := time.Now().UTC()
	if f.DetectedAt.IsZero() {
		f.DetectedAt = now
	}
	f.UpdatedAt = now
	if f.Status == "" {
		f.Status = enforcement.StatusOpen
	}

	row := s.client.db.QueryRowContext(context.Background(),
		`INSERT INTO findings (id, type, reference, repo, ref, author, reason, status, detected_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT(type, reference) DO UPDATE SET
		   repo = EXCLUDED.repo,
		   ref = EXCLUDED.ref,
		   author = EXCLUDED.author,
		   updated_at = EXCLUDED.updated_at
		 RETURNING id, type, reference, repo, ref, author, reason, status, detected_at, updated_at`,
		f.ID, string(f.Type), f.Reference, f.Repo, f.Ref, f.Author, f.Reason,
		string(f.Status), f.DetectedAt, f.UpdatedAt,
	)
	stored, err := scanFindingPG(row)
	if err != nil {
		log.Printf("[findings] pg upsert failed: %v", err)
		return f
	}
	return *stored
}

// List returns findings newest-first, optionally filtered by status ("" = all).
func (s *PgFindingStore) List(status enforcement.FindingStatus) []enforcement.Finding {
	query := `SELECT id, type, reference, repo, ref, author, reason, status, detected_at, updated_at
		FROM findings`
	args := []any{}
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, string(status))
	}
	query += " ORDER BY detected_at DESC, id DESC"

	rows, err := s.client.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		log.Printf("[findings] pg list failed: %v", err)
		return nil
	}
	defer rows.Close()

	out := []enforcement.Finding{}
	for rows.Next() {
		f, err := scanFindingPG(rows)
		if err != nil {
			log.Printf("[findings] pg scan failed: %v", err)
			return out
		}
		out = append(out, *f)
	}
	return out
}

// Get returns a finding by ID.
func (s *PgFindingStore) Get(id string) (enforcement.Finding, bool) {
	row := s.client.db.QueryRowContext(context.Background(),
		`SELECT id, type, reference, repo, ref, author, reason, status, detected_at, updated_at
		 FROM findings WHERE id = $1`, id)
	f, err := scanFindingPG(row)
	if err != nil {
		return enforcement.Finding{}, false
	}
	return *f, true
}

// SetStatus updates a finding's status.
func (s *PgFindingStore) SetStatus(id string, status enforcement.FindingStatus) (enforcement.Finding, bool) {
	_, err := s.client.db.ExecContext(context.Background(),
		`UPDATE findings SET status = $1, updated_at = NOW() WHERE id = $2`,
		string(status), id)
	if err != nil {
		log.Printf("[findings] pg set-status failed: %v", err)
		return enforcement.Finding{}, false
	}
	return s.Get(id)
}

func scanFindingPG(row findingScanner) (*enforcement.Finding, error) {
	var f enforcement.Finding
	var typ, status string
	err := row.Scan(&f.ID, &typ, &f.Reference, &f.Repo, &f.Ref, &f.Author, &f.Reason, &status, &f.DetectedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	f.Type = enforcement.FindingType(typ)
	f.Status = enforcement.FindingStatus(status)
	return &f, nil
}

// newID generates a prefixed random ID. Used when callers leave ID empty.
func newID(prefix string) string {
	return prefix + uuid.NewString()[:24]
}
