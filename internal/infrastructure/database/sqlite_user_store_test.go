package database

import (
	"context"
	"errors"
	"testing"

	"github.com/adp/adp/internal/store"
)

func newTestUserStore(t *testing.T) *SQLiteUserStore {
	t.Helper()
	client, err := NewSQLiteClient(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return NewSQLiteUserStore(client)
}

func TestSQLiteUserStore_Create(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	user, err := s.Create(ctx, store.CreateUserInput{
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: "$2a$12$fakehash",
		Role:         "admin",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if user.ID == "" {
		t.Error("expected non-empty ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", user.Email)
	}
	if user.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", user.Name)
	}
	if user.Role != "admin" {
		t.Errorf("expected role admin, got %s", user.Role)
	}
	if user.Status != "active" {
		t.Errorf("expected status active, got %s", user.Status)
	}
	if user.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestSQLiteUserStore_CreateDuplicateEmail(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, store.CreateUserInput{
		Email:        "dup@example.com",
		Name:         "First",
		PasswordHash: "$2a$12$fakehash",
		Role:         "user",
	})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = s.Create(ctx, store.CreateUserInput{
		Email:        "dup@example.com",
		Name:         "Second",
		PasswordHash: "$2a$12$fakehash2",
		Role:         "user",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestSQLiteUserStore_CreateValidation(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, store.CreateUserInput{
		Email:        "",
		PasswordHash: "$2a$12$hash",
	})
	if err == nil {
		t.Fatal("expected error for empty email")
	}

	_, err = s.Create(ctx, store.CreateUserInput{
		Email:        "test@example.com",
		PasswordHash: "",
	})
	if err == nil {
		t.Fatal("expected error for empty password hash")
	}
}

func TestSQLiteUserStore_GetByID(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	created, _ := s.Create(ctx, store.CreateUserInput{
		Email:        "bob@example.com",
		Name:         "Bob",
		PasswordHash: "$2a$12$hash",
		Role:         "user",
	})

	user, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if user.Email != "bob@example.com" {
		t.Errorf("expected email bob@example.com, got %s", user.Email)
	}

	_, err = s.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestSQLiteUserStore_GetByEmail(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	s.Create(ctx, store.CreateUserInput{
		Email:        "carol@example.com",
		Name:         "Carol",
		PasswordHash: "$2a$12$hash",
		Role:         "user",
	})

	user, err := s.GetByEmail(ctx, "carol@example.com")
	if err != nil {
		t.Fatalf("GetByEmail failed: %v", err)
	}
	if user.Name != "Carol" {
		t.Errorf("expected name Carol, got %s", user.Name)
	}

	_, err = s.GetByEmail(ctx, "nonexistent@example.com")
	if err == nil {
		t.Fatal("expected error for nonexistent email")
	}
}

func TestSQLiteUserStore_Update(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	created, _ := s.Create(ctx, store.CreateUserInput{
		Email:        "dave@example.com",
		Name:         "Dave",
		PasswordHash: "$2a$12$hash",
		Role:         "user",
	})

	newName := "David"
	newRole := "admin"
	updated, err := s.Update(ctx, created.ID, store.UpdateUserInput{
		Name: &newName,
		Role: &newRole,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Name != "David" {
		t.Errorf("expected name David, got %s", updated.Name)
	}
	if updated.Role != "admin" {
		t.Errorf("expected role admin, got %s", updated.Role)
	}
	if updated.Email != "dave@example.com" {
		t.Errorf("email should not change, got %s", updated.Email)
	}
}

func TestSQLiteUserStore_UpdateNonexistent(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	name := "Test"
	_, err := s.Update(ctx, "nonexistent", store.UpdateUserInput{Name: &name})
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestSQLiteUserStore_List(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	// Create 3 users
	for _, email := range []string{"a@test.com", "b@test.com", "c@test.com"} {
		s.Create(ctx, store.CreateUserInput{
			Email:        email,
			Name:         email,
			PasswordHash: "$2a$12$hash",
			Role:         "user",
		})
	}

	// List all
	users, total, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}

	// Pagination
	users, total, err = s.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List with limit failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	users, _, err = s.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List with offset failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user at offset 2, got %d", len(users))
	}
}

func TestSQLiteUserStore_Delete(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	created, _ := s.Create(ctx, store.CreateUserInput{
		Email:        "eve@example.com",
		Name:         "Eve",
		PasswordHash: "$2a$12$hash",
		Role:         "user",
	})

	err := s.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify soft-delete
	user, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after delete failed: %v", err)
	}
	if user.Status != "disabled" {
		t.Errorf("expected status disabled after delete, got %s", user.Status)
	}

	// Delete nonexistent
	err = s.Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user delete")
	}
}

func TestSQLiteUserStore_DefaultRole(t *testing.T) {
	s := newTestUserStore(t)
	ctx := context.Background()

	user, err := s.Create(ctx, store.CreateUserInput{
		Email:        "frank@example.com",
		Name:         "Frank",
		PasswordHash: "$2a$12$hash",
		Role:         "", // empty → should default to "user"
	})
	if err != nil {
		t.Fatalf("Create with empty role failed: %v", err)
	}
	if user.Role != "user" {
		t.Errorf("expected default role 'user', got %s", user.Role)
	}
}
