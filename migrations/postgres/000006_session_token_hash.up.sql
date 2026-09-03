-- Session token hashes for git-hook/sidecar token validation (#12).
-- Brings the PG store to parity with the SQLite store, which has stored
-- token_hash since the initial schema. SHA-256 hex of the session token;
-- never the token itself.
ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS token_hash TEXT NOT NULL DEFAULT '';
