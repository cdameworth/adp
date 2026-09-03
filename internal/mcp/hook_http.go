package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/adp/adp/internal/sensitivepaths"
	"github.com/adp/adp/internal/store"
)

// HookHTTPHandler serves lightweight HTTP endpoints for git hook integration.
// Hooks call these endpoints during pre-commit, post-commit, and pre-push.
//
// Response format: top-level fields (no {"data":...} wrapper) to match
// what the hook scripts expect.
type HookHTTPHandler struct {
	commitStore  store.CommitStore
	sessionStore store.SessionStore
}

// NewHookHTTPHandler creates an HTTP handler for git hook endpoints.
func NewHookHTTPHandler(commitStore store.CommitStore, sessionStore store.SessionStore) http.Handler {
	h := &HookHTTPHandler{commitStore: commitStore, sessionStore: sessionStore}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/commits/prepare", h.handlePrepare)
	mux.HandleFunc("POST /v1/commits/register", h.handleRegister)
	mux.HandleFunc("POST /v1/commits/verify", h.handleVerify)
	mux.HandleFunc("GET /health", h.handleHealth)

	return mux
}

type hookPrepareRequest struct {
	SessionID string   `json:"session_id"`
	Files     []string `json:"files"`
	Message   string   `json:"message"`
}

type hookRegisterRequest struct {
	CommitToken string `json:"commit_token"`
	CommitSHA   string `json:"commit_sha"`
}

type hookVerifyRequest struct {
	CommitSHA string `json:"commit_sha"`
}

func (h *HookHTTPHandler) handlePrepare(w http.ResponseWriter, r *http.Request) {
	var req hookPrepareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		hookError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SessionID == "" {
		hookError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if len(req.Files) == 0 {
		hookError(w, http.StatusBadRequest, "files are required")
		return
	}

	// Validate session token from Authorization header
	if err := h.validateSessionToken(r, req.SessionID); err != nil {
		hookError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if h.commitStore == nil {
		hookError(w, http.StatusServiceUnavailable, "commit store not configured")
		return
	}

	commit, err := h.commitStore.Prepare(r.Context(), store.PrepareCommitInput{
		SessionID: req.SessionID,
		Files:     req.Files,
		Message:   req.Message,
	})
	if err != nil {
		log.Printf("[hook-http] prepare error: %v", err)
		hookError(w, http.StatusInternalServerError, "Failed to prepare commit")
		return
	}

	approved := true
	reason := "Automatic approval"
	for _, file := range req.Files {
		if hookIsSensitivePath(file) {
			approved = false
			reason = "Contains sensitive files"
			break
		}
	}

	hookJSON(w, http.StatusCreated, map[string]interface{}{
		"approved":     approved,
		"commit_token": commit.CommitToken,
		"reason":       reason,
	})
}

func (h *HookHTTPHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req hookRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		hookError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.CommitToken == "" {
		hookError(w, http.StatusBadRequest, "commit_token is required")
		return
	}
	if req.CommitSHA == "" {
		hookError(w, http.StatusBadRequest, "commit_sha is required")
		return
	}

	if h.commitStore == nil {
		hookError(w, http.StatusServiceUnavailable, "commit store not configured")
		return
	}

	commit, err := h.commitStore.RegisterCommit(r.Context(), req.CommitToken, req.CommitSHA)
	if err != nil {
		hookError(w, http.StatusNotFound, "Commit token not found")
		return
	}

	hookJSON(w, http.StatusOK, map[string]interface{}{
		"status":     commit.Status,
		"commit_sha": commit.CommitSHA,
	})
}

func (h *HookHTTPHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req hookVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		hookError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.CommitSHA == "" {
		hookError(w, http.StatusBadRequest, "commit_sha is required")
		return
	}

	if h.commitStore == nil {
		hookError(w, http.StatusServiceUnavailable, "commit store not configured")
		return
	}

	verified, err := h.commitStore.IsCommitVerified(r.Context(), req.CommitSHA)
	if err != nil {
		// Not found in DB — return verified: false.
		hookJSON(w, http.StatusOK, map[string]interface{}{
			"verified":   false,
			"commit_sha": req.CommitSHA,
			"reason":     "No audit trail found for this commit",
		})
		return
	}

	hookJSON(w, http.StatusOK, map[string]interface{}{
		"verified":   verified,
		"commit_sha": req.CommitSHA,
	})
}

func (h *HookHTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	hookJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"server":  "adp-mcp",
		"version": ServerVersion,
	})
}

// hookJSON writes a JSON response with top-level fields (no envelope).
func hookJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// hookError writes an error response matching hook expectations.
func hookError(w http.ResponseWriter, status int, message string) {
	hookJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

// validateSessionToken extracts the bearer token from the Authorization header
// and validates it against the session store. If no session store is configured,
// validation is skipped (local-only mode).
func (h *HookHTTPHandler) validateSessionToken(r *http.Request, sessionID string) error {
	if h.sessionStore == nil {
		return nil // No session store — skip validation (local-only mode)
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("missing Authorization header")
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return fmt.Errorf("invalid authorization format")
	}

	token := parts[1]
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	valid, err := h.sessionStore.ValidateToken(r.Context(), sessionID, tokenHash)
	if err != nil {
		log.Printf("[hook-http] token validation error: %v", err)
		return fmt.Errorf("token validation failed")
	}
	if !valid {
		return fmt.Errorf("invalid session token")
	}
	return nil
}

// hookIsSensitivePath checks if a file path is sensitive using the canonical
// sensitivepaths matcher (single source of truth; see #15).
func hookIsSensitivePath(path string) bool {
	return sensitivepaths.IsSensitive(path)
}
