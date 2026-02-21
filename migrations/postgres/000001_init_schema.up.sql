CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Agent sessions
CREATE TABLE agent_sessions (
    id VARCHAR(64) PRIMARY KEY,
    organization_id UUID NOT NULL,
    user_id UUID NOT NULL,
    tool VARCHAR(50) NOT NULL,
    trust_level INTEGER NOT NULL CHECK (trust_level BETWEEN 1 AND 5),
    capabilities JSONB DEFAULT '[]',
    constraints JSONB DEFAULT '[]',
    service_scope UUID[] DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'active',
    started_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

-- Decision records
CREATE TABLE decision_records (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(64) NOT NULL,
    decision_type VARCHAR(100) NOT NULL,
    action VARCHAR(255) NOT NULL,
    target JSONB NOT NULL,
    reasoning JSONB NOT NULL,
    confidence DECIMAL(3,2),
    alternatives JSONB DEFAULT '[]',
    context_snapshot JSONB DEFAULT '{}',
    policy_result JSONB,
    status VARCHAR(50) DEFAULT 'pending',
    outcome JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Services catalog
CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    tier VARCHAR(50) DEFAULT 'standard',
    agent_constraints JSONB DEFAULT '[]',
    context_config JSONB DEFAULT '{}',
    escalation_config JSONB DEFAULT '{}',
    spec JSONB,
    human_docs TEXT
);
