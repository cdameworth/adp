package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
	"github.com/adp/adp/internal/domain/verification"
)

// VerificationHandler serves the behavioral-verification endpoints (#20).
type VerificationHandler struct {
	store    verification.Store
	keys     verification.KeyStore
	findings enforcement.FindingStore // nil-able: self-attestation findings
	// lookupCommitSession resolves the session that prepared a commit, for
	// self-attestation detection. Nil disables the check (documented).
	lookupCommitSession func(ctx context.Context, sha string) (string, error)
	// govVerified reports governance-trail verification for the status endpoint.
	// Nil-able; status reports it as unknown when unset.
	govVerified func(ctx context.Context, sha string) (bool, error)
	// behavioralRequired reports whether the merge gate currently requires
	// behavioral verification (policy toggle / env). Nil → reported unknown.
	behavioralRequired func(ctx context.Context) bool
	now                func() time.Time
}

// NewVerificationHandler wires the handler. store and keys may be the same
// object (both stores implement both interfaces).
func NewVerificationHandler(
	store verification.Store,
	keys verification.KeyStore,
	lookupCommitSession func(ctx context.Context, sha string) (string, error),
	findings enforcement.FindingStore,
	govVerified func(ctx context.Context, sha string) (bool, error),
	behavioralRequired func(ctx context.Context) bool,
) *VerificationHandler {
	return &VerificationHandler{
		store:               store,
		keys:                keys,
		lookupCommitSession: lookupCommitSession,
		findings:            findings,
		govVerified:         govVerified,
		behavioralRequired:  behavioralRequired,
		now:                 time.Now,
	}
}

// IngestRequest is POST /v1/verifications body.
type IngestRequest struct {
	Repo           string `json:"repo"`
	CommitSHA      string `json:"commit_sha"`
	SessionID      string `json:"session_id,omitempty"`
	Status         string `json:"status"`
	PipelineURL    string `json:"pipeline_url,omitempty"`
	RunnerIdentity string `json:"runner_identity,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"` // RFC3339; defaults to now
}

// Ingest handles POST /v1/verifications. Authenticated by a per-repo
// verification key (X-Verification-Key header) — deliberately NOT the agent's
// session token: evidence must come from a different trust domain (#20).
func (h *VerificationHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.keys == nil {
		writeInternalError(w, "verification store not configured")
		return
	}

	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body")
		return
	}
	if req.Repo == "" || req.CommitSHA == "" || req.Status == "" {
		writeBadRequest(w, "repo, commit_sha, and status are required")
		return
	}
	status := verification.Status(req.Status)
	if status != verification.StatusPassed && status != verification.StatusFailed {
		writeBadRequest(w, "status must be 'passed' or 'failed'")
		return
	}

	key := r.Header.Get("X-Verification-Key")
	if key == "" {
		writeError(w, http.StatusUnauthorized, "verification key required", "unauthorized")
		return
	}
	valid, err := h.keys.ValidateKey(r.Context(), req.Repo, key)
	if err != nil {
		writeInternalError(w, "key validation failed")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid verification key", "unauthorized")
		return
	}

	// Self-attestation guard: the session that prepared the commit cannot
	// attest it. Same trust domain, no evidence value.
	if req.SessionID != "" && h.lookupCommitSession != nil {
		prepSession, lerr := h.lookupCommitSession(r.Context(), req.CommitSHA)
		if lerr == nil && prepSession != "" && subtle.ConstantTimeCompare([]byte(prepSession), []byte(req.SessionID)) == 1 {
			h.recordSelfAttestation(req)
			writeError(w, http.StatusForbidden, "self-attestation rejected: the session that prepared the commit cannot attest it", "self_attestation")
			return
		}
	}

	completedAt := h.now().UTC()
	if req.CompletedAt != "" {
		if t, perr := time.Parse(time.RFC3339, req.CompletedAt); perr == nil {
			completedAt = t.UTC()
		}
	}

	v := &verification.Verification{
		ID:             verification.NewID(),
		CommitSHA:      req.CommitSHA,
		SessionID:      req.SessionID,
		Status:         status,
		PipelineURL:    req.PipelineURL,
		RunnerIdentity: req.RunnerIdentity,
		EvidenceHash:   verification.EvidenceHash(req.CommitSHA, status, req.PipelineURL, req.RunnerIdentity, completedAt),
		CreatedAt:      completedAt,
		ReceivedAt:     h.now().UTC(),
	}
	if err := h.store.Save(r.Context(), v); err != nil {
		writeInternalError(w, "failed to save verification")
		return
	}

	writeCreated(w, map[string]interface{}{"data": v})
}

func (h *VerificationHandler) recordSelfAttestation(req IngestRequest) {
	if h.findings == nil {
		return
	}
	now := h.now().UTC()
	h.findings.Upsert(enforcement.Finding{
		Type:       enforcement.FindingSelfAttestation,
		Reference:  req.CommitSHA,
		Repo:       req.Repo,
		Author:     req.RunnerIdentity,
		Reason:     "attestation session matches the session that prepared the commit (same trust domain)",
		Status:     enforcement.StatusOpen,
		DetectedAt: now,
		UpdatedAt:  now,
	})
}

// GetBySHA handles GET /v1/verifications/{sha}.
func (h *VerificationHandler) GetBySHA(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeInternalError(w, "verification store not configured")
		return
	}
	v, err := h.store.GetBySHA(r.Context(), r.PathValue("sha"))
	if err != nil {
		writeInternalError(w, "lookup failed")
		return
	}
	if v == nil {
		writeNotFound(w, "no verification for this commit")
		return
	}
	writeSuccess(w, map[string]interface{}{"data": v})
}

// CommitStatus handles GET /v1/commits/{sha}/verification-status — the
// gate-friendly composition of governance + behavioral state.
func (h *VerificationHandler) CommitStatus(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	if sha == "" {
		writeBadRequest(w, "commit sha is required")
		return
	}

	resp := map[string]interface{}{
		"commit_sha": sha,
	}

	var govVerified *bool
	if h.govVerified != nil {
		ok, err := h.govVerified(r.Context(), sha)
		if err == nil {
			govVerified = &ok
		}
	}
	resp["governance_verified"] = govVerified

	var required *bool
	if h.behavioralRequired != nil {
		req := h.behavioralRequired(r.Context())
		required = &req
	}
	resp["behavioral_required"] = required

	var attestation interface{}
	if h.store != nil {
		v, err := h.store.GetBySHA(r.Context(), sha)
		if err == nil && v != nil {
			attestation = v
		}
	}
	resp["attestation"] = attestation

	mergeReady := false
	if govVerified != nil && *govVerified {
		if required == nil || !*required {
			mergeReady = true
		} else if v, _ := attestation.(*verification.Verification); v != nil && v.Status == verification.StatusPassed {
			mergeReady = true
		}
	}
	resp["merge_ready"] = mergeReady

	writeSuccess(w, resp)
}

// CreateKey handles POST /v1/verification-keys (admin only). The plaintext key
// is returned exactly once.
func (h *VerificationHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireAdmin(r)
	if !ok {
		writeError(w, http.StatusForbidden, "admin role required", "forbidden")
		return
	}
	if h.keys == nil {
		writeInternalError(w, "verification key store not configured")
		return
	}
	var req struct {
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Repo == "" {
		writeBadRequest(w, "repo is required")
		return
	}
	info, plaintext, err := h.keys.CreateKey(r.Context(), req.Repo, userID)
	if err != nil {
		writeInternalError(w, "failed to create key")
		return
	}
	writeCreated(w, map[string]interface{}{
		"data":    info,
		"key":     plaintext,
		"warning": "store this key now; it is never shown again",
	})
}

// RevokeKey handles DELETE /v1/verification-keys/{id} (admin only).
func (h *VerificationHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeError(w, http.StatusForbidden, "admin role required", "forbidden")
		return
	}
	if h.keys == nil {
		writeInternalError(w, "verification key store not configured")
		return
	}
	ok, err := h.keys.RevokeKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeInternalError(w, "failed to revoke key")
		return
	}
	if !ok {
		writeNotFound(w, "key not found or already revoked")
		return
	}
	writeSuccess(w, map[string]interface{}{"revoked": true})
}

// ListKeys handles GET /v1/verification-keys (admin only).
func (h *VerificationHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(r); !ok {
		writeError(w, http.StatusForbidden, "admin role required", "forbidden")
		return
	}
	if h.keys == nil {
		writeInternalError(w, "verification key store not configured")
		return
	}
	keys, err := h.keys.ListKeys(r.Context())
	if err != nil {
		writeInternalError(w, "failed to list keys")
		return
	}
	writeSuccess(w, map[string]interface{}{"items": keys, "total": len(keys)})
}
