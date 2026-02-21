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

// SQLiteUserStore implements store.UserStore backed by SQLite.
type SQLiteUserStore struct {
	client *SQLiteClient
}

// NewSQLiteUserStore creates a new SQLite-backed user store.
func NewSQLiteUserStore(client *SQLiteClient) *SQLiteUserStore {
	return &SQLiteUserStore{client: client}
}

// Create creates a new user account.
func (s *SQLiteUserStore) Create(ctx context.Context, input store.CreateUserInput) (*store.User, error) {
	if input.Email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidInput)
	}
	if input.PasswordHash == "" {
		return nil, fmt.Errorf("%w: password hash is required", ErrInvalidInput)
	}
	if input.Role == "" {
		input.Role = "user"
	}

	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := s.client.db.ExecContext(ctx,
		`INSERT INTO users (id, email, name, password_hash, role, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
		id, input.Email, input.Name, input.PasswordHash, input.Role, now, now,
	)
	if err != nil {
		if isSQLiteUniqueViolation(err) {
			return nil, fmt.Errorf("%w: email already exists", ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return s.GetByID(ctx, id)
}

// GetByID retrieves a user by ID.
func (s *SQLiteUserStore) GetByID(ctx context.Context, id string) (*store.User, error) {
	row := s.client.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetByEmail retrieves a user by email address.
func (s *SQLiteUserStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	row := s.client.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users WHERE email = ?`, email)
	return scanUser(row)
}

// Update updates a user's fields. Only non-nil fields in UpdateUserInput are changed.
func (s *SQLiteUserStore) Update(ctx context.Context, id string, input store.UpdateUserInput) (*store.User, error) {
	// Verify user exists first
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

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = s.client.db.ExecContext(ctx,
		`UPDATE users SET name = ?, role = ?, status = ?, password_hash = ?, updated_at = ? WHERE id = ?`,
		name, role, status, passwordHash, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return s.GetByID(ctx, id)
}

// List returns a paginated list of users and the total count.
func (s *SQLiteUserStore) List(ctx context.Context, limit, offset int) ([]*store.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Get total count
	var total int
	err := s.client.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	rows, err := s.client.db.QueryContext(ctx,
		`SELECT id, email, name, password_hash, role, status, created_at, updated_at
		 FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*store.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate users: %w", err)
	}

	return users, total, nil
}

// Delete soft-deletes a user by setting status to disabled.
func (s *SQLiteUserStore) Delete(ctx context.Context, id string) error {
	result, err := s.client.db.ExecContext(ctx,
		`UPDATE users SET status = 'disabled', updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	return nil
}

// scanUser scans a single user row from a QueryRow result.
func scanUser(row *sql.Row) (*store.User, error) {
	var u store.User
	var createdAt, updatedAt string

	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: user", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &u, nil
}

// scanUserRow scans a single user from a Rows iterator.
func scanUserRow(rows *sql.Rows) (*store.User, error) {
	var u store.User
	var createdAt, updatedAt string

	err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan user row: %w", err)
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &u, nil
}

// isSQLiteUniqueViolation checks if a SQLite error is a UNIQUE constraint violation.
func isSQLiteUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "unique constraint")
}
