package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// CommitRecord represents a prepared or verified commit.
type CommitRecord struct {
	ID             uuid.UUID  `json:"id"`
	SessionID      string     `json:"session_id"`
	CommitSHA      string     `json:"commit_sha,omitempty"`
	CommitToken    string     `json:"commit_token"`
	Files          []string   `json:"files"`
	Message        string     `json:"message,omitempty"`
	Status         string     `json:"status"`
	Approved       bool       `json:"approved"`
	ApprovalReason string     `json:"approval_reason,omitempty"`
	PreparedAt     time.Time  `json:"prepared_at"`
	CommittedAt    *time.Time `json:"committed_at,omitempty"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
}

// CommitStore provides CRUD operations for commit records.
type CommitStore struct {
	client *PostgresClient
}

// NewCommitStore creates a new commit store.
func NewCommitStore(client *PostgresClient) *CommitStore {
	return &CommitStore{client: client}
}

// PrepareCommitInput contains the input for preparing a commit.
type PrepareCommitInput struct {
	SessionID string
	Files     []string
	Message   string
}

// Prepare creates a new commit preparation record.
func (s *CommitStore) Prepare(ctx context.Context, input PrepareCommitInput) (*CommitRecord, error) {
	// Validate required fields
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if len(input.Files) == 0 {
		return nil, fmt.Errorf("%w: files are required", ErrInvalidInput)
	}

	// Generate commit token
	token, err := generateCommitToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate commit token: %w", err)
	}

	query := `
		INSERT INTO commit_records (
			session_id, commit_token, files, message, status, approved
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, prepared_at`

	var record CommitRecord
	err = s.client.db.QueryRowContext(ctx, query,
		input.SessionID,
		token,
		pq.Array(input.Files),
		input.Message,
		"prepared",
		false,
	).Scan(&record.ID, &record.PreparedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to prepare commit: %w", err)
	}

	record.SessionID = input.SessionID
	record.CommitToken = token
	record.Files = input.Files
	record.Message = input.Message
	record.Status = "prepared"
	record.Approved = false

	return &record, nil
}

// Get retrieves a commit record by ID.
func (s *CommitStore) Get(ctx context.Context, id uuid.UUID) (*CommitRecord, error) {
	query := `
		SELECT id, session_id, commit_sha, commit_token, files, message,
			   status, approved, approval_reason, prepared_at, committed_at, verified_at
		FROM commit_records
		WHERE id = $1`

	var record CommitRecord
	var commitSHA, message, approvalReason sql.NullString
	var files pq.StringArray

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&record.ID,
		&record.SessionID,
		&commitSHA,
		&record.CommitToken,
		&files,
		&message,
		&record.Status,
		&record.Approved,
		&approvalReason,
		&record.PreparedAt,
		&record.CommittedAt,
		&record.VerifiedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: commit record %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get commit record: %w", err)
	}

	record.Files = files
	if commitSHA.Valid {
		record.CommitSHA = commitSHA.String
	}
	if message.Valid {
		record.Message = message.String
	}
	if approvalReason.Valid {
		record.ApprovalReason = approvalReason.String
	}

	return &record, nil
}

// GetByToken retrieves a commit record by its token.
func (s *CommitStore) GetByToken(ctx context.Context, token string) (*CommitRecord, error) {
	query := `
		SELECT id FROM commit_records WHERE commit_token = $1`

	var id uuid.UUID
	err := s.client.db.QueryRowContext(ctx, query, token).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: commit token %s", ErrNotFound, token)
		}
		return nil, fmt.Errorf("failed to get commit by token: %w", err)
	}

	return s.Get(ctx, id)
}

// GetBySHA retrieves a commit record by its SHA.
func (s *CommitStore) GetBySHA(ctx context.Context, sha string) (*CommitRecord, error) {
	query := `
		SELECT id FROM commit_records WHERE commit_sha = $1`

	var id uuid.UUID
	err := s.client.db.QueryRowContext(ctx, query, sha).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: commit SHA %s", ErrNotFound, sha)
		}
		return nil, fmt.Errorf("failed to get commit by SHA: %w", err)
	}

	return s.Get(ctx, id)
}

// Approve marks a prepared commit as approved.
func (s *CommitStore) Approve(ctx context.Context, id uuid.UUID, reason string) (*CommitRecord, error) {
	query := `
		UPDATE commit_records
		SET approved = true, approval_reason = $1
		WHERE id = $2 AND status = 'prepared'`

	result, err := s.client.db.ExecContext(ctx, query, reason, id)
	if err != nil {
		return nil, fmt.Errorf("failed to approve commit: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: commit record %s not found or not prepared", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// RegisterCommit updates the commit record with the actual commit SHA.
// Implements store.CommitStore.RegisterCommit.
func (s *CommitStore) RegisterCommit(ctx context.Context, token string, sha string) (*CommitRecord, error) {
	return s.MarkCommitted(ctx, token, sha)
}

// MarkCommitted updates the commit record with the actual commit SHA.
func (s *CommitStore) MarkCommitted(ctx context.Context, token string, sha string) (*CommitRecord, error) {
	query := `
		UPDATE commit_records
		SET commit_sha = $1, status = 'committed', committed_at = NOW()
		WHERE commit_token = $2 AND status = 'prepared' AND approved = true`

	result, err := s.client.db.ExecContext(ctx, query, sha, token)
	if err != nil {
		return nil, fmt.Errorf("failed to mark commit: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: commit token %s not found, not approved, or not prepared", ErrNotFound, token)
	}

	return s.GetByToken(ctx, token)
}

// Verify marks a commit as verified.
func (s *CommitStore) Verify(ctx context.Context, sha string) (*CommitRecord, error) {
	query := `
		UPDATE commit_records
		SET status = 'verified', verified_at = NOW()
		WHERE commit_sha = $1 AND status = 'committed'`

	result, err := s.client.db.ExecContext(ctx, query, sha)
	if err != nil {
		return nil, fmt.Errorf("failed to verify commit: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: commit SHA %s not found or not committed", ErrNotFound, sha)
	}

	return s.GetBySHA(ctx, sha)
}

// Reject rejects a prepared commit.
func (s *CommitStore) Reject(ctx context.Context, id uuid.UUID, reason string) error {
	query := `
		UPDATE commit_records
		SET status = 'rejected', approval_reason = $1
		WHERE id = $2 AND status = 'prepared'`

	result, err := s.client.db.ExecContext(ctx, query, reason, id)
	if err != nil {
		return fmt.Errorf("failed to reject commit: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: commit record %s not found or not prepared", ErrNotFound, id)
	}

	return nil
}

// CommitFilter defines criteria for listing commit records.
type CommitFilter struct {
	SessionID string
	Status    string
	Approved  *bool
	Since     *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
}

// List retrieves commit records matching the filter criteria.
func (s *CommitStore) List(ctx context.Context, filter CommitFilter) ([]*CommitRecord, error) {
	query := `
		SELECT id, session_id, commit_sha, commit_token, files, message,
			   status, approved, approval_reason, prepared_at, committed_at, verified_at
		FROM commit_records
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", argIdx)
		args = append(args, filter.SessionID)
		argIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Approved != nil {
		query += fmt.Sprintf(" AND approved = $%d", argIdx)
		args = append(args, *filter.Approved)
		argIdx++
	}
	if filter.Since != nil {
		query += fmt.Sprintf(" AND prepared_at >= $%d", argIdx)
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		query += fmt.Sprintf(" AND prepared_at <= $%d", argIdx)
		args = append(args, *filter.Until)
		argIdx++
	}

	query += " ORDER BY prepared_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
	}

	rows, err := s.client.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list commit records: %w", err)
	}
	defer rows.Close()

	var records []*CommitRecord
	for rows.Next() {
		var record CommitRecord
		var commitSHA, message, approvalReason sql.NullString
		var files pq.StringArray

		err := rows.Scan(
			&record.ID,
			&record.SessionID,
			&commitSHA,
			&record.CommitToken,
			&files,
			&message,
			&record.Status,
			&record.Approved,
			&approvalReason,
			&record.PreparedAt,
			&record.CommittedAt,
			&record.VerifiedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan commit record: %w", err)
		}

		record.Files = files
		if commitSHA.Valid {
			record.CommitSHA = commitSHA.String
		}
		if message.Valid {
			record.Message = message.String
		}
		if approvalReason.Valid {
			record.ApprovalReason = approvalReason.String
		}

		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating commit records: %w", err)
	}

	return records, nil
}

// IsCommitVerified checks if a commit SHA has been verified.
func (s *CommitStore) IsCommitVerified(ctx context.Context, sha string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM commit_records
			WHERE commit_sha = $1 AND status = 'verified'
		)`

	var verified bool
	err := s.client.db.QueryRowContext(ctx, query, sha).Scan(&verified)
	if err != nil {
		return false, fmt.Errorf("failed to check commit verification: %w", err)
	}

	return verified, nil
}

// CountByStatus returns counts of commits by status for a session.
func (s *CommitStore) CountByStatus(ctx context.Context, sessionID string) (map[string]int64, error) {
	query := `
		SELECT status, COUNT(*)
		FROM commit_records
		WHERE session_id = $1
		GROUP BY status`

	rows, err := s.client.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to count commits: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[status] = count
	}

	return counts, rows.Err()
}

// generateCommitToken generates a secure random commit token.
func generateCommitToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "adp_" + hex.EncodeToString(bytes), nil
}
