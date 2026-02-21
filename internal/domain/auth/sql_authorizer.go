package auth

import (
	"context"
	"strings"

	"github.com/adp/adp/internal/store"
)

// SQLAuthorizer implements the middleware.Authorizer interface using the SQL user store.
// For single-tenant alpha: admin can do everything, regular users get read access.
type SQLAuthorizer struct {
	users store.UserStore
}

// NewSQLAuthorizer creates a new SQL-based authorizer.
func NewSQLAuthorizer(users store.UserStore) *SQLAuthorizer {
	return &SQLAuthorizer{users: users}
}

// Authorize checks if the subject is allowed to perform the action on the resource.
func (a *SQLAuthorizer) Authorize(ctx context.Context, subject, action, resource string) (bool, error) {
	u, err := a.users.GetByID(ctx, subject)
	if err != nil {
		return false, nil // user not found → deny
	}
	if u.Status != "active" {
		return false, nil
	}

	// Admin can do everything
	if u.Role == "admin" {
		return true, nil
	}

	// Regular users: read access to most resources
	if isReadAction(action) {
		return true, nil
	}

	// Regular users can write to their own sessions
	if resource == "sessions" {
		return true, nil
	}

	return false, nil
}

func isReadAction(action string) bool {
	a := strings.ToLower(action)
	return a == "read" || a == "list" || a == "get" || a == "view"
}
