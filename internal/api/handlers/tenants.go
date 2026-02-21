// Package handlers provides HTTP handlers for the ADP API.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/adp/adp/internal/domain/tenant"
)

// TenantHandler handles tenant management API endpoints
type TenantHandler struct {
	tenantService *tenant.Service
	tenantStore   tenant.TenantStore
}

// NewTenantHandler creates a new tenant handler
func NewTenantHandler(service *tenant.Service, store tenant.TenantStore) *TenantHandler {
	return &TenantHandler{
		tenantService: service,
		tenantStore:   store,
	}
}

// CreateTenantRequest represents a request to create a tenant
type CreateTenantRequest struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Plan  string `json:"plan"` // enterprise, pro, starter
	Email string `json:"admin_email,omitempty"`
}

// CreateTenantResponse represents the response after creating a tenant
type CreateTenantResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantResponse represents a tenant in API responses
type TenantResponse struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Status      string          `json:"status"`
	Plan        string          `json:"plan"`
	Settings    tenant.Settings `json:"settings"`
	Quotas      tenant.Quotas   `json:"quotas"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	DisabledAt  *time.Time      `json:"disabled_at,omitempty"`
	TrialEndsAt *time.Time      `json:"trial_ends_at,omitempty"`
}

// CreateTenant handles POST /v1/tenants
func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Validate required fields
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required", "MISSING_REQUIRED_FIELDS")
		return
	}

	// Set default plan if not provided
	if req.Plan == "" {
		req.Plan = "starter"
	}

	// Validate plan
	validPlans := map[string]bool{"enterprise": true, "pro": true, "starter": true, "trial": true}
	if !validPlans[req.Plan] {
		writeError(w, http.StatusBadRequest, "Invalid plan. Must be one of: enterprise, pro, starter, trial", "INVALID_PLAN")
		return
	}

	// Create tenant with default settings and quotas
	t := &tenant.Tenant{
		ID:        uuid.New(),
		Name:      req.Name,
		Slug:      req.Slug,
		Status:    tenant.TenantStatusActive,
		Plan:      req.Plan,
		Settings:  tenant.DefaultSettings(),
		Quotas:    tenant.DefaultQuotas(req.Plan),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Handle trial status
	if req.Plan == "trial" {
		t.Status = tenant.TenantStatusTrial
		trialEnd := time.Now().Add(14 * 24 * time.Hour) // 14-day trial
		t.TrialEndsAt = &trialEnd
	}

	if err := h.tenantStore.CreateTenant(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create tenant", "CREATE_FAILED")
		return
	}

	resp := CreateTenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    string(t.Status),
		Plan:      t.Plan,
		CreatedAt: t.CreatedAt,
	}

	writeJSON(w, http.StatusCreated, resp)
}

// GetTenant handles GET /v1/tenants/{id}
func (h *TenantHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid tenant ID", "INVALID_ID")
		return
	}

	t, err := h.tenantStore.GetTenant(r.Context(), id)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeError(w, http.StatusNotFound, "Tenant not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to get tenant", "GET_FAILED")
		return
	}

	resp := TenantResponse{
		ID:          t.ID,
		Name:        t.Name,
		Slug:        t.Slug,
		Status:      string(t.Status),
		Plan:        t.Plan,
		Settings:    t.Settings,
		Quotas:      t.Quotas,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		DisabledAt:  t.DisabledAt,
		TrialEndsAt: t.TrialEndsAt,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListTenants handles GET /v1/tenants
func (h *TenantHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	filter := tenant.TenantFilter{
		Limit:  100,
		Offset: 0,
	}

	// Parse query parameters
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = tenant.TenantStatus(status)
	}
	if plan := r.URL.Query().Get("plan"); plan != "" {
		filter.Plan = plan
	}

	tenants, err := h.tenantStore.ListTenants(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list tenants", "LIST_FAILED")
		return
	}

	var resp []TenantResponse
	for _, t := range tenants {
		resp = append(resp, TenantResponse{
			ID:          t.ID,
			Name:        t.Name,
			Slug:        t.Slug,
			Status:      string(t.Status),
			Plan:        t.Plan,
			Settings:    t.Settings,
			Quotas:      t.Quotas,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
			DisabledAt:  t.DisabledAt,
			TrialEndsAt: t.TrialEndsAt,
		})
	}

	if resp == nil {
		resp = []TenantResponse{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpdateTenantRequest represents a request to update a tenant
type UpdateTenantRequest struct {
	Name     *string          `json:"name,omitempty"`
	Status   *string          `json:"status,omitempty"`
	Plan     *string          `json:"plan,omitempty"`
	Settings *tenant.Settings `json:"settings,omitempty"`
	Quotas   *tenant.Quotas   `json:"quotas,omitempty"`
}

// UpdateTenant handles PATCH /v1/tenants/{id}
func (h *TenantHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid tenant ID", "INVALID_ID")
		return
	}

	var req UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Get existing tenant
	t, err := h.tenantStore.GetTenant(r.Context(), id)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeError(w, http.StatusNotFound, "Tenant not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to get tenant", "GET_FAILED")
		return
	}

	// Apply updates
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Status != nil {
		validStatuses := map[string]bool{
			"active": true, "disabled": true, "suspended": true, "trial": true,
		}
		if !validStatuses[*req.Status] {
			writeError(w, http.StatusBadRequest, "Invalid status", "INVALID_STATUS")
			return
		}
		t.Status = tenant.TenantStatus(*req.Status)
		if t.Status == tenant.TenantStatusDisabled {
			now := time.Now()
			t.DisabledAt = &now
		}
	}
	if req.Plan != nil {
		validPlans := map[string]bool{"enterprise": true, "pro": true, "starter": true, "trial": true}
		if !validPlans[*req.Plan] {
			writeError(w, http.StatusBadRequest, "Invalid plan", "INVALID_PLAN")
			return
		}
		t.Plan = *req.Plan
		// Update quotas to match new plan
		t.Quotas = tenant.DefaultQuotas(t.Plan)
	}
	if req.Settings != nil {
		t.Settings = *req.Settings
	}
	if req.Quotas != nil {
		t.Quotas = *req.Quotas
	}

	t.UpdatedAt = time.Now()

	if err := h.tenantStore.UpdateTenant(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update tenant", "UPDATE_FAILED")
		return
	}

	resp := TenantResponse{
		ID:          t.ID,
		Name:        t.Name,
		Slug:        t.Slug,
		Status:      string(t.Status),
		Plan:        t.Plan,
		Settings:    t.Settings,
		Quotas:      t.Quotas,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		DisabledAt:  t.DisabledAt,
		TrialEndsAt: t.TrialEndsAt,
	}

	writeJSON(w, http.StatusOK, resp)
}

// OrganizationHandler handles organization management API endpoints

// CreateOrganizationRequest represents a request to create an organization
type CreateOrganizationRequest struct {
	Name        string              `json:"name"`
	Slug        string              `json:"slug"`
	Description string              `json:"description,omitempty"`
	ParentID    *uuid.UUID          `json:"parent_id,omitempty"`
	Settings    *tenant.OrgSettings `json:"settings,omitempty"`
}

// OrganizationResponse represents an organization in API responses
type OrganizationResponse struct {
	ID          uuid.UUID          `json:"id"`
	TenantID    uuid.UUID          `json:"tenant_id"`
	ParentID    *uuid.UUID         `json:"parent_id,omitempty"`
	Name        string             `json:"name"`
	Slug        string             `json:"slug"`
	Description string             `json:"description"`
	Settings    tenant.OrgSettings `json:"settings"`
	Metadata    map[string]string  `json:"metadata"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// CreateOrganization handles POST /v1/tenants/{tenant_id}/organizations
func (h *TenantHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := r.PathValue("tenant_id")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid tenant ID", "INVALID_ID")
		return
	}

	// Verify tenant exists
	t, err := h.tenantStore.GetTenant(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeError(w, http.StatusNotFound, "Tenant not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to get tenant", "GET_FAILED")
		return
	}

	// Check quota
	orgs, err := h.tenantStore.ListOrganizations(r.Context(), tenantID)
	if err == nil && len(orgs) >= t.Quotas.MaxOrganizations {
		writeError(w, http.StatusForbidden, "Organization quota exceeded", "QUOTA_EXCEEDED")
		return
	}

	var req CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required", "MISSING_REQUIRED_FIELDS")
		return
	}

	org := &tenant.Organization{
		ID:          uuid.New(),
		TenantID:    tenantID,
		ParentID:    req.ParentID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Settings: tenant.OrgSettings{
			DefaultTrustLevel: 2,
			FeatureFlags:      make(map[string]bool),
		},
		Metadata:  make(map[string]string),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if req.Settings != nil {
		org.Settings = *req.Settings
	}

	// Validate hierarchy if parent specified
	if req.ParentID != nil {
		if err := h.tenantService.ValidateOrganizationHierarchy(r.Context(), org, req.ParentID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "INVALID_HIERARCHY")
			return
		}
	}

	if err := h.tenantStore.CreateOrganization(r.Context(), org); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create organization", "CREATE_FAILED")
		return
	}

	resp := OrganizationResponse{
		ID:          org.ID,
		TenantID:    org.TenantID,
		ParentID:    org.ParentID,
		Name:        org.Name,
		Slug:        org.Slug,
		Description: org.Description,
		Settings:    org.Settings,
		Metadata:    org.Metadata,
		CreatedAt:   org.CreatedAt,
		UpdatedAt:   org.UpdatedAt,
	}

	writeJSON(w, http.StatusCreated, resp)
}

// ListOrganizations handles GET /v1/tenants/{tenant_id}/organizations
func (h *TenantHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := r.PathValue("tenant_id")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid tenant ID", "INVALID_ID")
		return
	}

	orgs, err := h.tenantStore.ListOrganizations(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list organizations", "LIST_FAILED")
		return
	}

	var resp []OrganizationResponse
	for _, org := range orgs {
		resp = append(resp, OrganizationResponse{
			ID:          org.ID,
			TenantID:    org.TenantID,
			ParentID:    org.ParentID,
			Name:        org.Name,
			Slug:        org.Slug,
			Description: org.Description,
			Settings:    org.Settings,
			Metadata:    org.Metadata,
			CreatedAt:   org.CreatedAt,
			UpdatedAt:   org.UpdatedAt,
		})
	}

	if resp == nil {
		resp = []OrganizationResponse{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetOrganization handles GET /v1/organizations/{id}
func (h *TenantHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid organization ID", "INVALID_ID")
		return
	}

	org, err := h.tenantStore.GetOrganization(r.Context(), id)
	if err != nil {
		if errors.Is(err, tenant.ErrOrganizationNotFound) {
			writeError(w, http.StatusNotFound, "Organization not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to get organization", "GET_FAILED")
		return
	}

	resp := OrganizationResponse{
		ID:          org.ID,
		TenantID:    org.TenantID,
		ParentID:    org.ParentID,
		Name:        org.Name,
		Slug:        org.Slug,
		Description: org.Description,
		Settings:    org.Settings,
		Metadata:    org.Metadata,
		CreatedAt:   org.CreatedAt,
		UpdatedAt:   org.UpdatedAt,
	}

	writeJSON(w, http.StatusOK, resp)
}

// TeamHandler handles team management API endpoints

// TeamResponse represents a team in API responses
type TeamResponse struct {
	ID             uuid.UUID           `json:"id"`
	OrganizationID uuid.UUID           `json:"organization_id"`
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	Permissions    []tenant.Permission `json:"permissions"`
	ServiceScope   []uuid.UUID         `json:"service_scope"`
	MaxTrustLevel  int                 `json:"max_trust_level"`
	Metadata       map[string]string   `json:"metadata"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// CreateTeamRequest represents a request to create a team
type CreateTeamRequest struct {
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	Permissions   []tenant.Permission `json:"permissions,omitempty"`
	ServiceScope  []uuid.UUID         `json:"service_scope,omitempty"`
	MaxTrustLevel int                 `json:"max_trust_level,omitempty"`
}

// CreateTeam handles POST /v1/organizations/{org_id}/teams
func (h *TenantHandler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.PathValue("org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid organization ID", "INVALID_ID")
		return
	}

	// Verify organization exists
	_, err = h.tenantStore.GetOrganization(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, tenant.ErrOrganizationNotFound) {
			writeError(w, http.StatusNotFound, "Organization not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to get organization", "GET_FAILED")
		return
	}

	var req CreateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "MISSING_REQUIRED_FIELDS")
		return
	}

	team := &tenant.Team{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           req.Name,
		Description:    req.Description,
		Permissions:    req.Permissions,
		ServiceScope:   req.ServiceScope,
		MaxTrustLevel:  req.MaxTrustLevel,
		Metadata:       make(map[string]string),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Set defaults
	if team.Permissions == nil {
		team.Permissions = []tenant.Permission{}
	}
	if team.ServiceScope == nil {
		team.ServiceScope = []uuid.UUID{}
	}
	if team.MaxTrustLevel == 0 {
		team.MaxTrustLevel = 2 // Default to Contributor
	}

	if err := h.tenantStore.CreateTeam(r.Context(), team); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create team", "CREATE_FAILED")
		return
	}

	resp := TeamResponse{
		ID:             team.ID,
		OrganizationID: team.OrganizationID,
		Name:           team.Name,
		Description:    team.Description,
		Permissions:    team.Permissions,
		ServiceScope:   team.ServiceScope,
		MaxTrustLevel:  team.MaxTrustLevel,
		Metadata:       team.Metadata,
		CreatedAt:      team.CreatedAt,
		UpdatedAt:      team.UpdatedAt,
	}

	writeJSON(w, http.StatusCreated, resp)
}

// ListTeams handles GET /v1/organizations/{org_id}/teams
func (h *TenantHandler) ListTeams(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.PathValue("org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid organization ID", "INVALID_ID")
		return
	}

	teams, err := h.tenantStore.ListTeams(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list teams", "LIST_FAILED")
		return
	}

	var resp []TeamResponse
	for _, team := range teams {
		resp = append(resp, TeamResponse{
			ID:             team.ID,
			OrganizationID: team.OrganizationID,
			Name:           team.Name,
			Description:    team.Description,
			Permissions:    team.Permissions,
			ServiceScope:   team.ServiceScope,
			MaxTrustLevel:  team.MaxTrustLevel,
			Metadata:       team.Metadata,
			CreatedAt:      team.CreatedAt,
			UpdatedAt:      team.UpdatedAt,
		})
	}

	if resp == nil {
		resp = []TeamResponse{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetTeam handles GET /v1/teams/{id}
func (h *TenantHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid team ID", "INVALID_ID")
		return
	}

	team, err := h.tenantStore.GetTeam(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Team not found", "NOT_FOUND")
		return
	}

	resp := TeamResponse{
		ID:             team.ID,
		OrganizationID: team.OrganizationID,
		Name:           team.Name,
		Description:    team.Description,
		Permissions:    team.Permissions,
		ServiceScope:   team.ServiceScope,
		MaxTrustLevel:  team.MaxTrustLevel,
		Metadata:       team.Metadata,
		CreatedAt:      team.CreatedAt,
		UpdatedAt:      team.UpdatedAt,
	}

	writeJSON(w, http.StatusOK, resp)
}

// TeamMemberRequest represents a request to manage team membership
type TeamMemberRequest struct {
	UserID    uuid.UUID  `json:"user_id"`
	Role      string     `json:"role"` // owner, admin, member, viewer
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// AddTeamMember handles POST /v1/teams/{id}/members
func (h *TenantHandler) AddTeamMember(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	teamID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid team ID", "INVALID_ID")
		return
	}

	var req TeamMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.UserID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "user_id is required", "MISSING_REQUIRED_FIELDS")
		return
	}

	// Validate role
	validRoles := map[string]bool{"owner": true, "admin": true, "member": true, "viewer": true}
	if req.Role == "" {
		req.Role = "member"
	}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "Invalid role", "INVALID_ROLE")
		return
	}

	member := &tenant.TeamMember{
		TeamID:    teamID,
		UserID:    req.UserID,
		Role:      tenant.TeamRole(req.Role),
		JoinedAt:  time.Now(),
		ExpiresAt: req.ExpiresAt,
	}

	if err := h.tenantStore.AddTeamMember(r.Context(), member); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to add team member", "CREATE_FAILED")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// RemoveTeamMember handles DELETE /v1/teams/{id}/members/{user_id}
func (h *TenantHandler) RemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	teamIDStr := r.PathValue("id")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid team ID", "INVALID_ID")
		return
	}

	userIDStr := r.PathValue("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user ID", "INVALID_ID")
		return
	}

	if err := h.tenantStore.RemoveTeamMember(r.Context(), teamID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to remove team member", "DELETE_FAILED")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetUserTeams handles GET /v1/users/{user_id}/teams
func (h *TenantHandler) GetUserTeams(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user ID", "INVALID_ID")
		return
	}

	teams, err := h.tenantStore.GetUserTeams(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user teams", "GET_FAILED")
		return
	}

	var resp []TeamResponse
	for _, team := range teams {
		resp = append(resp, TeamResponse{
			ID:             team.ID,
			OrganizationID: team.OrganizationID,
			Name:           team.Name,
			Description:    team.Description,
			Permissions:    team.Permissions,
			ServiceScope:   team.ServiceScope,
			MaxTrustLevel:  team.MaxTrustLevel,
			Metadata:       team.Metadata,
			CreatedAt:      team.CreatedAt,
			UpdatedAt:      team.UpdatedAt,
		})
	}

	if resp == nil {
		resp = []TeamResponse{}
	}

	writeJSON(w, http.StatusOK, resp)
}
