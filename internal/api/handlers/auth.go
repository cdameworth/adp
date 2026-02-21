package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/adp/adp/internal/api/middleware"
	"github.com/adp/adp/internal/domain/user"
	"github.com/adp/adp/internal/store"
)

// AuthHandlerImpl implements the AuthHandler interface for user authentication.
// Works with both SQLite and PostgreSQL via the store.UserStore interface.
type AuthHandlerImpl struct {
	users   store.UserStore
	tokens  *user.TokenService
	openReg bool
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(users store.UserStore, tokens *user.TokenService, openReg bool) *AuthHandlerImpl {
	return &AuthHandlerImpl{
		users:   users,
		tokens:  tokens,
		openReg: openReg,
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type updateProfileRequest struct {
	Name string `json:"name"`
}

type authResponse struct {
	User         *userResponse `json:"user"`
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	ExpiresIn    int           `json:"expires_in"`
	TokenType    string        `json:"token_type"`
}

type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func toUserResponse(u *store.User) *userResponse {
	return &userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// Register creates a new user account.
// The first user is automatically granted admin role.
// Subsequent registrations require open registration or admin access.
func (h *AuthHandlerImpl) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeBadRequest(w, "Email and password are required")
		return
	}
	if len(req.Password) < 8 {
		writeBadRequest(w, "Password must be at least 8 characters")
		return
	}

	// Determine role: first user gets admin, rest get user
	role := string(user.UserRoleUser)
	users, total, err := h.users.List(r.Context(), 1, 0)
	if err != nil {
		slog.Error("failed to count users", "error", err)
		writeInternalError(w, "Failed to check user count")
		return
	}
	_ = users

	if total == 0 {
		role = string(user.UserRoleAdmin)
	} else if !h.openReg {
		// Check if the requester is an admin (for invite-only registration)
		_, hasUser := middleware.GetUserFromContext(r.Context())
		roles, hasRoles := middleware.GetRolesFromContext(r.Context())
		isAdmin := false
		if hasUser && hasRoles {
			for _, r := range roles {
				if r == string(user.UserRoleAdmin) {
					isAdmin = true
					break
				}
			}
		}
		if !isAdmin {
			writeForbidden(w, "Registration is disabled. Contact an administrator.")
			return
		}
	}

	// Hash password
	hash, err := user.HashPassword(req.Password)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		writeInternalError(w, "Failed to process password")
		return
	}

	// Create user
	created, err := h.users.Create(r.Context(), store.CreateUserInput{
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Role:         role,
	})
	if err != nil {
		if isAlreadyExistsError(err) {
			writeConflict(w, "A user with this email already exists")
			return
		}
		slog.Error("failed to create user", "error", err)
		writeInternalError(w, "Failed to create user")
		return
	}

	// Generate tokens
	pair, err := h.tokens.GenerateTokenPair(created.ID, created.Email, created.Role)
	if err != nil {
		slog.Error("failed to generate tokens", "error", err)
		writeInternalError(w, "Failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		User:         toUserResponse(created),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		TokenType:    pair.TokenType,
	})
}

// Login authenticates a user with email and password.
func (h *AuthHandlerImpl) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeBadRequest(w, "Email and password are required")
		return
	}

	// Look up user by email
	u, err := h.users.GetByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil {
		writeUnauthorized(w, "Invalid email or password")
		return
	}

	// Check account status
	if u.Status != string(user.UserStatusActive) {
		writeForbidden(w, "Account is disabled")
		return
	}

	// Verify password
	if !user.CheckPassword(u.PasswordHash, req.Password) {
		writeUnauthorized(w, "Invalid email or password")
		return
	}

	// Generate tokens
	pair, err := h.tokens.GenerateTokenPair(u.ID, u.Email, u.Role)
	if err != nil {
		slog.Error("failed to generate tokens", "error", err)
		writeInternalError(w, "Failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		User:         toUserResponse(u),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		TokenType:    pair.TokenType,
	})
}

// RefreshToken exchanges a refresh token for a new token pair.
func (h *AuthHandlerImpl) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}

	if req.RefreshToken == "" {
		writeBadRequest(w, "Refresh token is required")
		return
	}

	// Validate refresh token
	claims, err := h.tokens.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		writeUnauthorized(w, "Invalid or expired refresh token")
		return
	}

	// Look up user to ensure they still exist and are active
	u, err := h.users.GetByID(r.Context(), claims.Subject)
	if err != nil {
		writeUnauthorized(w, "User not found")
		return
	}
	if u.Status != string(user.UserStatusActive) {
		writeForbidden(w, "Account is disabled")
		return
	}

	// Generate new token pair
	pair, err := h.tokens.GenerateTokenPair(u.ID, u.Email, u.Role)
	if err != nil {
		slog.Error("failed to generate tokens", "error", err)
		writeInternalError(w, "Failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		User:         toUserResponse(u),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		TokenType:    pair.TokenType,
	})
}

// GetProfile returns the current user's profile.
func (h *AuthHandlerImpl) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, "Authentication required")
		return
	}

	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		writeNotFound(w, "User not found")
		return
	}

	writeSuccess(w, toUserResponse(u))
}

// UpdateProfile updates the current user's profile.
func (h *AuthHandlerImpl) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, "Authentication required")
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	updated, err := h.users.Update(r.Context(), userID, store.UpdateUserInput{
		Name: &name,
	})
	if err != nil {
		slog.Error("failed to update profile", "error", err)
		writeInternalError(w, "Failed to update profile")
		return
	}

	writeSuccess(w, toUserResponse(updated))
}
