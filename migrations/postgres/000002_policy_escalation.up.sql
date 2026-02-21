-- Policy evaluations for audit and metrics
CREATE TABLE policy_evaluations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(64) NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    policy_name VARCHAR(255) NOT NULL,
    policy_version VARCHAR(50),
    input JSONB NOT NULL,
    result JSONB NOT NULL,
    allowed BOOLEAN NOT NULL,
    denied_reasons TEXT[],
    duration_ms INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Escalation requests for human approval workflow
CREATE TABLE escalation_requests (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(64) NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    decision_id UUID REFERENCES decision_records(id) ON DELETE SET NULL,
    action VARCHAR(255) NOT NULL,
    action_type VARCHAR(100) NOT NULL,
    target JSONB NOT NULL,
    reason TEXT NOT NULL,
    policy_names TEXT[] DEFAULT '{}',
    context_summary JSONB DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    priority VARCHAR(20) DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'critical')),
    approver_id UUID,
    approver_comment TEXT,
    requested_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE,
    resolved_at TIMESTAMP WITH TIME ZONE
);

-- Commit records for tracking agent commits
CREATE TABLE commit_records (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(64) NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    commit_sha VARCHAR(64),
    commit_token VARCHAR(255) UNIQUE NOT NULL,
    files TEXT[] NOT NULL,
    message TEXT,
    status VARCHAR(50) DEFAULT 'prepared' CHECK (status IN ('prepared', 'committed', 'verified', 'rejected')),
    approved BOOLEAN DEFAULT false,
    approval_reason TEXT,
    prepared_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    committed_at TIMESTAMP WITH TIME ZONE,
    verified_at TIMESTAMP WITH TIME ZONE
);

-- Add metadata column to sessions if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'agent_sessions' AND column_name = 'metadata'
    ) THEN
        ALTER TABLE agent_sessions ADD COLUMN metadata JSONB DEFAULT '{}';
    END IF;
END $$;

-- Add last_heartbeat column to sessions if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'agent_sessions' AND column_name = 'last_heartbeat'
    ) THEN
        ALTER TABLE agent_sessions ADD COLUMN last_heartbeat TIMESTAMP WITH TIME ZONE;
    END IF;
END $$;

-- Add created_at and updated_at to services if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'services' AND column_name = 'created_at'
    ) THEN
        ALTER TABLE services ADD COLUMN created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'services' AND column_name = 'updated_at'
    ) THEN
        ALTER TABLE services ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
    END IF;
END $$;

-- Create indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sessions_org ON agent_sessions(organization_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON agent_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON agent_sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_tool ON agent_sessions(tool);

CREATE INDEX IF NOT EXISTS idx_decisions_session ON decision_records(session_id);
CREATE INDEX IF NOT EXISTS idx_decisions_type ON decision_records(decision_type);
CREATE INDEX IF NOT EXISTS idx_decisions_status ON decision_records(status);
CREATE INDEX IF NOT EXISTS idx_decisions_created ON decision_records(created_at);

CREATE INDEX IF NOT EXISTS idx_policy_eval_session ON policy_evaluations(session_id);
CREATE INDEX IF NOT EXISTS idx_policy_eval_name ON policy_evaluations(policy_name);
CREATE INDEX IF NOT EXISTS idx_policy_eval_allowed ON policy_evaluations(allowed);
CREATE INDEX IF NOT EXISTS idx_policy_eval_created ON policy_evaluations(created_at);

CREATE INDEX IF NOT EXISTS idx_escalations_session ON escalation_requests(session_id);
CREATE INDEX IF NOT EXISTS idx_escalations_status ON escalation_requests(status);
CREATE INDEX IF NOT EXISTS idx_escalations_priority ON escalation_requests(priority);
CREATE INDEX IF NOT EXISTS idx_escalations_requested ON escalation_requests(requested_at);

CREATE INDEX IF NOT EXISTS idx_commits_session ON commit_records(session_id);
CREATE INDEX IF NOT EXISTS idx_commits_sha ON commit_records(commit_sha);
CREATE INDEX IF NOT EXISTS idx_commits_status ON commit_records(status);

CREATE INDEX IF NOT EXISTS idx_services_org ON services(organization_id);
CREATE INDEX IF NOT EXISTS idx_services_name ON services(organization_id, name);
CREATE INDEX IF NOT EXISTS idx_services_tier ON services(tier);
