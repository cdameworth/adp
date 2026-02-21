package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// PgUserStore implements store.UserStore backed by PostgreSQL.
type PgUserStore struct {
	client *PostgresClient
}

// NewPgUserStore creates a new PostgreSQL-backed user store.
func NewPgUserStore(client *PostgresClient) *PgUserStore {
	return &PgUserStore{client: client}
}

// Create creates a new user account.
func (s *PgUserStore) Create(ctx context.Context, input store.CreateUserInput) (*store.User, error) {
	if input.Email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidInput)
	}
	if input.PasswordHash == "" {
		return nil, fmt.Errorf("%w: password hash is required", ErrInvalidInput)
	}
	if input.Role == "" {
		input.Role = "user"
	}

	id := uuid.New()
	var u store.User
	var createdAt, updatedAt time.Time

	err := s.client.db.QueryRowContext(ctx,
		`INSERT INTO users (id, email, name, password_hash, role, status)
		 VALUES ($1, $2, $3, $4, $5, 'active')
		 RETURNING id, email, name, password_hash, role, status, created_at, updated_at`,
		id, input.Email, input.Name, input.PasswordHash, input.Role,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt)
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, fmt.Errorf("%w: email already exists", ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	u.CreatedAt = createdAt
	u.UpdatedAt = updatedAt
	return &u, nil
}

// GetByID retrieves a user by ID.
func (s *PgUserStore) GetByID(ctx context.Context, id string) (*store.User, error) {
	return s.queryUser(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users WHERE id = $1`, id)
}

// GetByEmail retrieves a user by email address.
func (s *PgUserStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	return s.queryUser(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users WHERE email = $1`, email)
}

// Update updates a user's fields. Only non-nil fields in UpdateUserInput are changed.
func (s *PgUserStore) Update(ctx context.Context, id string, input store.UpdateUserInput) (*store.User, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	role := existing.Role
	status := existing.Status
	passwordHash := existing.PasswordHash

	if input.Name != nil {
		name = *input.Name
	}
	if input.Role != nil {
		role = *input.Role
	}
	if input.Status != nil {
		status = *input.Status
	}
	if input.PasswordHash != nil {
		passwordHash = *input.PasswordHash
	}

	var u store.User
	var createdAt, updatedAt time.Time
	err = s.client.db.QueryRowContext(ctx,
		`UPDATE users SET name = $1, role = $2, status = $3, password_hash = $4, updated_at = NOW()
		 WHERE id = $5
		 RETURNING id, email, name, password_hash, role, status, created_at, updated_at`,
		name, role, status, passwordHash, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	u.CreatedAt = createdAt
	u.UpdatedAt = updatedAt
	return &u, nil
}

// List returns a paginated list of users and the total count.
func (s *PgUserStore) List(ctx context.Context, limit, offset int) ([]*store.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	err := s.client.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*store.User
	for rows.Next() {
		var u store.User
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan user row: %w", err)
		}
		u.CreatedAt = createdAt
		u.UpdatedAt = updatedAt
		users = append(users, &u)
	}

	return users, total, nil
}

// Delete soft-deletes a user by setting status to disabled.
func (s *PgUserStore) Delete(ctx context.Context, id string) error {
	result, err := s.client.db.ExecContext(ctx,
		`UPDATE users SET status = 'disabled', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	return nil
}

// queryUser is a helper to scan a single user from a query.
func (s *PgUserStore) queryUser(ctx context.Context, query string, args ...any) (*store.User, error) {
	var u store.User
	var createdAt, updatedAt time.Time

	err := s.client.db.QueryRowContext(ctx, query, args...).Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: user", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	u.CreatedAt = createdAt
	u.UpdatedAt = updatedAt
	return &u, nil
}

// isPgUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
func isPgUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// lib/pq unique_violation error code is 23505
	msg := err.Error()
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
