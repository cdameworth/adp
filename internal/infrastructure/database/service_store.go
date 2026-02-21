package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Service represents a service in the catalog.
type Service struct {
	ID               uuid.UUID              `json:"id"`
	OrganizationID   uuid.UUID              `json:"organization_id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Tier             string                 `json:"tier"`
	AgentConstraints []AgentConstraint      `json:"agent_constraints"`
	ContextConfig    ContextConfig          `json:"context_config"`
	EscalationConfig EscalationConfig       `json:"escalation_config"`
	Spec             map[string]interface{} `json:"spec,omitempty"`
	HumanDocs        string                 `json:"human_docs,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// AgentConstraint defines a constraint on agent behavior for a service.
type AgentConstraint struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "warning", "error"
	Pattern     string `json:"pattern,omitempty"`
}

// ContextConfig defines how context is assembled for a service.
type ContextConfig struct {
	EssentialTokenBudget    int      `json:"essential_token_budget"`
	TaskRelevantTokenBudget int      `json:"task_relevant_token_budget"`
	SupportingTokenBudget   int      `json:"supporting_token_budget"`
	IncludePaths            []string `json:"include_paths,omitempty"`
	ExcludePaths            []string `json:"exclude_paths,omitempty"`
	PriorityFiles           []string `json:"priority_files,omitempty"`
}

// EscalationConfig defines escalation behavior for a service.
type EscalationConfig struct {
	DefaultApprovers []string `json:"default_approvers"`
	TimeoutMinutes   int      `json:"timeout_minutes"`
	AutoApprove      bool     `json:"auto_approve"`
	NotifyOnEscalate bool     `json:"notify_on_escalate"`
}

// ServiceStore provides CRUD operations for services.
type ServiceStore struct {
	client *PostgresClient
}

// NewServiceStore creates a new service store.
func NewServiceStore(client *PostgresClient) *ServiceStore {
	return &ServiceStore{client: client}
}

// CreateServiceInput contains the input for creating a service.
type CreateServiceInput struct {
	OrganizationID   uuid.UUID
	Name             string
	Description      string
	Tier             string
	AgentConstraints []AgentConstraint
	ContextConfig    ContextConfig
	EscalationConfig EscalationConfig
	Spec             map[string]interface{}
	HumanDocs        string
}

// Create creates a new service.
func (s *ServiceStore) Create(ctx context.Context, input CreateServiceInput) (*Service, error) {
	// Validate required fields
	if input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	// Default values
	if input.Tier == "" {
		input.Tier = "standard"
	}
	if input.AgentConstraints == nil {
		input.AgentConstraints = []AgentConstraint{}
	}
	if input.Spec == nil {
		input.Spec = map[string]interface{}{}
	}

	// Set default context config
	if input.ContextConfig.EssentialTokenBudget == 0 {
		input.ContextConfig.EssentialTokenBudget = 4000
	}
	if input.ContextConfig.TaskRelevantTokenBudget == 0 {
		input.ContextConfig.TaskRelevantTokenBudget = 12000
	}
	if input.ContextConfig.SupportingTokenBudget == 0 {
		input.ContextConfig.SupportingTokenBudget = 8000
	}

	// Set default escalation config
	if input.EscalationConfig.TimeoutMinutes == 0 {
		input.EscalationConfig.TimeoutMinutes = 60
	}

	constraintsJSON, err := json.Marshal(input.AgentConstraints)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent constraints: %w", err)
	}

	contextConfigJSON, err := json.Marshal(input.ContextConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal context config: %w", err)
	}

	escalationConfigJSON, err := json.Marshal(input.EscalationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal escalation config: %w", err)
	}

	specJSON, err := json.Marshal(input.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	query := `
		INSERT INTO services (
			organization_id, name, description, tier,
			agent_constraints, context_config, escalation_config,
			spec, human_docs
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`

	var service Service
	err = s.client.db.QueryRowContext(ctx, query,
		input.OrganizationID,
		input.Name,
		input.Description,
		input.Tier,
		constraintsJSON,
		contextConfigJSON,
		escalationConfigJSON,
		specJSON,
		input.HumanDocs,
	).Scan(&service.ID, &service.CreatedAt, &service.UpdatedAt)

	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, fmt.Errorf("%w: service name already exists", ErrDuplicateKey)
		}
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	service.OrganizationID = input.OrganizationID
	service.Name = input.Name
	service.Description = input.Description
	service.Tier = input.Tier
	service.AgentConstraints = input.AgentConstraints
	service.ContextConfig = input.ContextConfig
	service.EscalationConfig = input.EscalationConfig
	service.Spec = input.Spec
	service.HumanDocs = input.HumanDocs

	return &service, nil
}

// Get retrieves a service by ID.
func (s *ServiceStore) Get(ctx context.Context, id uuid.UUID) (*Service, error) {
	query := `
		SELECT id, organization_id, name, description, tier,
			   agent_constraints, context_config, escalation_config,
			   spec, human_docs, created_at, updated_at
		FROM services
		WHERE id = $1`

	var service Service
	var constraintsJSON, contextConfigJSON, escalationConfigJSON, specJSON []byte
	var humanDocs sql.NullString

	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&service.ID,
		&service.OrganizationID,
		&service.Name,
		&service.Description,
		&service.Tier,
		&constraintsJSON,
		&contextConfigJSON,
		&escalationConfigJSON,
		&specJSON,
		&humanDocs,
		&service.CreatedAt,
		&service.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: service %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(constraintsJSON, &service.AgentConstraints); err != nil {
		service.AgentConstraints = []AgentConstraint{}
	}
	if err := json.Unmarshal(contextConfigJSON, &service.ContextConfig); err != nil {
		service.ContextConfig = ContextConfig{}
	}
	if err := json.Unmarshal(escalationConfigJSON, &service.EscalationConfig); err != nil {
		service.EscalationConfig = EscalationConfig{}
	}
	if err := json.Unmarshal(specJSON, &service.Spec); err != nil {
		service.Spec = map[string]interface{}{}
	}
	if humanDocs.Valid {
		service.HumanDocs = humanDocs.String
	}

	return &service, nil
}

// GetByName retrieves a service by organization and name.
func (s *ServiceStore) GetByName(ctx context.Context, orgID uuid.UUID, name string) (*Service, error) {
	query := `
		SELECT id FROM services
		WHERE organization_id = $1 AND name = $2`

	var id uuid.UUID
	err := s.client.db.QueryRowContext(ctx, query, orgID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: service %s", ErrNotFound, name)
		}
		return nil, fmt.Errorf("failed to get service by name: %w", err)
	}

	return s.Get(ctx, id)
}

// ServiceFilter defines criteria for listing services.
type ServiceFilter struct {
	OrganizationID *uuid.UUID
	Tier           string
	NameContains   string
	Limit          int
	Offset         int
}

// List retrieves services matching the filter criteria.
func (s *ServiceStore) List(ctx context.Context, filter ServiceFilter) ([]*Service, error) {
	query := `
		SELECT id, organization_id, name, description, tier,
			   agent_constraints, context_config, escalation_config,
			   spec, human_docs, created_at, updated_at
		FROM services
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.OrganizationID != nil {
		query += fmt.Sprintf(" AND organization_id = $%d", argIdx)
		args = append(args, *filter.OrganizationID)
		argIdx++
	}
	if filter.Tier != "" {
		query += fmt.Sprintf(" AND tier = $%d", argIdx)
		args = append(args, filter.Tier)
		argIdx++
	}
	if filter.NameContains != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argIdx)
		args = append(args, "%"+filter.NameContains+"%")
		argIdx++
	}

	query += " ORDER BY name ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
	}

	rows, err := s.client.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}
	defer rows.Close()

	var services []*Service
	for rows.Next() {
		var service Service
		var constraintsJSON, contextConfigJSON, escalationConfigJSON, specJSON []byte
		var humanDocs sql.NullString

		err := rows.Scan(
			&service.ID,
			&service.OrganizationID,
			&service.Name,
			&service.Description,
			&service.Tier,
			&constraintsJSON,
			&contextConfigJSON,
			&escalationConfigJSON,
			&specJSON,
			&humanDocs,
			&service.CreatedAt,
			&service.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service: %w", err)
		}

		json.Unmarshal(constraintsJSON, &service.AgentConstraints)
		json.Unmarshal(contextConfigJSON, &service.ContextConfig)
		json.Unmarshal(escalationConfigJSON, &service.EscalationConfig)
		json.Unmarshal(specJSON, &service.Spec)
		if humanDocs.Valid {
			service.HumanDocs = humanDocs.String
		}

		services = append(services, &service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating services: %w", err)
	}

	return services, nil
}

// UpdateServiceInput contains fields that can be updated on a service.
type UpdateServiceInput struct {
	Name             string
	Description      string
	Tier             string
	AgentConstraints []AgentConstraint
	ContextConfig    *ContextConfig
	EscalationConfig *EscalationConfig
	Spec             map[string]interface{}
	HumanDocs        *string
}

// Update updates an existing service.
func (s *ServiceStore) Update(ctx context.Context, id uuid.UUID, input UpdateServiceInput) (*Service, error) {
	query := "UPDATE services SET updated_at = NOW()"
	args := []interface{}{}
	argIdx := 1

	if input.Name != "" {
		query += fmt.Sprintf(", name = $%d", argIdx)
		args = append(args, input.Name)
		argIdx++
	}
	if input.Description != "" {
		query += fmt.Sprintf(", description = $%d", argIdx)
		args = append(args, input.Description)
		argIdx++
	}
	if input.Tier != "" {
		query += fmt.Sprintf(", tier = $%d", argIdx)
		args = append(args, input.Tier)
		argIdx++
	}
	if input.AgentConstraints != nil {
		constraintsJSON, err := json.Marshal(input.AgentConstraints)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal agent constraints: %w", err)
		}
		query += fmt.Sprintf(", agent_constraints = $%d", argIdx)
		args = append(args, constraintsJSON)
		argIdx++
	}
	if input.ContextConfig != nil {
		contextConfigJSON, err := json.Marshal(input.ContextConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal context config: %w", err)
		}
		query += fmt.Sprintf(", context_config = $%d", argIdx)
		args = append(args, contextConfigJSON)
		argIdx++
	}
	if input.EscalationConfig != nil {
		escalationConfigJSON, err := json.Marshal(input.EscalationConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal escalation config: %w", err)
		}
		query += fmt.Sprintf(", escalation_config = $%d", argIdx)
		args = append(args, escalationConfigJSON)
		argIdx++
	}
	if input.Spec != nil {
		specJSON, err := json.Marshal(input.Spec)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal spec: %w", err)
		}
		query += fmt.Sprintf(", spec = $%d", argIdx)
		args = append(args, specJSON)
		argIdx++
	}
	if input.HumanDocs != nil {
		query += fmt.Sprintf(", human_docs = $%d", argIdx)
		args = append(args, *input.HumanDocs)
		argIdx++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	result, err := s.client.db.ExecContext(ctx, query, args...)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, fmt.Errorf("%w: service name already exists", ErrDuplicateKey)
		}
		return nil, fmt.Errorf("failed to update service: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: service %s", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// Delete permanently removes a service.
func (s *ServiceStore) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM services WHERE id = $1`

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: service %s", ErrNotFound, id)
	}

	return nil
}

// CountByOrganization returns the count of services for an organization.
func (s *ServiceStore) CountByOrganization(ctx context.Context, orgID uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM services WHERE organization_id = $1`

	var count int64
	err := s.client.db.QueryRowContext(ctx, query, orgID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count services: %w", err)
	}

	return count, nil
}

// CountByTier returns counts of services by tier for an organization.
func (s *ServiceStore) CountByTier(ctx context.Context, orgID uuid.UUID) (map[string]int64, error) {
	query := `
		SELECT tier, COUNT(*)
		FROM services
		WHERE organization_id = $1
		GROUP BY tier`

	rows, err := s.client.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to count services by tier: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var tier string
		var count int64
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[tier] = count
	}

	return counts, rows.Err()
}
