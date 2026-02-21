package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// SQLiteEscalationStore implements store.EscalationStore backed by SQLite.
type SQLiteEscalationStore struct {
	client *SQLiteClient
}

// NewSQLiteEscalationStore creates a new SQLite-backed escalation store.
func NewSQLiteEscalationStore(client *SQLiteClient) *SQLiteEscalationStore {
	return &SQLiteEscalationStore{client: client}
}

// Create inserts a new escalation request into SQLite.
func (s *SQLiteEscalationStore) Create(ctx context.Context, input store.CreateEscalationInput) (*store.EscalationRequest, error) {
	// Validate required fields.
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.Action == "" {
		return nil, fmt.Errorf("%w: action is required", ErrInvalidInput)
	}
	if input.Reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidInput)
	}

	// Default values.
	if input.ActionType == "" {
		input.ActionType = "unknown"
	}
	if input.Target == nil {
		input.Target = map[string]any{}
	}
	if input.PolicyNames == nil {
		input.PolicyNames = []string{}
	}
	if input.ContextSummary == nil {
		input.ContextSummary = map[string]any{}
	}
	if input.Priority == "" {
		input.Priority = "normal"
	}

	targetJSON, err := json.Marshal(input.Target)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal target: %w", err)
	}

	policyNamesJSON, err := json.Marshal(input.PolicyNames)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal policy_names: %w", err)
	}

	contextJSON, err := json.Marshal(input.ContextSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal context_summary: %w", err)
	}

	var expiresAtStr sql.NullString
	if input.ExpiresAt != nil {
		expiresAtStr = sql.NullString{
			String: input.ExpiresAt.UTC().Format(time.RFC3339Nano),
			Valid:  true,
		}
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	requestedAt := now.Format(time.RFC3339Nano)

	query := `
		INSERT INTO escalation_requests (
			id, session_id, decision_id, action, action_type, target,
			reason, policy_names, context_summary, status, priority,
			requested_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.client.DB().ExecContext(ctx, query,
		id,
		input.SessionID,
		input.DecisionID,
		input.Action,
		input.ActionType,
		string(targetJSON),
		input.Reason,
		string(policyNamesJSON),
		string(contextJSON),
		"pending",
		input.Priority,
		requestedAt,
		expiresAtStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create escalation: %w", err)
	}

	return &store.EscalationRequest{
		ID:             id,
		SessionID:      input.SessionID,
		DecisionID:     input.DecisionID,
		Action:         input.Action,
		ActionType:     input.ActionType,
		Target:         input.Target,
		Reason:         input.Reason,
		PolicyNames:    input.PolicyNames,
		ContextSummary: input.ContextSummary,
		Status:         "pending",
		Priority:       input.Priority,
		RequestedAt:    now,
		ExpiresAt:      input.ExpiresAt,
	}, nil
}

// Get retrieves an escalation request by ID.
func (s *SQLiteEscalationStore) Get(ctx context.Context, id string) (*store.EscalationRequest, error) {
	query := `
		SELECT id, session_id, decision_id, action, action_type, target,
			   reason, policy_names, context_summary, status, priority,
			   approver_id, approver_comment, requested_at, expires_at, resolved_at
		FROM escalation_requests
		WHERE id = ?`

	var escalation store.EscalationRequest
	var decisionID, approverID, approverComment sql.NullString
	var targetStr, policyNamesStr, contextStr string
	var requestedAtStr string
	var expiresAtStr, resolvedAtStr sql.NullString

	err := s.client.DB().QueryRowContext(ctx, query, id).Scan(
		&escalation.ID,
		&escalation.SessionID,
		&decisionID,
		&escalation.Action,
		&escalation.ActionType,
		&targetStr,
		&escalation.Reason,
		&policyNamesStr,
		&contextStr,
		&escalation.Status,
		&escalation.Priority,
		&approverID,
		&approverComment,
		&requestedAtStr,
		&expiresAtStr,
		&resolvedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: escalation %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get escalation: %w", err)
	}

	// Parse timestamps.
	escalation.RequestedAt, _ = time.Parse(time.RFC3339Nano, requestedAtStr)
	if expiresAtStr.Valid && expiresAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, expiresAtStr.String)
		if err == nil {
			escalation.ExpiresAt = &t
		}
	}
	if resolvedAtStr.Valid && resolvedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, resolvedAtStr.String)
		if err == nil {
			escalation.ResolvedAt = &t
		}
	}

	// Parse nullable string fields.
	if decisionID.Valid {
		escalation.DecisionID = decisionID.String
	}
	if approverID.Valid {
		escalation.ApproverID = approverID.String
	}
	if approverComment.Valid {
		escalation.ApproverComment = approverComment.String
	}

	// Unmarshal JSON text fields.
	if err := json.Unmarshal([]byte(targetStr), &escalation.Target); err != nil {
		escalation.Target = map[string]any{}
	}
	if err := json.Unmarshal([]byte(policyNamesStr), &escalation.PolicyNames); err != nil {
		escalation.PolicyNames = []string{}
	}
	if err := json.Unmarshal([]byte(contextStr), &escalation.ContextSummary); err != nil {
		escalation.ContextSummary = map[string]any{}
	}

	return &escalation, nil
}

// SQLiteEscalationFilter defines filter options for listing escalation requests.
type SQLiteEscalationFilter struct {
	SessionID string
	Status    string
	Priority  string
	Limit     int
	Offset    int
}

// SQLiteResolveInput defines the fields for resolving an escalation request.
type SQLiteResolveInput struct {
	ApproverID string
	Approved   bool
	Comment    string
}

// List returns escalation requests matching the given filter criteria.
func (s *SQLiteEscalationStore) List(ctx context.Context, filter SQLiteEscalationFilter) ([]*store.EscalationRequest, error) {
	query := `SELECT id, session_id, decision_id, action, action_type, target,
		reason, policy_names, context_summary, status, priority,
		approver_id, approver_comment, requested_at, expires_at, resolved_at
	FROM escalation_requests WHERE 1=1`

	var args []any

	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Priority != "" {
		query += " AND priority = ?"
		args = append(args, filter.Priority)
	}

	query += " ORDER BY requested_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.client.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list escalations: %w", err)
	}
	defer rows.Close()

	var escalations []*store.EscalationRequest
	for rows.Next() {
		esc, err := scanEscalation(rows)
		if err != nil {
			return nil, err
		}
		escalations = append(escalations, esc)
	}
	return escalations, rows.Err()
}

// ListPending returns pending escalation requests.
func (s *SQLiteEscalationStore) ListPending(ctx context.Context, limit int) ([]*store.EscalationRequest, error) {
	return s.List(ctx, SQLiteEscalationFilter{Status: "pending", Limit: limit})
}

// Resolve approves or denies an escalation request.
func (s *SQLiteEscalationStore) Resolve(ctx context.Context, id string, input SQLiteResolveInput) (*store.EscalationRequest, error) {
	status := "denied"
	if input.Approved {
		status = "approved"
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	result, err := s.client.DB().ExecContext(ctx,
		`UPDATE escalation_requests
		SET status = ?, approver_id = ?, approver_comment = ?, resolved_at = ?
		WHERE id = ? AND status = 'pending'`,
		status, input.ApproverID, input.Comment, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve escalation: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: escalation %s not found or already resolved", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// scanEscalation reads a single row from the result set into a store.EscalationRequest.
func scanEscalation(rows *sql.Rows) (*store.EscalationRequest, error) {
	var esc store.EscalationRequest
	var decisionID, approverID, approverComment sql.NullString
	var targetStr, policyNamesStr, contextStr string
	var requestedAtStr string
	var expiresAtStr, resolvedAtStr sql.NullString

	err := rows.Scan(
		&esc.ID,
		&esc.SessionID,
		&decisionID,
		&esc.Action,
		&esc.ActionType,
		&targetStr,
		&esc.Reason,
		&policyNamesStr,
		&contextStr,
		&esc.Status,
		&esc.Priority,
		&approverID,
		&approverComment,
		&requestedAtStr,
		&expiresAtStr,
		&resolvedAtStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan escalation: %w", err)
	}

	esc.RequestedAt, _ = time.Parse(time.RFC3339Nano, requestedAtStr)
	if expiresAtStr.Valid && expiresAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, expiresAtStr.String)
		if err == nil {
			esc.ExpiresAt = &t
		}
	}
	if resolvedAtStr.Valid && resolvedAtStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, resolvedAtStr.String)
		if err == nil {
			esc.ResolvedAt = &t
		}
	}

	if decisionID.Valid {
		esc.DecisionID = decisionID.String
	}
	if approverID.Valid {
		esc.ApproverID = approverID.String
	}
	if approverComment.Valid {
		esc.ApproverComment = approverComment.String
	}

	if err := json.Unmarshal([]byte(targetStr), &esc.Target); err != nil {
		esc.Target = map[string]any{}
	}
	if err := json.Unmarshal([]byte(policyNamesStr), &esc.PolicyNames); err != nil {
		esc.PolicyNames = []string{}
	}
	if err := json.Unmarshal([]byte(contextStr), &esc.ContextSummary); err != nil {
		esc.ContextSummary = map[string]any{}
	}

	return &esc, nil
}
