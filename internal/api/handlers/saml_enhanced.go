// Package handlers provides HTTP handlers for the ADP API.
package handlers

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml/samlsp"
	"github.com/google/uuid"
)

// EnhancedSAMLConfig holds configuration for enhanced SAML SP with org mapping
type EnhancedSAMLConfig struct {
	RootURL     string
	IdPMetadata string
	CertFile    string
	KeyFile     string

	// Attribute mapping configuration
	AttributeMapping AttributeMapping

	// Organization mapping
	OrgMapping OrgMappingConfig

	// Session configuration
	SessionDuration time.Duration
	CookieName      string
	CookieDomain    string
	CookieSecure    bool
}

// AttributeMapping defines how SAML attributes map to user properties
type AttributeMapping struct {
	UserID       string   `json:"user_id"`       // SAML attribute for user ID (e.g., "uid", "email", "sub")
	Email        string   `json:"email"`         // SAML attribute for email
	DisplayName  string   `json:"display_name"`  // SAML attribute for display name
	Groups       string   `json:"groups"`        // SAML attribute for group membership
	Organization string   `json:"organization"`  // SAML attribute for organization
	Roles        string   `json:"roles"`         // SAML attribute for roles
	TenantID     string   `json:"tenant_id"`     // SAML attribute for tenant ID
	Department   string   `json:"department"`    // SAML attribute for department
	CustomClaims []string `json:"custom_claims"` // Additional claims to extract
}

// OrgMappingConfig defines how to map SAML groups/attributes to ADP organizations
type OrgMappingConfig struct {
	// DefaultTenantID is used when no tenant mapping is found
	DefaultTenantID string `json:"default_tenant_id"`
	// DefaultOrgID is used when no org mapping is found
	DefaultOrgID string `json:"default_org_id"`
	// TenantMappings maps SAML attribute values to tenant IDs
	TenantMappings map[string]string `json:"tenant_mappings"`
	// OrgMappings maps SAML groups to organization IDs
	OrgMappings map[string]string `json:"org_mappings"`
	// TeamMappings maps SAML groups to team IDs
	TeamMappings map[string]string `json:"team_mappings"`
	// RoleMappings maps SAML roles to ADP permission sets
	RoleMappings map[string][]string `json:"role_mappings"`
	// UseGroupForOrg if true, uses the first group as organization identifier
	UseGroupForOrg bool `json:"use_group_for_org"`
}

// SAMLUser represents a user extracted from SAML assertion
type SAMLUser struct {
	ID           string            `json:"id"`
	Email        string            `json:"email"`
	DisplayName  string            `json:"display_name"`
	Groups       []string          `json:"groups"`
	Roles        []string          `json:"roles"`
	TenantID     uuid.UUID         `json:"tenant_id"`
	OrgID        uuid.UUID         `json:"organization_id"`
	TeamIDs      []uuid.UUID       `json:"team_ids"`
	Permissions  []string          `json:"permissions"`
	Department   string            `json:"department,omitempty"`
	CustomClaims map[string]string `json:"custom_claims,omitempty"`
	AuthnTime    time.Time         `json:"authn_time"`
	SessionIndex string            `json:"session_index,omitempty"`
}

// EnhancedSAMLMiddleware wraps samlsp.Middleware with organization mapping
type EnhancedSAMLMiddleware struct {
	sp        *samlsp.Middleware
	config    EnhancedSAMLConfig
	orgMapper *OrgMapper
	userStore SAMLUserStore
}

// SAMLUserStore interface for persisting SAML user info
type SAMLUserStore interface {
	UpsertSAMLUser(ctx context.Context, user *SAMLUser) error
	GetSAMLUser(ctx context.Context, samlID string) (*SAMLUser, error)
	RecordSAMLLogin(ctx context.Context, userID string, sessionIndex string, metadata map[string]string) error
}

// OrgMapper handles mapping of SAML attributes to ADP organizations
type OrgMapper struct {
	config OrgMappingConfig
}

// NewOrgMapper creates a new organization mapper
func NewOrgMapper(config OrgMappingConfig) *OrgMapper {
	if config.TenantMappings == nil {
		config.TenantMappings = make(map[string]string)
	}
	if config.OrgMappings == nil {
		config.OrgMappings = make(map[string]string)
	}
	if config.TeamMappings == nil {
		config.TeamMappings = make(map[string]string)
	}
	if config.RoleMappings == nil {
		config.RoleMappings = make(map[string][]string)
	}
	return &OrgMapper{config: config}
}

// MapTenant maps SAML attributes to a tenant ID
func (m *OrgMapper) MapTenant(attributes map[string][]string, tenantAttr string) (uuid.UUID, error) {
	// Try direct tenant attribute first
	if tenantAttr != "" {
		if values, ok := attributes[tenantAttr]; ok && len(values) > 0 {
			if mapped, ok := m.config.TenantMappings[values[0]]; ok {
				return uuid.Parse(mapped)
			}
			// Try parsing the value directly as UUID
			if id, err := uuid.Parse(values[0]); err == nil {
				return id, nil
			}
		}
	}

	// Fall back to default
	if m.config.DefaultTenantID != "" {
		return uuid.Parse(m.config.DefaultTenantID)
	}

	return uuid.Nil, fmt.Errorf("no tenant mapping found")
}

// MapOrganization maps SAML groups to an organization ID
func (m *OrgMapper) MapOrganization(groups []string, orgAttr string, attributes map[string][]string) (uuid.UUID, error) {
	// Try direct organization attribute first
	if orgAttr != "" {
		if values, ok := attributes[orgAttr]; ok && len(values) > 0 {
			if mapped, ok := m.config.OrgMappings[values[0]]; ok {
				return uuid.Parse(mapped)
			}
			// Try parsing the value directly as UUID
			if id, err := uuid.Parse(values[0]); err == nil {
				return id, nil
			}
		}
	}

	// Try group-based mapping
	for _, group := range groups {
		if mapped, ok := m.config.OrgMappings[group]; ok {
			return uuid.Parse(mapped)
		}
	}

	// If UseGroupForOrg, use first group as org identifier
	if m.config.UseGroupForOrg && len(groups) > 0 {
		if mapped, ok := m.config.OrgMappings[groups[0]]; ok {
			return uuid.Parse(mapped)
		}
	}

	// Fall back to default
	if m.config.DefaultOrgID != "" {
		return uuid.Parse(m.config.DefaultOrgID)
	}

	return uuid.Nil, fmt.Errorf("no organization mapping found")
}

// MapTeams maps SAML groups to team IDs
func (m *OrgMapper) MapTeams(groups []string) []uuid.UUID {
	teamSet := make(map[uuid.UUID]bool)
	for _, group := range groups {
		if mapped, ok := m.config.TeamMappings[group]; ok {
			if id, err := uuid.Parse(mapped); err == nil {
				teamSet[id] = true
			}
		}
	}

	teams := make([]uuid.UUID, 0, len(teamSet))
	for id := range teamSet {
		teams = append(teams, id)
	}
	return teams
}

// MapPermissions maps SAML roles to ADP permissions
func (m *OrgMapper) MapPermissions(roles []string) []string {
	permSet := make(map[string]bool)
	for _, role := range roles {
		if perms, ok := m.config.RoleMappings[role]; ok {
			for _, p := range perms {
				permSet[p] = true
			}
		}
	}

	permissions := make([]string, 0, len(permSet))
	for p := range permSet {
		permissions = append(permissions, p)
	}
	return permissions
}

// NewEnhancedSAMLMiddleware creates a new enhanced SAML middleware
func NewEnhancedSAMLMiddleware(cfg EnhancedSAMLConfig, userStore SAMLUserStore) (*EnhancedSAMLMiddleware, error) {
	keyPair, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %w", err)
	}

	keyStore := keyPair.PrivateKey.(*rsa.PrivateKey)
	idpMetadataURL, err := url.Parse(cfg.IdPMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IdP metadata URL: %w", err)
	}

	rootURL, err := url.Parse(cfg.RootURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse root URL: %w", err)
	}

	x509Cert, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient, *idpMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IdP metadata: %w", err)
	}

	// Set defaults
	if cfg.SessionDuration == 0 {
		cfg.SessionDuration = 8 * time.Hour
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "adp_saml_session"
	}
	if cfg.AttributeMapping.UserID == "" {
		cfg.AttributeMapping.UserID = "uid"
	}
	if cfg.AttributeMapping.Email == "" {
		cfg.AttributeMapping.Email = "email"
	}

	// Create SAML SP with custom session handling
	samlSP, err := samlsp.New(samlsp.Options{
		URL:               *rootURL,
		Key:               keyStore,
		Certificate:       x509Cert,
		IDPMetadata:       idpMetadata,
		AllowIDPInitiated: true,
		CookieName:        cfg.CookieName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML SP: %w", err)
	}

	return &EnhancedSAMLMiddleware{
		sp:        samlSP,
		config:    cfg,
		orgMapper: NewOrgMapper(cfg.OrgMapping),
		userStore: userStore,
	}, nil
}

// RequireAccount returns middleware that requires SAML authentication
func (m *EnhancedSAMLMiddleware) RequireAccount(next http.Handler) http.Handler {
	return m.sp.RequireAccount(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract and process SAML session
		session := samlsp.SessionFromContext(r.Context())
		if session == nil {
			http.Error(w, "SAML session not found", http.StatusUnauthorized)
			return
		}

		sa, ok := session.(samlsp.SessionWithAttributes)
		if !ok {
			http.Error(w, "Invalid SAML session", http.StatusUnauthorized)
			return
		}

		// Extract user from SAML attributes
		user, err := m.extractUser(sa)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to extract user: %v", err), http.StatusInternalServerError)
			return
		}

		// Store user in context
		ctx := context.WithValue(r.Context(), samlUserContextKey, user)

		// Also store individual values for compatibility with existing middleware
		ctx = context.WithValue(ctx, userContextKey, user.ID)
		if user.TenantID != uuid.Nil {
			ctx = context.WithValue(ctx, orgContextKey, user.TenantID.String())
		}

		// Persist user info if store is available
		if m.userStore != nil {
			if err := m.userStore.UpsertSAMLUser(ctx, user); err != nil {
				// Log but don't fail - authentication succeeded
				fmt.Printf("Warning: failed to persist SAML user: %v\n", err)
			}

			// Record login
			metadata := map[string]string{
				"email":     user.Email,
				"tenant_id": user.TenantID.String(),
				"org_id":    user.OrgID.String(),
			}
			if err := m.userStore.RecordSAMLLogin(ctx, user.ID, user.SessionIndex, metadata); err != nil {
				fmt.Printf("Warning: failed to record SAML login: %v\n", err)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// extractUser extracts user information from SAML attributes
func (m *EnhancedSAMLMiddleware) extractUser(session samlsp.SessionWithAttributes) (*SAMLUser, error) {
	attrs := session.GetAttributes()
	mapping := m.config.AttributeMapping

	user := &SAMLUser{
		CustomClaims: make(map[string]string),
		AuthnTime:    time.Now(),
	}

	// Extract user ID
	if val := attrs.Get(mapping.UserID); val != "" {
		user.ID = val
	} else {
		return nil, fmt.Errorf("user ID attribute '%s' not found", mapping.UserID)
	}

	// Extract email
	if val := attrs.Get(mapping.Email); val != "" {
		user.Email = val
	}

	// Extract display name
	if mapping.DisplayName != "" {
		if val := attrs.Get(mapping.DisplayName); val != "" {
			user.DisplayName = val
		}
	}

	// Extract groups (may be multi-valued)
	if mapping.Groups != "" {
		user.Groups = m.getMultiValuedAttr(attrs, mapping.Groups)
	}

	// Extract roles (may be multi-valued)
	if mapping.Roles != "" {
		user.Roles = m.getMultiValuedAttr(attrs, mapping.Roles)
	}

	// Extract department
	if mapping.Department != "" {
		if val := attrs.Get(mapping.Department); val != "" {
			user.Department = val
		}
	}

	// Extract custom claims
	for _, claim := range mapping.CustomClaims {
		if val := attrs.Get(claim); val != "" {
			user.CustomClaims[claim] = val
		}
	}

	// Map to tenant
	attrMap := m.attributesToMap(attrs)
	tenantID, err := m.orgMapper.MapTenant(attrMap, mapping.TenantID)
	if err != nil {
		// Use default tenant if mapping fails
		if m.config.OrgMapping.DefaultTenantID != "" {
			tenantID, _ = uuid.Parse(m.config.OrgMapping.DefaultTenantID)
		}
	}
	user.TenantID = tenantID

	// Map to organization
	orgID, err := m.orgMapper.MapOrganization(user.Groups, mapping.Organization, attrMap)
	if err != nil {
		// Use default org if mapping fails
		if m.config.OrgMapping.DefaultOrgID != "" {
			orgID, _ = uuid.Parse(m.config.OrgMapping.DefaultOrgID)
		}
	}
	user.OrgID = orgID

	// Map teams from groups
	user.TeamIDs = m.orgMapper.MapTeams(user.Groups)

	// Map permissions from roles
	user.Permissions = m.orgMapper.MapPermissions(user.Roles)

	return user, nil
}

// getMultiValuedAttr handles attributes that may have multiple values
func (m *EnhancedSAMLMiddleware) getMultiValuedAttr(attrs samlsp.Attributes, name string) []string {
	// First try as a single value
	if val := attrs.Get(name); val != "" {
		// Check if it's a comma-separated list
		if strings.Contains(val, ",") {
			parts := strings.Split(val, ",")
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result
		}
		return []string{val}
	}
	return nil
}

// attributesToMap converts SAML attributes to a map for processing
func (m *EnhancedSAMLMiddleware) attributesToMap(attrs samlsp.Attributes) map[string][]string {
	result := make(map[string][]string)
	// Note: saml.Attributes doesn't expose iteration directly
	// We'll use known attribute names from our mapping
	mapping := m.config.AttributeMapping
	attrNames := []string{
		mapping.UserID,
		mapping.Email,
		mapping.DisplayName,
		mapping.Groups,
		mapping.Organization,
		mapping.Roles,
		mapping.TenantID,
		mapping.Department,
	}
	attrNames = append(attrNames, mapping.CustomClaims...)

	for _, name := range attrNames {
		if name != "" {
			if val := attrs.Get(name); val != "" {
				result[name] = []string{val}
			}
		}
	}
	return result
}

// GetServiceProvider returns the underlying SAML service provider
func (m *EnhancedSAMLMiddleware) GetServiceProvider() *samlsp.Middleware {
	return m.sp
}

// ServeHTTP handles SAML endpoints (metadata, ACS, SLO)
func (m *EnhancedSAMLMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.sp.ServeHTTP(w, r)
}

// Context keys for SAML user
type samlContextKey string

const (
	samlUserContextKey samlContextKey = "saml_user"
	userContextKey     samlContextKey = "user"
	orgContextKey      samlContextKey = "organization"
)

// GetSAMLUserFromContext extracts the SAML user from context
func GetSAMLUserFromContext(ctx context.Context) (*SAMLUser, bool) {
	user, ok := ctx.Value(samlUserContextKey).(*SAMLUser)
	return user, ok
}

// SAMLUserResponse is the API response for SAML user info
type SAMLUserResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	TenantID    string    `json:"tenant_id"`
	OrgID       string    `json:"organization_id"`
	Groups      []string  `json:"groups"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"permissions"`
	AuthnTime   time.Time `json:"authn_time"`
}

// SAMLUserInfoHandler returns the current SAML user's info
func SAMLUserInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetSAMLUserFromContext(r.Context())
		if !ok {
			http.Error(w, "SAML user not found in context", http.StatusUnauthorized)
			return
		}

		response := SAMLUserResponse{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			TenantID:    user.TenantID.String(),
			OrgID:       user.OrgID.String(),
			Groups:      user.Groups,
			Roles:       user.Roles,
			Permissions: user.Permissions,
			AuthnTime:   user.AuthnTime,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// DefaultAttributeMapping returns sensible defaults for common IdPs
func DefaultAttributeMapping() AttributeMapping {
	return AttributeMapping{
		UserID:       "uid",
		Email:        "email",
		DisplayName:  "displayName",
		Groups:       "memberOf",
		Roles:        "roles",
		Organization: "organization",
		TenantID:     "tenantId",
		Department:   "department",
	}
}

// OktaAttributeMapping returns attribute mapping for Okta
func OktaAttributeMapping() AttributeMapping {
	return AttributeMapping{
		UserID:       "uid",
		Email:        "email",
		DisplayName:  "firstName",
		Groups:       "groups",
		Roles:        "roles",
		Organization: "organization",
		Department:   "department",
	}
}

// AzureADAttributeMapping returns attribute mapping for Azure AD
func AzureADAttributeMapping() AttributeMapping {
	return AttributeMapping{
		UserID:      "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
		Email:       "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		DisplayName: "http://schemas.microsoft.com/identity/claims/displayname",
		Groups:      "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		Roles:       "http://schemas.microsoft.com/ws/2008/06/identity/claims/role",
		TenantID:    "http://schemas.microsoft.com/identity/claims/tenantid",
		Department:  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/department",
	}
}

// GoogleAttributeMapping returns attribute mapping for Google Workspace
func GoogleAttributeMapping() AttributeMapping {
	return AttributeMapping{
		UserID:       "uid",
		Email:        "email",
		DisplayName:  "name",
		Groups:       "groups",
		Organization: "org",
	}
}
