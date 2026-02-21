package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCombinedAuth_APIKeyViaHeader(t *testing.T) {
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "test-key"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("X-API-Key", "test-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCombinedAuth_APIKeyViaBearer(t *testing.T) {
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "test-key"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCombinedAuth_JWTLikeTokenWithNoJWTConfig(t *testing.T) {
	// A JWT-like token should fall through to API key when JWT is not configured
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "header.payload.signature"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer header.payload.signature")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCombinedAuth_MissingAuth(t *testing.T) {
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "test-key"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestCombinedAuth_WrongKey(t *testing.T) {
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "correct-key"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestCombinedAuth_OptionsPassthrough(t *testing.T) {
	mw := NewCombinedAuthMiddleware(CombinedAuthConfig{APIKey: "test-key"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS, got %d", rr.Code)
	}
}

func TestLooksLikeJWT(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.signature", true},
		{"a.b.c", true},
		{"simple-api-key", false},
		{"adp_tok_abc123", false},
		{"a.b", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := looksLikeJWT(tt.token); got != tt.want {
			t.Errorf("looksLikeJWT(%q) = %v, want %v", tt.token, got, tt.want)
		}
	}
}
