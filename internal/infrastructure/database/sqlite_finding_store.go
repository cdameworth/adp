package database

import (
	"context"
	"log"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
)

// SQLiteFindingStore persists reconciliation findings in SQLite (#10).
//
// The enforcement.FindingStore interface is synchronous and error-free by
// design (findings are a detection backstop, never on the critical path);
// database errors are logged and degrade gracefully instead of propagating.
type SQLiteFindingStore struct {
	client *SQLiteClient
}

// NewSQLiteFindingStore creates a SQLite-backed finding store.
func NewSQLiteFindingStore(client *SQLiteClient) *SQLiteFindingStore {
	return &SQLiteFindingStore{client: client}
}

// Upsert inserts a finding or, if one exists for (Type, Reference), refreshes
// repo/ref/author/updated_at while preserving ID, status, and detected_at —
// matching InMemoryFindingStore semantics.
func (s *SQLiteFindingStore) Upsert(f enforcement.Finding) enforcement.Finding {
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

	_, err := s.client.db.ExecContext(context.Background(),
		`INSERT INTO findings (id, type, reference, repo, ref, author, reason, status, detected_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(type, reference) DO UPDATE SET
		   repo = excluded.repo,
		   ref = excluded.ref,
		   author = excluded.author,
		   updated_at = excluded.updated_at`,
		f.ID, string(f.Type), f.Reference, f.Repo, f.Ref, f.Author, f.Reason,
		string(f.Status), f.DetectedAt.Format(time.RFC3339Nano), f.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		log.Printf("[findings] sqlite upsert failed: %v", err)
		return f
	}

	if stored, ok := s.Get(f.ID); ok {
		return stored
	}
	// Conflict path: ID changed on conflict — fetch by dedup key.
	return s.getByKey(f.Type, f.Reference)
}

func (s *SQLiteFindingStore) getByKey(t enforcement.FindingType, ref string) enforcement.Finding {
	row := s.client.db.QueryRowContext(context.Background(),
		`SELECT id, type, reference, repo, ref, author, reason, status, detected_at, updated_at
		 FROM findings WHERE type = ? AND reference = ?`, string(t), ref)
	f, err := scanFinding(row)
	if err != nil {
		log.Printf("[findings] sqlite getByKey failed: %v", err)
		return enforcement.Finding{}
	}
	return *f
}

// List returns findings newest-first, optionally filtered by status ("" = all).
func (s *SQLiteFindingStore) List(status enforcement.FindingStatus) []enforcement.Finding {
	query := `SELECT id, type, reference, repo, ref, author, reason, status, detected_at, updated_at
		FROM findings`
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, string(status))
	}
	query += " ORDER BY detected_at DESC, id DESC"

	rows, err := s.client.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		log.Printf("[findings] sqlite list failed: %v", err)
		return nil
	}
	defer rows.Close()

	out := []enforcement.Finding{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			log.Printf("[findings] sqlite scan failed: %v", err)
			return out
		}
		out = append(out, *f)
	}
	return out
}

// Get returns a finding by ID.
func (s *SQLiteFindingStore) Get(id string) (enforcement.Finding, bool) {
	row := s.client.db.QueryRowContext(context.Background(),
		`SELECT id, type, reference, repo, ref, author, reason, status, detected_at, updated_at
		 FROM findings WHERE id = ?`, id)
	f, err := scanFinding(row)
	if err != nil {
		return enforcement.Finding{}, false
	}
	return *f, true
}

// SetStatus updates a finding's status.
func (s *SQLiteFindingStore) SetStatus(id string, status enforcement.FindingStatus) (enforcement.Finding, bool) {
	_, err := s.client.db.ExecContext(context.Background(),
		`UPDATE findings SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		log.Printf("[findings] sqlite set-status failed: %v", err)
		return enforcement.Finding{}, false
	}
	return s.Get(id)
}

type findingScanner interface {
	Scan(dest ...any) error
}

func scanFinding(row findingScanner) (*enforcement.Finding, error) {
	var f enforcement.Finding
	var typ, status, detectedAt, updatedAt string
	err := row.Scan(&f.ID, &typ, &f.Reference, &f.Repo, &f.Ref, &f.Author, &f.Reason, &status, &detectedAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	f.Type = enforcement.FindingType(typ)
	f.Status = enforcement.FindingStatus(status)
	f.DetectedAt, _ = time.Parse(time.RFC3339Nano, detectedAt)
	f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &f, nil
}
