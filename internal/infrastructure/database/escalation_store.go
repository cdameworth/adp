package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// EscalationRequest represents a request for human approval.
type EscalationRequest struct {
	ID              uuid.UUID              `json:"id"`
	SessionID       string                 `json:"session_id"`
	DecisionID      *uuid.UUID             `json:"decision_id,omitempty"`
	Action          string                 `json:"action"`
	ActionType      string                 `json:"action_type"`
	Target          map[string]interface{} `json:"target"`
	Reason          string                 `json:"reason"`
	PolicyNames     []string               `json:"policy_names"`
	ContextSummary  map[string]interface{} `json:"context_summary"`
	Status          string                 `json:"status"`
	Priority        string                 `json:"priority"`
	ApproverID      *uuid.UUID             `json:"approver_id,omitempty"`
	ApproverComment string                 `json:"approver_comment,omitempty"`
	RequestedAt     time.Time              `json:"requested_at"`
	ExpiresAt       *time.Time             `json:"expires_at,omitempty"`
	ResolvedAt      *time.Time             `json:"resolved_at,omitempty"`
}

// EscalationStore provides CRUD operations for escalation requests.
type EscalationStore struct {
	client *PostgresClient
}

// NewEscalationStore creates a new escalation store.
func NewEscalationStore(client *PostgresClient) *EscalationStore {
	return &EscalationStore{client: client}
}

// CreateEscalationInput contains the input for creating an escalation request.
type CreateEscalationInput struct {
	SessionID      string
	DecisionID     *uuid.UUID
	Action         string
	ActionType     string
	Target         map[string]interface{}
	Reason         string
	PolicyNames    []string
	ContextSummary map[string]interface{}
	Priority       string
	ExpiresAt      *time.Time
}

// Create creates a new escalation request.
func (s *EscalationStore) Create(ctx context.Context, input CreateEscalationInput) (*EscalationRequest, error) {
	// Validate required fields
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.Action == "" {
		return nil, fmt.Errorf("%w: action is required", ErrInvalidInput)
	}
	if input.Reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidInput)
	}

	// Default values
	if input.ActionType == "" {
		input.ActionType = "unknown"
	}
	if input.Target == nil {
		input.Target = map[string]interface{}{}
	}
	if input.PolicyNames == nil {
		input.PolicyNames = []string{}
	}
	if input.ContextSummary == nil {
		input.ContextSummary = map[string]interface{}{}
	}
	if input.Priority == "" {
		input.Priority = "normal"
	}

	targetJSON, err := json.Marshal(input.Target)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal target: %w", err)
	}

	contextJSON, err := json.Marshal(input.ContextSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal context summary: %w", err)
	}

	query := `
		INSERT INTO escalation_requests (
			session_id, decision_id, action, action_type, target,
			reason, policy_names, context_summary, priority, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, requested_at`

	var escalation EscalationRequest
	err = s.client.db.QueryRowContext(ctx, query,
		input.SessionID,
		input.DecisionID,
		input.Action,
		input.ActionType,
		targetJSON,
		input.Reason,
		pq.Array(input.PolicyNames),
		contextJSON,
		input.Priority,
		input.ExpiresAt,
	).Scan(&escalation.ID, &escalation.RequestedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create escalation: %w", err)
	}

	escalation.SessionID = input.SessionID
	escalation.DecisionID = input.DecisionID
	escalation.Action = input.Action
	escalation.ActionType = input.ActionType
	escalation.Target = input.Target
	escalation.Reason = input.Reason
	escalation.PolicyNames = input.PolicyNames
	escalation.ContextSummary = input.ContextSummary
	escalation.Status = "pending"
	escalation.Priority = input.Priority
	escalation.ExpiresAt = input.ExpiresAt

	return &escalation, nil
}

// Get retrieves an escalation request by ID.
func (s *EscalationStore) Get(ctx context.Context, id uuid.UUID) (*EscalationRequest, error) {
	query := `
		SELECT id, session_id, decision_id, action, action_type, target,
			   reason, policy_names, context_summary, status, priority,
			   approver_id, approver_comment, requested_at, expires_at, resolved_at
		FROM escalation_requests
		WHERE id = $1`

	var escalation EscalationRequest
	var targetJSON, contextJSON []byte
	var policyNames pq.StringArray

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&escalation.ID,
		&escalation.SessionID,
		&escalation.DecisionID,
		&escalation.Action,
		&escalation.ActionType,
		&targetJSON,
		&escalation.Reason,
		&policyNames,
		&contextJSON,
		&escalation.Status,
		&escalation.Priority,
		&escalation.ApproverID,
		&escalation.ApproverComment,
		&escalation.RequestedAt,
		&escalation.ExpiresAt,
		&escalation.ResolvedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: escalation %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get escalation: %w", err)
	}

	json.Unmarshal(targetJSON, &escalation.Target)
	json.Unmarshal(contextJSON, &escalation.ContextSummary)
	escalation.PolicyNames = policyNames

	return &escalation, nil
}

// EscalationFilter defines criteria for listing escalation requests.
type EscalationFilter struct {
	SessionID  string
	Status     string
	Priority   string
	ApproverID *uuid.UUID
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// List retrieves escalation requests matching the filter criteria.
func (s *EscalationStore) List(ctx context.Context, filter EscalationFilter) ([]*EscalationRequest, error) {
	query := `
		SELECT id, session_id, decision_id, action, action_type, target,
			   reason, policy_names, context_summary, status, priority,
			   approver_id, approver_comment, requested_at, expires_at, resolved_at
		FROM escalation_requests
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
	if filter.Priority != "" {
		query += fmt.Sprintf(" AND priority = $%d", argIdx)
		args = append(args, filter.Priority)
		argIdx++
	}
	if filter.ApproverID != nil {
		query += fmt.Sprintf(" AND approver_id = $%d", argIdx)
		args = append(args, *filter.ApproverID)
		argIdx++
	}
	if filter.Since != nil {
		query += fmt.Sprintf(" AND requested_at >= $%d", argIdx)
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		query += fmt.Sprintf(" AND requested_at <= $%d", argIdx)
		args = append(args, *filter.Until)
		argIdx++
	}

	query += " ORDER BY CASE priority WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'normal' THEN 3 ELSE 4 END, requested_at ASC"

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
		return nil, fmt.Errorf("failed to list escalations: %w", err)
	}
	defer rows.Close()

	var escalations []*EscalationRequest
	for rows.Next() {
		var escalation EscalationRequest
		var targetJSON, contextJSON []byte
		var policyNames pq.StringArray

		err := rows.Scan(
			&escalation.ID,
			&escalation.SessionID,
			&escalation.DecisionID,
			&escalation.Action,
			&escalation.ActionType,
			&targetJSON,
			&escalation.Reason,
			&policyNames,
			&contextJSON,
			&escalation.Status,
			&escalation.Priority,
			&escalation.ApproverID,
			&escalation.ApproverComment,
			&escalation.RequestedAt,
			&escalation.ExpiresAt,
			&escalation.ResolvedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan escalation: %w", err)
		}

		json.Unmarshal(targetJSON, &escalation.Target)
		json.Unmarshal(contextJSON, &escalation.ContextSummary)
		escalation.PolicyNames = policyNames

		escalations = append(escalations, &escalation)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating escalations: %w", err)
	}

	return escalations, nil
}

// ListPending retrieves all pending escalation requests.
func (s *EscalationStore) ListPending(ctx context.Context, limit int) ([]*EscalationRequest, error) {
	return s.List(ctx, EscalationFilter{
		Status: "pending",
		Limit:  limit,
	})
}

// ResolveInput contains the input for resolving an escalation request.
type ResolveInput struct {
	ApproverID uuid.UUID
	Approved   bool
	Comment    string
}

// Resolve resolves an escalation request (approve or reject).
func (s *EscalationStore) Resolve(ctx context.Context, id uuid.UUID, input ResolveInput) (*EscalationRequest, error) {
	status := "rejected"
	if input.Approved {
		status = "approved"
	}

	query := `
		UPDATE escalation_requests
		SET status = $1, approver_id = $2, approver_comment = $3, resolved_at = NOW()
		WHERE id = $4 AND status = 'pending'`

	result, err := s.client.db.ExecContext(ctx, query, status, input.ApproverID, input.Comment, id)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve escalation: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: escalation %s not found or not pending", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// Cancel cancels a pending escalation request.
func (s *EscalationStore) Cancel(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE escalation_requests
		SET status = 'cancelled', resolved_at = NOW()
		WHERE id = $1 AND status = 'pending'`

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to cancel escalation: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: escalation %s not found or not pending", ErrNotFound, id)
	}

	return nil
}

// ExpireStale marks expired escalation requests as expired.
func (s *EscalationStore) ExpireStale(ctx context.Context) (int64, error) {
	query := `
		UPDATE escalation_requests
		SET status = 'expired', resolved_at = NOW()
		WHERE status = 'pending' AND expires_at < NOW()`

	result, err := s.client.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to expire escalations: %w", err)
	}

	return result.RowsAffected()
}

// CountByStatus returns counts of escalations by status.
func (s *EscalationStore) CountByStatus(ctx context.Context, sessionID string) (map[string]int64, error) {
	query := `
		SELECT status, COUNT(*)
		FROM escalation_requests
		WHERE session_id = $1
		GROUP BY status`

	rows, err := s.client.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to count escalations: %w", err)
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

// GetPendingCount returns the count of pending escalations.
func (s *EscalationStore) GetPendingCount(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM escalation_requests WHERE status = 'pending'`

	var count int64
	err := s.client.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count pending escalations: %w", err)
	}

	return count, nil
}
