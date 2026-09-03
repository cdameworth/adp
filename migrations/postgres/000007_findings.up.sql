-- Reconciliation findings persistence (#10). The enforcement backstop's
-- findings previously lived in an in-memory store and vanished on restart.
CREATE TABLE IF NOT EXISTS findings (
    id          VARCHAR(64) PRIMARY KEY,
    type        VARCHAR(50) NOT NULL,
    reference   VARCHAR(255) NOT NULL,
    repo        TEXT DEFAULT '',
    ref         TEXT DEFAULT '',
    author      TEXT DEFAULT '',
    reason      TEXT DEFAULT '',
    status      VARCHAR(20) NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'acknowledged', 'resolved')),
    detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (type, reference)
);

CREATE INDEX IF NOT EXISTS idx_findings_status ON findings(status);
CREATE INDEX IF NOT EXISTS idx_findings_detected ON findings(detected_at DESC);
