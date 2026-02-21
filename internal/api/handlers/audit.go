package handlers

import (
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// AuditHandler handles audit-related HTTP requests
type AuditHandler struct {
	decisionStore *database.DecisionStore
	commitStore   *database.CommitStore
}

// NewAuditHandler creates a new audit handler
func NewAuditHandler(ds *database.DecisionStore, cs *database.CommitStore) *AuditHandler {
	return &AuditHandler{
		decisionStore: ds,
		commitStore:   cs,
	}
}

// API request types

type LogDecisionRequest struct {
	SessionID       string                 `json:"session_id"`
	DecisionType    string                 `json:"decision_type"`
	Action          string                 `json:"action"`
	Target          map[string]interface{} `json:"target"`
	Reasoning       map[string]interface{} `json:"reasoning"`
	Confidence      float64                `json:"confidence"`
	Alternatives    []AlternativeRequest   `json:"alternatives"`
	ContextSnapshot map[string]interface{} `json:"context_snapshot"`
	PolicyResult    *PolicyResultRequest   `json:"policy_result,omitempty"`
}

type AlternativeRequest struct {
	Action     string  `json:"action"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

type PolicyResultRequest struct {
	Allowed       bool     `json:"allowed"`
	DeniedReasons []string `json:"denied_reasons,omitempty"`
	PolicyNames   []string `json:"policy_names"`
}

type PrepareCommitRequest struct {
	SessionID string   `json:"session_id"`
	Files     []string `json:"files"`
	Message   string   `json:"message"`
}

type RegisterCommitRequest struct {
	CommitToken string `json:"commit_token"`
	CommitSHA   string `json:"commit_sha"`
}

type VerifyCommitRequest struct {
	CommitSHA string `json:"commit_sha"`
}

// LogDecision handles POST /v1/audit/decisions
func (h *AuditHandler) LogDecision(w http.ResponseWriter, r *http.Request) {
	var req LogDecisionRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.SessionID == "" {
		writeBadRequest(w, "session_id is required")
		return
	}
	if req.DecisionType == "" {
		writeBadRequest(w, "decision_type is required")
		return
	}
	if req.Action == "" {
		writeBadRequest(w, "action is required")
		return
	}

	// Default confidence
	if req.Confidence == 0 {
		req.Confidence = 0.8
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		writeBadRequest(w, "confidence must be between 0 and 1")
		return
	}

	// Convert alternatives
	var alternatives []database.Alternative
	for _, alt := range req.Alternatives {
		alternatives = append(alternatives, database.Alternative{
			Action:     alt.Action,
			Reason:     alt.Reason,
			Confidence: alt.Confidence,
		})
	}

	// Convert policy result
	var policyResult *database.PolicyResult
	if req.PolicyResult != nil {
		policyResult = &database.PolicyResult{
			Allowed:       req.PolicyResult.Allowed,
			DeniedReasons: req.PolicyResult.DeniedReasons,
			PolicyNames:   req.PolicyResult.PolicyNames,
			EvaluatedAt:   time.Now().Format(time.RFC3339),
		}
	}

	input := database.CreateDecisionInput{
		SessionID:       req.SessionID,
		DecisionType:    req.DecisionType,
		Action:          req.Action,
		Target:          req.Target,
		Reasoning:       req.Reasoning,
		Confidence:      req.Confidence,
		Alternatives:    alternatives,
		ContextSnapshot: req.ContextSnapshot,
		PolicyResult:    policyResult,
	}

	decision, err := h.decisionStore.Create(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to log decision: "+err.Error())
		return
	}

	writeCreated(w, decision)
}

// GetDecision handles GET /v1/audit/decisions/{id}
func (h *AuditHandler) GetDecision(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Decision ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid decision ID")
		return
	}

	decision, err := h.decisionStore.Get(r.Context(), id)
	if err != nil {
		writeNotFound(w, "Decision not found")
		return
	}

	writeSuccess(w, decision)
}

// ListDecisions handles GET /v1/audit/decisions
func (h *AuditHandler) ListDecisions(w http.ResponseWriter, r *http.Request) {
	filter := database.DecisionFilter{
		SessionID:    getQueryParam(r, "session_id", ""),
		DecisionType: getQueryParam(r, "decision_type", ""),
		Status:       getQueryParam(r, "status", ""),
		Limit:        getQueryParamInt(r, "limit", 50),
		Offset:       getQueryParamInt(r, "offset", 0),
	}

	decisions, err := h.decisionStore.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list decisions: "+err.Error())
		return
	}

	writeList(w, decisions, -1, filter.Limit, filter.Offset)
}

// GetLineage handles GET /v1/audit/decisions/{id}/lineage
func (h *AuditHandler) GetLineage(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Decision ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid decision ID")
		return
	}

	depth := getQueryParamInt(r, "depth", 10)

	lineage, err := h.decisionStore.GetLineage(r.Context(), id, depth)
	if err != nil {
		writeInternalError(w, "Failed to get lineage: "+err.Error())
		return
	}

	writeSuccess(w, lineage)
}

// PrepareCommit handles POST /v1/commits/prepare
func (h *AuditHandler) PrepareCommit(w http.ResponseWriter, r *http.Request) {
	var req PrepareCommitRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.SessionID == "" {
		writeBadRequest(w, "session_id is required")
		return
	}
	if len(req.Files) == 0 {
		writeBadRequest(w, "files are required")
		return
	}

	input := database.PrepareCommitInput{
		SessionID: req.SessionID,
		Files:     req.Files,
		Message:   req.Message,
	}

	commit, err := h.commitStore.Prepare(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to prepare commit: "+err.Error())
		return
	}

	// Auto-approve if no sensitive files
	approved := true
	reason := "Automatic approval"
	for _, file := range req.Files {
		if isSensitivePath(file) {
			approved = false
			reason = "Contains sensitive files"
			break
		}
	}

	if approved {
		commit, err = h.commitStore.Approve(r.Context(), commit.ID, reason)
		if err != nil {
			writeInternalError(w, "Failed to approve commit: "+err.Error())
			return
		}
	}

	writeCreated(w, map[string]interface{}{
		"commit_token": commit.CommitToken,
		"approved":     commit.Approved,
		"reason":       reason,
	})
}

// RegisterCommit handles POST /v1/commits/register
func (h *AuditHandler) RegisterCommit(w http.ResponseWriter, r *http.Request) {
	var req RegisterCommitRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.CommitToken == "" {
		writeBadRequest(w, "commit_token is required")
		return
	}
	if req.CommitSHA == "" {
		writeBadRequest(w, "commit_sha is required")
		return
	}

	commit, err := h.commitStore.RegisterCommit(r.Context(), req.CommitToken, req.CommitSHA)
	if err != nil {
		if isNotFoundError(err) {
			writeNotFound(w, "Commit token not found or not in registerable state")
			return
		}
		writeInternalError(w, "Failed to register commit: "+err.Error())
		return
	}

	writeSuccess(w, commit)
}

// VerifyCommit handles POST /v1/commits/verify
func (h *AuditHandler) VerifyCommit(w http.ResponseWriter, r *http.Request) {
	var req VerifyCommitRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.CommitSHA == "" {
		writeBadRequest(w, "commit_sha is required")
		return
	}

	verified, err := h.commitStore.IsCommitVerified(r.Context(), req.CommitSHA)
	if err != nil {
		// Not found or error - check if we have a commit record
		commit, getErr := h.commitStore.GetBySHA(r.Context(), req.CommitSHA)
		if getErr != nil {
			// No record found
			writeSuccess(w, map[string]interface{}{
				"verified":   false,
				"commit_sha": req.CommitSHA,
				"reason":     "No audit trail found for this commit",
			})
			return
		}

		writeSuccess(w, map[string]interface{}{
			"verified":   false,
			"commit_sha": req.CommitSHA,
			"session_id": commit.SessionID,
			"status":     commit.Status,
			"reason":     "Commit found but not verified",
		})
		return
	}

	commit, _ := h.commitStore.GetBySHA(r.Context(), req.CommitSHA)
	var sessionID string
	var verifiedAt *time.Time
	if commit != nil {
		sessionID = commit.SessionID
		verifiedAt = commit.VerifiedAt
	}

	writeSuccess(w, map[string]interface{}{
		"verified":    verified,
		"commit_sha":  req.CommitSHA,
		"session_id":  sessionID,
		"verified_at": verifiedAt,
	})
}

// isSensitivePath checks if a path is sensitive
func isSensitivePath(path string) bool {
	sensitivePaths := []string{
		".env",
		".secrets",
		"credentials",
		"secrets.yaml",
		"secrets.json",
		".aws",
		".ssh",
		"id_rsa",
		"private_key",
		"service_account",
	}

	for _, sensitive := range sensitivePaths {
		if len(path) >= len(sensitive) &&
			(path == sensitive ||
				path[len(path)-len(sensitive):] == sensitive ||
				containsSubstring(path, "/"+sensitive) ||
				containsSubstring(path, sensitive+"/")) {
			return true
		}
	}

	// Extension-based checks for certificate/key files
	sensitiveExtensions := []string{".pem", ".key"}
	for _, ext := range sensitiveExtensions {
		if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}

	return false
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
