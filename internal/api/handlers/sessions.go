package handlers

import (
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// SessionHandler handles session-related HTTP requests
type SessionHandler struct {
	store *database.SessionStore
}

// NewSessionHandler creates a new session handler
func NewSessionHandler(store *database.SessionStore) *SessionHandler {
	return &SessionHandler{store: store}
}

// API request/response types

type CreateSessionRequest struct {
	OrganizationID string   `json:"organization_id"`
	UserID         string   `json:"user_id"`
	Tool           string   `json:"tool"`
	TrustLevel     int      `json:"trust_level"`
	Capabilities   []string `json:"capabilities"`
	ServiceScope   []string `json:"service_scope"`
	TTLHours       int      `json:"ttl_hours"`
}

type UpdateSessionRequest struct {
	TrustLevel   *int     `json:"trust_level,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	Status       string   `json:"status,omitempty"`
}

// CreateSession handles POST /v1/sessions
func (h *SessionHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.Tool == "" {
		writeBadRequest(w, "tool is required")
		return
	}

	// Parse UUIDs
	orgID, err := uuid.Parse(req.OrganizationID)
	if err != nil {
		writeBadRequest(w, "Invalid organization_id: must be a valid UUID")
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		writeBadRequest(w, "Invalid user_id: must be a valid UUID")
		return
	}

	// Parse service scope UUIDs
	var serviceScope []uuid.UUID
	for _, s := range req.ServiceScope {
		id, err := uuid.Parse(s)
		if err != nil {
			writeBadRequest(w, "Invalid service_scope: must be valid UUIDs")
			return
		}
		serviceScope = append(serviceScope, id)
	}

	// Default values
	trustLevel := req.TrustLevel
	if trustLevel == 0 {
		trustLevel = 2 // Contributor
	}
	if trustLevel < 1 || trustLevel > 5 {
		writeBadRequest(w, "trust_level must be between 1 and 5")
		return
	}

	ttlHours := req.TTLHours
	if ttlHours == 0 {
		ttlHours = 8
	}

	// Generate session ID
	sessionID := generateSessionID()

	input := database.CreateSessionInput{
		ID:             sessionID,
		OrganizationID: orgID,
		UserID:         userID,
		Tool:           req.Tool,
		TrustLevel:     trustLevel,
		Capabilities:   req.Capabilities,
		ServiceScope:   serviceScope,
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
func (h *SessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
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
func (h *SessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	filter := database.SessionFilter{
		Tool:          getQueryParam(r, "tool", ""),
		Status:        getQueryParam(r, "status", ""),
		MinTrustLevel: getQueryParamInt(r, "min_trust_level", 0),
		MaxTrustLevel: getQueryParamInt(r, "max_trust_level", 0),
		Limit:         getQueryParamInt(r, "limit", 50),
		Offset:        getQueryParamInt(r, "offset", 0),
	}

	// Parse organization_id if provided
	if orgIDStr := getQueryParam(r, "organization_id", ""); orgIDStr != "" {
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			writeBadRequest(w, "Invalid organization_id")
			return
		}
		filter.OrganizationID = &orgID
	}

	// Parse user_id if provided
	if userIDStr := getQueryParam(r, "user_id", ""); userIDStr != "" {
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			writeBadRequest(w, "Invalid user_id")
			return
		}
		filter.UserID = &userID
	}

	sessions, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list sessions: "+err.Error())
		return
	}

	writeList(w, sessions, -1, filter.Limit, filter.Offset)
}

// UpdateSession handles PATCH /v1/sessions/{id}
func (h *SessionHandler) UpdateSession(w http.ResponseWriter, r *http.Request) {
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

	input := database.UpdateSessionInput{
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
func (h *SessionHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
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
func (h *SessionHandler) EndSession(w http.ResponseWriter, r *http.Request) {
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

// Helper function to generate session ID
func generateSessionID() string {
	return "adp_" + uuid.New().String()[:8] + "_" + time.Now().Format("20060102")
}
