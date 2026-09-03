-- Documentation store for PG mode (#11). Mirrors the SQLite documentation
-- table so /v1/docs works identically against PostgreSQL.
CREATE TABLE IF NOT EXISTS documentation (
    id          VARCHAR(64) PRIMARY KEY,
    session_id  VARCHAR(64),
    category    VARCHAR(100) NOT NULL,
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docs_category ON documentation(category);
CREATE INDEX IF NOT EXISTS idx_docs_session ON documentation(session_id);
