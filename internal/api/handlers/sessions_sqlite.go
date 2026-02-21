package handlers

import (
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

// SQLiteSessionHandler handles session-related HTTP requests backed by SQLite.
type SQLiteSessionHandler struct {
	store *database.SQLiteSessionStore
}

// NewSQLiteSessionHandler creates a new SQLite-backed session handler.
func NewSQLiteSessionHandler(store *database.SQLiteSessionStore) *SQLiteSessionHandler {
	return &SQLiteSessionHandler{store: store}
}

// CreateSession handles POST /v1/sessions
func (h *SQLiteSessionHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.Tool == "" {
		writeBadRequest(w, "tool is required")
		return
	}

	trustLevel := req.TrustLevel
	if trustLevel == 0 {
		trustLevel = 2
	}
	if trustLevel < 1 || trustLevel > 5 {
		writeBadRequest(w, "trust_level must be between 1 and 5")
		return
	}

	ttlHours := req.TTLHours
	if ttlHours == 0 {
		ttlHours = 8
	}

	sessionID := generateSessionID()

	input := store.CreateSessionInput{
		ID:             sessionID,
		OrganizationID: req.OrganizationID,
		UserID:         req.UserID,
		Tool:           req.Tool,
		TrustLevel:     trustLevel,
		Capabilities:   req.Capabilities,
		ServiceScope:   req.ServiceScope,
		ExpiresAt:      time.Now().Add(time.Duration(ttlHours) * time.Hour),
	}

	session, err := h.store.Create(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to create session: "+err.Error())
		return
	}

	writeCreated(w, session)
}

// GetSession handles GET /v1/sessions/{id}
func (h *SQLiteSessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Session ID is required")
		return
	}

	session, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeNotFound(w, "Session not found")
		return
	}

	writeSuccess(w, session)
}

// ListSessions handles GET /v1/sessions
func (h *SQLiteSessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	filter := database.SQLiteSessionFilter{
		OrganizationID: getQueryParam(r, "organization_id", ""),
		UserID:         getQueryParam(r, "user_id", ""),
		Tool:           getQueryParam(r, "tool", ""),
		Status:         getQueryParam(r, "status", ""),
		MinTrustLevel:  getQueryParamInt(r, "min_trust_level", 0),
		MaxTrustLevel:  getQueryParamInt(r, "max_trust_level", 0),
		Limit:          getQueryParamInt(r, "limit", 50),
		Offset:         getQueryParamInt(r, "offset", 0),
	}

	sessions, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list sessions: "+err.Error())
		return
	}

	writeList(w, sessions, -1, filter.Limit, filter.Offset)
}

// UpdateSession handles PATCH /v1/sessions/{id}
func (h *SQLiteSessionHandler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Session ID is required")
		return
	}

	var req UpdateSessionRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	input := database.SQLiteUpdateSessionInput{
		TrustLevel:   req.TrustLevel,
		Capabilities: req.Capabilities,
		Constraints:  req.Constraints,
		Status:       req.Status,
	}

	session, err := h.store.Update(r.Context(), id, input)
	if err != nil {
		writeNotFound(w, "Session not found")
		return
	}

	writeSuccess(w, session)
}

// Heartbeat handles PATCH /v1/sessions/{id}/heartbeat
func (h *SQLiteSessionHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Session ID is required")
		return
	}

	if err := h.store.Heartbeat(r.Context(), id); err != nil {
		writeNotFound(w, "Session not found or not active")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"session_id":     id,
		"last_heartbeat": time.Now().Format(time.RFC3339),
	})
}

// EndSession handles DELETE /v1/sessions/{id}
func (h *SQLiteSessionHandler) EndSession(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Session ID is required")
		return
	}

	if err := h.store.End(r.Context(), id); err != nil {
		writeNotFound(w, "Session not found or already ended")
		return
	}

	writeSuccess(w, map[string]string{
		"message": "Session ended successfully",
	})
}
