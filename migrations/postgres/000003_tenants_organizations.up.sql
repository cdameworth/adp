-- Migration: Add multi-tenant support with organizations and teams
-- Version: 000003
-- Phase 5: Production Readiness - Enterprise Features

-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Tenants table (top-level enterprise customers)
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL UNIQUE,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    plan VARCHAR(50) NOT NULL DEFAULT 'starter',
    settings JSONB NOT NULL DEFAULT '{}',
    quotas JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    disabled_at TIMESTAMP WITH TIME ZONE,
    trial_ends_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT chk_tenant_status CHECK (status IN ('active', 'disabled', 'suspended', 'trial')),
    CONSTRAINT chk_tenant_plan CHECK (plan IN ('enterprise', 'pro', 'starter', 'trial'))
);

-- Organizations table (subdivisions within tenants)
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    description TEXT DEFAULT '',
    settings JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT uq_org_tenant_slug UNIQUE (tenant_id, slug)
);

-- Teams table (groups within organizations)
CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    permissions JSONB NOT NULL DEFAULT '[]',
    service_scope UUID[] DEFAULT '{}',
    max_trust_level INTEGER NOT NULL DEFAULT 3,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT chk_team_trust_level CHECK (max_trust_level >= 1 AND max_trust_level <= 5),
    CONSTRAINT uq_team_org_name UNIQUE (organization_id, name)
);

-- Team members table (user membership in teams)
CREATE TABLE IF NOT EXISTS team_members (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'member',
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (team_id, user_id),
    CONSTRAINT chk_member_role CHECK (role IN ('owner', 'admin', 'member', 'viewer'))
);

-- Add tenant_id to existing agent_sessions table
ALTER TABLE agent_sessions
ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);

-- Add tenant_id to existing services table
ALTER TABLE services
ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);

-- Indexes for tenants
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_plan ON tenants(plan);

-- Indexes for organizations
CREATE INDEX IF NOT EXISTS idx_org_tenant ON organizations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_org_parent ON organizations(parent_id);
CREATE INDEX IF NOT EXISTS idx_org_slug ON organizations(tenant_id, slug);

-- Indexes for teams
CREATE INDEX IF NOT EXISTS idx_teams_org ON teams(organization_id);
CREATE INDEX IF NOT EXISTS idx_teams_name ON teams(organization_id, name);

-- Indexes for team members
CREATE INDEX IF NOT EXISTS idx_team_members_user ON team_members(user_id);
CREATE INDEX IF NOT EXISTS idx_team_members_role ON team_members(role);
CREATE INDEX IF NOT EXISTS idx_team_members_expires ON team_members(expires_at) WHERE expires_at IS NOT NULL;

-- Index for tenant_id on existing tables
CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON agent_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_services_tenant ON services(tenant_id);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Triggers for updated_at
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;
CREATE TRIGGER update_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_organizations_updated_at ON organizations;
CREATE TRIGGER update_organizations_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_teams_updated_at ON teams;
CREATE TRIGGER update_teams_updated_at
    BEFORE UPDATE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Default tenant for existing data (single-tenant mode)
INSERT INTO tenants (id, name, slug, status, plan, settings, quotas)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Default Tenant',
    'default',
    'active',
    'enterprise',
    '{"default_trust_level": 2, "enforce_sso": false, "retention_days": 90}',
    '{"max_organizations": 100, "max_users_per_org": 1000, "max_sessions_per_user": 10}'
)
ON CONFLICT (slug) DO NOTHING;

-- Default organization for existing data
INSERT INTO organizations (id, tenant_id, name, slug, description)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000001',
    'Default Organization',
    'default',
    'Default organization for existing data'
)
ON CONFLICT (tenant_id, slug) DO NOTHING;

-- Update existing sessions and services to use default tenant/org
UPDATE agent_sessions
SET tenant_id = '00000000-0000-0000-0000-000000000001',
    organization_id = '00000000-0000-0000-0000-000000000001'
WHERE tenant_id IS NULL;

UPDATE services
SET tenant_id = '00000000-0000-0000-0000-000000000001',
    organization_id = '00000000-0000-0000-0000-000000000001'
WHERE tenant_id IS NULL;

-- Add organization_id to services if not present
ALTER TABLE services
ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id);

-- Create default admin team
INSERT INTO teams (id, organization_id, name, description, permissions, max_trust_level)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000001',
    'Administrators',
    'Full access to all resources',
    '[{"resource": "*", "actions": ["*"], "scope": "all"}]',
    5
)
ON CONFLICT (organization_id, name) DO NOTHING;

COMMENT ON TABLE tenants IS 'Top-level enterprise customers for multi-tenant isolation';
COMMENT ON TABLE organizations IS 'Subdivisions within tenants supporting hierarchy';
COMMENT ON TABLE teams IS 'Groups within organizations with role-based permissions';
COMMENT ON TABLE team_members IS 'User membership in teams with optional expiration';
