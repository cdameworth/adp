package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sqlitePolicy is the in-memory policy representation for the SQLite policy handler.
type sqlitePolicy struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	Enabled       bool     `json:"enabled"`
	Priority      int      `json:"priority"`
	PolicyType    string   `json:"policy_type"`
	MinTrustLevel int      `json:"min_trust_level"`
	Tags          []string `json:"tags"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// SQLitePolicyHandler handles policy HTTP requests using an in-memory store.
type SQLitePolicyHandler struct {
	mu       sync.RWMutex
	policies map[string]*sqlitePolicy
}

// NewSQLitePolicyHandler creates a new SQLite-backed policy handler pre-seeded
// with the 6 builtin governance policies.
func NewSQLitePolicyHandler() *SQLitePolicyHandler {
	now := time.Now().Format(time.RFC3339)

	builtins := []*sqlitePolicy{
		{
			ID:            "pol_builtin1",
			Name:          "sensitive_files",
			Description:   "Detects modifications to sensitive files such as secrets, credentials, and configuration",
			Category:      "security",
			Enabled:       true,
			Priority:      100,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"security", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "pol_builtin2",
			Name:          "blast_radius",
			Description:   "Limits the number of files that can be changed in a single commit",
			Category:      "governance",
			Enabled:       true,
			Priority:      90,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"governance", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "pol_builtin3",
			Name:          "off_hours",
			Description:   "Restricts certain operations outside of business hours",
			Category:      "time_based",
			Enabled:       true,
			Priority:      80,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"time_based", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "pol_builtin4",
			Name:          "cost_limits",
			Description:   "Enforces cost thresholds for operations with financial impact",
			Category:      "financial",
			Enabled:       true,
			Priority:      70,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"financial", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "pol_builtin5",
			Name:          "migration_approval",
			Description:   "Requires explicit approval for database migration operations",
			Category:      "governance",
			Enabled:       true,
			Priority:      60,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"governance", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "pol_builtin6",
			Name:          "rate_limiting",
			Description:   "Enforces rate limits on agent operations to prevent resource exhaustion",
			Category:      "performance",
			Enabled:       true,
			Priority:      50,
			PolicyType:    "builtin",
			MinTrustLevel: 0,
			Tags:          []string{"performance", "builtin"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	policies := make(map[string]*sqlitePolicy, len(builtins))
	for _, p := range builtins {
		policies[p.ID] = p
	}

	return &SQLitePolicyHandler{
		policies: policies,
	}
}

// CreatePolicy handles POST /v1/policies
func (h *SQLitePolicyHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Category      string   `json:"category"`
		Enabled       *bool    `json:"enabled"`
		Priority      int      `json:"priority"`
		PolicyType    string   `json:"policy_type"`
		MinTrustLevel int      `json:"min_trust_level"`
		Tags          []string `json:"tags"`
	}

	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeBadRequest(w, "name is required")
		return
	}
	if req.Category == "" {
		writeBadRequest(w, "category is required")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now().Format(time.RFC3339)
	policy := &sqlitePolicy{
		ID:            "pol_" + uuid.New().String()[:8],
		Name:          req.Name,
		Description:   req.Description,
		Category:      req.Category,
		Enabled:       enabled,
		Priority:      req.Priority,
		PolicyType:    req.PolicyType,
		MinTrustLevel: req.MinTrustLevel,
		Tags:          req.Tags,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if policy.Tags == nil {
		policy.Tags = []string{}
	}

	h.mu.Lock()
	h.policies[policy.ID] = policy
	h.mu.Unlock()

	writeCreated(w, policy)
}

// GetPolicy handles GET /v1/policies/{id}
func (h *SQLitePolicyHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "policy id is required")
		return
	}

	h.mu.RLock()
	policy, ok := h.policies[id]
	h.mu.RUnlock()

	if !ok {
		writeNotFound(w, "policy not found")
		return
	}

	writeSuccess(w, policy)
}

// ListPolicies handles GET /v1/policies
func (h *SQLitePolicyHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	category := getQueryParam(r, "category", "")
	enabledStr := getQueryParam(r, "enabled", "")
	limit := getQueryParamInt(r, "limit", 50)
	offset := getQueryParamInt(r, "offset", 0)

	h.mu.RLock()
	var filtered []*sqlitePolicy
	for _, p := range h.policies {
		if category != "" && !strings.EqualFold(p.Category, category) {
			continue
		}
		if enabledStr != "" {
			wantEnabled := enabledStr == "true"
			if p.Enabled != wantEnabled {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	h.mu.RUnlock()

	// Sort by priority descending for deterministic output.
	sortPoliciesByPriority(filtered)

	total := len(filtered)

	// Apply pagination.
	if offset > len(filtered) {
		filtered = nil
	} else {
		filtered = filtered[offset:]
		if limit > 0 && len(filtered) > limit {
			filtered = filtered[:limit]
		}
	}

	writeList(w, filtered, total, limit, offset)
}

// UpdatePolicy handles PATCH /v1/policies/{id}
func (h *SQLitePolicyHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "policy id is required")
		return
	}

	var req struct {
		Name          *string  `json:"name"`
		Description   *string  `json:"description"`
		Category      *string  `json:"category"`
		Enabled       *bool    `json:"enabled"`
		Priority      *int     `json:"priority"`
		PolicyType    *string  `json:"policy_type"`
		MinTrustLevel *int     `json:"min_trust_level"`
		Tags          []string `json:"tags"`
	}

	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "invalid request body: "+err.Error())
		return
	}

	h.mu.Lock()
	policy, ok := h.policies[id]
	if !ok {
		h.mu.Unlock()
		writeNotFound(w, "policy not found")
		return
	}

	if req.Name != nil {
		policy.Name = *req.Name
	}
	if req.Description != nil {
		policy.Description = *req.Description
	}
	if req.Category != nil {
		policy.Category = *req.Category
	}
	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		policy.Priority = *req.Priority
	}
	if req.PolicyType != nil {
		policy.PolicyType = *req.PolicyType
	}
	if req.MinTrustLevel != nil {
		policy.MinTrustLevel = *req.MinTrustLevel
	}
	if req.Tags != nil {
		policy.Tags = req.Tags
	}

	policy.UpdatedAt = time.Now().Format(time.RFC3339)
	h.mu.Unlock()

	writeSuccess(w, policy)
}

// DeletePolicy handles DELETE /v1/policies/{id}
func (h *SQLitePolicyHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "policy id is required")
		return
	}

	h.mu.Lock()
	_, ok := h.policies[id]
	if !ok {
		h.mu.Unlock()
		writeNotFound(w, "policy not found")
		return
	}
	delete(h.policies, id)
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// TogglePolicyEnabled handles PATCH /v1/policies/{id}/toggle
func (h *SQLitePolicyHandler) TogglePolicyEnabled(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "policy id is required")
		return
	}

	h.mu.Lock()
	policy, ok := h.policies[id]
	if !ok {
		h.mu.Unlock()
		writeNotFound(w, "policy not found")
		return
	}

	policy.Enabled = !policy.Enabled
	policy.UpdatedAt = time.Now().Format(time.RFC3339)
	h.mu.Unlock()

	writeSuccess(w, policy)
}

// sortPoliciesByPriority sorts policies by priority descending (highest first).
func sortPoliciesByPriority(policies []*sqlitePolicy) {
	for i := 1; i < len(policies); i++ {
		for j := i; j > 0 && policies[j].Priority > policies[j-1].Priority; j-- {
			policies[j], policies[j-1] = policies[j-1], policies[j]
		}
	}
}
