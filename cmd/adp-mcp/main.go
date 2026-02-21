package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ctxengine "github.com/adp/adp/internal/domain/context"
	"github.com/adp/adp/internal/domain/documentation"
	"github.com/adp/adp/internal/domain/governance"
	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/mcp"
)

func main() {
	server := mcp.NewServer()

	// Layer 0: SQLite (default, zero-config)
	storeMode := getEnvOrDefault("ADP_STORE", "sqlite")

	if storeMode == "sqlite" {
		dbPath := getEnvOrDefault("ADP_SQLITE_PATH", database.DefaultSQLitePath())
		sqliteClient, err := database.NewSQLiteClient(database.SQLiteConfig{Path: dbPath})
		if err != nil {
			log.Fatalf("Failed to initialize SQLite store at %s: %v", dbPath, err)
		}
		defer sqliteClient.Close()

		server.SessionStore = database.NewSQLiteSessionStore(sqliteClient)
		server.DecisionStore = database.NewSQLiteDecisionStore(sqliteClient)
		server.CommitStore = database.NewSQLiteCommitStore(sqliteClient)
		server.EscalationStore = database.NewSQLiteEscalationStore(sqliteClient)
		server.DocStore = database.NewSQLiteDocStore(sqliteClient)

		log.Printf("MCP server using SQLite store: %s", dbPath)
	}

	// Layer 1: PostgreSQL (optional, enables policy engine when configured)
	pgPassword := getEnvOrDefault("ADP_DATABASE_POSTGRES_PASSWORD", "")
	if pgPassword != "" || storeMode == "postgres" {
		pgHost := getEnvOrDefault("ADP_DATABASE_POSTGRES_HOST", "localhost")
		pgPort := getEnvOrDefaultInt("ADP_DATABASE_POSTGRES_PORT", 5432)
		pgDatabase := getEnvOrDefault("ADP_DATABASE_POSTGRES_DATABASE", "adp")
		pgUsername := getEnvOrDefault("ADP_DATABASE_POSTGRES_USERNAME", "adp")
		pgSSLMode := getEnvOrDefault("ADP_DATABASE_POSTGRES_SSLMODE", "disable")

		pgConfig := database.PostgresConfig{
			Host:         pgHost,
			Port:         pgPort,
			Database:     pgDatabase,
			Username:     pgUsername,
			Password:     pgPassword,
			SSLMode:      pgSSLMode,
			MaxOpenConns: 5,
			MaxIdleConns: 2,
			MaxLifetime:  5 * time.Minute,
		}

		pgClient, err := database.NewPostgresClient(pgConfig)
		if err != nil {
			log.Printf("Warning: Failed to connect to PostgreSQL: %v", err)
			log.Printf("MCP server will use fallback policy checks (hardcoded rules)")
		} else {
			defer pgClient.Close()

			// Wire policy engine from PostgreSQL
			policyDefinitionStore := database.NewPolicyDefinitionStore(pgClient)
			policyStoreAdapter := governance.NewPolicyStoreAdapter(policyDefinitionStore)

			basePolicyPath := getEnvOrDefault("ADP_POLICY_PATH", "")
			unifiedPolicyEngine := governance.NewUnifiedPolicyEngine(policyStoreAdapter, basePolicyPath)

			engineAdapter := mcp.NewGovernanceEngineAdapter(unifiedPolicyEngine)
			server.UnifiedPolicyEngine = engineAdapter

			log.Println("MCP server connected to policy engine via PostgreSQL")
		}
	}

	// Initialize context engine (works with nil dependencies — returns empty layers
	// until a vector store and embedding provider are configured)
	contextEngine := ctxengine.NewEngine(nil, nil, nil, nil, nil)
	server.ContextEngine = mcp.NewContextEngineAdapter(contextEngine)
	log.Println("Context engine initialized (template mode, no vector store)")

	// Start documentation agent if stores are available
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if server.SessionStore != nil && server.DecisionStore != nil && server.DocStore != nil {
		docAgent, err := docengine.NewDocAgent(
			server.DecisionStore,
			server.SessionStore,
			server.DocStore,
			docengine.DocAgentConfig{
				LLMAPIKey: os.Getenv("ADP_DOC_LLM_API_KEY"),
				LLMModel:  os.Getenv("ADP_DOC_LLM_MODEL"),
			},
		)
		if err != nil {
			log.Printf("Warning: Failed to initialize doc agent: %v", err)
		} else {
			go docAgent.Start(ctx)
			log.Println("Documentation agent started")
		}
	}

	// Start HTTP sidecar for git hook integration.
	// Git hooks call these endpoints during pre-commit, post-commit, pre-push.
	httpPort := getEnvOrDefaultInt("ADP_HTTP_PORT", 8081)
	server.HTTPPort = httpPort

	if server.CommitStore != nil {
		hookHandler := mcp.NewHookHTTPHandler(server.CommitStore, server.SessionStore)
		httpServer := &http.Server{
			Addr:    fmt.Sprintf(":%d", httpPort),
			Handler: hookHandler,
		}
		go func() {
			log.Printf("Hook HTTP sidecar listening on :%d", httpPort)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Warning: Hook HTTP sidecar failed: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			httpServer.Close()
		}()
	}

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	// Start MCP server (blocking, reads from stdin)
	if err := server.Start(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("MCP server failed: %v", err)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := parseIntFromString(value, &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func parseIntFromString(s string, result *int) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*result = n
	return n, nil
}
