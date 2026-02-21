-- Policy definitions for managing governance policies
CREATE TABLE policy_definitions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    category VARCHAR(100) NOT NULL CHECK (category IN ('security', 'governance', 'time_based', 'financial', 'performance', 'custom')),
    enabled BOOLEAN DEFAULT true,
    priority INTEGER DEFAULT 100,

    -- Policy configuration
    policy_type VARCHAR(50) NOT NULL CHECK (policy_type IN ('rego', 'builtin', 'custom')),
    rego_code TEXT,  -- For Rego policies
    builtin_name VARCHAR(255),  -- For built-in policies like 'deny_sensitive_files', 'blast_radius_limit'
    config JSONB DEFAULT '{}',  -- Configuration parameters

    -- Trust level requirements
    min_trust_level INTEGER DEFAULT 1 CHECK (min_trust_level >= 1 AND min_trust_level <= 5),

    -- Metadata
    tags TEXT[] DEFAULT '{}',
    metadata JSONB DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,

    -- Unique constraint on name within organization
    CONSTRAINT unique_policy_name_per_org UNIQUE (organization_id, name)
);

-- Policy statistics cache (updated periodically)
CREATE TABLE policy_stats_cache (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    policy_id UUID NOT NULL REFERENCES policy_definitions(id) ON DELETE CASCADE,
    period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    period_end TIMESTAMP WITH TIME ZONE NOT NULL,
    trigger_count BIGINT DEFAULT 0,
    allowed_count BIGINT DEFAULT 0,
    denied_count BIGINT DEFAULT 0,
    avg_evaluation_ms DECIMAL(10, 2),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_policy_stats_period UNIQUE (policy_id, period_start)
);

-- Indexes for policy definitions
CREATE INDEX IF NOT EXISTS idx_policy_defs_org ON policy_definitions(organization_id);
CREATE INDEX IF NOT EXISTS idx_policy_defs_category ON policy_definitions(category);
CREATE INDEX IF NOT EXISTS idx_policy_defs_enabled ON policy_definitions(enabled);
CREATE INDEX IF NOT EXISTS idx_policy_defs_type ON policy_definitions(policy_type);
CREATE INDEX IF NOT EXISTS idx_policy_defs_tags ON policy_definitions USING GIN(tags);

-- Indexes for stats cache
CREATE INDEX IF NOT EXISTS idx_policy_stats_policy ON policy_stats_cache(policy_id);
CREATE INDEX IF NOT EXISTS idx_policy_stats_period ON policy_stats_cache(period_start);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_policy_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update updated_at
DROP TRIGGER IF EXISTS policy_definitions_updated_at ON policy_definitions;
CREATE TRIGGER policy_definitions_updated_at
    BEFORE UPDATE ON policy_definitions
    FOR EACH ROW
    EXECUTE FUNCTION update_policy_updated_at();

-- Insert default built-in policies (these are templates that can be enabled/configured)
INSERT INTO policy_definitions (name, description, category, policy_type, builtin_name, enabled, priority, config) VALUES
    ('Deny Sensitive Files', 'Blocks access to .env, .pem, .key, and other sensitive files', 'security', 'builtin', 'deny_sensitive_files', true, 10, '{"patterns": [".env", ".pem", ".key", "*.secret", "credentials.*"]}'),
    ('Blast Radius Limit', 'Limits the number of files that can be modified in a single commit', 'governance', 'builtin', 'blast_radius_limit', true, 20, '{"max_files": 10, "trust_level_override": 4}'),
    ('Off-Hours Production', 'Blocks production deployments between 10PM and 6AM', 'time_based', 'builtin', 'off_hours_production', true, 30, '{"start_hour": 22, "end_hour": 6, "min_trust_level": 5}'),
    ('Cost Limit', 'Restricts resource provisioning based on estimated monthly cost', 'financial', 'builtin', 'cost_limit', true, 40, '{"limits_by_trust": {"1": 10, "2": 50, "3": 200, "4": 1000, "5": 10000}}'),
    ('Require Approval for Migrations', 'Escalates database migration changes to human approval', 'governance', 'builtin', 'require_migration_approval', true, 50, '{"action_types": ["migrate_database", "alter_schema"]}'),
    ('Rate Limit API Calls', 'Limits the number of API calls an agent can make per minute', 'performance', 'builtin', 'rate_limit_api', false, 60, '{"requests_per_minute": 60, "burst_size": 10}')
ON CONFLICT DO NOTHING;
