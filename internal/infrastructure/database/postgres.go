// Package database provides database client implementations for ADP.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// Common errors
var (
	ErrNotFound          = errors.New("record not found")
	ErrDuplicateKey      = errors.New("duplicate key violation")
	ErrInvalidInput      = errors.New("invalid input")
	ErrAlreadyExists     = errors.New("already exists")
	ErrConnectionFailed  = errors.New("database connection failed")
	ErrTransactionFailed = errors.New("transaction failed")
)

// PostgresConfig holds PostgreSQL connection configuration.
type PostgresConfig struct {
	Host         string
	Port         int
	Database     string
	Username     string
	Password     string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
	// DatabaseURL is a full connection string (e.g. from Railway's DATABASE_URL).
	// When set, it takes precedence over individual host/port/user/password fields.
	DatabaseURL string
}

// DSN returns the PostgreSQL connection string.
func (c PostgresConfig) DSN() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.Username, c.Password, c.Database, c.SSLMode,
	)
}

// PostgresClient provides access to PostgreSQL database operations.
type PostgresClient struct {
	db     *sql.DB
	config PostgresConfig

	// Health check state
	mu          sync.RWMutex
	healthy     bool
	lastChecked time.Time
}

// NewPostgresClient creates a new PostgreSQL client with connection pooling.
func NewPostgresClient(cfg PostgresConfig) (*PostgresClient, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxLifetime)

	client := &PostgresClient{
		db:      db,
		config:  cfg,
		healthy: false,
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	client.healthy = true
	client.lastChecked = time.Now()

	return client, nil
}

// DB returns the underlying database connection for advanced operations.
func (c *PostgresClient) DB() *sql.DB {
	return c.db
}

// Ping verifies the database connection is alive.
func (c *PostgresClient) Ping(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		c.setHealthy(false)
		return err
	}
	c.setHealthy(true)
	return nil
}

// Close closes the database connection pool.
func (c *PostgresClient) Close() error {
	return c.db.Close()
}

// IsHealthy returns the current health status.
func (c *PostgresClient) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// setHealthy updates the health status thread-safely.
func (c *PostgresClient) setHealthy(healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthy = healthy
	c.lastChecked = time.Now()
}

// HealthCheck performs a comprehensive health check.
func (c *PostgresClient) HealthCheck(ctx context.Context) HealthStatus {
	start := time.Now()

	status := HealthStatus{
		Component:   "postgres",
		LastChecked: time.Now(),
	}

	// Check basic connectivity
	if err := c.Ping(ctx); err != nil {
		status.Healthy = false
		status.Message = fmt.Sprintf("ping failed: %v", err)
		status.Latency = time.Since(start)
		return status
	}

	// Check connection pool stats
	stats := c.db.Stats()
	status.Details = map[string]interface{}{
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration.String(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
		"max_open_connections": c.config.MaxOpenConns,
	}

	// Warn if connection pool is stressed
	utilizationPct := float64(stats.InUse) / float64(c.config.MaxOpenConns) * 100
	if utilizationPct > 80 {
		status.Message = fmt.Sprintf("high connection pool utilization: %.1f%%", utilizationPct)
	} else {
		status.Message = "healthy"
	}

	status.Healthy = true
	status.Latency = time.Since(start)

	return status
}

// HealthStatus represents the health check result for a component.
type HealthStatus struct {
	Component   string                 `json:"component"`
	Healthy     bool                   `json:"healthy"`
	Message     string                 `json:"message,omitempty"`
	Latency     time.Duration          `json:"latency"`
	LastChecked time.Time              `json:"last_checked"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// Transaction executes a function within a database transaction.
func (c *PostgresClient) Transaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
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

// RunMigrations executes pending PostgreSQL migration files from the given directory.
// It tracks applied migrations in a schema_migrations table and applies only new ones.
func (c *PostgresClient) RunMigrations(ctx context.Context, migrationsDir string) error {
	// Create tracking table
	_, err := c.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Read migration files
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory %s: %w", migrationsDir, err)
	}

	// Collect and sort .up.sql files
	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, filename := range upFiles {
		// Check if already applied
		version := strings.TrimSuffix(filename, ".up.sql")
		var count int
		err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = $1", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute
		path := filepath.Join(migrationsDir, filename)
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		if _, err := c.db.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", filename, err)
		}

		// Record as applied
		if _, err := c.db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}
	}

	return nil
}

// QueryLogger wraps a database query with logging capabilities.
type QueryLogger struct {
	client  *PostgresClient
	enabled bool
	logger  func(query string, args []interface{}, duration time.Duration, err error)
}

// NewQueryLogger creates a new query logger.
func NewQueryLogger(client *PostgresClient, logger func(query string, args []interface{}, duration time.Duration, err error)) *QueryLogger {
	return &QueryLogger{
		client:  client,
		enabled: true,
		logger:  logger,
	}
}

// SetEnabled enables or disables query logging.
func (l *QueryLogger) SetEnabled(enabled bool) {
	l.enabled = enabled
}

// Query executes a query and logs it.
func (l *QueryLogger) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	rows, err := l.client.db.QueryContext(ctx, query, args...)
	if l.enabled && l.logger != nil {
		l.logger(query, args, time.Since(start), err)
	}
	return rows, err
}

// QueryRow executes a query returning a single row and logs it.
func (l *QueryLogger) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	start := time.Now()
	row := l.client.db.QueryRowContext(ctx, query, args...)
	if l.enabled && l.logger != nil {
		l.logger(query, args, time.Since(start), nil)
	}
	return row
}

// Exec executes a query without returning rows and logs it.
func (l *QueryLogger) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	result, err := l.client.db.ExecContext(ctx, query, args...)
	if l.enabled && l.logger != nil {
		l.logger(query, args, time.Since(start), err)
	}
	return result, err
}
