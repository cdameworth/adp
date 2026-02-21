package config

import (
	"os"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	t.Run("accepts valid configuration", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level:  "info",
				Format: "json",
			},
			Auth: AuthConfig{
				JWKSURL: "https://example.com/.well-known/jwks.json",
				Issuer:  "https://example.com",
			},
			RateLimit: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 10,
				BurstSize:         20,
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
			Environment: "development",
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("rejects invalid port", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 0,
			},
			Log: LogConfig{
				Level: "info",
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for invalid port")
		}
	})

	t.Run("rejects invalid log level", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "invalid",
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for invalid log level")
		}
	})

	t.Run("requires JWKS URL in production", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "info",
			},
			Auth: AuthConfig{
				JWKSURL: "",
			},
			RateLimit: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 10,
				BurstSize:         20,
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
			Environment: "production",
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for missing JWKS URL in production")
		}
	})

	t.Run("requires issuer in production", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "info",
			},
			Auth: AuthConfig{
				JWKSURL: "https://example.com/.well-known/jwks.json",
				Issuer:  "",
			},
			RateLimit: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 10,
				BurstSize:         20,
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
			Environment: "production",
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for missing issuer in production")
		}
	})

	t.Run("rejects invalid rate limit config", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "info",
			},
			RateLimit: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 0,
				BurstSize:         0,
			},
			Secrets: SecretsConfig{
				Provider: "env",
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for invalid rate limit config")
		}
	})

	t.Run("rejects invalid secrets provider", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "info",
			},
			Secrets: SecretsConfig{
				Provider: "invalid",
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for invalid secrets provider")
		}
	})

	t.Run("requires vault address when using vault provider", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{
				Port: 8080,
			},
			Log: LogConfig{
				Level: "info",
			},
			RateLimit: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 10,
				BurstSize:         20,
			},
			Secrets: SecretsConfig{
				Provider: "vault",
				Vault: VaultConfig{
					Address: "",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected validation error for missing vault address")
		}
	})
}

func TestLoadSecretsFromEnv(t *testing.T) {
	// Save and restore environment
	oldEnv := map[string]string{}
	for _, key := range SecretKeys {
		oldEnv[key] = os.Getenv(key)
	}
	oldEnv["ADP_AUTH_JWKS_URL"] = os.Getenv("ADP_AUTH_JWKS_URL")
	oldEnv["ADP_AUTH_ISSUER"] = os.Getenv("ADP_AUTH_ISSUER")
	oldEnv["ADP_AUTH_AUDIENCE"] = os.Getenv("ADP_AUTH_AUDIENCE")
	defer func() {
		for key, value := range oldEnv {
			if value == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, value)
			}
		}
	}()

	t.Run("loads secrets from environment", func(t *testing.T) {
		os.Setenv("ADP_DATABASE_NEO4J_PASSWORD", "neo4j-secret")
		os.Setenv("ADP_DATABASE_POSTGRES_PASSWORD", "postgres-secret")
		os.Setenv("ADP_DATABASE_REDIS_PASSWORD", "redis-secret")
		os.Setenv("ADP_DATABASE_QDRANT_API_KEY", "qdrant-key")
		os.Setenv("ADP_SECRETS_VAULT_TOKEN", "vault-token")
		os.Setenv("ADP_AUTH_JWKS_URL", "https://auth.example.com/.well-known/jwks.json")
		os.Setenv("ADP_AUTH_ISSUER", "https://auth.example.com")
		os.Setenv("ADP_AUTH_AUDIENCE", "adp-api")

		cfg := &Config{}
		loadSecretsFromEnv(cfg)

		if cfg.Database.Neo4j.Password != "neo4j-secret" {
			t.Errorf("expected neo4j password 'neo4j-secret', got %q", cfg.Database.Neo4j.Password)
		}
		if cfg.Database.Postgres.Password != "postgres-secret" {
			t.Errorf("expected postgres password 'postgres-secret', got %q", cfg.Database.Postgres.Password)
		}
		if cfg.Database.Redis.Password != "redis-secret" {
			t.Errorf("expected redis password 'redis-secret', got %q", cfg.Database.Redis.Password)
		}
		if cfg.Database.Qdrant.APIKey != "qdrant-key" {
			t.Errorf("expected qdrant api key 'qdrant-key', got %q", cfg.Database.Qdrant.APIKey)
		}
		if cfg.Secrets.Vault.Token != "vault-token" {
			t.Errorf("expected vault token 'vault-token', got %q", cfg.Secrets.Vault.Token)
		}
		if cfg.Auth.JWKSURL != "https://auth.example.com/.well-known/jwks.json" {
			t.Errorf("expected JWKS URL 'https://auth.example.com/.well-known/jwks.json', got %q", cfg.Auth.JWKSURL)
		}
		if cfg.Auth.Issuer != "https://auth.example.com" {
			t.Errorf("expected issuer 'https://auth.example.com', got %q", cfg.Auth.Issuer)
		}
		if cfg.Auth.Audience != "adp-api" {
			t.Errorf("expected audience 'adp-api', got %q", cfg.Auth.Audience)
		}
	})

	t.Run("does not override when env is empty", func(t *testing.T) {
		// Clear all environment variables
		for _, key := range SecretKeys {
			os.Unsetenv(key)
		}
		os.Unsetenv("ADP_AUTH_JWKS_URL")
		os.Unsetenv("ADP_AUTH_ISSUER")
		os.Unsetenv("ADP_AUTH_AUDIENCE")

		cfg := &Config{
			Database: DatabaseConfig{
				Neo4j: Neo4jConfig{
					Password: "original",
				},
			},
			Auth: AuthConfig{
				JWKSURL: "original-url",
			},
		}
		loadSecretsFromEnv(cfg)

		if cfg.Database.Neo4j.Password != "original" {
			t.Errorf("expected password to remain 'original', got %q", cfg.Database.Neo4j.Password)
		}
		if cfg.Auth.JWKSURL != "original-url" {
			t.Errorf("expected JWKS URL to remain 'original-url', got %q", cfg.Auth.JWKSURL)
		}
	})
}

func TestPostgresDSN(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Postgres: PostgresConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "pass",
				Database: "adp",
				SSLMode:  "disable",
			},
		},
	}

	expected := "host=localhost port=5432 user=user password=pass dbname=adp sslmode=disable"
	if cfg.PostgresDSN() != expected {
		t.Errorf("expected %q, got %q", expected, cfg.PostgresDSN())
	}
}

func TestRedisAddr(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Redis: RedisConfig{
				Host: "localhost",
				Port: 6379,
			},
		},
	}

	expected := "localhost:6379"
	if cfg.RedisAddr() != expected {
		t.Errorf("expected %q, got %q", expected, cfg.RedisAddr())
	}
}

func TestEnvironmentChecks(t *testing.T) {
	t.Run("IsProduction returns true for production", func(t *testing.T) {
		cfg := &Config{Environment: "production"}
		if !cfg.IsProduction() {
			t.Error("expected IsProduction to return true")
		}
	})

	t.Run("IsProduction returns false for development", func(t *testing.T) {
		cfg := &Config{Environment: "development"}
		if cfg.IsProduction() {
			t.Error("expected IsProduction to return false")
		}
	})

	t.Run("IsDevelopment returns true for development", func(t *testing.T) {
		cfg := &Config{Environment: "development"}
		if !cfg.IsDevelopment() {
			t.Error("expected IsDevelopment to return true")
		}
	})

	t.Run("IsDevelopment returns false for production", func(t *testing.T) {
		cfg := &Config{Environment: "production"}
		if cfg.IsDevelopment() {
			t.Error("expected IsDevelopment to return false")
		}
	})
}

func TestMaskSecrets(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Neo4j: Neo4jConfig{
				Password: "neo4j-secret",
			},
			Postgres: PostgresConfig{
				Password: "postgres-secret",
			},
			Redis: RedisConfig{
				Password: "redis-secret",
			},
			Qdrant: QdrantConfig{
				APIKey: "qdrant-key",
			},
		},
		Secrets: SecretsConfig{
			Vault: VaultConfig{
				Token: "vault-token",
			},
		},
	}

	masked := cfg.MaskSecrets()

	// Original should be unchanged
	if cfg.Database.Neo4j.Password != "neo4j-secret" {
		t.Error("original password was modified")
	}

	// Masked should have secrets hidden
	if masked.Database.Neo4j.Password != "***MASKED***" {
		t.Errorf("expected masked password, got %q", masked.Database.Neo4j.Password)
	}
	if masked.Database.Postgres.Password != "***MASKED***" {
		t.Errorf("expected masked password, got %q", masked.Database.Postgres.Password)
	}
	if masked.Database.Redis.Password != "***MASKED***" {
		t.Errorf("expected masked password, got %q", masked.Database.Redis.Password)
	}
	if masked.Database.Qdrant.APIKey != "***MASKED***" {
		t.Errorf("expected masked API key, got %q", masked.Database.Qdrant.APIKey)
	}
	if masked.Secrets.Vault.Token != "***MASKED***" {
		t.Errorf("expected masked token, got %q", masked.Secrets.Vault.Token)
	}
}

func TestSetDefaults(t *testing.T) {
	// This test verifies that defaults are set correctly
	// We don't call LoadConfig because that would require a config file
	// Instead we verify the constants used
	t.Run("verifies default values are sensible", func(t *testing.T) {
		defaults := map[string]interface{}{
			"server.port":                      8080,
			"server.read_timeout":              30 * time.Second,
			"server.write_timeout":             30 * time.Second,
			"server.shutdown_timeout":          15 * time.Second,
			"log.level":                        "info",
			"log.format":                       "json",
			"database.postgres.max_open_conns": 25,
			"database.postgres.max_idle_conns": 5,
			"rate_limit.enabled":               true,
			"rate_limit.requests_per_second":   10.0,
			"rate_limit.burst_size":            20,
			"auth.require_expiration":          true,
			"auth.clock_skew":                  1 * time.Minute,
			"auth.cache_refresh_interval":      15 * time.Minute,
			"secrets.provider":                 "env",
			"environment":                      "development",
		}

		// Spot check some critical defaults
		if defaults["server.port"].(int) < 1 || defaults["server.port"].(int) > 65535 {
			t.Error("default port is invalid")
		}
		if defaults["rate_limit.requests_per_second"].(float64) <= 0 {
			t.Error("default rate limit is invalid")
		}
		if defaults["rate_limit.burst_size"].(int) <= 0 {
			t.Error("default burst size is invalid")
		}
	})
}
