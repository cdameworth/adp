package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DecisionRecord represents a decision made by an agent, stored in the database.
type DecisionRecord struct {
	ID              uuid.UUID              `json:"id"`
	SessionID       string                 `json:"session_id"`
	DecisionType    string                 `json:"decision_type"`
	Action          string                 `json:"action"`
	Target          map[string]interface{} `json:"target"`
	Reasoning       map[string]interface{} `json:"reasoning"`
	Confidence      float64                `json:"confidence"`
	Alternatives    []Alternative          `json:"alternatives"`
	ContextSnapshot map[string]interface{} `json:"context_snapshot"`
	PolicyResult    *PolicyResult          `json:"policy_result,omitempty"`
	Status          string                 `json:"status"`
	Outcome         map[string]interface{} `json:"outcome,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}

// Alternative represents an alternative action considered by the agent.
type Alternative struct {
	Action     string  `json:"action"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// PolicyResult represents the result of policy evaluation.
type PolicyResult struct {
	Allowed       bool     `json:"allowed"`
	DeniedReasons []string `json:"denied_reasons,omitempty"`
	PolicyNames   []string `json:"policy_names"`
	EvaluatedAt   string   `json:"evaluated_at"`
}

// DecisionStore provides CRUD operations for decision records.
type DecisionStore struct {
	client *PostgresClient
}

// NewDecisionStore creates a new decision store.
func NewDecisionStore(client *PostgresClient) *DecisionStore {
	return &DecisionStore{client: client}
}

// CreateDecisionInput contains the input for creating a decision record.
type CreateDecisionInput struct {
	SessionID       string
	DecisionType    string
	Action          string
	Target          map[string]interface{}
	Reasoning       map[string]interface{}
	Confidence      float64
	Alternatives    []Alternative
	ContextSnapshot map[string]interface{}
	PolicyResult    *PolicyResult
}

// Create creates a new decision record.
func (s *DecisionStore) Create(ctx context.Context, input CreateDecisionInput) (*DecisionRecord, error) {
	// Validate required fields
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.DecisionType == "" {
		return nil, fmt.Errorf("%w: decision type is required", ErrInvalidInput)
	}
	if input.Action == "" {
		return nil, fmt.Errorf("%w: action is required", ErrInvalidInput)
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return nil, fmt.Errorf("%w: confidence must be between 0 and 1", ErrInvalidInput)
	}

	// Default values
	if input.Target == nil {
		input.Target = map[string]interface{}{}
	}
	if input.Reasoning == nil {
		input.Reasoning = map[string]interface{}{}
	}
	if input.Alternatives == nil {
		input.Alternatives = []Alternative{}
	}
	if input.ContextSnapshot == nil {
		input.ContextSnapshot = map[string]interface{}{}
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

	var policyResultJSON []byte
	if input.PolicyResult != nil {
		policyResultJSON, err = json.Marshal(input.PolicyResult)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal policy result: %w", err)
		}
	}

	query := `
		INSERT INTO decision_records (
			session_id, decision_type, action, target, reasoning,
			confidence, alternatives, context_snapshot, policy_result, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`

	var record DecisionRecord
	err = s.client.db.QueryRowContext(ctx, query,
		input.SessionID,
		input.DecisionType,
		input.Action,
		targetJSON,
		reasoningJSON,
		input.Confidence,
		alternativesJSON,
		contextJSON,
		policyResultJSON,
		"pending",
	).Scan(&record.ID, &record.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create decision: %w", err)
	}

	record.SessionID = input.SessionID
	record.DecisionType = input.DecisionType
	record.Action = input.Action
	record.Target = input.Target
	record.Reasoning = input.Reasoning
	record.Confidence = input.Confidence
	record.Alternatives = input.Alternatives
	record.ContextSnapshot = input.ContextSnapshot
	record.PolicyResult = input.PolicyResult
	record.Status = "pending"

	return &record, nil
}

// Get retrieves a decision record by ID.
func (s *DecisionStore) Get(ctx context.Context, id uuid.UUID) (*DecisionRecord, error) {
	query := `
		SELECT id, session_id, decision_type, action, target, reasoning,
			   confidence, alternatives, context_snapshot, policy_result,
			   status, outcome, created_at
		FROM decision_records
		WHERE id = $1`

	var record DecisionRecord
	var targetJSON, reasoningJSON, alternativesJSON, contextJSON, policyResultJSON, outcomeJSON []byte

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&record.ID,
		&record.SessionID,
		&record.DecisionType,
		&record.Action,
		&targetJSON,
		&reasoningJSON,
		&record.Confidence,
		&alternativesJSON,
		&contextJSON,
		&policyResultJSON,
		&record.Status,
		&outcomeJSON,
		&record.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: decision %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get decision: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(targetJSON, &record.Target); err != nil {
		record.Target = map[string]interface{}{}
	}
	if err := json.Unmarshal(reasoningJSON, &record.Reasoning); err != nil {
		record.Reasoning = map[string]interface{}{}
	}
	if err := json.Unmarshal(alternativesJSON, &record.Alternatives); err != nil {
		record.Alternatives = []Alternative{}
	}
	if err := json.Unmarshal(contextJSON, &record.ContextSnapshot); err != nil {
		record.ContextSnapshot = map[string]interface{}{}
	}
	if policyResultJSON != nil {
		var pr PolicyResult
		if err := json.Unmarshal(policyResultJSON, &pr); err == nil {
			record.PolicyResult = &pr
		}
	}
	if outcomeJSON != nil {
		json.Unmarshal(outcomeJSON, &record.Outcome)
	}

	return &record, nil
}

// DecisionFilter defines criteria for listing decisions.
type DecisionFilter struct {
	SessionID     string
	DecisionType  string
	Status        string
	MinConfidence float64
	MaxConfidence float64
	Since         *time.Time
	Until         *time.Time
	Limit         int
	Offset        int
}

// List retrieves decision records matching the filter criteria.
func (s *DecisionStore) List(ctx context.Context, filter DecisionFilter) ([]*DecisionRecord, error) {
	query := `
		SELECT id, session_id, decision_type, action, target, reasoning,
			   confidence, alternatives, context_snapshot, policy_result,
			   status, outcome, created_at
		FROM decision_records
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", argIdx)
		args = append(args, filter.SessionID)
		argIdx++
	}
	if filter.DecisionType != "" {
		query += fmt.Sprintf(" AND decision_type = $%d", argIdx)
		args = append(args, filter.DecisionType)
		argIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.MinConfidence > 0 {
		query += fmt.Sprintf(" AND confidence >= $%d", argIdx)
		args = append(args, filter.MinConfidence)
		argIdx++
	}
	if filter.MaxConfidence > 0 {
		query += fmt.Sprintf(" AND confidence <= $%d", argIdx)
		args = append(args, filter.MaxConfidence)
		argIdx++
	}
	if filter.Since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filter.Until)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

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
		return nil, fmt.Errorf("failed to list decisions: %w", err)
	}
	defer rows.Close()

	var records []*DecisionRecord
	for rows.Next() {
		var record DecisionRecord
		var targetJSON, reasoningJSON, alternativesJSON, contextJSON, policyResultJSON, outcomeJSON []byte

		err := rows.Scan(
			&record.ID,
			&record.SessionID,
			&record.DecisionType,
			&record.Action,
			&targetJSON,
			&reasoningJSON,
			&record.Confidence,
			&alternativesJSON,
			&contextJSON,
			&policyResultJSON,
			&record.Status,
			&outcomeJSON,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision: %w", err)
		}

		// Unmarshal JSON fields
		json.Unmarshal(targetJSON, &record.Target)
		json.Unmarshal(reasoningJSON, &record.Reasoning)
		json.Unmarshal(alternativesJSON, &record.Alternatives)
		json.Unmarshal(contextJSON, &record.ContextSnapshot)
		if policyResultJSON != nil {
			var pr PolicyResult
			if err := json.Unmarshal(policyResultJSON, &pr); err == nil {
				record.PolicyResult = &pr
			}
		}
		if outcomeJSON != nil {
			json.Unmarshal(outcomeJSON, &record.Outcome)
		}

		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating decisions: %w", err)
	}

	return records, nil
}

// UpdateDecisionInput contains fields that can be updated on a decision.
type UpdateDecisionInput struct {
	Status       string
	Outcome      map[string]interface{}
	PolicyResult *PolicyResult
}

// Update updates an existing decision record.
func (s *DecisionStore) Update(ctx context.Context, id uuid.UUID, input UpdateDecisionInput) (*DecisionRecord, error) {
	query := "UPDATE decision_records SET "
	args := []interface{}{}
	argIdx := 1
	updates := []string{}

	if input.Status != "" {
		updates = append(updates, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, input.Status)
		argIdx++
	}
	if input.Outcome != nil {
		outcomeJSON, err := json.Marshal(input.Outcome)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal outcome: %w", err)
		}
		updates = append(updates, fmt.Sprintf("outcome = $%d", argIdx))
		args = append(args, outcomeJSON)
		argIdx++
	}
	if input.PolicyResult != nil {
		policyResultJSON, err := json.Marshal(input.PolicyResult)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal policy result: %w", err)
		}
		updates = append(updates, fmt.Sprintf("policy_result = $%d", argIdx))
		args = append(args, policyResultJSON)
		argIdx++
	}

	if len(updates) == 0 {
		return s.Get(ctx, id)
	}

	query += updates[0]
	for _, u := range updates[1:] {
		query += ", " + u
	}
	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	result, err := s.client.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update decision: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: decision %s", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// ListBySession retrieves all decisions for a session in chronological order.
func (s *DecisionStore) ListBySession(ctx context.Context, sessionID string) ([]*DecisionRecord, error) {
	return s.List(ctx, DecisionFilter{
		SessionID: sessionID,
		Limit:     1000,
	})
}

// CountByStatus returns counts of decisions by status for a session.
func (s *DecisionStore) CountByStatus(ctx context.Context, sessionID string) (map[string]int64, error) {
	query := `
		SELECT status, COUNT(*)
		FROM decision_records
		WHERE session_id = $1
		GROUP BY status`

	rows, err := s.client.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to count decisions: %w", err)
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

// GetLineage retrieves the decision chain leading to a specific decision.
// This requires decisions to be linked via the context_snapshot field.
func (s *DecisionStore) GetLineage(ctx context.Context, id uuid.UUID, depth int) ([]*DecisionRecord, error) {
	// First get the target decision
	record, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	lineage := []*DecisionRecord{record}

	// Look for parent decision in context_snapshot
	current := record
	for i := 0; i < depth && current != nil; i++ {
		parentIDStr, ok := current.ContextSnapshot["parent_decision_id"].(string)
		if !ok || parentIDStr == "" {
			break
		}

		parentID, err := uuid.Parse(parentIDStr)
		if err != nil {
			break
		}

		parent, err := s.Get(ctx, parentID)
		if err != nil {
			break
		}

		lineage = append(lineage, parent)
		current = parent
	}

	return lineage, nil
}
