package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/adp/adp/internal/domain/verification"
)

// PgVerificationStore implements verification.Store and verification.KeyStore
// backed by PostgreSQL (#20). Mirrors SQLiteVerificationStore.
type PgVerificationStore struct {
	client *PostgresClient
}

// NewPgVerificationStore creates a PostgreSQL-backed verification store.
func NewPgVerificationStore(client *PostgresClient) *PgVerificationStore {
	return &PgVerificationStore{client: client}
}

// Save upserts a verification by commit SHA (latest run wins).
func (s *PgVerificationStore) Save(ctx context.Context, v *verification.Verification) error {
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (commit_sha) DO UPDATE SET
		   id = EXCLUDED.id,
		   session_id = EXCLUDED.session_id,
		   status = EXCLUDED.status,
		   pipeline_url = EXCLUDED.pipeline_url,
		   runner_identity = EXCLUDED.runner_identity,
		   evidence_hash = EXCLUDED.evidence_hash,
		   created_at = EXCLUDED.created_at,
		   received_at = EXCLUDED.received_at`,
		v.ID, v.CommitSHA, nullString(v.SessionID), string(v.Status), v.PipelineURL, v.RunnerIdentity, v.EvidenceHash,
		v.CreatedAt.UTC(), v.ReceivedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to save verification: %w", err)
	}
	return nil
}

// GetBySHA returns (nil, nil) when no attestation exists for the SHA.
func (s *PgVerificationStore) GetBySHA(ctx context.Context, sha string) (*verification.Verification, error) {
	row := s.client.db.QueryRowContext(ctx,
		`SELECT id, commit_sha, session_id, status, pipeline_url, runner_identity, evidence_hash, created_at, received_at
		 FROM verifications WHERE commit_sha = $1`, sha)
	v, err := scanVerificationPG(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get verification: %w", err)
	}
	return v, nil
}

// List returns verifications newest-first, optionally filtered by status.
func (s *PgVerificationStore) List(ctx context.Context, status string, limit, offset int) ([]*verification.Verification, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, commit_sha, session_id, status, pipeline_url, runner_identity, evidence_hash, created_at, received_at
		FROM verifications`
	args := []any{}
	idx := 1
	if status != "" {
		query += fmt.Sprintf(" WHERE status = $%d", idx)
		args = append(args, status)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY received_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := s.client.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list verifications: %w", err)
	}
	defer rows.Close()

	var out []*verification.Verification
	for rows.Next() {
		v, err := scanVerificationPG(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan verification: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanVerificationPG(row verificationScanner) (*verification.Verification, error) {
	var v verification.Verification
	var sessionID sql.NullString
	err := row.Scan(&v.ID, &v.CommitSHA, &sessionID, &v.Status, &v.PipelineURL, &v.RunnerIdentity, &v.EvidenceHash, &v.CreatedAt, &v.ReceivedAt)
	if err != nil {
		return nil, err
	}
	if sessionID.Valid {
		v.SessionID = sessionID.String
	}
	return &v, nil
}

// CreateKey stores the hash of a new verification key for repo and returns the
// record plus the plaintext key (shown to the caller exactly once).
func (s *PgVerificationStore) CreateKey(ctx context.Context, repo, createdBy string) (*verification.KeyInfo, string, error) {
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
		`INSERT INTO verification_keys (id, repo, key_hash, created_by, created_at) VALUES ($1, $2, $3, $4, $5)`,
		info.ID, repo, hash, createdBy, info.CreatedAt,
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create verification key: %w", err)
	}
	return info, plaintext, nil
}

// ValidateKey reports whether key is valid for repo (exists, not revoked).
func (s *PgVerificationStore) ValidateKey(ctx context.Context, repo, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var count int
	err := s.client.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM verification_keys WHERE repo = $1 AND key_hash = $2 AND revoked_at IS NULL`,
		repo, verification.HashKey(key),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to validate verification key: %w", err)
	}
	return count > 0, nil
}

// RevokeKey marks a key revoked.
func (s *PgVerificationStore) RevokeKey(ctx context.Context, id string) (bool, error) {
	res, err := s.client.db.ExecContext(ctx,
		`UPDATE verification_keys SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return false, fmt.Errorf("failed to revoke verification key: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListKeys returns all keys (hashes are never selected).
func (s *PgVerificationStore) ListKeys(ctx context.Context) ([]*verification.KeyInfo, error) {
	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, repo, created_by, created_at, revoked_at FROM verification_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list verification keys: %w", err)
	}
	defer rows.Close()

	var out []*verification.KeyInfo
	for rows.Next() {
		var k verification.KeyInfo
		if err := rows.Scan(&k.ID, &k.Repo, &k.CreatedBy, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, fmt.Errorf("failed to scan verification key: %w", err)
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}
