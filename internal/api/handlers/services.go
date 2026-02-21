package handlers

import (
	"net/http"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// ServiceHandler handles service-related HTTP requests
type ServiceHandler struct {
	store *database.ServiceStore
}

// NewServiceHandler creates a new service handler
func NewServiceHandler(store *database.ServiceStore) *ServiceHandler {
	return &ServiceHandler{store: store}
}

// API request types

type CreateServiceRequest struct {
	OrganizationID   string                   `json:"organization_id"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	Tier             string                   `json:"tier"`
	AgentConstraints []AgentConstraintRequest `json:"agent_constraints"`
	ContextConfig    *ContextConfigRequest    `json:"context_config,omitempty"`
	EscalationConfig *EscalationConfigRequest `json:"escalation_config,omitempty"`
	Spec             map[string]interface{}   `json:"spec,omitempty"`
	HumanDocs        string                   `json:"human_docs,omitempty"`
}

type AgentConstraintRequest struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Pattern     string `json:"pattern,omitempty"`
}

type ContextConfigRequest struct {
	EssentialTokenBudget    int      `json:"essential_token_budget"`
	TaskRelevantTokenBudget int      `json:"task_relevant_token_budget"`
	SupportingTokenBudget   int      `json:"supporting_token_budget"`
	IncludePaths            []string `json:"include_paths,omitempty"`
	ExcludePaths            []string `json:"exclude_paths,omitempty"`
	PriorityFiles           []string `json:"priority_files,omitempty"`
}

type EscalationConfigRequest struct {
	DefaultApprovers []string `json:"default_approvers"`
	TimeoutMinutes   int      `json:"timeout_minutes"`
	AutoApprove      bool     `json:"auto_approve"`
	NotifyOnEscalate bool     `json:"notify_on_escalate"`
}

type UpdateServiceRequest struct {
	Name             string                   `json:"name,omitempty"`
	Description      string                   `json:"description,omitempty"`
	Tier             string                   `json:"tier,omitempty"`
	AgentConstraints []AgentConstraintRequest `json:"agent_constraints,omitempty"`
	ContextConfig    *ContextConfigRequest    `json:"context_config,omitempty"`
	EscalationConfig *EscalationConfigRequest `json:"escalation_config,omitempty"`
	Spec             map[string]interface{}   `json:"spec,omitempty"`
	HumanDocs        *string                  `json:"human_docs,omitempty"`
}

// CreateService handles POST /v1/services
func (h *ServiceHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeBadRequest(w, "name is required")
		return
	}

	orgID, err := uuid.Parse(req.OrganizationID)
	if err != nil {
		writeBadRequest(w, "Invalid organization_id")
		return
	}

	// Convert constraints
	var constraints []database.AgentConstraint
	for _, c := range req.AgentConstraints {
		constraints = append(constraints, database.AgentConstraint{
			Type:        c.Type,
			Description: c.Description,
			Severity:    c.Severity,
			Pattern:     c.Pattern,
		})
	}

	// Default context config
	var contextConfig database.ContextConfig
	if req.ContextConfig != nil {
		contextConfig = database.ContextConfig{
			EssentialTokenBudget:    req.ContextConfig.EssentialTokenBudget,
			TaskRelevantTokenBudget: req.ContextConfig.TaskRelevantTokenBudget,
			SupportingTokenBudget:   req.ContextConfig.SupportingTokenBudget,
			IncludePaths:            req.ContextConfig.IncludePaths,
			ExcludePaths:            req.ContextConfig.ExcludePaths,
			PriorityFiles:           req.ContextConfig.PriorityFiles,
		}
	}

	// Default escalation config
	var escalationConfig database.EscalationConfig
	if req.EscalationConfig != nil {
		escalationConfig = database.EscalationConfig{
			DefaultApprovers: req.EscalationConfig.DefaultApprovers,
			TimeoutMinutes:   req.EscalationConfig.TimeoutMinutes,
			AutoApprove:      req.EscalationConfig.AutoApprove,
			NotifyOnEscalate: req.EscalationConfig.NotifyOnEscalate,
		}
	}

	input := database.CreateServiceInput{
		OrganizationID:   orgID,
		Name:             req.Name,
		Description:      req.Description,
		Tier:             req.Tier,
		AgentConstraints: constraints,
		ContextConfig:    contextConfig,
		EscalationConfig: escalationConfig,
		Spec:             req.Spec,
		HumanDocs:        req.HumanDocs,
	}

	service, err := h.store.Create(r.Context(), input)
	if err != nil {
		writeInternalError(w, "Failed to create service: "+err.Error())
		return
	}

	writeCreated(w, service)
}

// GetService handles GET /v1/services/{id}
func (h *ServiceHandler) GetService(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid service ID")
		return
	}

	service, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeNotFound(w, "Service not found")
		return
	}

	writeSuccess(w, service)
}

// ListServices handles GET /v1/services
func (h *ServiceHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	filter := database.ServiceFilter{
		Tier:         getQueryParam(r, "tier", ""),
		NameContains: getQueryParam(r, "name", ""),
		Limit:        getQueryParamInt(r, "limit", 50),
		Offset:       getQueryParamInt(r, "offset", 0),
	}

	if orgIDStr := getQueryParam(r, "organization_id", ""); orgIDStr != "" {
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			writeBadRequest(w, "Invalid organization_id")
			return
		}
		filter.OrganizationID = &orgID
	}

	services, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list services: "+err.Error())
		return
	}

	writeList(w, services, -1, filter.Limit, filter.Offset)
}

// UpdateService handles PATCH /v1/services/{id}
func (h *ServiceHandler) UpdateService(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid service ID")
		return
	}

	var req UpdateServiceRequest
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	// Convert constraints
	var constraints []database.AgentConstraint
	for _, c := range req.AgentConstraints {
		constraints = append(constraints, database.AgentConstraint{
			Type:        c.Type,
			Description: c.Description,
			Severity:    c.Severity,
			Pattern:     c.Pattern,
		})
	}

	// Convert context config
	var contextConfig *database.ContextConfig
	if req.ContextConfig != nil {
		contextConfig = &database.ContextConfig{
			EssentialTokenBudget:    req.ContextConfig.EssentialTokenBudget,
			TaskRelevantTokenBudget: req.ContextConfig.TaskRelevantTokenBudget,
			SupportingTokenBudget:   req.ContextConfig.SupportingTokenBudget,
			IncludePaths:            req.ContextConfig.IncludePaths,
			ExcludePaths:            req.ContextConfig.ExcludePaths,
			PriorityFiles:           req.ContextConfig.PriorityFiles,
		}
	}

	// Convert escalation config
	var escalationConfig *database.EscalationConfig
	if req.EscalationConfig != nil {
		escalationConfig = &database.EscalationConfig{
			DefaultApprovers: req.EscalationConfig.DefaultApprovers,
			TimeoutMinutes:   req.EscalationConfig.TimeoutMinutes,
			AutoApprove:      req.EscalationConfig.AutoApprove,
			NotifyOnEscalate: req.EscalationConfig.NotifyOnEscalate,
		}
	}

	input := database.UpdateServiceInput{
		Name:             req.Name,
		Description:      req.Description,
		Tier:             req.Tier,
		AgentConstraints: constraints,
		ContextConfig:    contextConfig,
		EscalationConfig: escalationConfig,
		Spec:             req.Spec,
		HumanDocs:        req.HumanDocs,
	}

	service, err := h.store.Update(r.Context(), id, input)
	if err != nil {
		writeNotFound(w, "Service not found")
		return
	}

	writeSuccess(w, service)
}

// DeleteService handles DELETE /v1/services/{id}
func (h *ServiceHandler) DeleteService(w http.ResponseWriter, r *http.Request) {
	idStr := getPathParam(r, "id")
	if idStr == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeBadRequest(w, "Invalid service ID")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		writeNotFound(w, "Service not found")
		return
	}

	writeSuccess(w, map[string]string{
		"message": "Service deleted successfully",
	})
}
