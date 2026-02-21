package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adp/adp/internal/api/middleware"
	"github.com/adp/adp/internal/domain/user"
	"github.com/adp/adp/internal/infrastructure/database"
)

const testJWTSecret = "test-secret-key-32-bytes-long!!!"

func setupAuthTest(t *testing.T) (*AuthHandlerImpl, *database.SQLiteUserStore) {
	t.Helper()
	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	userStore := database.NewSQLiteUserStore(client)
	tokenService := user.NewTokenService(testJWTSecret, "adp", 15*time.Minute, 7*24*time.Hour)
	handler := NewAuthHandler(userStore, tokenService, true) // open registration
	return handler, userStore
}

func TestAuthHandler_RegisterFirstUser(t *testing.T) {
	h, _ := setupAuthTest(t)

	body := `{"email":"admin@test.com","name":"Admin","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result authResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.User.Role != "admin" {
		t.Errorf("first user should be admin, got %s", result.User.Role)
	}
	if result.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if result.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if result.User.Email != "admin@test.com" {
		t.Errorf("expected email admin@test.com, got %s", result.User.Email)
	}
}

func TestAuthHandler_RegisterSecondUser(t *testing.T) {
	h, _ := setupAuthTest(t)

	// Register first user (admin)
	body := `{"email":"admin@test.com","name":"Admin","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	// Register second user
	body = `{"email":"user@test.com","name":"User","password":"password123"}`
	req = httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Register(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result authResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.User.Role != "user" {
		t.Errorf("second user should be user, got %s", result.User.Role)
	}
}

func TestAuthHandler_RegisterDuplicate(t *testing.T) {
	h, _ := setupAuthTest(t)

	body := `{"email":"dup@test.com","name":"First","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	// Try again with same email
	req = httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Register(w, req)

	if w.Result().StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for duplicate, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_RegisterShortPassword(t *testing.T) {
	h, _ := setupAuthTest(t)

	body := `{"email":"test@test.com","name":"Test","password":"short"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_Login(t *testing.T) {
	h, _ := setupAuthTest(t)

	// Register
	body := `{"email":"login@test.com","name":"Tester","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	// Login
	body = `{"email":"login@test.com","password":"password123"}`
	req = httptest.NewRequest("POST", "/v1/auth/login", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Login(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result authResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if result.User.Email != "login@test.com" {
		t.Errorf("expected email login@test.com, got %s", result.User.Email)
	}
}

func TestAuthHandler_LoginWrongPassword(t *testing.T) {
	h, _ := setupAuthTest(t)

	// Register
	body := `{"email":"wrong@test.com","name":"Test","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	// Login with wrong password
	body = `{"email":"wrong@test.com","password":"wrongpassword"}`
	req = httptest.NewRequest("POST", "/v1/auth/login", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Login(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_LoginNonexistentUser(t *testing.T) {
	h, _ := setupAuthTest(t)

	body := `{"email":"nobody@test.com","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_RefreshToken(t *testing.T) {
	h, _ := setupAuthTest(t)

	// Register to get tokens
	body := `{"email":"refresh@test.com","name":"Test","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	var registerResult authResponse
	json.NewDecoder(w.Result().Body).Decode(&registerResult)

	// Refresh
	refreshBody, _ := json.Marshal(refreshRequest{RefreshToken: registerResult.RefreshToken})
	req = httptest.NewRequest("POST", "/v1/auth/refresh", bytes.NewBuffer(refreshBody))
	w = httptest.NewRecorder()
	h.RefreshToken(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result authResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.AccessToken == "" {
		t.Error("expected new access token")
	}
}

func TestAuthHandler_GetProfile(t *testing.T) {
	h, _ := setupAuthTest(t)

	// Register
	body := `{"email":"profile@test.com","name":"Profile","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	var registerResult authResponse
	json.NewDecoder(w.Result().Body).Decode(&registerResult)

	// Get profile with auth context
	req = httptest.NewRequest("GET", "/v1/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, registerResult.User.ID)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.GetProfile(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_GetProfileUnauthenticated(t *testing.T) {
	h, _ := setupAuthTest(t)

	req := httptest.NewRequest("GET", "/v1/auth/me", nil)
	w := httptest.NewRecorder()
	h.GetProfile(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestAuthHandler_LoginDisabledUser(t *testing.T) {
	h, userStore := setupAuthTest(t)

	// Register
	body := `{"email":"disabled@test.com","name":"Disabled","password":"password123"}`
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Register(w, req)

	var registerResult authResponse
	json.NewDecoder(w.Result().Body).Decode(&registerResult)

	// Disable user
	userStore.Delete(context.Background(), registerResult.User.ID)

	// Try to login
	body = `{"email":"disabled@test.com","password":"password123"}`
	req = httptest.NewRequest("POST", "/v1/auth/login", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Login(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for disabled user, got %d", w.Result().StatusCode)
	}
}
