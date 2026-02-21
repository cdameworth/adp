package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// SQLiteCommitStore implements store.CommitStore backed by SQLite.
type SQLiteCommitStore struct {
	client *SQLiteClient
}

// NewSQLiteCommitStore creates a new SQLite-backed commit store.
func NewSQLiteCommitStore(client *SQLiteClient) *SQLiteCommitStore {
	return &SQLiteCommitStore{client: client}
}

// Prepare creates a new commit preparation record.
func (s *SQLiteCommitStore) Prepare(ctx context.Context, input store.PrepareCommitInput) (*store.CommitRecord, error) {
	// Validate required fields.
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if len(input.Files) == 0 {
		return nil, fmt.Errorf("%w: files are required", ErrInvalidInput)
	}

	// Generate commit token: 32 random bytes, hex encoded, prefixed "adp_".
	token, err := generateSQLiteCommitToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate commit token: %w", err)
	}

	filesJSON, err := json.Marshal(input.Files)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal files: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	preparedAt := now.Format(time.RFC3339Nano)

	query := `
		INSERT INTO commit_records (
			id, session_id, commit_token, files, message, status, approved, prepared_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.client.DB().ExecContext(ctx, query,
		id,
		input.SessionID,
		token,
		string(filesJSON),
		input.Message,
		"prepared",
		0, // approved = false
		preparedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare commit: %w", err)
	}

	return &store.CommitRecord{
		ID:          id,
		SessionID:   input.SessionID,
		CommitToken: token,
		Files:       input.Files,
		Message:     input.Message,
		Status:      "prepared",
		Approved:    false,
		PreparedAt:  now,
	}, nil
}

// RegisterCommit updates a commit record with the actual commit SHA and marks it as committed.
// Implements store.CommitStore.RegisterCommit.
func (s *SQLiteCommitStore) RegisterCommit(ctx context.Context, token string, sha string) (*store.CommitRecord, error) {
	now := time.Now().UTC()
	committedAt := now.Format(time.RFC3339Nano)

	query := `
		UPDATE commit_records
		SET commit_sha = ?, status = 'committed', committed_at = ?
		WHERE commit_token = ?`

	result, err := s.client.DB().ExecContext(ctx, query, sha, committedAt, token)
	if err != nil {
		return nil, fmt.Errorf("failed to mark commit: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: commit token %s", ErrNotFound, token)
	}

	// Retrieve the updated record.
	return s.getByToken(ctx, token)
}

// IsCommitVerified checks if a commit SHA has been committed or verified.
func (s *SQLiteCommitStore) IsCommitVerified(ctx context.Context, sha string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM commit_records
			WHERE commit_sha = ? AND (status = 'committed' OR status = 'verified')
		)`

	var verified bool
	err := s.client.DB().QueryRowContext(ctx, query, sha).Scan(&verified)
	if err != nil {
		return false, fmt.Errorf("failed to check commit verification: %w", err)
	}

	return verified, nil
}

// getByToken retrieves a commit record by its token.
func (s *SQLiteCommitStore) getByToken(ctx context.Context, token string) (*store.CommitRecord, error) {
	query := `
		SELECT id, session_id, commit_sha, commit_token, files, message,
			   status, approved, approval_reason, prepared_at, committed_at, verified_at
		FROM commit_records
		WHERE commit_token = ?`

	var record store.CommitRecord
	var commitSHA, message, approvalReason sql.NullString
	var filesStr string
	var approvedInt int
	var preparedAtStr string
	var committedAtStr, verifiedAtStr sql.NullString

	err := s.client.DB().QueryRowContext(ctx, query, token).Scan(
		&record.ID,
		&record.SessionID,
		&commitSHA,
		&record.CommitToken,
		&filesStr,
		&message,
		&record.Status,
		&approvedInt,
		&approvalReason,
		&preparedAtStr,
		&committedAtStr,
		&verifiedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: commit token %s", ErrNotFound, token)
		}
		return nil, fmt.Errorf("failed to get commit by token: %w", err)
	}

	// Parse timestamps.
	record.PreparedAt, _ = time.Parse(time.RFC3339Nano, preparedAtStr)
	if committedAtStr.Valid && committedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, committedAtStr.String)
		if err == nil {
			record.CommittedAt = &t
		}
	}
	if verifiedAtStr.Valid && verifiedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, verifiedAtStr.String)
		if err == nil {
			record.VerifiedAt = &t
		}
	}

	// Parse scalar nullable fields.
	record.Approved = approvedInt != 0
	if commitSHA.Valid {
		record.CommitSHA = commitSHA.String
	}
	if message.Valid {
		record.Message = message.String
	}
	if approvalReason.Valid {
		record.ApprovalReason = approvalReason.String
	}

	// Unmarshal JSON text fields.
	if err := json.Unmarshal([]byte(filesStr), &record.Files); err != nil {
		record.Files = []string{}
	}

	return &record, nil
}

// GetBySHA retrieves a commit record by its commit SHA.
func (s *SQLiteCommitStore) GetBySHA(ctx context.Context, sha string) (*store.CommitRecord, error) {
	query := `
		SELECT id, session_id, commit_sha, commit_token, files, message,
			   status, approved, approval_reason, prepared_at, committed_at, verified_at
		FROM commit_records
		WHERE commit_sha = ?`

	var record store.CommitRecord
	var commitSHA, message, approvalReason sql.NullString
	var filesStr string
	var approvedInt int
	var preparedAtStr string
	var committedAtStr, verifiedAtStr sql.NullString

	err := s.client.DB().QueryRowContext(ctx, query, sha).Scan(
		&record.ID,
		&record.SessionID,
		&commitSHA,
		&record.CommitToken,
		&filesStr,
		&message,
		&record.Status,
		&approvedInt,
		&approvalReason,
		&preparedAtStr,
		&committedAtStr,
		&verifiedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: commit SHA %s", ErrNotFound, sha)
		}
		return nil, fmt.Errorf("failed to get commit by SHA: %w", err)
	}

	record.PreparedAt, _ = time.Parse(time.RFC3339Nano, preparedAtStr)
	if committedAtStr.Valid && committedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, committedAtStr.String)
		if err == nil {
			record.CommittedAt = &t
		}
	}
	if verifiedAtStr.Valid && verifiedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, verifiedAtStr.String)
		if err == nil {
			record.VerifiedAt = &t
		}
	}

	record.Approved = approvedInt != 0
	if commitSHA.Valid {
		record.CommitSHA = commitSHA.String
	}
	if message.Valid {
		record.Message = message.String
	}
	if approvalReason.Valid {
		record.ApprovalReason = approvalReason.String
	}
	if err := json.Unmarshal([]byte(filesStr), &record.Files); err != nil {
		record.Files = []string{}
	}

	return &record, nil
}

// Approve marks a commit record as approved.
func (s *SQLiteCommitStore) Approve(ctx context.Context, id string, reason string) (*store.CommitRecord, error) {
	result, err := s.client.DB().ExecContext(ctx,
		`UPDATE commit_records SET approved = 1, approval_reason = ? WHERE id = ?`,
		reason, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to approve commit: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: commit %s", ErrNotFound, id)
	}

	// Re-fetch by querying the record. Use the ID to find the token first.
	var token string
	err = s.client.DB().QueryRowContext(ctx, "SELECT commit_token FROM commit_records WHERE id = ?", id).Scan(&token)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit token: %w", err)
	}
	return s.getByToken(ctx, token)
}

// generateSQLiteCommitToken generates a secure random commit token.
func generateSQLiteCommitToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "adp_" + hex.EncodeToString(bytes), nil
}
