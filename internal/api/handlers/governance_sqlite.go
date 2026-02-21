package handlers

import (
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

// SQLiteGovernanceHandler handles governance-related HTTP requests backed by SQLite.
type SQLiteGovernanceHandler struct {
	escalationStore *database.SQLiteEscalationStore
}

// NewSQLiteGovernanceHandler creates a new SQLite-backed governance handler.
func NewSQLiteGovernanceHandler(es *database.SQLiteEscalationStore) *SQLiteGovernanceHandler {
	return &SQLiteGovernanceHandler{escalationStore: es}
}

// CheckAction handles POST /v1/governance/check
// In SQLite mode there is no policy engine — default allow with a warning.
func (h *SQLiteGovernanceHandler) CheckAction(w http.ResponseWriter, r *http.Request) {
	var req CheckActionRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.SessionID == "" {
		writeBadRequest(w, "session_id is required")
		return
	}
	if req.ActionType == "" {
		writeBadRequest(w, "action_type is required")
		return
	}

	response := CheckActionResponse{
		Allowed:     true,
		PolicyNames: []string{},
		Warnings:    []string{"no policy engine configured (SQLite mode)"},
	}

	writeSuccess(w, response)
}

// RequestApproval handles POST /v1/governance/approvals
func (h *SQLiteGovernanceHandler) RequestApproval(w http.ResponseWriter, r *http.Request) {
	var req RequestApprovalRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.SessionID == "" {
		writeBadRequest(w, "session_id is required")
		return
	}
	if req.Action == "" {
		writeBadRequest(w, "action is required")
		return
	}
	if req.Reason == "" {
		writeBadRequest(w, "reason is required")
		return
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}

	var expiresAt time.Time
	switch req.Priority {
	case "critical":
		expiresAt = time.Now().Add(15 * time.Minute)
	case "high":
		expiresAt = time.Now().Add(30 * time.Minute)
	case "normal":
		expiresAt = time.Now().Add(1 * time.Hour)
	case "low":
		expiresAt = time.Now().Add(4 * time.Hour)
	default:
		expiresAt = time.Now().Add(1 * time.Hour)
	}

	input := store.CreateEscalationInput{
		SessionID:      req.SessionID,
		DecisionID:     req.DecisionID,
		Action:         req.Action,
		ActionType:     req.ActionType,
		Target:         req.Target,
		Reason:         req.Reason,
		PolicyNames:    req.PolicyNames,
		ContextSummary: req.ContextSummary,
		Priority:       req.Priority,
		ExpiresAt:      &expiresAt,
	}

	escalation, err := h.escalationStore.Create(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to create approval request: "+err.Error())
		return
	}

	writeCreated(w, escalation)
}

// GetApproval handles GET /v1/governance/approvals/{id}
func (h *SQLiteGovernanceHandler) GetApproval(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Approval ID is required")
		return
	}

	escalation, err := h.escalationStore.Get(r.Context(), id)
	if err != nil {
		writeNotFound(w, "Approval request not found")
		return
	}

	writeSuccess(w, escalation)
}

// ListApprovals handles GET /v1/governance/approvals
func (h *SQLiteGovernanceHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	filter := database.SQLiteEscalationFilter{
		SessionID: getQueryParam(r, "session_id", ""),
		Status:    getQueryParam(r, "status", ""),
		Priority:  getQueryParam(r, "priority", ""),
		Limit:     getQueryParamInt(r, "limit", 50),
		Offset:    getQueryParamInt(r, "offset", 0),
	}

	escalations, err := h.escalationStore.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list approval requests: "+err.Error())
		return
	}

	writeList(w, escalations, -1, filter.Limit, filter.Offset)
}

// ListPendingApprovals handles GET /v1/governance/approvals/pending
func (h *SQLiteGovernanceHandler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	limit := getQueryParamInt(r, "limit", 50)
	offset := getQueryParamInt(r, "offset", 0)

	escalations, err := h.escalationStore.ListPending(r.Context(), limit)
	if err != nil {
		writeInternalError(w, "Failed to list pending approvals: "+err.Error())
		return
	}

	writeList(w, escalations, -1, limit, offset)
}

// ResolveApproval handles PATCH /v1/governance/approvals/{id}
func (h *SQLiteGovernanceHandler) ResolveApproval(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Approval ID is required")
		return
	}

	var req ResolveApprovalRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	input := database.SQLiteResolveInput{
		ApproverID: "local-user",
		Approved:   req.Approved,
		Comment:    req.Comment,
	}

	escalation, err := h.escalationStore.Resolve(r.Context(), id, input)
	if err != nil {
		writeNotFound(w, "Approval request not found or already resolved")
		return
	}

	writeSuccess(w, escalation)
}
