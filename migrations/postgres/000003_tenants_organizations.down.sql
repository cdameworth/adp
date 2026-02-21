-- Rollback migration: Remove multi-tenant support
-- Version: 000003

-- Remove indexes
DROP INDEX IF EXISTS idx_sessions_tenant;
DROP INDEX IF EXISTS idx_services_tenant;
DROP INDEX IF EXISTS idx_team_members_expires;
DROP INDEX IF EXISTS idx_team_members_role;
DROP INDEX IF EXISTS idx_team_members_user;
DROP INDEX IF EXISTS idx_teams_name;
DROP INDEX IF EXISTS idx_teams_org;
DROP INDEX IF EXISTS idx_org_slug;
DROP INDEX IF EXISTS idx_org_parent;
DROP INDEX IF EXISTS idx_org_tenant;
DROP INDEX IF EXISTS idx_tenants_plan;
DROP INDEX IF EXISTS idx_tenants_status;
DROP INDEX IF EXISTS idx_tenants_slug;

-- Remove triggers
DROP TRIGGER IF EXISTS update_teams_updated_at ON teams;
DROP TRIGGER IF EXISTS update_organizations_updated_at ON organizations;
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;

-- Remove columns from existing tables
ALTER TABLE services DROP COLUMN IF EXISTS organization_id;
ALTER TABLE services DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE agent_sessions DROP COLUMN IF EXISTS tenant_id;

-- Drop tables in dependency order
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS tenants;

-- Note: The update_updated_at_column function is left in place as it may be used by other tables
