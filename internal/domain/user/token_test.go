package user

import (
	"testing"
	"time"
)

func TestTokenService_GenerateAndValidate(t *testing.T) {
	svc := NewTokenService("test-secret-key-32-bytes-long!!!", "adp", 15*time.Minute, 7*24*time.Hour)

	pair, err := svc.GenerateTokenPair("user-123", "alice@example.com", "admin")
	if err != nil {
		t.Fatalf("GenerateTokenPair failed: %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if pair.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if pair.TokenType != "Bearer" {
		t.Errorf("expected token type Bearer, got %s", pair.TokenType)
	}
	if pair.ExpiresIn != 900 { // 15 min = 900s
		t.Errorf("expected expires_in 900, got %d", pair.ExpiresIn)
	}

	// Validate access token
	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("expected subject user-123, got %s", claims.Subject)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", claims.Email)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role admin, got %s", claims.Role)
	}
	if claims.Type != "access" {
		t.Errorf("expected type access, got %s", claims.Type)
	}

	// Validate refresh token
	refreshClaims, err := svc.ValidateRefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken failed: %v", err)
	}
	if refreshClaims.Subject != "user-123" {
		t.Errorf("expected subject user-123, got %s", refreshClaims.Subject)
	}
	if refreshClaims.Type != "refresh" {
		t.Errorf("expected type refresh, got %s", refreshClaims.Type)
	}
}

func TestTokenService_WrongTokenType(t *testing.T) {
	svc := NewTokenService("test-secret-key-32-bytes-long!!!", "adp", 15*time.Minute, 7*24*time.Hour)

	pair, _ := svc.GenerateTokenPair("user-123", "test@example.com", "user")

	// Access token should fail refresh validation
	_, err := svc.ValidateRefreshToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error validating access token as refresh")
	}

	// Refresh token should fail access validation
	_, err = svc.ValidateAccessToken(pair.RefreshToken)
	if err == nil {
		t.Error("expected error validating refresh token as access")
	}
}

func TestTokenService_WrongKey(t *testing.T) {
	svc1 := NewTokenService("key-one-32-bytes-long-pad-!!!!!!", "adp", 15*time.Minute, 7*24*time.Hour)
	svc2 := NewTokenService("key-two-32-bytes-long-pad-!!!!!!", "adp", 15*time.Minute, 7*24*time.Hour)

	pair, _ := svc1.GenerateTokenPair("user-123", "test@example.com", "user")

	_, err := svc2.ValidateAccessToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error validating token with wrong key")
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	// Create service with 0 TTL (expires immediately)
	svc := NewTokenService("test-secret-key-32-bytes-long!!!", "adp", -1*time.Second, -1*time.Second)

	pair, _ := svc.GenerateTokenPair("user-123", "test@example.com", "user")

	_, err := svc.ValidateAccessToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error for expired access token")
	}
}
