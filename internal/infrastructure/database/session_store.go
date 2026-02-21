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

// Session represents an agent session in the database.
type Session struct {
	ID             string      `json:"id"`
	OrganizationID uuid.UUID   `json:"organization_id"`
	UserID         uuid.UUID   `json:"user_id"`
	Tool           string      `json:"tool"`
	TrustLevel     int         `json:"trust_level"`
	Capabilities   []string    `json:"capabilities"`
	Constraints    []string    `json:"constraints"`
	ServiceScope   []uuid.UUID `json:"service_scope"`
	Status         string      `json:"status"`
	StartedAt      time.Time   `json:"started_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
	LastHeartbeat  *time.Time  `json:"last_heartbeat,omitempty"`
	Metadata       Metadata    `json:"metadata,omitempty"`
}

// Metadata holds arbitrary key-value pairs for sessions.
type Metadata map[string]interface{}

// SessionStore provides CRUD operations for agent sessions.
type SessionStore struct {
	client *PostgresClient
}

// NewSessionStore creates a new session store.
func NewSessionStore(client *PostgresClient) *SessionStore {
	return &SessionStore{client: client}
}

// CreateSessionInput contains the input for creating a session.
type CreateSessionInput struct {
	ID             string
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Tool           string
	TrustLevel     int
	Capabilities   []string
	Constraints    []string
	ServiceScope   []uuid.UUID
	ExpiresAt      time.Time
	Metadata       Metadata
}

// Create creates a new session.
func (s *SessionStore) Create(ctx context.Context, input CreateSessionInput) (*Session, error) {
	// Validate input
	if input.ID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.TrustLevel < 1 || input.TrustLevel > 5 {
		return nil, fmt.Errorf("%w: trust level must be between 1 and 5", ErrInvalidInput)
	}
	if input.Tool == "" {
		return nil, fmt.Errorf("%w: tool is required", ErrInvalidInput)
	}

	// Default values
	if input.Capabilities == nil {
		input.Capabilities = []string{}
	}
	if input.Constraints == nil {
		input.Constraints = []string{}
	}
	if input.ServiceScope == nil {
		input.ServiceScope = []uuid.UUID{}
	}
	if input.Metadata == nil {
		input.Metadata = Metadata{}
	}

	capsJSON, err := json.Marshal(input.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}

	constraintsJSON, err := json.Marshal(input.Constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal constraints: %w", err)
	}

	metadataJSON, err := json.Marshal(input.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agent_sessions (
			id, organization_id, user_id, tool, trust_level,
			capabilities, constraints, service_scope, status, expires_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING started_at`

	var startedAt time.Time
	err = s.client.db.QueryRowContext(ctx, query,
		input.ID,
		input.OrganizationID,
		input.UserID,
		input.Tool,
		input.TrustLevel,
		capsJSON,
		constraintsJSON,
		pq.Array(input.ServiceScope),
		"active",
		input.ExpiresAt,
		metadataJSON,
	).Scan(&startedAt)

	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, fmt.Errorf("%w: session ID already exists", ErrDuplicateKey)
		}
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &Session{
		ID:             input.ID,
		OrganizationID: input.OrganizationID,
		UserID:         input.UserID,
		Tool:           input.Tool,
		TrustLevel:     input.TrustLevel,
		Capabilities:   input.Capabilities,
		Constraints:    input.Constraints,
		ServiceScope:   input.ServiceScope,
		Status:         "active",
		StartedAt:      startedAt,
		ExpiresAt:      input.ExpiresAt,
		Metadata:       input.Metadata,
	}, nil
}

// Get retrieves a session by ID.
func (s *SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	query := `
		SELECT id, organization_id, user_id, tool, trust_level,
			   capabilities, constraints, service_scope, status,
			   started_at, expires_at, last_heartbeat, metadata
		FROM agent_sessions
		WHERE id = $1`

	var session Session
	var capsJSON, constraintsJSON, metadataJSON []byte
	var serviceScope []uuid.UUID

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&session.ID,
		&session.OrganizationID,
		&session.UserID,
		&session.Tool,
		&session.TrustLevel,
		&capsJSON,
		&constraintsJSON,
		pq.Array(&serviceScope),
		&session.Status,
		&session.StartedAt,
		&session.ExpiresAt,
		&session.LastHeartbeat,
		&metadataJSON,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(capsJSON, &session.Capabilities); err != nil {
		session.Capabilities = []string{}
	}
	if err := json.Unmarshal(constraintsJSON, &session.Constraints); err != nil {
		session.Constraints = []string{}
	}
	if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
		session.Metadata = Metadata{}
	}
	session.ServiceScope = serviceScope

	return &session, nil
}

// SessionFilter defines criteria for listing sessions.
type SessionFilter struct {
	OrganizationID *uuid.UUID
	UserID         *uuid.UUID
	Tool           string
	Status         string
	MinTrustLevel  int
	MaxTrustLevel  int
	Limit          int
	Offset         int
}

// List retrieves sessions matching the filter criteria.
func (s *SessionStore) List(ctx context.Context, filter SessionFilter) ([]*Session, error) {
	query := `
		SELECT id, organization_id, user_id, tool, trust_level,
			   capabilities, constraints, service_scope, status,
			   started_at, expires_at, last_heartbeat, metadata
		FROM agent_sessions
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.OrganizationID != nil {
		query += fmt.Sprintf(" AND organization_id = $%d", argIdx)
		args = append(args, *filter.OrganizationID)
		argIdx++
	}
	if filter.UserID != nil {
		query += fmt.Sprintf(" AND user_id = $%d", argIdx)
		args = append(args, *filter.UserID)
		argIdx++
	}
	if filter.Tool != "" {
		query += fmt.Sprintf(" AND tool = $%d", argIdx)
		args = append(args, filter.Tool)
		argIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.MinTrustLevel > 0 {
		query += fmt.Sprintf(" AND trust_level >= $%d", argIdx)
		args = append(args, filter.MinTrustLevel)
		argIdx++
	}
	if filter.MaxTrustLevel > 0 {
		query += fmt.Sprintf(" AND trust_level <= $%d", argIdx)
		args = append(args, filter.MaxTrustLevel)
		argIdx++
	}

	query += " ORDER BY started_at DESC"

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
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		var capsJSON, constraintsJSON, metadataJSON []byte
		var serviceScope []uuid.UUID

		err := rows.Scan(
			&session.ID,
			&session.OrganizationID,
			&session.UserID,
			&session.Tool,
			&session.TrustLevel,
			&capsJSON,
			&constraintsJSON,
			pq.Array(&serviceScope),
			&session.Status,
			&session.StartedAt,
			&session.ExpiresAt,
			&session.LastHeartbeat,
			&metadataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if err := json.Unmarshal(capsJSON, &session.Capabilities); err != nil {
			session.Capabilities = []string{}
		}
		if err := json.Unmarshal(constraintsJSON, &session.Constraints); err != nil {
			session.Constraints = []string{}
		}
		if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
			session.Metadata = Metadata{}
		}
		session.ServiceScope = serviceScope

		sessions = append(sessions, &session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// UpdateSessionInput contains fields that can be updated on a session.
type UpdateSessionInput struct {
	TrustLevel   *int
	Capabilities []string
	Constraints  []string
	ServiceScope []uuid.UUID
	Status       string
	ExpiresAt    *time.Time
	Metadata     Metadata
}

// Update updates an existing session.
func (s *SessionStore) Update(ctx context.Context, id string, input UpdateSessionInput) (*Session, error) {
	// Build dynamic update query
	query := "UPDATE agent_sessions SET "
	args := []interface{}{}
	argIdx := 1
	updates := []string{}

	if input.TrustLevel != nil {
		if *input.TrustLevel < 1 || *input.TrustLevel > 5 {
			return nil, fmt.Errorf("%w: trust level must be between 1 and 5", ErrInvalidInput)
		}
		updates = append(updates, fmt.Sprintf("trust_level = $%d", argIdx))
		args = append(args, *input.TrustLevel)
		argIdx++
	}
	if input.Capabilities != nil {
		capsJSON, err := json.Marshal(input.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
		}
		updates = append(updates, fmt.Sprintf("capabilities = $%d", argIdx))
		args = append(args, capsJSON)
		argIdx++
	}
	if input.Constraints != nil {
		constraintsJSON, err := json.Marshal(input.Constraints)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal constraints: %w", err)
		}
		updates = append(updates, fmt.Sprintf("constraints = $%d", argIdx))
		args = append(args, constraintsJSON)
		argIdx++
	}
	if input.ServiceScope != nil {
		updates = append(updates, fmt.Sprintf("service_scope = $%d", argIdx))
		args = append(args, pq.Array(input.ServiceScope))
		argIdx++
	}
	if input.Status != "" {
		updates = append(updates, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, input.Status)
		argIdx++
	}
	if input.ExpiresAt != nil {
		updates = append(updates, fmt.Sprintf("expires_at = $%d", argIdx))
		args = append(args, *input.ExpiresAt)
		argIdx++
	}
	if input.Metadata != nil {
		metadataJSON, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		updates = append(updates, fmt.Sprintf("metadata = $%d", argIdx))
		args = append(args, metadataJSON)
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
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: session %s", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// Heartbeat updates the session's last heartbeat timestamp.
func (s *SessionStore) Heartbeat(ctx context.Context, id string) error {
	query := `
		UPDATE agent_sessions
		SET last_heartbeat = NOW()
		WHERE id = $1 AND status = 'active'`

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session %s not found or not active", ErrNotFound, id)
	}

	return nil
}

// End marks a session as ended.
func (s *SessionStore) End(ctx context.Context, id string) error {
	query := `
		UPDATE agent_sessions
		SET status = 'ended'
		WHERE id = $1 AND status = 'active'`

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session %s not found or already ended", ErrNotFound, id)
	}

	return nil
}

// Delete permanently removes a session.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM agent_sessions WHERE id = $1`

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session %s", ErrNotFound, id)
	}

	return nil
}

// ExpireStaleSessions marks sessions as expired based on their expiration time.
func (s *SessionStore) ExpireStaleSessions(ctx context.Context) (int64, error) {
	query := `
		UPDATE agent_sessions
		SET status = 'expired'
		WHERE status = 'active' AND expires_at < NOW()`

	result, err := s.client.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to expire sessions: %w", err)
	}

	return result.RowsAffected()
}

// CountByStatus returns counts of sessions by status for an organization.
func (s *SessionStore) CountByStatus(ctx context.Context, orgID uuid.UUID) (map[string]int64, error) {
	query := `
		SELECT status, COUNT(*)
		FROM agent_sessions
		WHERE organization_id = $1
		GROUP BY status`

	rows, err := s.client.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to count sessions: %w", err)
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

// isDuplicateKeyError checks if the error is a PostgreSQL duplicate key violation.
func isDuplicateKeyError(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505" // unique_violation
	}
	return false
}
