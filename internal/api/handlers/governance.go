package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/adp/adp/internal/domain/governance"
	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// UnifiedPolicyEngine interface for the new unified policy engine
type UnifiedPolicyEngine interface {
	Evaluate(ctx context.Context, input *governance.EvaluationInput) (*governance.EvaluationResult, error)
}

// LegacyPolicyEngine interface for the old OPA-only engine (kept for backwards compatibility)
type LegacyPolicyEngine interface {
	Evaluate(ctx context.Context, input interface{}, query string) (bool, error)
}

// GovernanceHandler handles governance-related HTTP requests
type GovernanceHandler struct {
	unifiedEngine   UnifiedPolicyEngine
	legacyEngine    LegacyPolicyEngine
	escalationStore *database.EscalationStore
}

// NewGovernanceHandler creates a new governance handler with the unified engine
func NewGovernanceHandler(engine UnifiedPolicyEngine, es *database.EscalationStore) *GovernanceHandler {
	return &GovernanceHandler{
		unifiedEngine:   engine,
		escalationStore: es,
	}
}

// NewGovernanceHandlerLegacy creates a handler with the legacy OPA-only engine
func NewGovernanceHandlerLegacy(pe LegacyPolicyEngine, es *database.EscalationStore) *GovernanceHandler {
	return &GovernanceHandler{
		legacyEngine:    pe,
		escalationStore: es,
	}
}

// API request/response types

type CheckActionRequest struct {
	SessionID  string                 `json:"session_id"`
	TrustLevel int                    `json:"trust_level"`
	ActionType string                 `json:"action_type"`
	Target     map[string]interface{} `json:"target"`
	Context    map[string]interface{} `json:"context"`
}

type CheckActionResponse struct {
	Allowed          bool     `json:"allowed"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	DeniedReasons    []string `json:"denied_reasons,omitempty"`
	PolicyNames      []string `json:"policy_names"`
	Warnings         []string `json:"warnings,omitempty"`
	Restrictions     []string `json:"restrictions,omitempty"`
}

type RequestApprovalRequest struct {
	SessionID      string                 `json:"session_id"`
	DecisionID     string                 `json:"decision_id,omitempty"`
	Action         string                 `json:"action"`
	ActionType     string                 `json:"action_type"`
	Target         map[string]interface{} `json:"target"`
	Reason         string                 `json:"reason"`
	PolicyNames    []string               `json:"policy_names"`
	ContextSummary map[string]interface{} `json:"context_summary"`
	Priority       string                 `json:"priority"`
}

type ResolveApprovalRequest struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

// CheckAction handles POST /v1/governance/check
func (h *GovernanceHandler) CheckAction(w http.ResponseWriter, r *http.Request) {
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

	// Default trust level
	if req.TrustLevel == 0 {
		req.TrustLevel = 1
	}

	response := CheckActionResponse{
		PolicyNames:   []string{},
		DeniedReasons: []string{},
		Warnings:      []string{},
	}

	// Use unified engine if available, otherwise fall back to legacy
	if h.unifiedEngine != nil {
		result, err := h.evaluateWithUnifiedEngine(r.Context(), &req)
		if err != nil {
			response.Allowed = false
			response.DeniedReasons = []string{"Policy evaluation error: " + err.Error()}
		} else {
			response.Allowed = result.Allowed
			response.RequiresApproval = result.RequiresApproval
			response.DeniedReasons = result.DeniedReasons
			response.PolicyNames = result.MatchedPolicies
			response.Warnings = result.Warnings
		}
	} else if h.legacyEngine != nil {
		allowed, err := h.evaluateWithLegacyEngine(r.Context(), &req)
		if err != nil {
			response.Allowed = false
			response.DeniedReasons = []string{"Policy evaluation error: " + err.Error()}
		} else {
			response.Allowed = allowed
			response.PolicyNames = []string{"base_policy"}
		}

		if !response.Allowed {
			response.RequiresApproval = true
		}
	} else {
		// No engine configured - default allow
		response.Allowed = true
		response.Warnings = append(response.Warnings, "no policy engine configured")
	}

	// Add any restrictions based on target
	if req.Target != nil {
		if env, ok := req.Target["environment"].(string); ok && env == "production" {
			response.Restrictions = append(response.Restrictions, "production_environment_restrictions")
		}
	}

	writeSuccess(w, response)
}

func (h *GovernanceHandler) evaluateWithUnifiedEngine(ctx context.Context, req *CheckActionRequest) (*governance.EvaluationResult, error) {
	// Extract target paths
	var paths []string
	if req.Target != nil {
		if p, ok := req.Target["paths"].([]interface{}); ok {
			for _, path := range p {
				if s, ok := path.(string); ok {
					paths = append(paths, s)
				}
			}
		}
		if p, ok := req.Target["path"].(string); ok {
			paths = append(paths, p)
		}
	}

	// Extract target services
	var services []string
	if req.Target != nil {
		if s, ok := req.Target["services"].([]interface{}); ok {
			for _, svc := range s {
				if str, ok := svc.(string); ok {
					services = append(services, str)
				}
			}
		}
	}

	// Extract environment
	environment := ""
	if req.Target != nil {
		if env, ok := req.Target["environment"].(string); ok {
			environment = env
		}
	}
	if req.Context != nil {
		if env, ok := req.Context["environment"].(string); ok {
			environment = env
		}
	}

	// Extract hour from context
	hour := time.Now().Hour()
	if req.Context != nil {
		if h, ok := req.Context["hour"].(float64); ok {
			hour = int(h)
		}
	}

	// Build evaluation input
	input := &governance.EvaluationInput{
		SessionID:  req.SessionID,
		TrustLevel: req.TrustLevel,
		Action: governance.ActionEvalInput{
			Type: req.ActionType,
			Target: governance.TargetEvalInput{
				Paths:       paths,
				Services:    services,
				Environment: environment,
			},
			Metadata: req.Target,
		},
		Context: governance.ContextEvalInput{
			Environment: environment,
			Time:        time.Now(),
			Hour:        hour,
		},
		Session: governance.SessionEvalInput{
			TrustLevel: req.TrustLevel,
		},
	}

	return h.unifiedEngine.Evaluate(ctx, input)
}

func (h *GovernanceHandler) evaluateWithLegacyEngine(ctx context.Context, req *CheckActionRequest) (bool, error) {
	policyInput := map[string]interface{}{
		"session_id":  req.SessionID,
		"action_type": req.ActionType,
		"target":      req.Target,
		"context":     req.Context,
	}

	return h.legacyEngine.Evaluate(ctx, policyInput, "data.adp.governance.allow")
}

// RequestApproval handles POST /v1/governance/approvals
func (h *GovernanceHandler) RequestApproval(w http.ResponseWriter, r *http.Request) {
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

	// Default priority
	if req.Priority == "" {
		req.Priority = "normal"
	}

	// Parse decision ID if provided
	var decisionID *uuid.UUID
	if req.DecisionID != "" {
		id, err := uuid.Parse(req.DecisionID)
		if err != nil {
			writeBadRequest(w, "Invalid decision_id")
			return
		}
		decisionID = &id
	}

	// Calculate expiration based on priority
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

	input := database.CreateEscalationInput{
		SessionID:      req.SessionID,
		DecisionID:     decisionID,
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
func (h *GovernanceHandler) GetApproval(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Approval ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid approval ID")
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
func (h *GovernanceHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	filter := database.EscalationFilter{
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
func (h *GovernanceHandler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
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
func (h *GovernanceHandler) ResolveApproval(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Approval ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid approval ID")
		return
	}

	var req ResolveApprovalRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	// Get approver ID from auth context (placeholder - would come from JWT)
	approverID := uuid.New() // TODO: Extract from auth context

	input := database.ResolveInput{
		ApproverID: approverID,
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
