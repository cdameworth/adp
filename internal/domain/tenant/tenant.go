// Package tenant provides multi-tenant isolation and organization management for ADP.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Common errors for tenant operations
var (
	ErrTenantNotFound       = errors.New("tenant not found")
	ErrTenantDisabled       = errors.New("tenant is disabled")
	ErrInvalidTenant        = errors.New("invalid tenant")
	ErrOrganizationNotFound = errors.New("organization not found")
	ErrAccessDenied         = errors.New("access denied")
	ErrHierarchyViolation   = errors.New("organization hierarchy violation")
)

// TenantStatus represents the status of a tenant
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusDisabled  TenantStatus = "disabled"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusTrial     TenantStatus = "trial"
)

// Tenant represents a top-level tenant (enterprise customer) in the system
type Tenant struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Slug        string       `json:"slug"` // URL-friendly identifier
	Status      TenantStatus `json:"status"`
	Plan        string       `json:"plan"` // enterprise, pro, starter
	Settings    Settings     `json:"settings"`
	Quotas      Quotas       `json:"quotas"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	DisabledAt  *time.Time   `json:"disabled_at,omitempty"`
	TrialEndsAt *time.Time   `json:"trial_ends_at,omitempty"`
}

// Settings holds tenant-specific configuration
type Settings struct {
	AllowedDomains       []string          `json:"allowed_domains"`        // Email domains allowed for this tenant
	DefaultTrustLevel    int               `json:"default_trust_level"`    // Default trust level for new agents
	EnforceSSO           bool              `json:"enforce_sso"`            // Require SSO for all users
	AllowedAuthProviders []string          `json:"allowed_auth_providers"` // saml, oidc, github, etc.
	RetentionDays        int               `json:"retention_days"`         // Audit log retention
	CustomPolicies       bool              `json:"custom_policies"`        // Allow custom OPA policies
	APIRateLimit         int               `json:"api_rate_limit"`         // Requests per minute
	MaxConcurrentAgents  int               `json:"max_concurrent_agents"`  // Max active sessions
	FeatureFlags         map[string]bool   `json:"feature_flags"`          // Feature toggles
	Metadata             map[string]string `json:"metadata"`               // Custom metadata
}

// Quotas defines resource limits for a tenant
type Quotas struct {
	MaxOrganizations   int   `json:"max_organizations"`
	MaxUsersPerOrg     int   `json:"max_users_per_org"`
	MaxSessionsPerUser int   `json:"max_sessions_per_user"`
	MaxServicesPerOrg  int   `json:"max_services_per_org"`
	MaxPoliciesPerOrg  int   `json:"max_policies_per_org"`
	StorageGB          int   `json:"storage_gb"`
	APICallsPerMonth   int64 `json:"api_calls_per_month"`
	AuditRetentionDays int   `json:"audit_retention_days"`
}

// Organization represents a subdivision within a tenant
type Organization struct {
	ID          uuid.UUID         `json:"id"`
	TenantID    uuid.UUID         `json:"tenant_id"`
	ParentID    *uuid.UUID        `json:"parent_id,omitempty"` // For hierarchy
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Description string            `json:"description"`
	Settings    OrgSettings       `json:"settings"`
	Metadata    map[string]string `json:"metadata"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// OrgSettings holds organization-specific settings
type OrgSettings struct {
	DefaultTrustLevel   int               `json:"default_trust_level"`
	AllowedAgentTools   []string          `json:"allowed_agent_tools"` // claude_code, cursor, etc.
	PolicyOverrides     map[string]string `json:"policy_overrides"`    // Policy name -> action
	EscalationChannels  []string          `json:"escalation_channels"` // slack, email, etc.
	NotificationWebhook string            `json:"notification_webhook"`
	FeatureFlags        map[string]bool   `json:"feature_flags"`
}

// Team represents a group of users within an organization
type Team struct {
	ID             uuid.UUID         `json:"id"`
	OrganizationID uuid.UUID         `json:"organization_id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Permissions    []Permission      `json:"permissions"`
	ServiceScope   []uuid.UUID       `json:"service_scope"`   // Services this team can access
	MaxTrustLevel  int               `json:"max_trust_level"` // Maximum trust level members can use
	Metadata       map[string]string `json:"metadata"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// Permission defines what actions a team can perform
type Permission struct {
	Resource string   `json:"resource"` // sessions, services, policies, etc.
	Actions  []string `json:"actions"`  // create, read, update, delete, approve
	Scope    string   `json:"scope"`    // all, owned, team
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	TeamID    uuid.UUID  `json:"team_id"`
	UserID    uuid.UUID  `json:"user_id"`
	Role      TeamRole   `json:"role"`
	JoinedAt  time.Time  `json:"joined_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// TeamRole defines the role of a team member
type TeamRole string

const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
	TeamRoleViewer TeamRole = "viewer"
)

// TenantContext holds the resolved tenant context for a request
type TenantContext struct {
	Tenant       *Tenant
	Organization *Organization
	Teams        []*Team
	UserID       uuid.UUID
	Permissions  []Permission
}

// TenantStore defines the interface for tenant persistence
type TenantStore interface {
	// Tenant operations
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, id uuid.UUID) (*Tenant, error)
	GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error)
	UpdateTenant(ctx context.Context, tenant *Tenant) error
	ListTenants(ctx context.Context, filter TenantFilter) ([]*Tenant, error)

	// Organization operations
	CreateOrganization(ctx context.Context, org *Organization) error
	GetOrganization(ctx context.Context, id uuid.UUID) (*Organization, error)
	GetOrganizationBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Organization, error)
	UpdateOrganization(ctx context.Context, org *Organization) error
	ListOrganizations(ctx context.Context, tenantID uuid.UUID) ([]*Organization, error)
	GetOrganizationHierarchy(ctx context.Context, orgID uuid.UUID) ([]*Organization, error)

	// Team operations
	CreateTeam(ctx context.Context, team *Team) error
	GetTeam(ctx context.Context, id uuid.UUID) (*Team, error)
	UpdateTeam(ctx context.Context, team *Team) error
	ListTeams(ctx context.Context, orgID uuid.UUID) ([]*Team, error)
	AddTeamMember(ctx context.Context, member *TeamMember) error
	RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID) error
	GetUserTeams(ctx context.Context, userID uuid.UUID) ([]*Team, error)
}

// TenantFilter defines criteria for listing tenants
type TenantFilter struct {
	Status TenantStatus
	Plan   string
	Limit  int
	Offset int
}

// Service provides tenant management operations
type Service struct {
	store TenantStore
}

// NewService creates a new tenant service
func NewService(store TenantStore) *Service {
	return &Service{store: store}
}

// ResolveTenantContext resolves the full tenant context for a request
func (s *Service) ResolveTenantContext(ctx context.Context, tenantID, orgID, userID uuid.UUID) (*TenantContext, error) {
	// Get tenant
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	// Validate tenant status
	if tenant.Status == TenantStatusDisabled {
		return nil, ErrTenantDisabled
	}

	// Get organization
	org, err := s.store.GetOrganization(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}

	// Verify organization belongs to tenant
	if org.TenantID != tenantID {
		return nil, ErrAccessDenied
	}

	// Get user's teams
	teams, err := s.store.GetUserTeams(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}

	// Aggregate permissions
	var permissions []Permission
	for _, team := range teams {
		// Only include permissions from teams in this organization
		if team.OrganizationID == orgID {
			permissions = append(permissions, team.Permissions...)
		}
	}

	return &TenantContext{
		Tenant:       tenant,
		Organization: org,
		Teams:        teams,
		UserID:       userID,
		Permissions:  permissions,
	}, nil
}

// HasPermission checks if the tenant context has a specific permission
func (tc *TenantContext) HasPermission(resource, action string) bool {
	for _, p := range tc.Permissions {
		if p.Resource != resource && p.Resource != "*" {
			continue
		}
		for _, a := range p.Actions {
			if a == action || a == "*" {
				return true
			}
		}
	}
	return false
}

// GetMaxTrustLevel returns the maximum trust level allowed for this context
func (tc *TenantContext) GetMaxTrustLevel() int {
	maxLevel := tc.Organization.Settings.DefaultTrustLevel
	for _, team := range tc.Teams {
		if team.OrganizationID == tc.Organization.ID && team.MaxTrustLevel > maxLevel {
			maxLevel = team.MaxTrustLevel
		}
	}
	// Cap at tenant default if lower
	if tc.Tenant.Settings.DefaultTrustLevel > 0 && maxLevel > tc.Tenant.Settings.DefaultTrustLevel {
		maxLevel = tc.Tenant.Settings.DefaultTrustLevel
	}
	return maxLevel
}

// CanAccessService checks if this context can access a specific service
func (tc *TenantContext) CanAccessService(serviceID uuid.UUID) bool {
	for _, team := range tc.Teams {
		if team.OrganizationID != tc.Organization.ID {
			continue
		}
		// Empty service scope means access to all services
		if len(team.ServiceScope) == 0 {
			return true
		}
		for _, id := range team.ServiceScope {
			if id == serviceID {
				return true
			}
		}
	}
	return false
}

// ValidateOrganizationHierarchy validates that an organization change doesn't create cycles
func (s *Service) ValidateOrganizationHierarchy(ctx context.Context, org *Organization, newParentID *uuid.UUID) error {
	if newParentID == nil {
		return nil
	}

	// Check that new parent exists and is in the same tenant
	parent, err := s.store.GetOrganization(ctx, *newParentID)
	if err != nil {
		return fmt.Errorf("parent organization not found: %w", err)
	}
	if parent.TenantID != org.TenantID {
		return ErrHierarchyViolation
	}

	// Check for cycles by traversing up the hierarchy
	visited := make(map[uuid.UUID]bool)
	visited[org.ID] = true

	current := parent
	for current != nil {
		if visited[current.ID] {
			return fmt.Errorf("%w: cycle detected", ErrHierarchyViolation)
		}
		visited[current.ID] = true

		if current.ParentID == nil {
			break
		}
		current, err = s.store.GetOrganization(ctx, *current.ParentID)
		if err != nil {
			return err
		}
	}

	return nil
}

// DefaultQuotas returns default quotas for a given plan
func DefaultQuotas(plan string) Quotas {
	switch plan {
	case "enterprise":
		return Quotas{
			MaxOrganizations:   100,
			MaxUsersPerOrg:     1000,
			MaxSessionsPerUser: 10,
			MaxServicesPerOrg:  500,
			MaxPoliciesPerOrg:  100,
			StorageGB:          1000,
			APICallsPerMonth:   10000000,
			AuditRetentionDays: 365,
		}
	case "pro":
		return Quotas{
			MaxOrganizations:   10,
			MaxUsersPerOrg:     100,
			MaxSessionsPerUser: 5,
			MaxServicesPerOrg:  100,
			MaxPoliciesPerOrg:  50,
			StorageGB:          100,
			APICallsPerMonth:   1000000,
			AuditRetentionDays: 90,
		}
	default: // starter
		return Quotas{
			MaxOrganizations:   3,
			MaxUsersPerOrg:     25,
			MaxSessionsPerUser: 3,
			MaxServicesPerOrg:  25,
			MaxPoliciesPerOrg:  10,
			StorageGB:          10,
			APICallsPerMonth:   100000,
			AuditRetentionDays: 30,
		}
	}
}

// DefaultSettings returns default settings for a tenant
func DefaultSettings() Settings {
	return Settings{
		AllowedDomains:       []string{},
		DefaultTrustLevel:    2, // Contributor
		EnforceSSO:           false,
		AllowedAuthProviders: []string{"oidc", "saml"},
		RetentionDays:        90,
		CustomPolicies:       false,
		APIRateLimit:         1000,
		MaxConcurrentAgents:  50,
		FeatureFlags:         make(map[string]bool),
		Metadata:             make(map[string]string),
	}
}
