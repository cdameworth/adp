package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// APIKeyConfig holds configuration for API key authentication.
type APIKeyConfig struct {
	// APIKey is the expected API key value.
	APIKey string
}

// NewAPIKeyMiddleware returns middleware that validates requests using an API key.
// The key can be provided via the X-API-Key header or as a Bearer token in the
// Authorization header.
func NewAPIKeyMiddleware(cfg APIKeyConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow CORS preflight through without auth
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("X-API-Key")
			if key == "" {
				if authHeader := r.Header.Get("Authorization"); authHeader != "" {
					if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
						key = parts[1]
					}
				}
			}

			if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(cfg.APIKey)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="adp"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "Invalid or missing API key",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
