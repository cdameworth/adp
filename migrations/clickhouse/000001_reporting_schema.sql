-- ClickHouse Reporting Schema Migration
-- Version: 000001
-- Description: Time-series analytics tables for ADP reporting

-- ============================================
-- Agent Activity Events (High-Volume, Append-Only)
-- ============================================

CREATE TABLE IF NOT EXISTS agent_activity_events (
    event_id UUID DEFAULT generateUUIDv4(),
    timestamp DateTime64(3) DEFAULT now64(3),
    organization_id UUID,
    session_id String,
    user_id UUID,
    agent_tool String,  -- 'claude_code', 'cursor', 'copilot', etc.
    event_type Enum8(
        'session_start' = 1,
        'session_end' = 2,
        'context_request' = 3,
        'policy_check' = 4,
        'decision_logged' = 5,
        'commit_prepared' = 6,
        'escalation_requested' = 7,
        'escalation_resolved' = 8,
        'heartbeat' = 9,
        'error' = 10
    ),
    action_type String,
    target_service UUID,
    target_paths Array(String),
    policy_result Enum8('allowed' = 1, 'denied' = 2, 'escalated' = 3, 'na' = 0) DEFAULT 'na',
    policy_names Array(String),
    trust_level UInt8,
    confidence_score Float32 DEFAULT 0,
    tokens_used UInt32 DEFAULT 0,
    latency_ms UInt32 DEFAULT 0,
    error_message String DEFAULT '',
    metadata String DEFAULT '{}'  -- JSON for extensibility
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (organization_id, timestamp, session_id)
TTL timestamp + INTERVAL 90 DAY;

-- ============================================
-- Aggregated Metrics (Hourly Pre-Computation)
-- ============================================

CREATE TABLE IF NOT EXISTS agent_metrics_hourly (
    hour DateTime,
    organization_id UUID,
    agent_tool String,
    service_id UUID,

    -- Volume metrics
    sessions_started UInt32 DEFAULT 0,
    sessions_ended UInt32 DEFAULT 0,
    decisions_logged UInt32 DEFAULT 0,
    commits_prepared UInt32 DEFAULT 0,
    heartbeats UInt32 DEFAULT 0,
    errors UInt32 DEFAULT 0,

    -- Policy metrics
    policy_checks_total UInt32 DEFAULT 0,
    policy_allowed UInt32 DEFAULT 0,
    policy_denied UInt32 DEFAULT 0,
    policy_escalated UInt32 DEFAULT 0,

    -- Escalation metrics
    escalations_requested UInt32 DEFAULT 0,
    escalations_approved UInt32 DEFAULT 0,
    escalations_rejected UInt32 DEFAULT 0,
    escalation_resolution_minutes_sum Float32 DEFAULT 0,
    escalation_resolution_count UInt32 DEFAULT 0,

    -- Context metrics
    context_requests UInt32 DEFAULT 0,
    context_tokens_total UInt64 DEFAULT 0,
    context_latency_ms_sum UInt64 DEFAULT 0,
    context_latency_count UInt32 DEFAULT 0,

    -- Quality metrics
    confidence_score_sum Float32 DEFAULT 0,
    confidence_score_count UInt32 DEFAULT 0,
    decisions_low_confidence UInt32 DEFAULT 0,  -- confidence < 0.7

    -- Trust level distribution
    sessions_trust_level_1 UInt32 DEFAULT 0,
    sessions_trust_level_2 UInt32 DEFAULT 0,
    sessions_trust_level_3 UInt32 DEFAULT 0,
    sessions_trust_level_4 UInt32 DEFAULT 0,
    sessions_trust_level_5 UInt32 DEFAULT 0
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (organization_id, hour, agent_tool, service_id);

-- ============================================
-- Policy Effectiveness Tracking
-- ============================================

CREATE TABLE IF NOT EXISTS policy_effectiveness (
    date Date,
    organization_id UUID,
    policy_name String,

    -- Evaluation counts
    evaluations_total UInt32 DEFAULT 0,
    evaluations_allowed UInt32 DEFAULT 0,
    evaluations_denied UInt32 DEFAULT 0,

    -- Outcome tracking (did denial prevent issues?)
    denied_then_escalated UInt32 DEFAULT 0,
    denied_then_approved UInt32 DEFAULT 0,  -- False positive signal
    denied_then_abandoned UInt32 DEFAULT 0,

    -- Performance
    evaluation_time_ms_sum UInt64 DEFAULT 0,
    evaluation_time_count UInt32 DEFAULT 0,
    evaluation_time_max_ms UInt32 DEFAULT 0
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(date)
ORDER BY (organization_id, date, policy_name);

-- ============================================
-- Session Summary Table
-- ============================================

CREATE TABLE IF NOT EXISTS session_summaries (
    session_id String,
    organization_id UUID,
    user_id UUID,
    agent_tool String,
    trust_level UInt8,

    started_at DateTime64(3),
    ended_at DateTime64(3) DEFAULT now64(3),
    duration_seconds UInt32 DEFAULT 0,

    decisions_count UInt32 DEFAULT 0,
    commits_count UInt32 DEFAULT 0,
    escalations_count UInt32 DEFAULT 0,
    escalations_approved UInt32 DEFAULT 0,

    policy_checks_count UInt32 DEFAULT 0,
    policy_denied_count UInt32 DEFAULT 0,

    avg_confidence Float32 DEFAULT 0,
    min_confidence Float32 DEFAULT 1,

    services_affected Array(UUID),
    files_modified_count UInt32 DEFAULT 0,

    status Enum8('active' = 1, 'completed' = 2, 'expired' = 3, 'error' = 4) DEFAULT 'active'
) ENGINE = ReplacingMergeTree(ended_at)
PARTITION BY toYYYYMM(started_at)
ORDER BY (organization_id, session_id);

-- ============================================
-- Compliance Audit Trail
-- ============================================

CREATE TABLE IF NOT EXISTS compliance_audit (
    timestamp DateTime64(3) DEFAULT now64(3),
    organization_id UUID,
    event_type String,
    session_id String,
    user_id UUID,

    action String,
    target_paths Array(String),
    target_services Array(UUID),

    policy_name String,
    policy_result String,

    commit_sha String DEFAULT '',
    audit_token String DEFAULT '',

    is_sensitive_path UInt8 DEFAULT 0,
    is_off_hours UInt8 DEFAULT 0,
    is_production UInt8 DEFAULT 0,

    metadata String DEFAULT '{}'
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (organization_id, timestamp)
TTL timestamp + INTERVAL 2 YEAR;

-- ============================================
-- Materialized Views for Real-Time Aggregation
-- ============================================

-- Hourly metrics aggregation
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_agent_metrics_hourly
TO agent_metrics_hourly
AS SELECT
    toStartOfHour(timestamp) as hour,
    organization_id,
    agent_tool,
    target_service as service_id,

    countIf(event_type = 'session_start') as sessions_started,
    countIf(event_type = 'session_end') as sessions_ended,
    countIf(event_type = 'decision_logged') as decisions_logged,
    countIf(event_type = 'commit_prepared') as commits_prepared,
    countIf(event_type = 'heartbeat') as heartbeats,
    countIf(event_type = 'error') as errors,

    countIf(event_type = 'policy_check') as policy_checks_total,
    countIf(event_type = 'policy_check' AND policy_result = 'allowed') as policy_allowed,
    countIf(event_type = 'policy_check' AND policy_result = 'denied') as policy_denied,
    countIf(event_type = 'policy_check' AND policy_result = 'escalated') as policy_escalated,

    countIf(event_type = 'escalation_requested') as escalations_requested,
    0 as escalations_approved,
    0 as escalations_rejected,
    0 as escalation_resolution_minutes_sum,
    0 as escalation_resolution_count,

    countIf(event_type = 'context_request') as context_requests,
    sumIf(tokens_used, event_type = 'context_request') as context_tokens_total,
    sumIf(latency_ms, event_type = 'context_request') as context_latency_ms_sum,
    countIf(event_type = 'context_request' AND latency_ms > 0) as context_latency_count,

    sumIf(confidence_score, event_type = 'decision_logged') as confidence_score_sum,
    countIf(event_type = 'decision_logged' AND confidence_score > 0) as confidence_score_count,
    countIf(event_type = 'decision_logged' AND confidence_score < 0.7 AND confidence_score > 0) as decisions_low_confidence,

    countIf(event_type = 'session_start' AND trust_level = 1) as sessions_trust_level_1,
    countIf(event_type = 'session_start' AND trust_level = 2) as sessions_trust_level_2,
    countIf(event_type = 'session_start' AND trust_level = 3) as sessions_trust_level_3,
    countIf(event_type = 'session_start' AND trust_level = 4) as sessions_trust_level_4,
    countIf(event_type = 'session_start' AND trust_level = 5) as sessions_trust_level_5
FROM agent_activity_events
GROUP BY hour, organization_id, agent_tool, service_id;

-- Policy effectiveness aggregation
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_policy_effectiveness
TO policy_effectiveness
AS SELECT
    toDate(timestamp) as date,
    organization_id,
    arrayJoin(policy_names) as policy_name,

    count() as evaluations_total,
    countIf(policy_result = 'allowed') as evaluations_allowed,
    countIf(policy_result = 'denied') as evaluations_denied,

    0 as denied_then_escalated,
    0 as denied_then_approved,
    0 as denied_then_abandoned,

    sum(latency_ms) as evaluation_time_ms_sum,
    count() as evaluation_time_count,
    max(latency_ms) as evaluation_time_max_ms
FROM agent_activity_events
WHERE event_type = 'policy_check' AND length(policy_names) > 0
GROUP BY date, organization_id, policy_name;

-- ============================================
-- Useful Query Views
-- ============================================

-- Active sessions view
CREATE VIEW IF NOT EXISTS v_active_sessions AS
SELECT
    session_id,
    organization_id,
    user_id,
    agent_tool,
    trust_level,
    started_at,
    decisions_count,
    avg_confidence
FROM session_summaries
WHERE status = 'active'
ORDER BY started_at DESC;

-- Daily summary view
CREATE VIEW IF NOT EXISTS v_daily_summary AS
SELECT
    toDate(hour) as date,
    organization_id,
    sum(sessions_started) as total_sessions,
    sum(decisions_logged) as total_decisions,
    sum(commits_prepared) as total_commits,
    sum(policy_checks_total) as total_policy_checks,
    sum(policy_denied) as total_denials,
    sum(escalations_requested) as total_escalations,
    if(sum(confidence_score_count) > 0,
       sum(confidence_score_sum) / sum(confidence_score_count),
       0) as avg_confidence,
    if(sum(context_latency_count) > 0,
       sum(context_latency_ms_sum) / sum(context_latency_count),
       0) as avg_context_latency_ms
FROM agent_metrics_hourly
GROUP BY date, organization_id
ORDER BY date DESC;

-- Policy health view
CREATE VIEW IF NOT EXISTS v_policy_health AS
SELECT
    date,
    organization_id,
    policy_name,
    evaluations_total,
    evaluations_denied,
    if(evaluations_total > 0,
       evaluations_denied / evaluations_total,
       0) as denial_rate,
    if(evaluations_denied > 0,
       denied_then_approved / evaluations_denied,
       0) as false_positive_rate,
    if(evaluation_time_count > 0,
       evaluation_time_ms_sum / evaluation_time_count,
       0) as avg_evaluation_ms
FROM policy_effectiveness
WHERE evaluations_total > 0
ORDER BY date DESC, denial_rate DESC;
