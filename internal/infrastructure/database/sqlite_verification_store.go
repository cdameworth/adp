package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/adp/adp/internal/domain/verification"
)

// SQLiteVerificationStore implements verification.Store and verification.KeyStore
// backed by SQLite (#20).
type SQLiteVerificationStore struct {
	client *SQLiteClient
}

// NewSQLiteVerificationStore creates a SQLite-backed verification store.
func NewSQLiteVerificationStore(client *SQLiteClient) *SQLiteVerificationStore {
	return &SQLiteVerificationStore{client: client}
}

// Save upserts a verification by commit SHA (latest run wins).
func (s *SQLiteVerificationStore) Save(ctx context.Context, v *verification.Verification) error {
	if v.CommitSHA == "" {
		return fmt.Errorf("%w: commit_sha is required", ErrInvalidInput)
	}
	if v.Status != verification.StatusPassed && v.Status != verification.StatusFailed {
		return fmt.Errorf("%w: status must be passed or failed", ErrInvalidInput)
	}
	if v.ID == "" {
		v.ID = verification.NewID()
	}
	if v.ReceivedAt.IsZero() {
		v.ReceivedAt = time.Now().UTC()
	}
	_, err := s.client.db.ExecContext(ctx,
		`INSERT INTO verifications (id, commit_sha, session_id, status, pipeline_url, runner_identity, evidence_hash, created_at, received_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(commit_sha) DO UPDATE SET
		   id = excluded.id,
		   session_id = excluded.session_id,
		   status = excluded.status,
		   pipeline_url = excluded.pipeline_url,
		   runner_identity = excluded.runner_identity,
		   evidence_hash = excluded.evidence_hash,
		   created_at = excluded.created_at,
		   received_at = excluded.received_at`,
		v.ID, v.CommitSHA, v.SessionID, string(v.Status), v.PipelineURL, v.RunnerIdentity, v.EvidenceHash,
		v.CreatedAt.UTC().Format(time.RFC3339Nano), v.ReceivedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to save verification: %w", err)
	}
	return nil
}

// GetBySHA returns (nil, nil) when no attestation exists for the SHA.
func (s *SQLiteVerificationStore) GetBySHA(ctx context.Context, sha string) (*verification.Verification, error) {
	row := s.client.db.QueryRowContext(ctx,
		`SELECT id, commit_sha, session_id, status, pipeline_url, runner_identity, evidence_hash, created_at, received_at
		 FROM verifications WHERE commit_sha = ?`, sha)
	v, err := scanVerification(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get verification: %w", err)
	}
	return v, nil
}

// List returns verifications newest-first, optionally filtered by status.
func (s *SQLiteVerificationStore) List(ctx context.Context, status string, limit, offset int) ([]*verification.Verification, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, commit_sha, session_id, status, pipeline_url, runner_identity, evidence_hash, created_at, received_at
		FROM verifications`
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY received_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.client.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list verifications: %w", err)
	}
	defer rows.Close()

	var out []*verification.Verification
	for rows.Next() {
		v, err := scanVerification(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan verification: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type verificationScanner interface {
	Scan(dest ...any) error
}

func scanVerification(row verificationScanner) (*verification.Verification, error) {
	var v verification.Verification
	var sessionID sql.NullString
	var createdAt, receivedAt string
	err := row.Scan(&v.ID, &v.CommitSHA, &sessionID, &v.Status, &v.PipelineURL, &v.RunnerIdentity, &v.EvidenceHash, &createdAt, &receivedAt)
	if err != nil {
		return nil, err
	}
	if sessionID.Valid {
		v.SessionID = sessionID.String
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	v.ReceivedAt, _ = time.Parse(time.RFC3339Nano, receivedAt)
	return &v, nil
}

// CreateKey stores the hash of a new verification key for repo and returns the
// record plus the plaintext key (shown to the caller exactly once).
func (s *SQLiteVerificationStore) CreateKey(ctx context.Context, repo, createdBy string) (*verification.KeyInfo, string, error) {
	if repo == "" {
		return nil, "", fmt.Errorf("%w: repo is required", ErrInvalidInput)
	}
	plaintext, hash := verification.GenerateKey()
	info := &verification.KeyInfo{
		ID:        verification.NewID(),
		Repo:      repo,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.client.db.ExecContext(ctx,
		`INSERT INTO verification_keys (id, repo, key_hash, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		info.ID, repo, hash, createdBy, info.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create verification key: %w", err)
	}
	return info, plaintext, nil
}

// ValidateKey reports whether key is valid for repo (exists, not revoked).
func (s *SQLiteVerificationStore) ValidateKey(ctx context.Context, repo, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var count int
	err := s.client.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM verification_keys WHERE repo = ? AND key_hash = ? AND revoked_at IS NULL`,
		repo, verification.HashKey(key),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to validate verification key: %w", err)
	}
	return count > 0, nil
}

// RevokeKey marks a key revoked.
func (s *SQLiteVerificationStore) RevokeKey(ctx context.Context, id string) (bool, error) {
	res, err := s.client.db.ExecContext(ctx,
		`UPDATE verification_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return false, fmt.Errorf("failed to revoke verification key: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListKeys returns all keys (hashes are never selected).
func (s *SQLiteVerificationStore) ListKeys(ctx context.Context) ([]*verification.KeyInfo, error) {
	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, repo, created_by, created_at, revoked_at FROM verification_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list verification keys: %w", err)
	}
	defer rows.Close()

	var out []*verification.KeyInfo
	for rows.Next() {
		var k verification.KeyInfo
		var createdAt string
		var revokedAt sql.NullString
		if err := rows.Scan(&k.ID, &k.Repo, &k.CreatedBy, &createdAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("failed to scan verification key: %w", err)
		}
		k.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if revokedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, revokedAt.String)
			if err == nil {
				k.RevokedAt = &t
			}
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}
