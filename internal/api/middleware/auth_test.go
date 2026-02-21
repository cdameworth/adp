package middleware

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// TestJWKSProvider tests the JWKS caching provider
func TestJWKSProvider(t *testing.T) {
	// Create a test JWKS server
	keySet, _, err := createTestKeySet()
	if err != nil {
		t.Fatalf("failed to create test key set: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	}))
	defer server.Close()

	t.Run("fetches JWKS successfully", func(t *testing.T) {
		provider := NewJWKSProvider(server.URL, 15*time.Minute)
		ks, err := provider.GetKeySet(context.Background())
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if ks.Len() != 1 {
			t.Errorf("expected 1 key, got %d", ks.Len())
		}
	})

	t.Run("caches JWKS", func(t *testing.T) {
		callCount := 0
		countingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(keySet)
		}))
		defer countingServer.Close()

		provider := NewJWKSProvider(countingServer.URL, 1*time.Hour)

		// First call
		_, err := provider.GetKeySet(context.Background())
		if err != nil {
			t.Fatalf("first call failed: %v", err)
		}

		// Second call should use cache
		_, err = provider.GetKeySet(context.Background())
		if err != nil {
			t.Fatalf("second call failed: %v", err)
		}

		if callCount != 1 {
			t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
		}
	})

	t.Run("handles server error with cached value", func(t *testing.T) {
		failAfterFirst := 0
		failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			failAfterFirst++
			if failAfterFirst > 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(keySet)
		}))
		defer failingServer.Close()

		provider := NewJWKSProvider(failingServer.URL, 1*time.Millisecond)

		// First call succeeds
		_, err := provider.GetKeySet(context.Background())
		if err != nil {
			t.Fatalf("first call failed: %v", err)
		}

		// Wait for cache to expire
		time.Sleep(5 * time.Millisecond)

		// Second call should return cached value despite server error
		ks, err := provider.GetKeySet(context.Background())
		if err != nil {
			t.Fatalf("expected cached value on error, got: %v", err)
		}
		if ks.Len() != 1 {
			t.Errorf("expected cached key set with 1 key, got %d", ks.Len())
		}
	})

	t.Run("fails when no cache and server error", func(t *testing.T) {
		failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer failingServer.Close()

		provider := NewJWKSProvider(failingServer.URL, 15*time.Minute)

		_, err := provider.GetKeySet(context.Background())
		if err == nil {
			t.Error("expected error when server fails and no cache")
		}
	})
}

// TestAuthMiddleware tests the authentication middleware
func TestAuthMiddleware(t *testing.T) {
	keySet, privateKey, err := createTestKeySet()
	if err != nil {
		t.Fatalf("failed to create test key set: %v", err)
	}

	// Create JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	}))
	defer server.Close()

	authConfig := AuthConfig{
		JWKSURL:              server.URL,
		Issuer:               "test-issuer",
		Audience:             "test-audience",
		RequireExpiration:    true,
		ClockSkew:            1 * time.Minute,
		CacheRefreshInterval: 15 * time.Minute,
	}

	authMiddleware, err := NewAuthMiddleware(authConfig)
	if err != nil {
		t.Fatalf("failed to create auth middleware: %v", err)
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetUserFromContext(r.Context())
		if !ok {
			http.Error(w, "no user in context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(userID))
	})

	t.Run("rejects missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects invalid authorization format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects empty token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ")
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects malformed token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer not-a-jwt")
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects expired token", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Subject("user-123").
			Issuer("test-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(-1*time.Hour)).
			IssuedAt(time.Now().Add(-2*time.Hour)))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects wrong issuer", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Subject("user-123").
			Issuer("wrong-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects wrong audience", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Subject("user-123").
			Issuer("test-issuer").
			Audience([]string{"wrong-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects token without subject", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Issuer("test-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("accepts valid token", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Subject("user-123").
			Issuer("test-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		if rr.Body.String() != "user-123" {
			t.Errorf("expected body 'user-123', got '%s'", rr.Body.String())
		}
	})

	t.Run("extracts organization from token", func(t *testing.T) {
		builder := jwt.NewBuilder().
			Subject("user-123").
			Issuer("test-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()).
			Claim("org", "acme-corp")
		token := createTestToken(t, privateKey, builder)

		var extractedOrg string
		orgHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			org, ok := GetOrganizationFromContext(r.Context())
			if ok {
				extractedOrg = org
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(orgHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		if extractedOrg != "acme-corp" {
			t.Errorf("expected org 'acme-corp', got '%s'", extractedOrg)
		}
	})

	t.Run("extracts roles from token", func(t *testing.T) {
		builder := jwt.NewBuilder().
			Subject("user-123").
			Issuer("test-issuer").
			Audience([]string{"test-audience"}).
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()).
			Claim("roles", []interface{}{"admin", "developer"})
		token := createTestToken(t, privateKey, builder)

		var extractedRoles []string
		rolesHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roles, ok := GetRolesFromContext(r.Context())
			if ok {
				extractedRoles = roles
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.Middleware(rolesHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		if len(extractedRoles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(extractedRoles))
		}
	})
}

// TestOptionalAuthMiddleware tests the optional auth middleware
func TestOptionalAuthMiddleware(t *testing.T) {
	keySet, privateKey, err := createTestKeySet()
	if err != nil {
		t.Fatalf("failed to create test key set: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	}))
	defer server.Close()

	authConfig := AuthConfig{
		JWKSURL:              server.URL,
		CacheRefreshInterval: 15 * time.Minute,
	}

	authMiddleware, err := NewAuthMiddleware(authConfig)
	if err != nil {
		t.Fatalf("failed to create auth middleware: %v", err)
	}

	t.Run("allows requests without auth header", func(t *testing.T) {
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := GetUserFromContext(r.Context())
			if ok {
				t.Error("expected no user in context for unauthenticated request")
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		authMiddleware.OptionalAuthMiddleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("extracts user when auth header present", func(t *testing.T) {
		token := createTestToken(t, privateKey, jwt.NewBuilder().
			Subject("user-123").
			Expiration(time.Now().Add(1*time.Hour)).
			IssuedAt(time.Now()))

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserFromContext(r.Context())
			if !ok {
				t.Error("expected user in context")
			}
			if userID != "user-123" {
				t.Errorf("expected user-123, got %s", userID)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		authMiddleware.OptionalAuthMiddleware(testHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

// Helper functions

func createTestKeySet() (jwk.Set, *ecdsa.PrivateKey, error) {
	// Generate ECDSA key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	// Create JWK from public key
	publicJWK, err := jwk.Import(privateKey.Public())
	if err != nil {
		return nil, nil, err
	}

	// Set key ID and algorithm
	if err := publicJWK.Set(jwk.KeyIDKey, "test-key-1"); err != nil {
		return nil, nil, err
	}
	if err := publicJWK.Set(jwk.AlgorithmKey, jwa.ES256()); err != nil {
		return nil, nil, err
	}
	if err := publicJWK.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, nil, err
	}

	// Create key set
	keySet := jwk.NewSet()
	if err := keySet.AddKey(publicJWK); err != nil {
		return nil, nil, err
	}

	return keySet, privateKey, nil
}

func createTestToken(t *testing.T, privateKey *ecdsa.PrivateKey, builder *jwt.Builder) string {
	t.Helper()

	tok, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	// Create signing key
	signingKey, err := jwk.Import(privateKey)
	if err != nil {
		t.Fatalf("failed to import signing key: %v", err)
	}
	if err := signingKey.Set(jwk.KeyIDKey, "test-key-1"); err != nil {
		t.Fatalf("failed to set key ID: %v", err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.ES256(), signingKey))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return string(signed)
}
