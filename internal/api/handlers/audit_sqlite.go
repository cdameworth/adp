package handlers

import (
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

// SQLiteAuditHandler handles audit-related HTTP requests backed by SQLite.
type SQLiteAuditHandler struct {
	decisionStore *database.SQLiteDecisionStore
	commitStore   *database.SQLiteCommitStore
}

// NewSQLiteAuditHandler creates a new SQLite-backed audit handler.
func NewSQLiteAuditHandler(ds *database.SQLiteDecisionStore, cs *database.SQLiteCommitStore) *SQLiteAuditHandler {
	return &SQLiteAuditHandler{
		decisionStore: ds,
		commitStore:   cs,
	}
}

// LogDecision handles POST /v1/audit/decisions
func (h *SQLiteAuditHandler) LogDecision(w http.ResponseWriter, r *http.Request) {
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

	if req.Confidence == 0 {
		req.Confidence = 0.8
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		writeBadRequest(w, "confidence must be between 0 and 1")
		return
	}

	var alternatives []store.Alternative
	for _, alt := range req.Alternatives {
		alternatives = append(alternatives, store.Alternative{
			Action:     alt.Action,
			Reason:     alt.Reason,
			Confidence: alt.Confidence,
		})
	}

	var policyResult *store.PolicyResult
	if req.PolicyResult != nil {
		policyResult = &store.PolicyResult{
			Allowed:       req.PolicyResult.Allowed,
			DeniedReasons: req.PolicyResult.DeniedReasons,
			PolicyNames:   req.PolicyResult.PolicyNames,
			EvaluatedAt:   time.Now().Format(time.RFC3339),
		}
	}

	input := store.CreateDecisionInput{
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
func (h *SQLiteAuditHandler) GetDecision(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Decision ID is required")
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
func (h *SQLiteAuditHandler) ListDecisions(w http.ResponseWriter, r *http.Request) {
	filter := database.SQLiteDecisionFilter{
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
func (h *SQLiteAuditHandler) GetLineage(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Decision ID is required")
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
func (h *SQLiteAuditHandler) PrepareCommit(w http.ResponseWriter, r *http.Request) {
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

	input := store.PrepareCommitInput{
		SessionID: req.SessionID,
		Files:     req.Files,
		Message:   req.Message,
	}

	commit, err := h.commitStore.Prepare(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to prepare commit: "+err.Error())
		return
	}

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
func (h *SQLiteAuditHandler) RegisterCommit(w http.ResponseWriter, r *http.Request) {
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
func (h *SQLiteAuditHandler) VerifyCommit(w http.ResponseWriter, r *http.Request) {
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
		commit, getErr := h.commitStore.GetBySHA(r.Context(), req.CommitSHA)
		if getErr != nil {
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
