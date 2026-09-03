package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adp/adp/internal/store"
)

// SQLiteSessionStore implements store.SessionStore backed by SQLite.
type SQLiteSessionStore struct {
	client *SQLiteClient
}

// NewSQLiteSessionStore creates a new SQLite-backed session store.
func NewSQLiteSessionStore(client *SQLiteClient) *SQLiteSessionStore {
	return &SQLiteSessionStore{client: client}
}

// Create creates a new agent session.
func (s *SQLiteSessionStore) Create(ctx context.Context, input store.CreateSessionInput) (*store.Session, error) {
	if input.ID == "" {
		return nil, fmt.Errorf("%w: session ID is required", ErrInvalidInput)
	}
	if input.TrustLevel < 1 || input.TrustLevel > 5 {
		return nil, fmt.Errorf("%w: trust level must be between 1 and 5", ErrInvalidInput)
	}
	if input.Tool == "" {
		return nil, fmt.Errorf("%w: tool is required", ErrInvalidInput)
	}

	if input.Capabilities == nil {
		input.Capabilities = []string{}
	}
	if input.Constraints == nil {
		input.Constraints = []string{}
	}
	if input.ServiceScope == nil {
		input.ServiceScope = []string{}
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}

	capsJSON, err := json.Marshal(input.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	constraintsJSON, err := json.Marshal(input.Constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal constraints: %w", err)
	}
	scopeJSON, err := json.Marshal(input.ServiceScope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service scope: %w", err)
	}
	metadataJSON, err := json.Marshal(input.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	expiresStr := input.ExpiresAt.UTC().Format(time.RFC3339Nano)

	query := `INSERT INTO agent_sessions (
		id, organization_id, user_id, tool, trust_level,
		capabilities, constraints, service_scope, status, started_at, expires_at, metadata, token_hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.client.db.ExecContext(ctx, query,
		input.ID,
		input.OrganizationID,
		input.UserID,
		input.Tool,
		input.TrustLevel,
		string(capsJSON),
		string(constraintsJSON),
		string(scopeJSON),
		"active",
		nowStr,
		expiresStr,
		string(metadataJSON),
		input.TokenHash,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &store.Session{
		ID:             input.ID,
		OrganizationID: input.OrganizationID,
		UserID:         input.UserID,
		Tool:           input.Tool,
		TrustLevel:     input.TrustLevel,
		Capabilities:   input.Capabilities,
		Constraints:    input.Constraints,
		ServiceScope:   input.ServiceScope,
		Status:         "active",
		StartedAt:      now,
		ExpiresAt:      input.ExpiresAt,
		Metadata:       input.Metadata,
		TokenHash:      input.TokenHash,
	}, nil
}

// Get retrieves a session by ID.
func (s *SQLiteSessionStore) Get(ctx context.Context, id string) (*store.Session, error) {
	query := `SELECT id, organization_id, user_id, tool, trust_level,
		capabilities, constraints, service_scope, status, started_at, expires_at,
		last_heartbeat, metadata
	FROM agent_sessions WHERE id = ?`

	var session store.Session
	var capsJSON, constraintsJSON, scopeJSON, metadataJSON string
	var startedAtStr, expiresAtStr string
	var lastHeartbeatStr sql.NullString

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&session.ID,
		&session.OrganizationID,
		&session.UserID,
		&session.Tool,
		&session.TrustLevel,
		&capsJSON,
		&constraintsJSON,
		&scopeJSON,
		&session.Status,
		&startedAtStr,
		&expiresAtStr,
		&lastHeartbeatStr,
		&metadataJSON,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if err := json.Unmarshal([]byte(capsJSON), &session.Capabilities); err != nil {
		session.Capabilities = []string{}
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &session.Constraints); err != nil {
		session.Constraints = []string{}
	}
	if err := json.Unmarshal([]byte(scopeJSON), &session.ServiceScope); err != nil {
		session.ServiceScope = []string{}
	}
	if err := json.Unmarshal([]byte(metadataJSON), &session.Metadata); err != nil {
		session.Metadata = map[string]any{}
	}

	session.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAtStr)
	if lastHeartbeatStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastHeartbeatStr.String)
		if err == nil {
			session.LastHeartbeat = &t
		}
	}

	return &session, nil
}

// Heartbeat updates the last heartbeat time for a session.
func (s *SQLiteSessionStore) Heartbeat(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.client.db.ExecContext(ctx,
		"UPDATE agent_sessions SET last_heartbeat = ? WHERE id = ? AND status = 'active'",
		now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// End marks a session as ended.
func (s *SQLiteSessionStore) End(ctx context.Context, id string) error {
	result, err := s.client.db.ExecContext(ctx,
		"UPDATE agent_sessions SET status = 'ended' WHERE id = ? AND status = 'active'",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListEnded returns sessions with status "ended" after a given cursor ID, ordered by started_at.
func (s *SQLiteSessionStore) ListEnded(ctx context.Context, afterID string, limit int) ([]*store.Session, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if afterID == "" {
		rows, err = s.client.db.QueryContext(ctx,
			`SELECT id, organization_id, user_id, tool, trust_level,
				capabilities, constraints, service_scope, status, started_at, expires_at,
				last_heartbeat, metadata
			FROM agent_sessions
			WHERE status = 'ended'
			ORDER BY started_at ASC
			LIMIT ?`, limit,
		)
	} else {
		rows, err = s.client.db.QueryContext(ctx,
			`SELECT id, organization_id, user_id, tool, trust_level,
				capabilities, constraints, service_scope, status, started_at, expires_at,
				last_heartbeat, metadata
			FROM agent_sessions
			WHERE status = 'ended' AND id > ?
			ORDER BY started_at ASC
			LIMIT ?`, afterID, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list ended sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*store.Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// SQLiteSessionFilter defines filter options for listing sessions.
type SQLiteSessionFilter struct {
	OrganizationID string
	UserID         string
	Tool           string
	Status         string
	MinTrustLevel  int
	MaxTrustLevel  int
	Limit          int
	Offset         int
}

// SQLiteUpdateSessionInput defines fields that can be updated on a session.
type SQLiteUpdateSessionInput struct {
	TrustLevel   *int
	Capabilities []string
	Constraints  []string
	Status       string
}

// List returns sessions matching the given filter criteria.
func (s *SQLiteSessionStore) List(ctx context.Context, filter SQLiteSessionFilter) ([]*store.Session, error) {
	query := `SELECT id, organization_id, user_id, tool, trust_level,
		capabilities, constraints, service_scope, status, started_at, expires_at,
		last_heartbeat, metadata
	FROM agent_sessions WHERE 1=1`

	var args []any

	if filter.OrganizationID != "" {
		query += " AND organization_id = ?"
		args = append(args, filter.OrganizationID)
	}
	if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.Tool != "" {
		query += " AND tool = ?"
		args = append(args, filter.Tool)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.MinTrustLevel > 0 {
		query += " AND trust_level >= ?"
		args = append(args, filter.MinTrustLevel)
	}
	if filter.MaxTrustLevel > 0 {
		query += " AND trust_level <= ?"
		args = append(args, filter.MaxTrustLevel)
	}

	query += " ORDER BY started_at DESC"

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

	rows, err := s.client.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*store.Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// Update modifies an existing session.
func (s *SQLiteSessionStore) Update(ctx context.Context, id string, input SQLiteUpdateSessionInput) (*store.Session, error) {
	var setClauses []string
	var args []any

	if input.TrustLevel != nil {
		setClauses = append(setClauses, "trust_level = ?")
		args = append(args, *input.TrustLevel)
	}
	if input.Capabilities != nil {
		capsJSON, err := json.Marshal(input.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
		}
		setClauses = append(setClauses, "capabilities = ?")
		args = append(args, string(capsJSON))
	}
	if input.Constraints != nil {
		constraintsJSON, err := json.Marshal(input.Constraints)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal constraints: %w", err)
		}
		setClauses = append(setClauses, "constraints = ?")
		args = append(args, string(constraintsJSON))
	}
	if input.Status != "" {
		setClauses = append(setClauses, "status = ?")
		args = append(args, input.Status)
	}

	if len(setClauses) == 0 {
		return s.Get(ctx, id)
	}

	query := "UPDATE agent_sessions SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += " WHERE id = ?"
	args = append(args, id)

	result, err := s.client.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}

	return s.Get(ctx, id)
}

// ValidateToken checks if the given token hash belongs to an active session.
// The empty-hash guard prevents sessions created without a token from
// validating against an empty presented hash (#12).
func (s *SQLiteSessionStore) ValidateToken(ctx context.Context, sessionID, tokenHash string) (bool, error) {
	var count int
	err := s.client.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_sessions WHERE id = ? AND token_hash = ? AND status = 'active' AND token_hash != ''",
		sessionID, tokenHash,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to validate token: %w", err)
	}
	return count > 0, nil
}

// scanSession scans a sql.Rows row into a store.Session.
func scanSession(rows *sql.Rows) (*store.Session, error) {
	var session store.Session
	var capsJSON, constraintsJSON, scopeJSON, metadataJSON string
	var startedAtStr, expiresAtStr string
	var lastHeartbeatStr sql.NullString

	err := rows.Scan(
		&session.ID,
		&session.OrganizationID,
		&session.UserID,
		&session.Tool,
		&session.TrustLevel,
		&capsJSON,
		&constraintsJSON,
		&scopeJSON,
		&session.Status,
		&startedAtStr,
		&expiresAtStr,
		&lastHeartbeatStr,
		&metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan session: %w", err)
	}

	json.Unmarshal([]byte(capsJSON), &session.Capabilities)
	json.Unmarshal([]byte(constraintsJSON), &session.Constraints)
	json.Unmarshal([]byte(scopeJSON), &session.ServiceScope)
	json.Unmarshal([]byte(metadataJSON), &session.Metadata)

	if session.Capabilities == nil {
		session.Capabilities = []string{}
	}
	if session.Constraints == nil {
		session.Constraints = []string{}
	}
	if session.ServiceScope == nil {
		session.ServiceScope = []string{}
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}

	session.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAtStr)
	if lastHeartbeatStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastHeartbeatStr.String)
		if err == nil {
			session.LastHeartbeat = &t
		}
	}

	return &session, nil
}
