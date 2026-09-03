package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteSchema is the DDL applied when a new database is created.
// Kept inline because go:embed cannot traverse parent directories.
// The canonical copy lives at migrations/sqlite/000001_init_schema.sql.
const sqliteSchema = `
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
    metadata TEXT DEFAULT '{}',
    token_hash TEXT DEFAULT ''
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

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
`

// SQLiteConfig holds SQLite connection configuration.
type SQLiteConfig struct {
	Path string // Database file path. Use ":memory:" for in-memory.
}

// SQLiteClient provides access to a SQLite database.
type SQLiteClient struct {
	db     *sql.DB
	config SQLiteConfig

	mu          sync.RWMutex
	healthy     bool
	lastChecked time.Time
}

// NewSQLiteClient creates a new SQLite client, ensures the directory exists,
// enables WAL mode, and runs migrations.
func NewSQLiteClient(cfg SQLiteConfig) (*SQLiteClient, error) {
	if cfg.Path == "" {
		cfg.Path = DefaultSQLitePath()
	}

	// Ensure parent directory exists for file-based databases
	if cfg.Path != ":memory:" {
		dir := filepath.Dir(cfg.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	dsn := cfg.Path
	if cfg.Path != ":memory:" {
		dsn = cfg.Path + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	} else {
		dsn = ":memory:?_foreign_keys=ON"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// SQLite performs best with limited concurrency
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	client := &SQLiteClient{
		db:     db,
		config: cfg,
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// Run migrations
	if err := client.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	client.healthy = true
	client.lastChecked = time.Now()

	return client, nil
}

// migrate runs the embedded schema migration.
func (c *SQLiteClient) migrate(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, sqliteSchema)
	return err
}

// DB returns the underlying database connection.
func (c *SQLiteClient) DB() *sql.DB {
	return c.db
}

// Ping verifies the database connection is alive.
func (c *SQLiteClient) Ping(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		c.setHealthy(false)
		return err
	}
	c.setHealthy(true)
	return nil
}

// Close closes the database connection.
func (c *SQLiteClient) Close() error {
	return c.db.Close()
}

// IsHealthy returns the current health status.
func (c *SQLiteClient) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

func (c *SQLiteClient) setHealthy(healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthy = healthy
	c.lastChecked = time.Now()
}

// HealthCheck performs a comprehensive health check.
func (c *SQLiteClient) HealthCheck(ctx context.Context) HealthStatus {
	start := time.Now()
	status := HealthStatus{
		Component:   "sqlite",
		LastChecked: time.Now(),
	}

	if err := c.Ping(ctx); err != nil {
		status.Healthy = false
		status.Message = fmt.Sprintf("ping failed: %v", err)
		status.Latency = time.Since(start)
		return status
	}

	status.Healthy = true
	status.Message = "healthy"
	status.Latency = time.Since(start)
	status.Details = map[string]interface{}{
		"path": c.config.Path,
	}
	return status
}

// Transaction executes a function within a database transaction.
func (c *SQLiteClient) Transaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin failed: %v", ErrTransactionFailed, err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w: %v (rollback error: %v)", ErrTransactionFailed, err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit failed: %v", ErrTransactionFailed, err)
	}

	return nil
}

// DefaultSQLitePath returns the default database path (~/.adp/adp.db).
func DefaultSQLitePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "adp.db"
	}
	return filepath.Join(home, ".adp", "adp.db")
}
