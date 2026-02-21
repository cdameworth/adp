-- Rollback policy definitions
DROP TRIGGER IF EXISTS policy_definitions_updated_at ON policy_definitions;
DROP FUNCTION IF EXISTS update_policy_updated_at();
DROP TABLE IF EXISTS policy_stats_cache;
DROP TABLE IF EXISTS policy_definitions;
