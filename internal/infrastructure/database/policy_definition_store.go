package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PolicyDefinition represents a governance policy definition.
type PolicyDefinition struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID *uuid.UUID             `json:"organization_id,omitempty"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	Category       string                 `json:"category"`
	Enabled        bool                   `json:"enabled"`
	Priority       int                    `json:"priority"`
	PolicyType     string                 `json:"policy_type"`
	RegoCode       string                 `json:"rego_code,omitempty"`
	BuiltinName    string                 `json:"builtin_name,omitempty"`
	Config         map[string]interface{} `json:"config,omitempty"`
	MinTrustLevel  int                    `json:"min_trust_level"`
	Tags           []string               `json:"tags,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	CreatedBy      *uuid.UUID             `json:"created_by,omitempty"`
	UpdatedBy      *uuid.UUID             `json:"updated_by,omitempty"`
	// Stats (populated separately)
	TriggerCount  int64  `json:"trigger_count,omitempty"`
	LastTriggered string `json:"last_triggered,omitempty"`
}

// PolicyDefinitionStore provides operations for policy definitions.
type PolicyDefinitionStore struct {
	client *PostgresClient
}

// NewPolicyDefinitionStore creates a new policy definition store.
func NewPolicyDefinitionStore(client *PostgresClient) *PolicyDefinitionStore {
	return &PolicyDefinitionStore{client: client}
}

// CreatePolicyDefinitionInput contains the input for creating a policy definition.
type CreatePolicyDefinitionInput struct {
	OrganizationID *uuid.UUID
	Name           string
	Description    string
	Category       string
	Enabled        bool
	Priority       int
	PolicyType     string
	RegoCode       string
	BuiltinName    string
	Config         map[string]interface{}
	MinTrustLevel  int
	Tags           []string
	Metadata       map[string]interface{}
	CreatedBy      *uuid.UUID
}

// Create creates a new policy definition.
func (s *PolicyDefinitionStore) Create(ctx context.Context, input CreatePolicyDefinitionInput) (*PolicyDefinition, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if input.Category == "" {
		return nil, fmt.Errorf("%w: category is required", ErrInvalidInput)
	}
	if input.PolicyType == "" {
		input.PolicyType = "builtin"
	}
	if input.Priority == 0 {
		input.Priority = 100
	}
	if input.MinTrustLevel == 0 {
		input.MinTrustLevel = 1
	}
	if input.Config == nil {
		input.Config = map[string]interface{}{}
	}
	if input.Metadata == nil {
		input.Metadata = map[string]interface{}{}
	}
	if input.Tags == nil {
		input.Tags = []string{}
	}

	configJSON, err := json.Marshal(input.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	metadataJSON, err := json.Marshal(input.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO policy_definitions (
			organization_id, name, description, category, enabled, priority,
			policy_type, rego_code, builtin_name, config, min_trust_level,
			tags, metadata, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
		RETURNING id, created_at, updated_at`

	var policy PolicyDefinition
	err = s.client.db.QueryRowContext(ctx, query,
		input.OrganizationID,
		input.Name,
		input.Description,
		input.Category,
		input.Enabled,
		input.Priority,
		input.PolicyType,
		nullString(input.RegoCode),
		nullString(input.BuiltinName),
		configJSON,
		input.MinTrustLevel,
		pq.Array(input.Tags),
		metadataJSON,
		input.CreatedBy,
	).Scan(&policy.ID, &policy.CreatedAt, &policy.UpdatedAt)

	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: policy with name '%s' already exists", ErrAlreadyExists, input.Name)
		}
		return nil, fmt.Errorf("failed to create policy definition: %w", err)
	}

	policy.OrganizationID = input.OrganizationID
	policy.Name = input.Name
	policy.Description = input.Description
	policy.Category = input.Category
	policy.Enabled = input.Enabled
	policy.Priority = input.Priority
	policy.PolicyType = input.PolicyType
	policy.RegoCode = input.RegoCode
	policy.BuiltinName = input.BuiltinName
	policy.Config = input.Config
	policy.MinTrustLevel = input.MinTrustLevel
	policy.Tags = input.Tags
	policy.Metadata = input.Metadata
	policy.CreatedBy = input.CreatedBy
	policy.UpdatedBy = input.CreatedBy

	return &policy, nil
}

// Get retrieves a policy definition by ID.
func (s *PolicyDefinitionStore) Get(ctx context.Context, id uuid.UUID) (*PolicyDefinition, error) {
	query := `
		SELECT id, organization_id, name, description, category, enabled, priority,
			   policy_type, rego_code, builtin_name, config, min_trust_level,
			   tags, metadata, created_at, updated_at, created_by, updated_by
		FROM policy_definitions
		WHERE id = $1`

	return s.scanPolicy(ctx, query, id)
}

// GetByName retrieves a policy definition by name.
func (s *PolicyDefinitionStore) GetByName(ctx context.Context, orgID *uuid.UUID, name string) (*PolicyDefinition, error) {
	query := `
		SELECT id, organization_id, name, description, category, enabled, priority,
			   policy_type, rego_code, builtin_name, config, min_trust_level,
			   tags, metadata, created_at, updated_at, created_by, updated_by
		FROM policy_definitions
		WHERE name = $1 AND (organization_id = $2 OR organization_id IS NULL)
		ORDER BY organization_id NULLS LAST
		LIMIT 1`

	return s.scanPolicy(ctx, query, name, orgID)
}

func (s *PolicyDefinitionStore) scanPolicy(ctx context.Context, query string, args ...interface{}) (*PolicyDefinition, error) {
	var policy PolicyDefinition
	var orgID, createdBy, updatedBy sql.NullString
	var description, regoCode, builtinName sql.NullString
	var configJSON, metadataJSON []byte
	var tags pq.StringArray

	err := s.client.db.QueryRowContext(ctx, query, args...).Scan(
		&policy.ID,
		&orgID,
		&policy.Name,
		&description,
		&policy.Category,
		&policy.Enabled,
		&policy.Priority,
		&policy.PolicyType,
		&regoCode,
		&builtinName,
		&configJSON,
		&policy.MinTrustLevel,
		&tags,
		&metadataJSON,
		&policy.CreatedAt,
		&policy.UpdatedAt,
		&createdBy,
		&updatedBy,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: policy definition", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get policy definition: %w", err)
	}

	if orgID.Valid {
		id := uuid.MustParse(orgID.String)
		policy.OrganizationID = &id
	}
	if description.Valid {
		policy.Description = description.String
	}
	if regoCode.Valid {
		policy.RegoCode = regoCode.String
	}
	if builtinName.Valid {
		policy.BuiltinName = builtinName.String
	}
	if createdBy.Valid {
		id := uuid.MustParse(createdBy.String)
		policy.CreatedBy = &id
	}
	if updatedBy.Valid {
		id := uuid.MustParse(updatedBy.String)
		policy.UpdatedBy = &id
	}

	json.Unmarshal(configJSON, &policy.Config)
	json.Unmarshal(metadataJSON, &policy.Metadata)
	policy.Tags = tags

	return &policy, nil
}

// PolicyDefinitionFilter defines criteria for listing policy definitions.
type PolicyDefinitionFilter struct {
	OrganizationID *uuid.UUID
	Category       string
	Enabled        *bool
	PolicyType     string
	Tags           []string
	Limit          int
	Offset         int
}

// List retrieves policy definitions matching the filter criteria.
func (s *PolicyDefinitionStore) List(ctx context.Context, filter PolicyDefinitionFilter) ([]*PolicyDefinition, int, error) {
	baseQuery := `
		FROM policy_definitions
		WHERE (organization_id = $1 OR organization_id IS NULL)`

	args := []interface{}{filter.OrganizationID}
	argIdx := 2

	if filter.Category != "" {
		baseQuery += fmt.Sprintf(" AND category = $%d", argIdx)
		args = append(args, filter.Category)
		argIdx++
	}
	if filter.Enabled != nil {
		baseQuery += fmt.Sprintf(" AND enabled = $%d", argIdx)
		args = append(args, *filter.Enabled)
		argIdx++
	}
	if filter.PolicyType != "" {
		baseQuery += fmt.Sprintf(" AND policy_type = $%d", argIdx)
		args = append(args, filter.PolicyType)
		argIdx++
	}
	if len(filter.Tags) > 0 {
		baseQuery += fmt.Sprintf(" AND tags && $%d", argIdx)
		args = append(args, pq.Array(filter.Tags))
		argIdx++
	}

	// Get total count
	var total int
	countQuery := "SELECT COUNT(*) " + baseQuery
	if err := s.client.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count policy definitions: %w", err)
	}

	// Get paginated results
	selectQuery := `
		SELECT id, organization_id, name, description, category, enabled, priority,
			   policy_type, rego_code, builtin_name, config, min_trust_level,
			   tags, metadata, created_at, updated_at, created_by, updated_by
		` + baseQuery + " ORDER BY priority ASC, name ASC"

	if filter.Limit > 0 {
		selectQuery += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		selectQuery += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
	}

	rows, err := s.client.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list policy definitions: %w", err)
	}
	defer rows.Close()

	var policies []*PolicyDefinition
	for rows.Next() {
		var policy PolicyDefinition
		var orgID, createdBy, updatedBy sql.NullString
		var description, regoCode, builtinName sql.NullString
		var configJSON, metadataJSON []byte
		var tags pq.StringArray

		err := rows.Scan(
			&policy.ID,
			&orgID,
			&policy.Name,
			&description,
			&policy.Category,
			&policy.Enabled,
			&policy.Priority,
			&policy.PolicyType,
			&regoCode,
			&builtinName,
			&configJSON,
			&policy.MinTrustLevel,
			&tags,
			&metadataJSON,
			&policy.CreatedAt,
			&policy.UpdatedAt,
			&createdBy,
			&updatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan policy definition: %w", err)
		}

		if orgID.Valid {
			id := uuid.MustParse(orgID.String)
			policy.OrganizationID = &id
		}
		if description.Valid {
			policy.Description = description.String
		}
		if regoCode.Valid {
			policy.RegoCode = regoCode.String
		}
		if builtinName.Valid {
			policy.BuiltinName = builtinName.String
		}
		if createdBy.Valid {
			id := uuid.MustParse(createdBy.String)
			policy.CreatedBy = &id
		}
		if updatedBy.Valid {
			id := uuid.MustParse(updatedBy.String)
			policy.UpdatedBy = &id
		}

		json.Unmarshal(configJSON, &policy.Config)
		json.Unmarshal(metadataJSON, &policy.Metadata)
		policy.Tags = tags

		policies = append(policies, &policy)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating policy definitions: %w", err)
	}

	return policies, total, nil
}

// UpdatePolicyDefinitionInput contains the input for updating a policy definition.
type UpdatePolicyDefinitionInput struct {
	Name          *string
	Description   *string
	Category      *string
	Enabled       *bool
	Priority      *int
	PolicyType    *string
	RegoCode      *string
	BuiltinName   *string
	Config        map[string]interface{}
	MinTrustLevel *int
	Tags          []string
	Metadata      map[string]interface{}
	UpdatedBy     *uuid.UUID
}

// Update updates a policy definition.
func (s *PolicyDefinitionStore) Update(ctx context.Context, id uuid.UUID, input UpdatePolicyDefinitionInput) (*PolicyDefinition, error) {
	// Build dynamic update query
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *input.Name)
		argIdx++
	}
	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *input.Description)
		argIdx++
	}
	if input.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, *input.Category)
		argIdx++
	}
	if input.Enabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *input.Enabled)
		argIdx++
	}
	if input.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *input.Priority)
		argIdx++
	}
	if input.PolicyType != nil {
		setClauses = append(setClauses, fmt.Sprintf("policy_type = $%d", argIdx))
		args = append(args, *input.PolicyType)
		argIdx++
	}
	if input.RegoCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("rego_code = $%d", argIdx))
		args = append(args, *input.RegoCode)
		argIdx++
	}
	if input.BuiltinName != nil {
		setClauses = append(setClauses, fmt.Sprintf("builtin_name = $%d", argIdx))
		args = append(args, *input.BuiltinName)
		argIdx++
	}
	if input.Config != nil {
		configJSON, err := json.Marshal(input.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("config = $%d", argIdx))
		args = append(args, configJSON)
		argIdx++
	}
	if input.MinTrustLevel != nil {
		setClauses = append(setClauses, fmt.Sprintf("min_trust_level = $%d", argIdx))
		args = append(args, *input.MinTrustLevel)
		argIdx++
	}
	if input.Tags != nil {
		setClauses = append(setClauses, fmt.Sprintf("tags = $%d", argIdx))
		args = append(args, pq.Array(input.Tags))
		argIdx++
	}
	if input.Metadata != nil {
		metadataJSON, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", argIdx))
		args = append(args, metadataJSON)
		argIdx++
	}
	if input.UpdatedBy != nil {
		setClauses = append(setClauses, fmt.Sprintf("updated_by = $%d", argIdx))
		args = append(args, *input.UpdatedBy)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.Get(ctx, id)
	}

	query := "UPDATE policy_definitions SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	result, err := s.client.db.ExecContext(ctx, query, args...)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: policy with this name already exists", ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to update policy definition: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("%w: policy definition %s", ErrNotFound, id)
	}

	return s.Get(ctx, id)
}

// Delete deletes a policy definition.
func (s *PolicyDefinitionStore) Delete(ctx context.Context, id uuid.UUID) error {
	query := "DELETE FROM policy_definitions WHERE id = $1"

	result, err := s.client.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete policy definition: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: policy definition %s", ErrNotFound, id)
	}

	return nil
}

// ListEnabled returns all enabled policies for use by the policy engine.
// This implements the governance.PolicyStore interface.
func (s *PolicyDefinitionStore) ListEnabled(ctx context.Context) ([]*PolicyDefinitionForEngine, error) {
	query := `
		SELECT id, name, description, category, enabled, priority,
			   policy_type, rego_code, builtin_name, config, min_trust_level
		FROM policy_definitions
		WHERE enabled = true
		ORDER BY priority ASC`

	rows, err := s.client.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled policies: %w", err)
	}
	defer rows.Close()

	var policies []*PolicyDefinitionForEngine
	for rows.Next() {
		var p PolicyDefinitionForEngine
		var description, regoCode, builtinName sql.NullString
		var configJSON []byte

		err := rows.Scan(
			&p.ID,
			&p.Name,
			&description,
			&p.Category,
			&p.Enabled,
			&p.Priority,
			&p.PolicyType,
			&regoCode,
			&builtinName,
			&configJSON,
			&p.MinTrustLevel,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}

		if description.Valid {
			p.Description = description.String
		}
		if regoCode.Valid {
			p.RegoCode = regoCode.String
		}
		if builtinName.Valid {
			p.BuiltinName = builtinName.String
		}

		json.Unmarshal(configJSON, &p.Config)

		policies = append(policies, &p)
	}

	return policies, rows.Err()
}

// PolicyDefinitionForEngine is a simplified policy definition for the engine.
type PolicyDefinitionForEngine struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Category      string                 `json:"category"`
	Enabled       bool                   `json:"enabled"`
	Priority      int                    `json:"priority"`
	PolicyType    string                 `json:"policy_type"`
	RegoCode      string                 `json:"rego_code"`
	BuiltinName   string                 `json:"builtin_name"`
	Config        map[string]interface{} `json:"config"`
	MinTrustLevel int                    `json:"min_trust_level"`
}

// GetStatsForPolicy retrieves statistics for a policy definition.
func (s *PolicyDefinitionStore) GetStatsForPolicy(ctx context.Context, policyID uuid.UUID) (*PolicyDefinitionStats, error) {
	// Get stats from policy_evaluations table based on policy name
	query := `
		SELECT
			COUNT(*) as total,
			MAX(created_at) as last_triggered
		FROM policy_evaluations pe
		JOIN policy_definitions pd ON pd.name = pe.policy_name OR pd.builtin_name = pe.policy_name
		WHERE pd.id = $1`

	var stats PolicyDefinitionStats
	var lastTriggered sql.NullTime

	err := s.client.db.QueryRowContext(ctx, query, policyID).Scan(&stats.TriggerCount, &lastTriggered)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get policy stats: %w", err)
	}

	stats.PolicyID = policyID
	if lastTriggered.Valid {
		stats.LastTriggered = &lastTriggered.Time
	}

	return &stats, nil
}

// PolicyDefinitionStats holds statistics for a policy definition.
type PolicyDefinitionStats struct {
	PolicyID      uuid.UUID  `json:"policy_id"`
	TriggerCount  int64      `json:"trigger_count"`
	LastTriggered *time.Time `json:"last_triggered,omitempty"`
}

// Helper to check for unique constraint violation
func isUniqueViolation(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505"
	}
	return false
}

// Helper to create nullable string
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
