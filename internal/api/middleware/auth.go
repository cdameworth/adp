package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

type contextKey string

const (
	UserContextKey         contextKey = "user"
	OrganizationContextKey contextKey = "organization"
	RolesContextKey        contextKey = "roles"
)

// AuthConfig holds configuration for JWT authentication
type AuthConfig struct {
	// JWKSURL is the URL to fetch JSON Web Key Set for signature verification
	JWKSURL string
	// Issuer is the expected token issuer (iss claim)
	Issuer string
	// Audience is the expected token audience (aud claim)
	Audience string
	// RequireExpiration enforces exp claim presence
	RequireExpiration bool
	// ClockSkew allows for clock differences between servers
	ClockSkew time.Duration
	// CacheRefreshInterval controls how often JWKS is refreshed
	CacheRefreshInterval time.Duration
}

// JWKSProvider manages JWKS fetching and caching
type JWKSProvider struct {
	jwksURL       string
	cache         jwk.Set
	cacheMu       sync.RWMutex
	lastRefresh   time.Time
	refreshPeriod time.Duration
	httpClient    *http.Client
}

// NewJWKSProvider creates a new JWKS provider with caching
func NewJWKSProvider(jwksURL string, refreshPeriod time.Duration) *JWKSProvider {
	if refreshPeriod == 0 {
		refreshPeriod = 15 * time.Minute
	}
	return &JWKSProvider{
		jwksURL:       jwksURL,
		refreshPeriod: refreshPeriod,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetKeySet returns the cached JWKS, refreshing if necessary
func (p *JWKSProvider) GetKeySet(ctx context.Context) (jwk.Set, error) {
	p.cacheMu.RLock()
	if p.cache != nil && time.Since(p.lastRefresh) < p.refreshPeriod {
		defer p.cacheMu.RUnlock()
		return p.cache, nil
	}
	p.cacheMu.RUnlock()

	return p.refresh(ctx)
}

// refresh fetches the JWKS from the remote endpoint
func (p *JWKSProvider) refresh(ctx context.Context) (jwk.Set, error) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if p.cache != nil && time.Since(p.lastRefresh) < p.refreshPeriod {
		return p.cache, nil
	}

	keySet, err := jwk.Fetch(ctx, p.jwksURL, jwk.WithHTTPClient(p.httpClient))
	if err != nil {
		// If we have a cached version, return it even if stale
		if p.cache != nil {
			return p.cache, nil
		}
		return nil, fmt.Errorf("failed to fetch JWKS from %s: %w", p.jwksURL, err)
	}

	p.cache = keySet
	p.lastRefresh = time.Now()
	return keySet, nil
}

// ForceRefresh forces a refresh of the JWKS cache
func (p *JWKSProvider) ForceRefresh(ctx context.Context) error {
	p.cacheMu.Lock()
	p.lastRefresh = time.Time{} // Reset last refresh to force update
	p.cacheMu.Unlock()

	_, err := p.refresh(ctx)
	return err
}

// AuthMiddlewareConfig creates a configurable auth middleware
type AuthMiddlewareConfig struct {
	JWKSProvider *JWKSProvider
	Config       AuthConfig
}

// NewAuthMiddleware creates a new authentication middleware with full JWT verification
func NewAuthMiddleware(cfg AuthConfig) (*AuthMiddlewareConfig, error) {
	if cfg.JWKSURL == "" {
		return nil, fmt.Errorf("JWKS URL is required for JWT verification")
	}

	provider := NewJWKSProvider(cfg.JWKSURL, cfg.CacheRefreshInterval)

	// Pre-fetch JWKS to validate configuration
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := provider.GetKeySet(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize JWKS: %w", err)
	}

	return &AuthMiddlewareConfig{
		JWKSProvider: provider,
		Config:       cfg,
	}, nil
}

// Middleware returns the HTTP middleware handler
func (a *AuthMiddlewareConfig) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAuthError(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || !strings.EqualFold(tokenParts[0], "Bearer") {
			writeAuthError(w, "Invalid authorization header format. Expected: Bearer <token>", http.StatusUnauthorized)
			return
		}

		tokenStr := tokenParts[1]
		if tokenStr == "" {
			writeAuthError(w, "Token is empty", http.StatusUnauthorized)
			return
		}

		// Fetch JWKS for signature verification
		keySet, err := a.JWKSProvider.GetKeySet(r.Context())
		if err != nil {
			writeAuthError(w, "Failed to retrieve signing keys", http.StatusInternalServerError)
			return
		}

		// Build parse options
		parseOpts := []jwt.ParseOption{
			jwt.WithKeySet(keySet),
		}

		// Parse and verify token signature
		token, err := jwt.Parse([]byte(tokenStr), parseOpts...)
		if err != nil {
			writeAuthError(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		// Build validation options
		validateOpts := []jwt.ValidateOption{}

		// Validate issuer if configured
		if a.Config.Issuer != "" {
			validateOpts = append(validateOpts, jwt.WithIssuer(a.Config.Issuer))
		}

		// Validate audience if configured
		if a.Config.Audience != "" {
			validateOpts = append(validateOpts, jwt.WithAudience(a.Config.Audience))
		}

		// Apply clock skew tolerance
		if a.Config.ClockSkew > 0 {
			validateOpts = append(validateOpts, jwt.WithAcceptableSkew(a.Config.ClockSkew))
		}

		// Validate claims (expiration, not-before, issued-at)
		if err := jwt.Validate(token, validateOpts...); err != nil {
			writeAuthError(w, fmt.Sprintf("Token validation failed: %v", err), http.StatusUnauthorized)
			return
		}

		// Check expiration is present if required
		if a.Config.RequireExpiration {
			if _, ok := token.Expiration(); !ok {
				writeAuthError(w, "Token missing required expiration claim", http.StatusUnauthorized)
				return
			}
		}

		// Extract user ID (subject)
		userID, ok := token.Subject()
		if !ok || userID == "" {
			writeAuthError(w, "Token missing subject claim", http.StatusUnauthorized)
			return
		}

		// Build context with claims
		ctx := context.WithValue(r.Context(), UserContextKey, userID)

		// Extract organization from custom claim if present
		var orgClaim interface{}
		if err := token.Get("org", &orgClaim); err == nil {
			if org, ok := orgClaim.(string); ok {
				ctx = context.WithValue(ctx, OrganizationContextKey, org)
			}
		}

		// Extract roles from custom claim if present
		var rolesClaim interface{}
		if err := token.Get("roles", &rolesClaim); err == nil {
			switch roles := rolesClaim.(type) {
			case []interface{}:
				roleStrings := make([]string, 0, len(roles))
				for _, r := range roles {
					if s, ok := r.(string); ok {
						roleStrings = append(roleStrings, s)
					}
				}
				ctx = context.WithValue(ctx, RolesContextKey, roleStrings)
			case []string:
				ctx = context.WithValue(ctx, RolesContextKey, roles)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeAuthError writes a JSON error response for authentication failures
func writeAuthError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="adp"`)
	w.WriteHeader(statusCode)
	fmt.Fprintf(w, `{"error":"unauthorized","message":%q}`, message)
}

// GetUserFromContext retrieves the user ID from the context
func GetUserFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserContextKey).(string)
	return userID, ok
}

// GetOrganizationFromContext retrieves the organization from the context
func GetOrganizationFromContext(ctx context.Context) (string, bool) {
	org, ok := ctx.Value(OrganizationContextKey).(string)
	return org, ok
}

// GetRolesFromContext retrieves the roles from the context
func GetRolesFromContext(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(RolesContextKey).([]string)
	return roles, ok
}

// OptionalAuthMiddleware allows requests without authentication but extracts claims if present
func (a *AuthMiddlewareConfig) OptionalAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// No auth header, continue without user context
			next.ServeHTTP(w, r)
			return
		}

		// Delegate to full auth middleware
		a.Middleware(next).ServeHTTP(w, r)
	})
}
