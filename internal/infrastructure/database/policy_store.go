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

// PolicyEvaluation represents a recorded policy evaluation.
type PolicyEvaluation struct {
	ID            uuid.UUID              `json:"id"`
	SessionID     string                 `json:"session_id"`
	PolicyName    string                 `json:"policy_name"`
	PolicyVersion string                 `json:"policy_version,omitempty"`
	Input         map[string]interface{} `json:"input"`
	Result        map[string]interface{} `json:"result"`
	Allowed       bool                   `json:"allowed"`
	DeniedReasons []string               `json:"denied_reasons,omitempty"`
	DurationMs    int                    `json:"duration_ms"`
	CreatedAt     time.Time              `json:"created_at"`
}

// PolicyStore provides operations for policy evaluation records.
type PolicyStore struct {
	client *PostgresClient
}

// NewPolicyStore creates a new policy store.
func NewPolicyStore(client *PostgresClient) *PolicyStore {
	return &PolicyStore{client: client}
}

// CreatePolicyEvaluationInput contains the input for creating a policy evaluation.
type CreatePolicyEvaluationInput struct {
	SessionID     string
	PolicyName    string
	PolicyVersion string
	Input         map[string]interface{}
	Result        map[string]interface{}
	Allowed       bool
	DeniedReasons []string
	DurationMs    int
}

// Create creates a new policy evaluation record.
func (s *PolicyStore) Create(ctx context.Context, input CreatePolicyEvaluationInput) (*PolicyEvaluation, error) {
	// Validate required fields
	if input.SessionID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.PolicyName == "" {
		return nil, fmt.Errorf("%w: policy name is required", ErrInvalidInput)
	}

	// Default values
	if input.Input == nil {
		input.Input = map[string]interface{}{}
	}
	if input.Result == nil {
		input.Result = map[string]interface{}{}
	}
	if input.DeniedReasons == nil {
		input.DeniedReasons = []string{}
	}

	inputJSON, err := json.Marshal(input.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	resultJSON, err := json.Marshal(input.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	query := `
		INSERT INTO policy_evaluations (
			session_id, policy_name, policy_version, input, result,
			allowed, denied_reasons, duration_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`

	var evaluation PolicyEvaluation
	err = s.client.db.QueryRowContext(ctx, query,
		input.SessionID,
		input.PolicyName,
		input.PolicyVersion,
		inputJSON,
		resultJSON,
		input.Allowed,
		pq.Array(input.DeniedReasons),
		input.DurationMs,
	).Scan(&evaluation.ID, &evaluation.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create policy evaluation: %w", err)
	}

	evaluation.SessionID = input.SessionID
	evaluation.PolicyName = input.PolicyName
	evaluation.PolicyVersion = input.PolicyVersion
	evaluation.Input = input.Input
	evaluation.Result = input.Result
	evaluation.Allowed = input.Allowed
	evaluation.DeniedReasons = input.DeniedReasons
	evaluation.DurationMs = input.DurationMs

	return &evaluation, nil
}

// Get retrieves a policy evaluation by ID.
func (s *PolicyStore) Get(ctx context.Context, id uuid.UUID) (*PolicyEvaluation, error) {
	query := `
		SELECT id, session_id, policy_name, policy_version, input, result,
			   allowed, denied_reasons, duration_ms, created_at
		FROM policy_evaluations
		WHERE id = $1`

	var evaluation PolicyEvaluation
	var inputJSON, resultJSON []byte
	var deniedReasons pq.StringArray
	var policyVersion sql.NullString

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&evaluation.ID,
		&evaluation.SessionID,
		&evaluation.PolicyName,
		&policyVersion,
		&inputJSON,
		&resultJSON,
		&evaluation.Allowed,
		&deniedReasons,
		&evaluation.DurationMs,
		&evaluation.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: policy evaluation %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get policy evaluation: %w", err)
	}

	json.Unmarshal(inputJSON, &evaluation.Input)
	json.Unmarshal(resultJSON, &evaluation.Result)
	evaluation.DeniedReasons = deniedReasons
	if policyVersion.Valid {
		evaluation.PolicyVersion = policyVersion.String
	}

	return &evaluation, nil
}

// PolicyEvaluationFilter defines criteria for listing policy evaluations.
type PolicyEvaluationFilter struct {
	SessionID  string
	PolicyName string
	Allowed    *bool
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// List retrieves policy evaluations matching the filter criteria.
func (s *PolicyStore) List(ctx context.Context, filter PolicyEvaluationFilter) ([]*PolicyEvaluation, error) {
	query := `
		SELECT id, session_id, policy_name, policy_version, input, result,
			   allowed, denied_reasons, duration_ms, created_at
		FROM policy_evaluations
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", argIdx)
		args = append(args, filter.SessionID)
		argIdx++
	}
	if filter.PolicyName != "" {
		query += fmt.Sprintf(" AND policy_name = $%d", argIdx)
		args = append(args, filter.PolicyName)
		argIdx++
	}
	if filter.Allowed != nil {
		query += fmt.Sprintf(" AND allowed = $%d", argIdx)
		args = append(args, *filter.Allowed)
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
		return nil, fmt.Errorf("failed to list policy evaluations: %w", err)
	}
	defer rows.Close()

	var evaluations []*PolicyEvaluation
	for rows.Next() {
		var evaluation PolicyEvaluation
		var inputJSON, resultJSON []byte
		var deniedReasons pq.StringArray
		var policyVersion sql.NullString

		err := rows.Scan(
			&evaluation.ID,
			&evaluation.SessionID,
			&evaluation.PolicyName,
			&policyVersion,
			&inputJSON,
			&resultJSON,
			&evaluation.Allowed,
			&deniedReasons,
			&evaluation.DurationMs,
			&evaluation.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan policy evaluation: %w", err)
		}

		json.Unmarshal(inputJSON, &evaluation.Input)
		json.Unmarshal(resultJSON, &evaluation.Result)
		evaluation.DeniedReasons = deniedReasons
		if policyVersion.Valid {
			evaluation.PolicyVersion = policyVersion.String
		}

		evaluations = append(evaluations, &evaluation)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating policy evaluations: %w", err)
	}

	return evaluations, nil
}

// GetStatsByPolicy returns statistics for a specific policy.
func (s *PolicyStore) GetStatsByPolicy(ctx context.Context, policyName string, since time.Time) (*PolicyStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE allowed = true) as allowed,
			COUNT(*) FILTER (WHERE allowed = false) as denied,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms), 0) as p99_duration_ms
		FROM policy_evaluations
		WHERE policy_name = $1 AND created_at >= $2`

	var stats PolicyStats
	stats.PolicyName = policyName

	err := s.client.db.QueryRowContext(ctx, query, policyName, since).Scan(
		&stats.TotalEvaluations,
		&stats.AllowedCount,
		&stats.DeniedCount,
		&stats.AvgDurationMs,
		&stats.P99DurationMs,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get policy stats: %w", err)
	}

	if stats.TotalEvaluations > 0 {
		stats.AllowRate = float64(stats.AllowedCount) / float64(stats.TotalEvaluations)
		stats.DenyRate = float64(stats.DeniedCount) / float64(stats.TotalEvaluations)
	}

	return &stats, nil
}

// PolicyStats holds statistics for policy evaluations.
type PolicyStats struct {
	PolicyName       string  `json:"policy_name"`
	TotalEvaluations int64   `json:"total_evaluations"`
	AllowedCount     int64   `json:"allowed_count"`
	DeniedCount      int64   `json:"denied_count"`
	AllowRate        float64 `json:"allow_rate"`
	DenyRate         float64 `json:"deny_rate"`
	AvgDurationMs    float64 `json:"avg_duration_ms"`
	P99DurationMs    float64 `json:"p99_duration_ms"`
}

// GetAllPolicyStats returns statistics for all policies.
func (s *PolicyStore) GetAllPolicyStats(ctx context.Context, since time.Time) ([]*PolicyStats, error) {
	query := `
		SELECT
			policy_name,
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE allowed = true) as allowed,
			COUNT(*) FILTER (WHERE allowed = false) as denied,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM policy_evaluations
		WHERE created_at >= $1
		GROUP BY policy_name
		ORDER BY total DESC`

	rows, err := s.client.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get all policy stats: %w", err)
	}
	defer rows.Close()

	var stats []*PolicyStats
	for rows.Next() {
		var s PolicyStats
		err := rows.Scan(
			&s.PolicyName,
			&s.TotalEvaluations,
			&s.AllowedCount,
			&s.DeniedCount,
			&s.AvgDurationMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan policy stats: %w", err)
		}

		if s.TotalEvaluations > 0 {
			s.AllowRate = float64(s.AllowedCount) / float64(s.TotalEvaluations)
			s.DenyRate = float64(s.DeniedCount) / float64(s.TotalEvaluations)
		}

		stats = append(stats, &s)
	}

	return stats, rows.Err()
}

// CountBySession returns the count of policy evaluations for a session.
func (s *PolicyStore) CountBySession(ctx context.Context, sessionID string) (map[bool]int64, error) {
	query := `
		SELECT allowed, COUNT(*)
		FROM policy_evaluations
		WHERE session_id = $1
		GROUP BY allowed`

	rows, err := s.client.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to count evaluations: %w", err)
	}
	defer rows.Close()

	counts := make(map[bool]int64)
	for rows.Next() {
		var allowed bool
		var count int64
		if err := rows.Scan(&allowed, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[allowed] = count
	}

	return counts, rows.Err()
}
