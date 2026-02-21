package auth

import (
	"context"
	"testing"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

func setupAuthorizerTest(t *testing.T) (*SQLAuthorizer, store.UserStore) {
	t.Helper()
	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	userStore := database.NewSQLiteUserStore(client)
	authorizer := NewSQLAuthorizer(userStore)
	return authorizer, userStore
}

func TestSQLAuthorizer_AdminAllowsAll(t *testing.T) {
	authz, users := setupAuthorizerTest(t)
	ctx := context.Background()

	admin, _ := users.Create(ctx, store.CreateUserInput{
		Email:        "admin@test.com",
		Name:         "Admin",
		PasswordHash: "$2a$12$fakehash",
		Role:         "admin",
	})

	tests := []struct {
		action   string
		resource string
	}{
		{"manage", "users"},
		{"read", "sessions"},
		{"create", "policies"},
		{"delete", "services"},
	}

	for _, tt := range tests {
		allowed, err := authz.Authorize(ctx, admin.ID, tt.action, tt.resource)
		if err != nil {
			t.Errorf("Authorize(%s, %s) error: %v", tt.action, tt.resource, err)
		}
		if !allowed {
			t.Errorf("Admin should be allowed %s on %s", tt.action, tt.resource)
		}
	}
}

func TestSQLAuthorizer_UserReadAllowed(t *testing.T) {
	authz, users := setupAuthorizerTest(t)
	ctx := context.Background()

	u, _ := users.Create(ctx, store.CreateUserInput{
		Email:        "user@test.com",
		Name:         "User",
		PasswordHash: "$2a$12$fakehash",
		Role:         "user",
	})

	tests := []struct {
		action   string
		resource string
		allowed  bool
	}{
		{"read", "sessions", true},
		{"list", "decisions", true},
		{"get", "policies", true},
		{"view", "reports", true},
		{"create", "sessions", true}, // users can write their own sessions
		{"manage", "users", false},
		{"create", "policies", false},
		{"delete", "services", false},
	}

	for _, tt := range tests {
		allowed, err := authz.Authorize(ctx, u.ID, tt.action, tt.resource)
		if err != nil {
			t.Errorf("Authorize(%s, %s) error: %v", tt.action, tt.resource, err)
		}
		if allowed != tt.allowed {
			t.Errorf("User %s on %s: expected %v, got %v", tt.action, tt.resource, tt.allowed, allowed)
		}
	}
}

func TestSQLAuthorizer_DisabledUserDenied(t *testing.T) {
	authz, users := setupAuthorizerTest(t)
	ctx := context.Background()

	u, _ := users.Create(ctx, store.CreateUserInput{
		Email:        "disabled@test.com",
		Name:         "Disabled",
		PasswordHash: "$2a$12$fakehash",
		Role:         "admin",
	})
	users.Delete(ctx, u.ID) // soft-delete → disabled

	allowed, _ := authz.Authorize(ctx, u.ID, "read", "sessions")
	if allowed {
		t.Error("disabled user should be denied")
	}
}

func TestSQLAuthorizer_NonexistentUserDenied(t *testing.T) {
	authz, _ := setupAuthorizerTest(t)
	ctx := context.Background()

	allowed, _ := authz.Authorize(ctx, "nonexistent-id", "read", "sessions")
	if allowed {
		t.Error("nonexistent user should be denied")
	}
}
