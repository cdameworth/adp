package middleware

import (
	"context"
	"net/http"
)

// Authorizer defines the interface for authorization checks
type Authorizer interface {
	// Authorize checks if the subject is allowed to perform the action on the resource
	Authorize(ctx context.Context, subject string, action string, resource string) (bool, error)
}

// RequirePermission creates a middleware that enforces RBAC
func RequirePermission(authz Authorizer, action string, resource string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized: No user found in context", http.StatusUnauthorized)
				return
			}

			allowed, err := authz.Authorize(r.Context(), userID, action, resource)
			if err != nil {
				// Log error but deny access
				http.Error(w, "Authorization error", http.StatusInternalServerError)
				return
			}

			if !allowed {
				http.Error(w, "Forbidden: Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
