-- Behavioral verification (#20): attested build/test evidence from
-- independent runners (CI), one record per commit SHA, plus the per-repo
-- verification keys that authenticate attestation ingest.
CREATE TABLE IF NOT EXISTS verifications (
    id              VARCHAR(64) PRIMARY KEY,
    commit_sha      VARCHAR(64) NOT NULL UNIQUE,
    session_id      VARCHAR(64),
    status          VARCHAR(10) NOT NULL CHECK (status IN ('passed', 'failed')),
    pipeline_url    TEXT DEFAULT '',
    runner_identity TEXT DEFAULT '',
    evidence_hash   VARCHAR(64) NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL,
    received_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_verifications_session ON verifications(session_id);

CREATE TABLE IF NOT EXISTS verification_keys (
    id          VARCHAR(64) PRIMARY KEY,
    repo        TEXT NOT NULL,
    key_hash    VARCHAR(64) NOT NULL,
    created_by  TEXT DEFAULT '',
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    revoked_at  TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_verification_keys_repo ON verification_keys(repo);
