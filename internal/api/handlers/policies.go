package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// PolicyHandler handles policy definition HTTP requests.
type PolicyHandler struct {
	store *database.PolicyDefinitionStore
}

// NewPolicyHandler creates a new policy handler.
func NewPolicyHandler(store *database.PolicyDefinitionStore) *PolicyHandler {
	return &PolicyHandler{store: store}
}

// CreatePolicy handles POST /v1/policies
func (h *PolicyHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string                 `json:"name"`
		Description   string                 `json:"description"`
		Category      string                 `json:"category"`
		Enabled       *bool                  `json:"enabled"`
		Priority      int                    `json:"priority"`
		PolicyType    string                 `json:"policy_type"`
		RegoCode      string                 `json:"rego_code"`
		BuiltinName   string                 `json:"builtin_name"`
		Config        map[string]interface{} `json:"config"`
		MinTrustLevel int                    `json:"min_trust_level"`
		Tags          []string               `json:"tags"`
		Metadata      map[string]interface{} `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "INVALID_REQUEST")
		return
	}
	if req.Category == "" {
		writeError(w, http.StatusBadRequest, "category is required", "INVALID_REQUEST")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	input := database.CreatePolicyDefinitionInput{
		Name:          req.Name,
		Description:   req.Description,
		Category:      req.Category,
		Enabled:       enabled,
		Priority:      req.Priority,
		PolicyType:    req.PolicyType,
		RegoCode:      req.RegoCode,
		BuiltinName:   req.BuiltinName,
		Config:        req.Config,
		MinTrustLevel: req.MinTrustLevel,
		Tags:          req.Tags,
		Metadata:      req.Metadata,
	}

	policy, err := h.store.Create(r.Context(), input)
	if err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, err.Error(), "NOT_FOUND")
			return
		}
		if isAlreadyExistsError(err) {
			writeError(w, http.StatusConflict, err.Error(), "ALREADY_EXISTS")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create policy", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

// GetPolicy handles GET /v1/policies/{id}
func (h *PolicyHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id", "INVALID_ID")
		return
	}

	policy, err := h.store.Get(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, "policy not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get policy", "INTERNAL_ERROR")
		return
	}

	// Get stats for the policy
	stats, err := h.store.GetStatsForPolicy(r.Context(), id)
	if err == nil && stats != nil {
		policy.TriggerCount = stats.TriggerCount
		if stats.LastTriggered != nil {
			policy.LastTriggered = formatRelativeTime(*stats.LastTriggered)
		} else {
			policy.LastTriggered = "Never"
		}
	}

	writeJSON(w, http.StatusOK, policy)
}

// ListPolicies handles GET /v1/policies
func (h *PolicyHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	filter := database.PolicyDefinitionFilter{
		Category:   r.URL.Query().Get("category"),
		PolicyType: r.URL.Query().Get("policy_type"),
		Limit:      parseIntParam(r, "limit", 50),
		Offset:     parseIntParam(r, "offset", 0),
	}

	if enabledStr := r.URL.Query().Get("enabled"); enabledStr != "" {
		enabled := enabledStr == "true"
		filter.Enabled = &enabled
	}

	if tagsStr := r.URL.Query().Get("tags"); tagsStr != "" {
		filter.Tags = []string{tagsStr}
	}

	policies, total, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list policies", "INTERNAL_ERROR")
		return
	}

	// Enrich with stats
	for _, policy := range policies {
		stats, err := h.store.GetStatsForPolicy(r.Context(), policy.ID)
		if err == nil && stats != nil {
			policy.TriggerCount = stats.TriggerCount
			if stats.LastTriggered != nil {
				policy.LastTriggered = formatRelativeTime(*stats.LastTriggered)
			} else {
				policy.LastTriggered = "Never"
			}
		}
	}

	writeList(w, policies, total, filter.Limit, filter.Offset)
}

// UpdatePolicy handles PATCH /v1/policies/{id}
func (h *PolicyHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id", "INVALID_ID")
		return
	}

	var req struct {
		Name          *string                `json:"name"`
		Description   *string                `json:"description"`
		Category      *string                `json:"category"`
		Enabled       *bool                  `json:"enabled"`
		Priority      *int                   `json:"priority"`
		PolicyType    *string                `json:"policy_type"`
		RegoCode      *string                `json:"rego_code"`
		BuiltinName   *string                `json:"builtin_name"`
		Config        map[string]interface{} `json:"config"`
		MinTrustLevel *int                   `json:"min_trust_level"`
		Tags          []string               `json:"tags"`
		Metadata      map[string]interface{} `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_REQUEST")
		return
	}

	input := database.UpdatePolicyDefinitionInput{
		Name:          req.Name,
		Description:   req.Description,
		Category:      req.Category,
		Enabled:       req.Enabled,
		Priority:      req.Priority,
		PolicyType:    req.PolicyType,
		RegoCode:      req.RegoCode,
		BuiltinName:   req.BuiltinName,
		Config:        req.Config,
		MinTrustLevel: req.MinTrustLevel,
		Tags:          req.Tags,
		Metadata:      req.Metadata,
	}

	policy, err := h.store.Update(r.Context(), id, input)
	if err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, "policy not found", "NOT_FOUND")
			return
		}
		if isAlreadyExistsError(err) {
			writeError(w, http.StatusConflict, err.Error(), "ALREADY_EXISTS")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update policy", "INTERNAL_ERROR")
		return
	}

	// Get stats for the policy
	stats, err := h.store.GetStatsForPolicy(r.Context(), id)
	if err == nil && stats != nil {
		policy.TriggerCount = stats.TriggerCount
		if stats.LastTriggered != nil {
			policy.LastTriggered = formatRelativeTime(*stats.LastTriggered)
		} else {
			policy.LastTriggered = "Never"
		}
	}

	writeJSON(w, http.StatusOK, policy)
}

// DeletePolicy handles DELETE /v1/policies/{id}
func (h *PolicyHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id", "INVALID_ID")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, "policy not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete policy", "INTERNAL_ERROR")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TogglePolicyEnabled handles PATCH /v1/policies/{id}/toggle
func (h *PolicyHandler) TogglePolicyEnabled(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id", "INVALID_ID")
		return
	}

	// Get current policy
	policy, err := h.store.Get(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, "policy not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get policy", "INTERNAL_ERROR")
		return
	}

	// Toggle enabled
	newEnabled := !policy.Enabled
	input := database.UpdatePolicyDefinitionInput{
		Enabled: &newEnabled,
	}

	updatedPolicy, err := h.store.Update(r.Context(), id, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to toggle policy", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, updatedPolicy)
}
