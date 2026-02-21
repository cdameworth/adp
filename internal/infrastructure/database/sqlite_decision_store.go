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

// SQLiteDecisionStore implements store.DecisionStore backed by SQLite.
type SQLiteDecisionStore struct {
	client *SQLiteClient
}

// NewSQLiteDecisionStore creates a new SQLite-backed decision store.
func NewSQLiteDecisionStore(client *SQLiteClient) *SQLiteDecisionStore {
	return &SQLiteDecisionStore{client: client}
}

// Create inserts a new decision record into SQLite.
func (s *SQLiteDecisionStore) Create(ctx context.Context, input store.CreateDecisionInput) (*store.DecisionRecord, error) {
	// Validate required fields.
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.DecisionType == "" {
		return nil, fmt.Errorf("%w: decision type is required", ErrInvalidInput)
	}
	if input.Action == "" {
		return nil, fmt.Errorf("%w: action is required", ErrInvalidInput)
	}

	// Default confidence to 0.8 when not provided.
	if input.Confidence == 0 {
		input.Confidence = 0.8
	}

	// Default nil maps and slices to empty values for consistent JSON.
	if input.Target == nil {
		input.Target = map[string]any{}
	}
	if input.Reasoning == nil {
		input.Reasoning = map[string]any{}
	}
	if input.Alternatives == nil {
		input.Alternatives = []store.Alternative{}
	}
	if input.ContextSnapshot == nil {
		input.ContextSnapshot = map[string]any{}
	}

	targetJSON, err := json.Marshal(input.Target)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal target: %w", err)
	}

	reasoningJSON, err := json.Marshal(input.Reasoning)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
	}

	alternativesJSON, err := json.Marshal(input.Alternatives)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alternatives: %w", err)
	}

	contextJSON, err := json.Marshal(input.ContextSnapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal context snapshot: %w", err)
	}

	var policyResultStr sql.NullString
	if input.PolicyResult != nil {
		prJSON, err := json.Marshal(input.PolicyResult)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal policy result: %w", err)
		}
		policyResultStr = sql.NullString{String: string(prJSON), Valid: true}
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	createdAt := now.Format(time.RFC3339Nano)

	query := `
		INSERT INTO decision_records (
			id, session_id, decision_type, action, target, reasoning,
			confidence, alternatives, context_snapshot, policy_result,
			status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.client.DB().ExecContext(ctx, query,
		id,
		input.SessionID,
		input.DecisionType,
		input.Action,
		string(targetJSON),
		string(reasoningJSON),
		input.Confidence,
		string(alternativesJSON),
		string(contextJSON),
		policyResultStr,
		"pending",
		createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create decision: %w", err)
	}

	return &store.DecisionRecord{
		ID:              id,
		SessionID:       input.SessionID,
		DecisionType:    input.DecisionType,
		Action:          input.Action,
		Target:          input.Target,
		Reasoning:       input.Reasoning,
		Confidence:      input.Confidence,
		Alternatives:    input.Alternatives,
		ContextSnapshot: input.ContextSnapshot,
		PolicyResult:    input.PolicyResult,
		Status:          "pending",
		CreatedAt:       now,
	}, nil
}

// Get retrieves a decision record by ID.
func (s *SQLiteDecisionStore) Get(ctx context.Context, id string) (*store.DecisionRecord, error) {
	query := `
		SELECT id, session_id, decision_type, action, target, reasoning,
			   confidence, alternatives, context_snapshot, policy_result,
			   status, outcome, created_at
		FROM decision_records
		WHERE id = ?`

	var record store.DecisionRecord
	var targetStr, reasoningStr, alternativesStr, contextStr string
	var policyResultStr, outcomeStr sql.NullString
	var createdAtStr string

	err := s.client.DB().QueryRowContext(ctx, query, id).Scan(
		&record.ID,
		&record.SessionID,
		&record.DecisionType,
		&record.Action,
		&targetStr,
		&reasoningStr,
		&record.Confidence,
		&alternativesStr,
		&contextStr,
		&policyResultStr,
		&record.Status,
		&outcomeStr,
		&createdAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: decision %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get decision: %w", err)
	}

	// Parse timestamp.
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)

	// Unmarshal JSON text fields.
	if err := json.Unmarshal([]byte(targetStr), &record.Target); err != nil {
		record.Target = map[string]any{}
	}
	if err := json.Unmarshal([]byte(reasoningStr), &record.Reasoning); err != nil {
		record.Reasoning = map[string]any{}
	}
	if err := json.Unmarshal([]byte(alternativesStr), &record.Alternatives); err != nil {
		record.Alternatives = []store.Alternative{}
	}
	if err := json.Unmarshal([]byte(contextStr), &record.ContextSnapshot); err != nil {
		record.ContextSnapshot = map[string]any{}
	}
	if policyResultStr.Valid && policyResultStr.String != "" {
		var pr store.PolicyResult
		if err := json.Unmarshal([]byte(policyResultStr.String), &pr); err == nil {
			record.PolicyResult = &pr
		}
	}
	if outcomeStr.Valid && outcomeStr.String != "" {
		json.Unmarshal([]byte(outcomeStr.String), &record.Outcome)
	}

	return &record, nil
}

// GetLineage walks the parent chain starting from the given decision ID.
// It follows the context_snapshot.parent_decision_id link up to depth ancestors.
// Returns the chain starting with the given decision, then its parent, and so on.
func (s *SQLiteDecisionStore) GetLineage(ctx context.Context, id string, depth int) ([]*store.DecisionRecord, error) {
	// Retrieve the starting decision.
	record, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	lineage := []*store.DecisionRecord{record}

	current := record
	for i := 0; i < depth && current != nil; i++ {
		parentIDStr, ok := current.ContextSnapshot["parent_decision_id"].(string)
		if !ok || parentIDStr == "" {
			break
		}

		parent, err := s.Get(ctx, parentIDStr)
		if err != nil {
			// Parent may have been deleted or is missing; stop walking.
			break
		}

		lineage = append(lineage, parent)
		current = parent
	}

	return lineage, nil
}

// ListBySession retrieves all decisions for a session in chronological order.
func (s *SQLiteDecisionStore) ListBySession(ctx context.Context, sessionID string) ([]*store.DecisionRecord, error) {
	query := `
		SELECT id, session_id, decision_type, action, target, reasoning,
			   confidence, alternatives, context_snapshot, policy_result,
			   status, outcome, created_at
		FROM decision_records
		WHERE session_id = ?
		ORDER BY created_at ASC`

	rows, err := s.client.DB().QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list decisions by session: %w", err)
	}
	defer rows.Close()

	var records []*store.DecisionRecord
	for rows.Next() {
		record, err := s.scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating decisions: %w", err)
	}

	return records, nil
}

// SQLiteDecisionFilter defines filter options for listing decisions.
type SQLiteDecisionFilter struct {
	SessionID    string
	DecisionType string
	Status       string
	Limit        int
	Offset       int
}

// List returns decision records matching the given filter criteria.
func (s *SQLiteDecisionStore) List(ctx context.Context, filter SQLiteDecisionFilter) ([]*store.DecisionRecord, error) {
	query := `SELECT id, session_id, decision_type, action, target, reasoning,
		confidence, alternatives, context_snapshot, policy_result,
		status, outcome, created_at
	FROM decision_records WHERE 1=1`

	var args []any

	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}
	if filter.DecisionType != "" {
		query += " AND decision_type = ?"
		args = append(args, filter.DecisionType)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}

	query += " ORDER BY created_at DESC"

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
		return nil, fmt.Errorf("failed to list decisions: %w", err)
	}
	defer rows.Close()

	var records []*store.DecisionRecord
	for rows.Next() {
		record, err := s.scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// scanDecision reads a single row from the result set into a store.DecisionRecord.
func (s *SQLiteDecisionStore) scanDecision(rows *sql.Rows) (*store.DecisionRecord, error) {
	var record store.DecisionRecord
	var targetStr, reasoningStr, alternativesStr, contextStr string
	var policyResultStr, outcomeStr sql.NullString
	var createdAtStr string

	err := rows.Scan(
		&record.ID,
		&record.SessionID,
		&record.DecisionType,
		&record.Action,
		&targetStr,
		&reasoningStr,
		&record.Confidence,
		&alternativesStr,
		&contextStr,
		&policyResultStr,
		&record.Status,
		&outcomeStr,
		&createdAtStr,
	)
	if err != nil {
		return nil, err
	}

	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)

	if err := json.Unmarshal([]byte(targetStr), &record.Target); err != nil {
		record.Target = map[string]any{}
	}
	if err := json.Unmarshal([]byte(reasoningStr), &record.Reasoning); err != nil {
		record.Reasoning = map[string]any{}
	}
	if err := json.Unmarshal([]byte(alternativesStr), &record.Alternatives); err != nil {
		record.Alternatives = []store.Alternative{}
	}
	if err := json.Unmarshal([]byte(contextStr), &record.ContextSnapshot); err != nil {
		record.ContextSnapshot = map[string]any{}
	}
	if policyResultStr.Valid && policyResultStr.String != "" {
		var pr store.PolicyResult
		if err := json.Unmarshal([]byte(policyResultStr.String), &pr); err == nil {
			record.PolicyResult = &pr
		}
	}
	if outcomeStr.Valid && outcomeStr.String != "" {
		json.Unmarshal([]byte(outcomeStr.String), &record.Outcome)
	}

	return &record, nil
}
