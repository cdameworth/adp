package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

// CombinedAuthConfig supports both JWT and API key authentication.
// When both are configured, JWT is tried first for tokens that look like JWTs
// (three dot-separated segments). API key is used for all other bearer tokens
// and for the X-API-Key header.
type CombinedAuthConfig struct {
	JWTMiddleware  *AuthMiddlewareConfig // nil if JWT not configured
	APIKey         string                // empty if API key not configured
	LocalJWTSecret string                // HMAC secret for locally-generated JWTs (empty = disabled)
}

// NewCombinedAuthMiddleware returns middleware that supports both JWT and API key auth.
func NewCombinedAuthMiddleware(cfg CombinedAuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// X-API-Key header always uses API key path
			if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
				if cfg.APIKey != "" && subtle.ConstantTimeCompare([]byte(apiKey), []byte(cfg.APIKey)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
				combinedAuthError(w, "Invalid API key")
				return
			}

			// Authorization: Bearer <token>
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				combinedAuthError(w, "Authentication required")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				combinedAuthError(w, "Invalid authorization format")
				return
			}
			token := parts[1]

			// If token looks like a JWT, try local validation first, then JWKS
			if looksLikeJWT(token) {
				// Try local JWT validation (for locally-generated tokens)
				if cfg.LocalJWTSecret != "" {
					if ctx, ok := tryLocalJWT(r.Context(), token, cfg.LocalJWTSecret); ok {
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}

				// Try external JWKS-based JWT validation
				if cfg.JWTMiddleware != nil {
					cfg.JWTMiddleware.Middleware(next).ServeHTTP(w, r)
					return
				}
			}

			// Fall back to API key comparison
			if cfg.APIKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.APIKey)) == 1 {
				next.ServeHTTP(w, r)
				return
			}

			combinedAuthError(w, "Invalid credentials")
		})
	}
}

// looksLikeJWT returns true if the token has the three-segment structure of a JWT.
func looksLikeJWT(token string) bool {
	return strings.Count(token, ".") == 2
}

// tryLocalJWT attempts to validate a locally-signed JWT (HMAC-SHA256).
// On success, it returns a new context with user ID and roles injected.
func tryLocalJWT(ctx context.Context, tokenString, secret string) (context.Context, bool) {
	type localClaims struct {
		jwt.RegisteredClaims
		Email string `json:"email,omitempty"`
		Role  string `json:"role,omitempty"`
		Type  string `json:"type,omitempty"`
	}

	claims := &localClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return ctx, false
	}

	// Only accept access tokens (not refresh tokens)
	if claims.Type != "access" {
		return ctx, false
	}

	// Inject user context
	ctx = context.WithValue(ctx, UserContextKey, claims.Subject)
	if claims.Role != "" {
		ctx = context.WithValue(ctx, RolesContextKey, []string{claims.Role})
	}

	return ctx, true
}

func combinedAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="adp"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "unauthorized",
		"message": message,
	})
}
