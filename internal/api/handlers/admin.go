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

// AdminHandlerImpl implements the AdminHandler interface for user management.
type AdminHandlerImpl struct {
	users store.UserStore
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(users store.UserStore) *AdminHandlerImpl {
	return &AdminHandlerImpl{users: users}
}

// requireAdmin checks that the request comes from an admin user.
func requireAdmin(r *http.Request) (string, bool) {
	userID, hasUser := middleware.GetUserFromContext(r.Context())
	if !hasUser {
		return "", false
	}
	roles, hasRoles := middleware.GetRolesFromContext(r.Context())
	if !hasRoles {
		return "", false
	}
	for _, role := range roles {
		if role == string(user.UserRoleAdmin) {
			return userID, true
		}
	}
	return "", false
}

// ListUsers returns a paginated list of all users.
func (h *AdminHandlerImpl) ListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeForbidden(w, "Admin access required")
		return
	}

	limit := parseIntParam(r, "limit", 20)
	offset := parseIntParam(r, "offset", 0)

	users, total, err := h.users.List(r.Context(), limit, offset)
	if err != nil {
		slog.Error("failed to list users", "error", err)
		writeInternalError(w, "Failed to list users")
		return
	}

	items := make([]*userResponse, len(users))
	for i, u := range users {
		items[i] = toUserResponse(u)
	}

	writeList(w, items, total, limit, offset)
}

// GetUser returns a single user by ID.
func (h *AdminHandlerImpl) GetUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeForbidden(w, "Admin access required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "User ID is required")
		return
	}

	u, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeNotFound(w, "User not found")
			return
		}
		slog.Error("failed to get user", "error", err)
		writeInternalError(w, "Failed to get user")
		return
	}

	writeSuccess(w, toUserResponse(u))
}

type createUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type updateUserRequest struct {
	Name   *string `json:"name,omitempty"`
	Role   *string `json:"role,omitempty"`
	Status *string `json:"status,omitempty"`
}

// CreateUser creates a new user account (admin only).
func (h *AdminHandlerImpl) CreateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeForbidden(w, "Admin access required")
		return
	}

	var req createUserRequest
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
	if req.Role == "" {
		req.Role = string(user.UserRoleUser)
	}
	if !user.ValidRole(req.Role) {
		writeBadRequest(w, "Invalid role. Must be 'admin' or 'user'")
		return
	}

	hash, err := user.HashPassword(req.Password)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		writeInternalError(w, "Failed to process password")
		return
	}

	created, err := h.users.Create(r.Context(), store.CreateUserInput{
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Role:         req.Role,
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

	writeCreated(w, toUserResponse(created))
}

// UpdateUser updates a user's role, status, or name.
func (h *AdminHandlerImpl) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeForbidden(w, "Admin access required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "User ID is required")
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}

	// Validate role if provided
	if req.Role != nil && !user.ValidRole(*req.Role) {
		writeBadRequest(w, "Invalid role. Must be 'admin' or 'user'")
		return
	}
	// Validate status if provided
	if req.Status != nil && !user.ValidStatus(*req.Status) {
		writeBadRequest(w, "Invalid status. Must be 'active' or 'disabled'")
		return
	}

	updated, err := h.users.Update(r.Context(), id, store.UpdateUserInput{
		Name:   req.Name,
		Role:   req.Role,
		Status: req.Status,
	})
	if err != nil {
		if isNotFoundError(err) {
			writeNotFound(w, "User not found")
			return
		}
		slog.Error("failed to update user", "error", err)
		writeInternalError(w, "Failed to update user")
		return
	}

	writeSuccess(w, toUserResponse(updated))
}

// DisableUser soft-deletes a user by setting status to disabled.
func (h *AdminHandlerImpl) DisableUser(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdmin(r)
	if !ok {
		writeForbidden(w, "Admin access required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "User ID is required")
		return
	}

	// Prevent self-deletion
	if id == adminID {
		writeBadRequest(w, "Cannot disable your own account")
		return
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		if isNotFoundError(err) {
			writeNotFound(w, "User not found")
			return
		}
		slog.Error("failed to disable user", "error", err)
		writeInternalError(w, "Failed to disable user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "User disabled"})
}
