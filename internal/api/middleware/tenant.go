// Package middleware provides HTTP middleware for the ADP API.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// TenantContextKey is the context key for tenant information
type tenantContextKey string

const (
	TenantIDContextKey   tenantContextKey = "tenant_id"
	TenantSlugContextKey tenantContextKey = "tenant_slug"
	TenantContextObjKey  tenantContextKey = "tenant_context"
)

// TenantInfo holds resolved tenant information in the request context
type TenantInfo struct {
	TenantID       uuid.UUID   `json:"tenant_id"`
	TenantSlug     string      `json:"tenant_slug"`
	OrganizationID uuid.UUID   `json:"organization_id"`
	OrgSlug        string      `json:"org_slug"`
	UserID         uuid.UUID   `json:"user_id"`
	TeamIDs        []uuid.UUID `json:"team_ids,omitempty"`
	Permissions    []string    `json:"permissions,omitempty"`
	MaxTrustLevel  int         `json:"max_trust_level"`
}

// TenantResolver resolves tenant context from request/claims
type TenantResolver interface {
	ResolveTenant(ctx context.Context, tenantID uuid.UUID) (*TenantData, error)
	ResolveOrganization(ctx context.Context, orgID uuid.UUID) (*OrganizationData, error)
	GetUserPermissions(ctx context.Context, userID, orgID uuid.UUID) (*UserPermissions, error)
}

// TenantData contains basic tenant information
type TenantData struct {
	ID       uuid.UUID
	Slug     string
	Status   string
	Settings TenantSettings
}

// TenantSettings contains tenant-level settings
type TenantSettings struct {
	DefaultTrustLevel   int
	MaxConcurrentAgents int
	EnforceSSO          bool
}

// OrganizationData contains basic organization information
type OrganizationData struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Slug     string
	Settings OrgSettings
}

// OrgSettings contains organization-level settings
type OrgSettings struct {
	DefaultTrustLevel int
	AllowedAgentTools []string
}

// UserPermissions contains resolved user permissions
type UserPermissions struct {
	Permissions   []string
	TeamIDs       []uuid.UUID
	MaxTrustLevel int
	ServiceScope  []uuid.UUID
}

// TenantMiddlewareConfig holds configuration for tenant middleware
type TenantMiddlewareConfig struct {
	Resolver      TenantResolver
	RequireTenant bool       // If true, requests without tenant context are rejected
	DefaultTenant *uuid.UUID // Optional default tenant for single-tenant mode
}

// NewTenantMiddleware creates a middleware that resolves and enforces tenant context
func NewTenantMiddleware(cfg TenantMiddlewareConfig) *TenantMiddlewareHandler {
	return &TenantMiddlewareHandler{
		resolver:      cfg.Resolver,
		requireTenant: cfg.RequireTenant,
		defaultTenant: cfg.DefaultTenant,
	}
}

// TenantMiddlewareHandler handles tenant context resolution
type TenantMiddlewareHandler struct {
	resolver      TenantResolver
	requireTenant bool
	defaultTenant *uuid.UUID
}

// Middleware returns the HTTP middleware handler
func (t *TenantMiddlewareHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract tenant ID from JWT claims (set by auth middleware)
		tenantIDStr, hasTenant := GetOrganizationFromContext(ctx)
		userIDStr, hasUser := GetUserFromContext(ctx)

		// If no tenant in claims, check for default tenant
		var tenantID uuid.UUID
		if hasTenant && tenantIDStr != "" {
			var err error
			tenantID, err = uuid.Parse(tenantIDStr)
			if err != nil {
				writeTenantError(w, "Invalid tenant ID format", http.StatusBadRequest)
				return
			}
		} else if t.defaultTenant != nil {
			tenantID = *t.defaultTenant
		} else if t.requireTenant {
			writeTenantError(w, "Tenant context required", http.StatusUnauthorized)
			return
		} else {
			// No tenant context, proceed without
			next.ServeHTTP(w, r)
			return
		}

		// Parse user ID
		var userID uuid.UUID
		if hasUser && userIDStr != "" {
			var err error
			userID, err = uuid.Parse(userIDStr)
			if err != nil {
				writeTenantError(w, "Invalid user ID format", http.StatusBadRequest)
				return
			}
		}

		// Check for organization ID in header or query
		orgIDStr := r.Header.Get("X-Organization-ID")
		if orgIDStr == "" {
			orgIDStr = r.URL.Query().Get("org_id")
		}

		// Resolve tenant
		tenant, err := t.resolver.ResolveTenant(ctx, tenantID)
		if err != nil {
			writeTenantError(w, "Tenant not found", http.StatusNotFound)
			return
		}

		// Check tenant status
		if tenant.Status == "disabled" || tenant.Status == "suspended" {
			writeTenantError(w, "Tenant is not active", http.StatusForbidden)
			return
		}

		// Build tenant info
		info := &TenantInfo{
			TenantID:      tenantID,
			TenantSlug:    tenant.Slug,
			UserID:        userID,
			MaxTrustLevel: tenant.Settings.DefaultTrustLevel,
		}

		// Resolve organization if provided
		if orgIDStr != "" {
			orgID, err := uuid.Parse(orgIDStr)
			if err != nil {
				writeTenantError(w, "Invalid organization ID format", http.StatusBadRequest)
				return
			}

			org, err := t.resolver.ResolveOrganization(ctx, orgID)
			if err != nil {
				writeTenantError(w, "Organization not found", http.StatusNotFound)
				return
			}

			// Verify organization belongs to tenant
			if org.TenantID != tenantID {
				writeTenantError(w, "Organization does not belong to tenant", http.StatusForbidden)
				return
			}

			info.OrganizationID = orgID
			info.OrgSlug = org.Slug

			// Resolve user permissions if we have user ID
			if userID != uuid.Nil {
				perms, err := t.resolver.GetUserPermissions(ctx, userID, orgID)
				if err == nil {
					info.Permissions = perms.Permissions
					info.TeamIDs = perms.TeamIDs
					if perms.MaxTrustLevel > 0 {
						info.MaxTrustLevel = perms.MaxTrustLevel
					}
				}
			}
		}

		// Store tenant info in context
		ctx = context.WithValue(ctx, TenantIDContextKey, tenantID)
		ctx = context.WithValue(ctx, TenantSlugContextKey, tenant.Slug)
		ctx = context.WithValue(ctx, TenantContextObjKey, info)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetTenantIDFromContext retrieves the tenant ID from context
func GetTenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(TenantIDContextKey).(uuid.UUID)
	return id, ok
}

// GetTenantSlugFromContext retrieves the tenant slug from context
func GetTenantSlugFromContext(ctx context.Context) (string, bool) {
	slug, ok := ctx.Value(TenantSlugContextKey).(string)
	return slug, ok
}

// GetTenantInfoFromContext retrieves the full tenant info from context
func GetTenantInfoFromContext(ctx context.Context) (*TenantInfo, bool) {
	info, ok := ctx.Value(TenantContextObjKey).(*TenantInfo)
	return info, ok
}

// writeTenantError writes a JSON error response for tenant failures
func writeTenantError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   "tenant_error",
		"message": message,
	})
}

// RequireOrganization is a middleware that ensures organization context is present
func RequireOrganization(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := GetTenantInfoFromContext(r.Context())
		if !ok || info.OrganizationID == uuid.Nil {
			writeTenantError(w, "Organization context required", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireTenantPermission creates a middleware that checks for a specific permission in tenant context
func RequireTenantPermission(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := GetTenantInfoFromContext(r.Context())
			if !ok {
				writeTenantError(w, "Tenant context required", http.StatusUnauthorized)
				return
			}

			// Check for required permission
			requiredPerm := fmt.Sprintf("%s:%s", resource, action)
			hasPermission := false
			for _, p := range info.Permissions {
				if p == requiredPerm || p == resource+":*" || p == "*:*" {
					hasPermission = true
					break
				}
			}

			if !hasPermission {
				writeTenantError(w, fmt.Sprintf("Permission denied: %s", requiredPerm), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TenantScopedFilter adds tenant scoping to database queries
type TenantScopedFilter struct {
	TenantID       uuid.UUID
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	ServiceScope   []uuid.UUID
}

// NewTenantScopedFilter creates a filter from the request context
func NewTenantScopedFilter(ctx context.Context) (*TenantScopedFilter, error) {
	info, ok := GetTenantInfoFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("tenant context not found")
	}

	return &TenantScopedFilter{
		TenantID:       info.TenantID,
		OrganizationID: info.OrganizationID,
		UserID:         info.UserID,
	}, nil
}
