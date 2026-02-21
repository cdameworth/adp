package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adp/adp/internal/api"
	"github.com/adp/adp/internal/api/handlers"
	"github.com/adp/adp/internal/api/middleware"
	"github.com/adp/adp/internal/config"
	"github.com/adp/adp/internal/domain/auth"
	"github.com/adp/adp/internal/domain/governance"
	"github.com/adp/adp/internal/domain/user"
	"github.com/adp/adp/internal/infrastructure/database"
)

func main() {
	// Check for SQLite mode first (zero-config, no config.yaml needed)
	storeMode := os.Getenv("ADP_STORE")
	if storeMode == "sqlite" {
		runSQLiteMode()
		return
	}

	// PostgreSQL mode (default): full stack with config.yaml
	runPostgresMode()
}

// runSQLiteMode starts the API server backed by SQLite.
// Zero-config: reads from the same ~/.adp/adp.db that adp-mcp writes to.
// No PostgreSQL, Neo4j, ClickHouse, or auth required.
func runSQLiteMode() {
	// Setup logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if os.Getenv("ADP_LOG_LEVEL") == "debug" {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
	slog.SetDefault(logger)

	// Initialize SQLite
	dbPath := os.Getenv("ADP_SQLITE_PATH")
	if dbPath == "" {
		dbPath = database.DefaultSQLitePath()
	}

	sqliteClient, err := database.NewSQLiteClient(database.SQLiteConfig{Path: dbPath})
	if err != nil {
		log.Fatalf("Failed to initialize SQLite at %s: %v", dbPath, err)
	}
	defer sqliteClient.Close()

	logger.Info("API server using SQLite store", "path", dbPath)

	// Create SQLite stores
	sessionStore := database.NewSQLiteSessionStore(sqliteClient)
	escalationStore := database.NewSQLiteEscalationStore(sqliteClient)
	decisionStore := database.NewSQLiteDecisionStore(sqliteClient)
	commitStore := database.NewSQLiteCommitStore(sqliteClient)

	// Create SQLite-backed handlers
	sessionHandler := handlers.NewSQLiteSessionHandler(sessionStore)
	governanceHandler := handlers.NewSQLiteGovernanceHandler(escalationStore)
	auditHandler := handlers.NewSQLiteAuditHandler(decisionStore, commitStore)
	serviceHandler := handlers.NewSQLiteServiceHandler()
	policyHandler := handlers.NewSQLitePolicyHandler()
	reportHandler := handlers.NewSQLiteReportHandler(sessionStore, decisionStore, commitStore, escalationStore)

	// Create user store and auth handlers
	userStore := database.NewSQLiteUserStore(sqliteClient)
	jwtSecret := os.Getenv("ADP_JWT_SECRET")
	openReg := os.Getenv("ADP_OPEN_REGISTRATION") == "true"
	var authHandler *handlers.AuthHandlerImpl
	var adminHandler *handlers.AdminHandlerImpl
	var tokenService *user.TokenService
	if jwtSecret != "" {
		tokenService = user.NewTokenService(jwtSecret, "adp", 15*time.Minute, 7*24*time.Hour)
		authHandler = handlers.NewAuthHandler(userStore, tokenService, openReg)
		adminHandler = handlers.NewAdminHandler(userStore)
		logger.Info("User authentication enabled (local JWT)")
	} else {
		logger.Warn("No ADP_JWT_SECRET set — user authentication disabled")
	}

	// Configure authentication middleware
	var authMw api.Middleware
	apiKey := os.Getenv("ADP_API_KEY")
	if jwtSecret != "" || apiKey != "" {
		authMw = middleware.NewCombinedAuthMiddleware(middleware.CombinedAuthConfig{
			APIKey:         apiKey,
			LocalJWTSecret: jwtSecret,
		})
		logger.Info("Authentication middleware enabled", "api_key", apiKey != "", "local_jwt", jwtSecret != "")
	} else {
		logger.Warn("No ADP_API_KEY or ADP_JWT_SECRET set — API endpoints are unauthenticated")
	}

	// Configure rate limiter
	rateLimiter := middleware.NewRateLimiter(middleware.DefaultRateLimitConfig())
	defer rateLimiter.Stop()

	// Configure request validator
	validator := middleware.NewRequestValidator(middleware.DefaultRequestLimits())

	// Parse CORS allowed origins
	var corsOrigins []string
	if origins := os.Getenv("ADP_CORS_ALLOWED_ORIGINS"); origins != "" {
		corsOrigins = strings.Split(origins, ",")
	}

	// Build router with all available handlers for SQLite mode
	router := api.NewRouter(api.RouterConfig{
		SessionHandler:     sessionHandler,
		GovernanceHandler:  governanceHandler,
		AuditHandler:       auditHandler,
		ServiceHandler:     serviceHandler,
		PolicyHandler:      policyHandler,
		ReportsHandler:     reportHandler,
		AuthHandler:        authHandler,
		AdminHandler:       adminHandler,
		AuthMiddleware:     authMw,
		RateLimiter:        rateLimiter.Middleware,
		Validator:          validator.ValidateMiddleware,
		CORSAllowedOrigins: corsOrigins,
		ReadinessCheck: func() map[string]string {
			if err := sqliteClient.Ping(context.Background()); err != nil {
				return map[string]string{"database": "error"}
			}
			return map[string]string{"database": "ok"}
		},
	})

	port := os.Getenv("ADP_SERVER_PORT")
	if port == "" {
		port = os.Getenv("PORT") // Railway injects PORT
	}
	if port == "" {
		port = "8080"
	}

	logger.Info("Starting ADP server (SQLite mode)", "port", port,
		"features", "sessions, decisions, approvals, commits, services, policies, reports")
	if err := http.ListenAndServe(":"+port, router); err != nil {
		logger.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}

// runPostgresMode starts the API server with the full PostgreSQL stack.
// Requires config.yaml or environment variables for database configuration.
func runPostgresMode() {
	// Load configuration
	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup structured logging
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	if cfg.Log.Level == "debug" {
		opts.Level = slog.LevelDebug
	}

	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Initialize Neo4j (optional — not required for core API endpoints)
	neo4jStore, err := database.NewNeo4jStore(cfg.Database.Neo4j.URI, cfg.Database.Neo4j.Username, cfg.Database.Neo4j.Password)
	if err != nil {
		logger.Warn("Failed to initialize Neo4j, lineage features will be unavailable", "error", err)
	} else {
		defer neo4jStore.Close(context.Background())
		logger.Info("Neo4j initialized for lineage tracking")
	}

	// Initialize PostgreSQL
	pgConfig := database.PostgresConfig{
		Host:         cfg.Database.Postgres.Host,
		Port:         cfg.Database.Postgres.Port,
		Database:     cfg.Database.Postgres.Database,
		Username:     cfg.Database.Postgres.Username,
		Password:     cfg.Database.Postgres.Password,
		SSLMode:      cfg.Database.Postgres.SSLMode,
		MaxOpenConns: cfg.Database.Postgres.MaxOpenConns,
		MaxIdleConns: cfg.Database.Postgres.MaxIdleConns,
		MaxLifetime:  cfg.Database.Postgres.MaxLifetime,
		DatabaseURL:  cfg.Database.Postgres.DatabaseURL,
	}
	// Set defaults if not configured
	if pgConfig.SSLMode == "" {
		pgConfig.SSLMode = "disable"
	}
	if pgConfig.MaxOpenConns == 0 {
		pgConfig.MaxOpenConns = 25
	}
	if pgConfig.MaxIdleConns == 0 {
		pgConfig.MaxIdleConns = 5
	}
	if pgConfig.MaxLifetime == 0 {
		pgConfig.MaxLifetime = 5 * time.Minute
	}

	pgClient, err := database.NewPostgresClient(pgConfig)
	if err != nil {
		logger.Error("Failed to initialize PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pgClient.Close()

	// Run PostgreSQL migrations
	migrationsDir := os.Getenv("ADP_MIGRATIONS_DIR")
	if migrationsDir == "" {
		// Default: check bundled path first (Docker), then local
		if _, err := os.Stat("/etc/adp/migrations/postgres"); err == nil {
			migrationsDir = "/etc/adp/migrations/postgres"
		} else {
			migrationsDir = "migrations/postgres"
		}
	}
	if err := pgClient.RunMigrations(context.Background(), migrationsDir); err != nil {
		logger.Error("Failed to run PostgreSQL migrations", "error", err, "dir", migrationsDir)
		os.Exit(1)
	}
	logger.Info("PostgreSQL migrations applied", "dir", migrationsDir)

	// Initialize ClickHouse for reporting
	chConfig := &database.ClickHouseConfig{
		Host:     cfg.Database.ClickHouse.Host,
		Port:     cfg.Database.ClickHouse.Port,
		Database: cfg.Database.ClickHouse.Database,
		Username: cfg.Database.ClickHouse.Username,
		Password: cfg.Database.ClickHouse.Password,
	}
	// Set defaults if not configured
	if chConfig.Host == "" {
		chConfig.Host = "localhost"
	}
	if chConfig.Port == 0 {
		chConfig.Port = 9000
	}
	if chConfig.Database == "" {
		chConfig.Database = "adp"
	}
	if chConfig.Username == "" {
		chConfig.Username = "default"
	}

	var reportingStore *database.ReportingStore
	chClient, err := database.NewClickHouseClient(chConfig)
	if err != nil {
		logger.Warn("Failed to initialize ClickHouse, reports will be unavailable", "error", err)
	} else {
		defer chClient.Close()
		reportingStore = database.NewReportingStore(chClient)
		logger.Info("ClickHouse initialized for reporting")
	}

	// Initialize stores
	sessionStore := database.NewSessionStore(pgClient)
	serviceStore := database.NewServiceStore(pgClient)
	escalationStore := database.NewEscalationStore(pgClient)
	decisionStore := database.NewDecisionStore(pgClient)
	commitStore := database.NewCommitStore(pgClient)
	policyDefinitionStore := database.NewPolicyDefinitionStore(pgClient)

	// Initialize unified policy engine with database policies + base Rego policy
	policyStoreAdapter := governance.NewPolicyStoreAdapter(policyDefinitionStore)
	unifiedPolicyEngine := governance.NewUnifiedPolicyEngine(policyStoreAdapter, "policies/default.rego")

	// Initialize handlers
	sessionHandler := handlers.NewSessionHandler(sessionStore)
	serviceHandler := handlers.NewServiceHandler(serviceStore)
	governanceHandler := handlers.NewGovernanceHandler(unifiedPolicyEngine, escalationStore)
	auditHandler := handlers.NewAuditHandler(decisionStore, commitStore)
	policyHandler := handlers.NewPolicyHandler(policyDefinitionStore)

	// Initialize reports handler if ClickHouse is available
	var reportHandler *handlers.ReportHandler
	if reportingStore != nil {
		reportHandler = handlers.NewReportHandler(reportingStore)
	}

	// Initialize user store and auth handlers
	pgUserStore := database.NewPgUserStore(pgClient)
	var pgAuthHandler *handlers.AuthHandlerImpl
	var pgAdminHandler *handlers.AdminHandlerImpl
	var pgTokenService *user.TokenService
	if cfg.Auth.JWTSecret != "" {
		pgTokenService = user.NewTokenService(cfg.Auth.JWTSecret, "adp", 15*time.Minute, 7*24*time.Hour)
		pgAuthHandler = handlers.NewAuthHandler(pgUserStore, pgTokenService, cfg.Auth.OpenRegistration)
		pgAdminHandler = handlers.NewAdminHandler(pgUserStore)
		logger.Info("User authentication enabled (local JWT)")
	}

	// Initialize SQL-based Authorizer (replaces Neo4j).
	// Available for per-route RBAC enforcement via middleware.RequirePermission(authorizer, action, resource).
	// Currently, admin endpoints enforce RBAC internally via requireAdmin() in handlers.
	_ = auth.NewSQLAuthorizer(pgUserStore)

	// Configure authentication middleware
	var pgAuthMw api.Middleware
	if cfg.Auth.JWKSURL != "" {
		// JWT + optional API key combined auth
		jwtMw, err := middleware.NewAuthMiddleware(middleware.AuthConfig{
			JWKSURL:              cfg.Auth.JWKSURL,
			Issuer:               cfg.Auth.Issuer,
			Audience:             cfg.Auth.Audience,
			RequireExpiration:    cfg.Auth.RequireExpiration,
			ClockSkew:            cfg.Auth.ClockSkew,
			CacheRefreshInterval: cfg.Auth.CacheRefreshInterval,
		})
		if err != nil {
			logger.Error("Failed to initialize JWT auth", "error", err)
			os.Exit(1)
		}
		pgAuthMw = middleware.NewCombinedAuthMiddleware(middleware.CombinedAuthConfig{
			JWTMiddleware:  jwtMw,
			APIKey:         cfg.Auth.APIKey,
			LocalJWTSecret: cfg.Auth.JWTSecret,
		})
		logger.Info("Combined JWT + API key authentication enabled")
	} else if cfg.Auth.APIKey != "" || cfg.Auth.JWTSecret != "" {
		pgAuthMw = middleware.NewCombinedAuthMiddleware(middleware.CombinedAuthConfig{
			APIKey:         cfg.Auth.APIKey,
			LocalJWTSecret: cfg.Auth.JWTSecret,
		})
		logger.Info("Authentication enabled", "api_key", cfg.Auth.APIKey != "", "local_jwt", cfg.Auth.JWTSecret != "")
	} else if cfg.Environment == "production" {
		logger.Error("No authentication configured in production mode")
		os.Exit(1)
	} else {
		logger.Warn("No authentication configured — API endpoints are unauthenticated")
	}

	// Configure rate limiter
	pgRateLimiter := middleware.NewRateLimiter(middleware.DefaultRateLimitConfig())
	defer pgRateLimiter.Stop()

	// Configure request validator
	pgValidator := middleware.NewRequestValidator(middleware.DefaultRequestLimits())

	// Initialize Router with handlers
	router := api.NewRouter(api.RouterConfig{
		SessionHandler:     sessionHandler,
		ServiceHandler:     serviceHandler,
		GovernanceHandler:  governanceHandler,
		AuditHandler:       auditHandler,
		ReportsHandler:     reportHandler,
		PolicyHandler:      policyHandler,
		AuthHandler:        pgAuthHandler,
		AdminHandler:       pgAdminHandler,
		AuthMiddleware:     pgAuthMw,
		RateLimiter:        pgRateLimiter.Middleware,
		Validator:          pgValidator.ValidateMiddleware,
		CORSAllowedOrigins: cfg.Server.CORSAllowedOrigins,
		ReadinessCheck: func() map[string]string {
			if err := pgClient.Ping(context.Background()); err != nil {
				return map[string]string{"database": "error"}
			}
			return map[string]string{"database": "ok"}
		},
	})

	// Initialize SAML if enabled
	if cfg.Auth.SAML.Enabled {
		samlMiddleware, err := handlers.NewSAMLMiddleware(handlers.SAMLConfig{
			RootURL:     cfg.Auth.SAML.RootURL,
			IdPMetadata: cfg.Auth.SAML.IdPMetadata,
			CertFile:    cfg.Auth.SAML.CertFile,
			KeyFile:     cfg.Auth.SAML.KeyFile,
		})
		if err != nil {
			logger.Error("Failed to initialize SAML", "error", err)
			os.Exit(1)
		}
		logger.Info("SAML authentication enabled")
		_ = samlMiddleware
	}

	port := os.Getenv("PORT") // Railway injects PORT
	if port == "" {
		port = fmt.Sprintf("%d", cfg.Server.Port)
	}
	logger.Info("Starting ADP server", "port", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		logger.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}
