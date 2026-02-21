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

// TenantRecord represents a tenant in the database
type TenantRecord struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Status      string     `json:"status"`
	Plan        string     `json:"plan"`
	Settings    []byte     `json:"settings"`
	Quotas      []byte     `json:"quotas"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
}

// OrganizationRecord represents an organization in the database
type OrganizationRecord struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Settings    []byte     `json:"settings"`
	Metadata    []byte     `json:"metadata"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TeamRecord represents a team in the database
type TeamRecord struct {
	ID             uuid.UUID   `json:"id"`
	OrganizationID uuid.UUID   `json:"organization_id"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Permissions    []byte      `json:"permissions"`
	ServiceScope   []uuid.UUID `json:"service_scope"`
	MaxTrustLevel  int         `json:"max_trust_level"`
	Metadata       []byte      `json:"metadata"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// TeamMemberRecord represents a team member in the database
type TeamMemberRecord struct {
	TeamID    uuid.UUID  `json:"team_id"`
	UserID    uuid.UUID  `json:"user_id"`
	Role      string     `json:"role"`
	JoinedAt  time.Time  `json:"joined_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// TenantStore provides CRUD operations for tenants, organizations, and teams
type TenantStore struct {
	client *PostgresClient
}

// NewTenantStore creates a new tenant store
func NewTenantStore(client *PostgresClient) *TenantStore {
	return &TenantStore{client: client}
}

// CreateTenant creates a new tenant
func (s *TenantStore) CreateTenant(ctx context.Context, tenant *TenantRecord) error {
	query := `
		INSERT INTO tenants (
			id, name, slug, status, plan, settings, quotas,
			created_at, updated_at, disabled_at, trial_ends_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	now := time.Now()
	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	_, err := s.client.db.ExecContext(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.Slug,
		tenant.Status,
		tenant.Plan,
		tenant.Settings,
		tenant.Quotas,
		tenant.CreatedAt,
		tenant.UpdatedAt,
		tenant.DisabledAt,
		tenant.TrialEndsAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("%w: tenant already exists", ErrDuplicateKey)
		}
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	return nil
}

// GetTenant retrieves a tenant by ID
func (s *TenantStore) GetTenant(ctx context.Context, id uuid.UUID) (*TenantRecord, error) {
	query := `
		SELECT id, name, slug, status, plan, settings, quotas,
			   created_at, updated_at, disabled_at, trial_ends_at
		FROM tenants
		WHERE id = $1`

	var tenant TenantRecord
	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Slug,
		&tenant.Status,
		&tenant.Plan,
		&tenant.Settings,
		&tenant.Quotas,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DisabledAt,
		&tenant.TrialEndsAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: tenant %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	return &tenant, nil
}

// GetTenantBySlug retrieves a tenant by slug
func (s *TenantStore) GetTenantBySlug(ctx context.Context, slug string) (*TenantRecord, error) {
	query := `
		SELECT id, name, slug, status, plan, settings, quotas,
			   created_at, updated_at, disabled_at, trial_ends_at
		FROM tenants
		WHERE slug = $1`

	var tenant TenantRecord
	err := s.client.db.QueryRowContext(ctx, query, slug).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Slug,
		&tenant.Status,
		&tenant.Plan,
		&tenant.Settings,
		&tenant.Quotas,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DisabledAt,
		&tenant.TrialEndsAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: tenant with slug %s", ErrNotFound, slug)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	return &tenant, nil
}

// UpdateTenant updates an existing tenant
func (s *TenantStore) UpdateTenant(ctx context.Context, tenant *TenantRecord) error {
	query := `
		UPDATE tenants
		SET name = $2, slug = $3, status = $4, plan = $5, settings = $6,
			quotas = $7, updated_at = $8, disabled_at = $9, trial_ends_at = $10
		WHERE id = $1`

	tenant.UpdatedAt = time.Now()

	result, err := s.client.db.ExecContext(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.Slug,
		tenant.Status,
		tenant.Plan,
		tenant.Settings,
		tenant.Quotas,
		tenant.UpdatedAt,
		tenant.DisabledAt,
		tenant.TrialEndsAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("%w: tenant %s", ErrNotFound, tenant.ID)
	}

	return nil
}

// TenantFilter defines criteria for listing tenants
type TenantFilter struct {
	Status string
	Plan   string
	Limit  int
	Offset int
}

// ListTenants retrieves tenants matching the filter
func (s *TenantStore) ListTenants(ctx context.Context, filter TenantFilter) ([]*TenantRecord, error) {
	query := `
		SELECT id, name, slug, status, plan, settings, quotas,
			   created_at, updated_at, disabled_at, trial_ends_at
		FROM tenants
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Plan != "" {
		query += fmt.Sprintf(" AND plan = $%d", argIdx)
		args = append(args, filter.Plan)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

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
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*TenantRecord
	for rows.Next() {
		var tenant TenantRecord
		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Slug,
			&tenant.Status,
			&tenant.Plan,
			&tenant.Settings,
			&tenant.Quotas,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&tenant.DisabledAt,
			&tenant.TrialEndsAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &tenant)
	}

	return tenants, rows.Err()
}

// CreateOrganization creates a new organization
func (s *TenantStore) CreateOrganization(ctx context.Context, org *OrganizationRecord) error {
	query := `
		INSERT INTO organizations (
			id, tenant_id, parent_id, name, slug, description,
			settings, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	now := time.Now()
	if org.ID == uuid.Nil {
		org.ID = uuid.New()
	}
	org.CreatedAt = now
	org.UpdatedAt = now

	if org.Settings == nil {
		org.Settings = []byte("{}")
	}
	if org.Metadata == nil {
		org.Metadata = []byte("{}")
	}

	_, err := s.client.db.ExecContext(ctx, query,
		org.ID,
		org.TenantID,
		org.ParentID,
		org.Name,
		org.Slug,
		org.Description,
		org.Settings,
		org.Metadata,
		org.CreatedAt,
		org.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("%w: organization already exists", ErrDuplicateKey)
		}
		return fmt.Errorf("failed to create organization: %w", err)
	}

	return nil
}

// GetOrganization retrieves an organization by ID
func (s *TenantStore) GetOrganization(ctx context.Context, id uuid.UUID) (*OrganizationRecord, error) {
	query := `
		SELECT id, tenant_id, parent_id, name, slug, description,
			   settings, metadata, created_at, updated_at
		FROM organizations
		WHERE id = $1`

	var org OrganizationRecord
	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&org.ID,
		&org.TenantID,
		&org.ParentID,
		&org.Name,
		&org.Slug,
		&org.Description,
		&org.Settings,
		&org.Metadata,
		&org.CreatedAt,
		&org.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: organization %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}

	return &org, nil
}

// GetOrganizationBySlug retrieves an organization by tenant ID and slug
func (s *TenantStore) GetOrganizationBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*OrganizationRecord, error) {
	query := `
		SELECT id, tenant_id, parent_id, name, slug, description,
			   settings, metadata, created_at, updated_at
		FROM organizations
		WHERE tenant_id = $1 AND slug = $2`

	var org OrganizationRecord
	err := s.client.db.QueryRowContext(ctx, query, tenantID, slug).Scan(
		&org.ID,
		&org.TenantID,
		&org.ParentID,
		&org.Name,
		&org.Slug,
		&org.Description,
		&org.Settings,
		&org.Metadata,
		&org.CreatedAt,
		&org.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: organization with slug %s", ErrNotFound, slug)
		}
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}

	return &org, nil
}

// UpdateOrganization updates an existing organization
func (s *TenantStore) UpdateOrganization(ctx context.Context, org *OrganizationRecord) error {
	query := `
		UPDATE organizations
		SET parent_id = $2, name = $3, slug = $4, description = $5,
			settings = $6, metadata = $7, updated_at = $8
		WHERE id = $1`

	org.UpdatedAt = time.Now()

	result, err := s.client.db.ExecContext(ctx, query,
		org.ID,
		org.ParentID,
		org.Name,
		org.Slug,
		org.Description,
		org.Settings,
		org.Metadata,
		org.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update organization: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("%w: organization %s", ErrNotFound, org.ID)
	}

	return nil
}

// ListOrganizations retrieves all organizations for a tenant
func (s *TenantStore) ListOrganizations(ctx context.Context, tenantID uuid.UUID) ([]*OrganizationRecord, error) {
	query := `
		SELECT id, tenant_id, parent_id, name, slug, description,
			   settings, metadata, created_at, updated_at
		FROM organizations
		WHERE tenant_id = $1
		ORDER BY name`

	rows, err := s.client.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}
	defer rows.Close()

	var orgs []*OrganizationRecord
	for rows.Next() {
		var org OrganizationRecord
		err := rows.Scan(
			&org.ID,
			&org.TenantID,
			&org.ParentID,
			&org.Name,
			&org.Slug,
			&org.Description,
			&org.Settings,
			&org.Metadata,
			&org.CreatedAt,
			&org.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan organization: %w", err)
		}
		orgs = append(orgs, &org)
	}

	return orgs, rows.Err()
}

// GetOrganizationHierarchy retrieves an organization and all its ancestors
func (s *TenantStore) GetOrganizationHierarchy(ctx context.Context, orgID uuid.UUID) ([]*OrganizationRecord, error) {
	// Use a recursive CTE to get the hierarchy
	query := `
		WITH RECURSIVE org_hierarchy AS (
			SELECT id, tenant_id, parent_id, name, slug, description,
				   settings, metadata, created_at, updated_at, 0 as depth
			FROM organizations
			WHERE id = $1

			UNION ALL

			SELECT o.id, o.tenant_id, o.parent_id, o.name, o.slug, o.description,
				   o.settings, o.metadata, o.created_at, o.updated_at, h.depth + 1
			FROM organizations o
			INNER JOIN org_hierarchy h ON o.id = h.parent_id
		)
		SELECT id, tenant_id, parent_id, name, slug, description,
			   settings, metadata, created_at, updated_at
		FROM org_hierarchy
		ORDER BY depth DESC`

	rows, err := s.client.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization hierarchy: %w", err)
	}
	defer rows.Close()

	var orgs []*OrganizationRecord
	for rows.Next() {
		var org OrganizationRecord
		err := rows.Scan(
			&org.ID,
			&org.TenantID,
			&org.ParentID,
			&org.Name,
			&org.Slug,
			&org.Description,
			&org.Settings,
			&org.Metadata,
			&org.CreatedAt,
			&org.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan organization: %w", err)
		}
		orgs = append(orgs, &org)
	}

	return orgs, rows.Err()
}

// CreateTeam creates a new team
func (s *TenantStore) CreateTeam(ctx context.Context, team *TeamRecord) error {
	query := `
		INSERT INTO teams (
			id, organization_id, name, description, permissions,
			service_scope, max_trust_level, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	now := time.Now()
	if team.ID == uuid.Nil {
		team.ID = uuid.New()
	}
	team.CreatedAt = now
	team.UpdatedAt = now

	if team.Permissions == nil {
		team.Permissions = []byte("[]")
	}
	if team.Metadata == nil {
		team.Metadata = []byte("{}")
	}

	_, err := s.client.db.ExecContext(ctx, query,
		team.ID,
		team.OrganizationID,
		team.Name,
		team.Description,
		team.Permissions,
		pq.Array(team.ServiceScope),
		team.MaxTrustLevel,
		team.Metadata,
		team.CreatedAt,
		team.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("%w: team already exists", ErrDuplicateKey)
		}
		return fmt.Errorf("failed to create team: %w", err)
	}

	return nil
}

// GetTeam retrieves a team by ID
func (s *TenantStore) GetTeam(ctx context.Context, id uuid.UUID) (*TeamRecord, error) {
	query := `
		SELECT id, organization_id, name, description, permissions,
			   service_scope, max_trust_level, metadata, created_at, updated_at
		FROM teams
		WHERE id = $1`

	var team TeamRecord
	var serviceScope []uuid.UUID
	err := s.client.db.QueryRowContext(ctx, query, id).Scan(
		&team.ID,
		&team.OrganizationID,
		&team.Name,
		&team.Description,
		&team.Permissions,
		pq.Array(&serviceScope),
		&team.MaxTrustLevel,
		&team.Metadata,
		&team.CreatedAt,
		&team.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: team %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to get team: %w", err)
	}
	team.ServiceScope = serviceScope

	return &team, nil
}

// UpdateTeam updates an existing team
func (s *TenantStore) UpdateTeam(ctx context.Context, team *TeamRecord) error {
	query := `
		UPDATE teams
		SET name = $2, description = $3, permissions = $4, service_scope = $5,
			max_trust_level = $6, metadata = $7, updated_at = $8
		WHERE id = $1`

	team.UpdatedAt = time.Now()

	result, err := s.client.db.ExecContext(ctx, query,
		team.ID,
		team.Name,
		team.Description,
		team.Permissions,
		pq.Array(team.ServiceScope),
		team.MaxTrustLevel,
		team.Metadata,
		team.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("%w: team %s", ErrNotFound, team.ID)
	}

	return nil
}

// ListTeams retrieves all teams for an organization
func (s *TenantStore) ListTeams(ctx context.Context, orgID uuid.UUID) ([]*TeamRecord, error) {
	query := `
		SELECT id, organization_id, name, description, permissions,
			   service_scope, max_trust_level, metadata, created_at, updated_at
		FROM teams
		WHERE organization_id = $1
		ORDER BY name`

	rows, err := s.client.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}
	defer rows.Close()

	var teams []*TeamRecord
	for rows.Next() {
		var team TeamRecord
		var serviceScope []uuid.UUID
		err := rows.Scan(
			&team.ID,
			&team.OrganizationID,
			&team.Name,
			&team.Description,
			&team.Permissions,
			pq.Array(&serviceScope),
			&team.MaxTrustLevel,
			&team.Metadata,
			&team.CreatedAt,
			&team.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		team.ServiceScope = serviceScope
		teams = append(teams, &team)
	}

	return teams, rows.Err()
}

// AddTeamMember adds a user to a team
func (s *TenantStore) AddTeamMember(ctx context.Context, member *TeamMemberRecord) error {
	query := `
		INSERT INTO team_members (team_id, user_id, role, joined_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (team_id, user_id)
		DO UPDATE SET role = $3, expires_at = $5`

	member.JoinedAt = time.Now()

	_, err := s.client.db.ExecContext(ctx, query,
		member.TeamID,
		member.UserID,
		member.Role,
		member.JoinedAt,
		member.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to add team member: %w", err)
	}

	return nil
}

// RemoveTeamMember removes a user from a team
func (s *TenantStore) RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID) error {
	query := `DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`

	result, err := s.client.db.ExecContext(ctx, query, teamID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove team member: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("%w: team member not found", ErrNotFound)
	}

	return nil
}

// GetUserTeams retrieves all teams a user belongs to
func (s *TenantStore) GetUserTeams(ctx context.Context, userID uuid.UUID) ([]*TeamRecord, error) {
	query := `
		SELECT t.id, t.organization_id, t.name, t.description, t.permissions,
			   t.service_scope, t.max_trust_level, t.metadata, t.created_at, t.updated_at
		FROM teams t
		INNER JOIN team_members tm ON t.id = tm.team_id
		WHERE tm.user_id = $1
		  AND (tm.expires_at IS NULL OR tm.expires_at > NOW())
		ORDER BY t.name`

	rows, err := s.client.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}
	defer rows.Close()

	var teams []*TeamRecord
	for rows.Next() {
		var team TeamRecord
		var serviceScope []uuid.UUID
		err := rows.Scan(
			&team.ID,
			&team.OrganizationID,
			&team.Name,
			&team.Description,
			&team.Permissions,
			pq.Array(&serviceScope),
			&team.MaxTrustLevel,
			&team.Metadata,
			&team.CreatedAt,
			&team.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		team.ServiceScope = serviceScope
		teams = append(teams, &team)
	}

	return teams, rows.Err()
}

// GetUserPermissions retrieves aggregated permissions for a user in an organization
func (s *TenantStore) GetUserPermissions(ctx context.Context, userID, orgID uuid.UUID) ([]string, []uuid.UUID, int, error) {
	query := `
		SELECT t.permissions, t.service_scope, t.max_trust_level
		FROM teams t
		INNER JOIN team_members tm ON t.id = tm.team_id
		WHERE tm.user_id = $1 AND t.organization_id = $2
		  AND (tm.expires_at IS NULL OR tm.expires_at > NOW())`

	rows, err := s.client.db.QueryContext(ctx, query, userID, orgID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to get user permissions: %w", err)
	}
	defer rows.Close()

	permSet := make(map[string]bool)
	serviceSet := make(map[uuid.UUID]bool)
	maxTrustLevel := 0

	for rows.Next() {
		var permsJSON []byte
		var serviceScope []uuid.UUID
		var trustLevel int

		err := rows.Scan(&permsJSON, pq.Array(&serviceScope), &trustLevel)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to scan permissions: %w", err)
		}

		// Parse permissions
		var perms []map[string]interface{}
		if err := json.Unmarshal(permsJSON, &perms); err == nil {
			for _, p := range perms {
				resource, _ := p["resource"].(string)
				actions, _ := p["actions"].([]interface{})
				for _, a := range actions {
					if action, ok := a.(string); ok {
						permSet[resource+":"+action] = true
					}
				}
			}
		}

		// Aggregate service scope
		for _, id := range serviceScope {
			serviceSet[id] = true
		}

		// Track max trust level
		if trustLevel > maxTrustLevel {
			maxTrustLevel = trustLevel
		}
	}

	// Convert sets to slices
	permissions := make([]string, 0, len(permSet))
	for p := range permSet {
		permissions = append(permissions, p)
	}

	services := make([]uuid.UUID, 0, len(serviceSet))
	for id := range serviceSet {
		services = append(services, id)
	}

	return permissions, services, maxTrustLevel, rows.Err()
}
