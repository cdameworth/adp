-- Drop indexes
DROP INDEX IF EXISTS idx_services_tier;
DROP INDEX IF EXISTS idx_services_name;
DROP INDEX IF EXISTS idx_services_org;

DROP INDEX IF EXISTS idx_commits_status;
DROP INDEX IF EXISTS idx_commits_sha;
DROP INDEX IF EXISTS idx_commits_session;

DROP INDEX IF EXISTS idx_escalations_requested;
DROP INDEX IF EXISTS idx_escalations_priority;
DROP INDEX IF EXISTS idx_escalations_status;
DROP INDEX IF EXISTS idx_escalations_session;

DROP INDEX IF EXISTS idx_policy_eval_created;
DROP INDEX IF EXISTS idx_policy_eval_allowed;
DROP INDEX IF EXISTS idx_policy_eval_name;
DROP INDEX IF EXISTS idx_policy_eval_session;

DROP INDEX IF EXISTS idx_decisions_created;
DROP INDEX IF EXISTS idx_decisions_status;
DROP INDEX IF EXISTS idx_decisions_type;
DROP INDEX IF EXISTS idx_decisions_session;

DROP INDEX IF EXISTS idx_sessions_tool;
DROP INDEX IF EXISTS idx_sessions_status;
DROP INDEX IF EXISTS idx_sessions_user;
DROP INDEX IF EXISTS idx_sessions_org;

-- Drop tables
DROP TABLE IF EXISTS commit_records;
DROP TABLE IF EXISTS escalation_requests;
DROP TABLE IF EXISTS policy_evaluations;

-- Remove added columns (optional, kept for data preservation)
-- ALTER TABLE agent_sessions DROP COLUMN IF EXISTS metadata;
-- ALTER TABLE agent_sessions DROP COLUMN IF EXISTS last_heartbeat;
-- ALTER TABLE services DROP COLUMN IF EXISTS created_at;
-- ALTER TABLE services DROP COLUMN IF EXISTS updated_at;
