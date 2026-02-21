package user

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// TokenService generates and validates JWT tokens for local authentication.
type TokenService struct {
	signingKey []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// TokenPair contains access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds until access token expires
	TokenType    string `json:"token_type"` // "Bearer"
}

// Claims represents the custom JWT claims for ADP.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email,omitempty"`
	Role  string `json:"role,omitempty"`
	Type  string `json:"type,omitempty"` // "access" or "refresh"
}

// NewTokenService creates a new JWT token service.
func NewTokenService(signingKey string, issuer string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{
		signingKey: []byte(signingKey),
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// GenerateTokenPair creates a new access + refresh token pair for a user.
func (s *TokenService) GenerateTokenPair(userID, email, role string) (*TokenPair, error) {
	now := time.Now()

	// Access token
	accessClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
		Email: email,
		Role:  role,
		Type:  "access",
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.signingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token
	refreshClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		},
		Type: "refresh",
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(s.signingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int(s.accessTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

// ValidateAccessToken validates an access token and returns its claims.
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, "access")
}

// ValidateRefreshToken validates a refresh token and returns its claims.
func (s *TokenService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, "refresh")
}

func (s *TokenService) validateToken(tokenString, expectedType string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.signingKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.Type != expectedType {
		return nil, fmt.Errorf("expected %s token, got %s", expectedType, claims.Type)
	}
	if s.issuer != "" && claims.Issuer != s.issuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", s.issuer, claims.Issuer)
	}
	return claims, nil
}
