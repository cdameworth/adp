-- ADP SQLite Schema
-- Lightweight persistence for standalone MCP server mode.
-- All JSON fields stored as TEXT, UUIDs as TEXT, timestamps as TEXT (ISO 8601).

CREATE TABLE IF NOT EXISTS agent_sessions (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    trust_level INTEGER NOT NULL CHECK (trust_level BETWEEN 1 AND 5),
    capabilities TEXT DEFAULT '[]',
    constraints TEXT DEFAULT '[]',
    service_scope TEXT DEFAULT '[]',
    status TEXT DEFAULT 'active',
    started_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL,
    last_heartbeat TEXT,
    metadata TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS decision_records (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    decision_type TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '{}',
    reasoning TEXT NOT NULL DEFAULT '{}',
    confidence REAL DEFAULT 0.8,
    alternatives TEXT DEFAULT '[]',
    context_snapshot TEXT DEFAULT '{}',
    policy_result TEXT,
    status TEXT DEFAULT 'pending',
    outcome TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
);

CREATE TABLE IF NOT EXISTS commit_records (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    commit_sha TEXT,
    commit_token TEXT UNIQUE NOT NULL,
    files TEXT NOT NULL DEFAULT '[]',
    message TEXT,
    status TEXT DEFAULT 'prepared',
    approved INTEGER DEFAULT 0,
    approval_reason TEXT,
    prepared_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    committed_at TEXT,
    verified_at TEXT,
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
);

CREATE TABLE IF NOT EXISTS escalation_requests (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    decision_id TEXT,
    action TEXT NOT NULL,
    action_type TEXT NOT NULL DEFAULT 'unknown',
    target TEXT NOT NULL DEFAULT '{}',
    reason TEXT NOT NULL,
    policy_names TEXT DEFAULT '[]',
    context_summary TEXT DEFAULT '{}',
    status TEXT DEFAULT 'pending',
    priority TEXT DEFAULT 'normal',
    approver_id TEXT,
    approver_comment TEXT,
    requested_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT,
    resolved_at TEXT,
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id),
    FOREIGN KEY (decision_id) REFERENCES decision_records(id)
);

CREATE TABLE IF NOT EXISTS documentation (
    id TEXT PRIMARY KEY,
    session_id TEXT,
    category TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
);

-- Reconciliation findings (enforcement backstop, #10)
CREATE TABLE IF NOT EXISTS findings (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    reference TEXT NOT NULL,
    repo TEXT DEFAULT '',
    ref TEXT DEFAULT '',
    author TEXT DEFAULT '',
    reason TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved')),
    detected_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (type, reference)
);

-- Behavioral verification: attested build/test evidence (#20)
CREATE TABLE IF NOT EXISTS verifications (
    id TEXT PRIMARY KEY,
    commit_sha TEXT NOT NULL UNIQUE,
    session_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    pipeline_url TEXT DEFAULT '',
    runner_identity TEXT DEFAULT '',
    evidence_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS verification_keys (
    id TEXT PRIMARY KEY,
    repo TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    created_by TEXT DEFAULT '',
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    revoked_at TEXT
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sessions_status ON agent_sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_org ON agent_sessions(organization_id);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON agent_sessions(started_at);

CREATE INDEX IF NOT EXISTS idx_decisions_session ON decision_records(session_id);
CREATE INDEX IF NOT EXISTS idx_decisions_type ON decision_records(decision_type);
CREATE INDEX IF NOT EXISTS idx_decisions_created ON decision_records(created_at);

CREATE INDEX IF NOT EXISTS idx_commits_session ON commit_records(session_id);
CREATE INDEX IF NOT EXISTS idx_commits_sha ON commit_records(commit_sha);
CREATE INDEX IF NOT EXISTS idx_commits_token ON commit_records(commit_token);
CREATE INDEX IF NOT EXISTS idx_commits_status ON commit_records(status);

CREATE INDEX IF NOT EXISTS idx_escalations_session ON escalation_requests(session_id);
CREATE INDEX IF NOT EXISTS idx_escalations_status ON escalation_requests(status);

CREATE INDEX IF NOT EXISTS idx_docs_category ON documentation(category);
CREATE INDEX IF NOT EXISTS idx_docs_session ON documentation(session_id);

CREATE INDEX IF NOT EXISTS idx_findings_status ON findings(status);
CREATE INDEX IF NOT EXISTS idx_findings_detected ON findings(detected_at);

CREATE INDEX IF NOT EXISTS idx_verifications_session ON verifications(session_id);
CREATE INDEX IF NOT EXISTS idx_verification_keys_repo ON verification_keys(repo);
