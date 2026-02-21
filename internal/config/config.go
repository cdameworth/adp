package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the application configuration
type Config struct {
	Server      ServerConfig    `mapstructure:"server"`
	Log         LogConfig       `mapstructure:"log"`
	Database    DatabaseConfig  `mapstructure:"database"`
	Auth        AuthConfig      `mapstructure:"auth"`
	RateLimit   RateLimitConfig `mapstructure:"rate_limit"`
	Secrets     SecretsConfig   `mapstructure:"secrets"`
	Environment string          `mapstructure:"environment"`
}

type ServerConfig struct {
	Port               int           `mapstructure:"port"`
	ReadTimeout        time.Duration `mapstructure:"read_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout    time.Duration `mapstructure:"shutdown_timeout"`
	CORSAllowedOrigins []string      `mapstructure:"cors_allowed_origins"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type DatabaseConfig struct {
	Neo4j      Neo4jConfig      `mapstructure:"neo4j"`
	Qdrant     QdrantConfig     `mapstructure:"qdrant"`
	Postgres   PostgresConfig   `mapstructure:"postgres"`
	Redis      RedisConfig      `mapstructure:"redis"`
	ClickHouse ClickHouseConfig `mapstructure:"clickhouse"`
}

type ClickHouseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Database string `mapstructure:"database"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type Neo4jConfig struct {
	URI      string `mapstructure:"uri"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type QdrantConfig struct {
	Host   string `mapstructure:"host"`
	Port   int    `mapstructure:"port"`
	APIKey string `mapstructure:"api_key"`
}

type PostgresConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Database     string        `mapstructure:"database"`
	Username     string        `mapstructure:"username"`
	Password     string        `mapstructure:"password"`
	SSLMode      string        `mapstructure:"ssl_mode"`
	MaxOpenConns int           `mapstructure:"max_open_conns"`
	MaxIdleConns int           `mapstructure:"max_idle_conns"`
	MaxLifetime  time.Duration `mapstructure:"max_lifetime"`
	// DatabaseURL is a full connection string (e.g. Railway's DATABASE_URL).
	// When set, takes precedence over individual fields.
	DatabaseURL string `mapstructure:"database_url"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type AuthConfig struct {
	// APIKey is a shared secret for API key authentication
	APIKey string `mapstructure:"api_key"`
	// JWKSURL is the URL to fetch JSON Web Key Set for signature verification
	JWKSURL string `mapstructure:"jwks_url"`
	// Issuer is the expected token issuer (iss claim)
	Issuer string `mapstructure:"issuer"`
	// Audience is the expected token audience (aud claim)
	Audience string `mapstructure:"audience"`
	// RequireExpiration enforces exp claim presence
	RequireExpiration bool `mapstructure:"require_expiration"`
	// ClockSkew allows for clock differences between servers
	ClockSkew time.Duration `mapstructure:"clock_skew"`
	// CacheRefreshInterval controls how often JWKS is refreshed
	CacheRefreshInterval time.Duration `mapstructure:"cache_refresh_interval"`
	// SAML configuration
	SAML SAMLConfig `mapstructure:"saml"`
	// JWTSecret is the HMAC-SHA256 key for locally-generated JWTs
	JWTSecret string `mapstructure:"jwt_secret"`
	// OpenRegistration allows public user registration (default: false = admin invite only)
	OpenRegistration bool `mapstructure:"open_registration"`
}

type SAMLConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	RootURL     string `mapstructure:"root_url"`
	IdPMetadata string `mapstructure:"idp_metadata"`
	CertFile    string `mapstructure:"cert_file"`
	KeyFile     string `mapstructure:"key_file"`
}

type RateLimitConfig struct {
	// Enabled toggles rate limiting
	Enabled bool `mapstructure:"enabled"`
	// RequestsPerSecond is the default rate
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	// BurstSize is the maximum burst
	BurstSize int `mapstructure:"burst_size"`
	// UseRedis enables Redis-backed distributed rate limiting
	UseRedis bool `mapstructure:"use_redis"`
}

// SecretsConfig defines how secrets are managed
type SecretsConfig struct {
	// Provider specifies the secrets provider: "env", "vault", "aws-secrets-manager"
	Provider string `mapstructure:"provider"`
	// Vault configuration (if provider is "vault")
	Vault VaultConfig `mapstructure:"vault"`
}

type VaultConfig struct {
	// Address is the Vault server address
	Address string `mapstructure:"address"`
	// Token is the Vault token (should be set via environment)
	Token string `mapstructure:"token"`
	// Path is the secrets path prefix
	Path string `mapstructure:"path"`
}

// SecretKeys defines the expected secret environment variables
var SecretKeys = []string{
	"ADP_DATABASE_NEO4J_PASSWORD",
	"ADP_DATABASE_POSTGRES_PASSWORD",
	"ADP_DATABASE_REDIS_PASSWORD",
	"ADP_DATABASE_QDRANT_API_KEY",
	"ADP_SECRETS_VAULT_TOKEN",
}

// LoadConfig reads configuration from file or environment variables.
func LoadConfig(path string) (*Config, error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Set environment variable prefix
	viper.SetEnvPrefix("ADP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Explicitly bind nested environment variables
	// Viper's AutomaticEnv doesn't automatically work with nested structs
	bindNestedEnvVars()

	// Set defaults
	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found; ignore error if desired and rely on defaults/env
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Load secrets from environment (overrides config file values)
	loadSecretsFromEnv(&cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults() {
	// Server defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.read_timeout", 30*time.Second)
	viper.SetDefault("server.write_timeout", 30*time.Second)
	viper.SetDefault("server.shutdown_timeout", 15*time.Second)

	// Log defaults
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")

	// Database defaults
	viper.SetDefault("database.neo4j.uri", "bolt://localhost:7687")
	viper.SetDefault("database.qdrant.host", "localhost")
	viper.SetDefault("database.qdrant.port", 6334)
	viper.SetDefault("database.postgres.host", "localhost")
	viper.SetDefault("database.postgres.port", 5432)
	viper.SetDefault("database.postgres.database", "adp")
	viper.SetDefault("database.postgres.ssl_mode", "disable")
	viper.SetDefault("database.postgres.max_open_conns", 25)
	viper.SetDefault("database.postgres.max_idle_conns", 5)
	viper.SetDefault("database.postgres.max_lifetime", 5*time.Minute)
	viper.SetDefault("database.redis.host", "localhost")
	viper.SetDefault("database.redis.port", 6379)
	viper.SetDefault("database.redis.db", 0)
	viper.SetDefault("database.clickhouse.host", "localhost")
	viper.SetDefault("database.clickhouse.port", 9000)
	viper.SetDefault("database.clickhouse.database", "adp")
	viper.SetDefault("database.clickhouse.username", "default")

	// Auth defaults
	viper.SetDefault("auth.require_expiration", true)
	viper.SetDefault("auth.clock_skew", 1*time.Minute)
	viper.SetDefault("auth.cache_refresh_interval", 15*time.Minute)

	// Rate limit defaults
	viper.SetDefault("rate_limit.enabled", true)
	viper.SetDefault("rate_limit.requests_per_second", 10.0)
	viper.SetDefault("rate_limit.burst_size", 20)
	viper.SetDefault("rate_limit.use_redis", false)

	// Secrets defaults
	viper.SetDefault("secrets.provider", "env")

	// Environment default
	viper.SetDefault("environment", "development")
}

// bindNestedEnvVars explicitly binds nested environment variables to viper keys
// This is necessary because Viper's AutomaticEnv doesn't fully support nested structs
func bindNestedEnvVars() {
	// Database - Neo4j
	viper.BindEnv("database.neo4j.uri", "ADP_DATABASE_NEO4J_URI")
	viper.BindEnv("database.neo4j.username", "ADP_DATABASE_NEO4J_USERNAME")
	viper.BindEnv("database.neo4j.password", "ADP_DATABASE_NEO4J_PASSWORD")

	// Database - PostgreSQL
	viper.BindEnv("database.postgres.host", "ADP_DATABASE_POSTGRES_HOST")
	viper.BindEnv("database.postgres.port", "ADP_DATABASE_POSTGRES_PORT")
	viper.BindEnv("database.postgres.database", "ADP_DATABASE_POSTGRES_DATABASE")
	viper.BindEnv("database.postgres.username", "ADP_DATABASE_POSTGRES_USERNAME")
	viper.BindEnv("database.postgres.password", "ADP_DATABASE_POSTGRES_PASSWORD")
	viper.BindEnv("database.postgres.database_url", "DATABASE_URL")

	// Database - Redis
	viper.BindEnv("database.redis.host", "ADP_DATABASE_REDIS_HOST")
	viper.BindEnv("database.redis.port", "ADP_DATABASE_REDIS_PORT")
	viper.BindEnv("database.redis.password", "ADP_DATABASE_REDIS_PASSWORD")

	// Database - Qdrant
	viper.BindEnv("database.qdrant.host", "ADP_DATABASE_QDRANT_HOST")
	viper.BindEnv("database.qdrant.port", "ADP_DATABASE_QDRANT_PORT")

	// Database - ClickHouse
	viper.BindEnv("database.clickhouse.host", "ADP_DATABASE_CLICKHOUSE_HOST")
	viper.BindEnv("database.clickhouse.port", "ADP_DATABASE_CLICKHOUSE_PORT")
	viper.BindEnv("database.clickhouse.database", "ADP_DATABASE_CLICKHOUSE_DATABASE")
	viper.BindEnv("database.clickhouse.username", "ADP_DATABASE_CLICKHOUSE_USERNAME")
	viper.BindEnv("database.clickhouse.password", "ADP_DATABASE_CLICKHOUSE_PASSWORD")

	// Server
	viper.BindEnv("server.port", "ADP_SERVER_PORT")

	// Logging
	viper.BindEnv("log.level", "ADP_LOG_LEVEL")
	viper.BindEnv("log.format", "ADP_LOG_FORMAT")

	// Environment
	viper.BindEnv("environment", "ADP_ENVIRONMENT")

	// Auth
	viper.BindEnv("auth.api_key", "ADP_API_KEY")
	viper.BindEnv("auth.jwks_url", "ADP_AUTH_JWKS_URL")
	viper.BindEnv("auth.issuer", "ADP_AUTH_ISSUER")
	viper.BindEnv("auth.audience", "ADP_AUTH_AUDIENCE")
	viper.BindEnv("auth.jwt_secret", "ADP_JWT_SECRET")
	viper.BindEnv("auth.open_registration", "ADP_OPEN_REGISTRATION")

	// CORS
	viper.BindEnv("server.cors_allowed_origins", "ADP_CORS_ALLOWED_ORIGINS")
}

// loadSecretsFromEnv loads sensitive configuration from environment variables
func loadSecretsFromEnv(cfg *Config) {
	// Neo4j password
	if password := os.Getenv("ADP_DATABASE_NEO4J_PASSWORD"); password != "" {
		cfg.Database.Neo4j.Password = password
	}

	// PostgreSQL password
	if password := os.Getenv("ADP_DATABASE_POSTGRES_PASSWORD"); password != "" {
		cfg.Database.Postgres.Password = password
	}

	// Redis password
	if password := os.Getenv("ADP_DATABASE_REDIS_PASSWORD"); password != "" {
		cfg.Database.Redis.Password = password
	}

	// Qdrant API key
	if apiKey := os.Getenv("ADP_DATABASE_QDRANT_API_KEY"); apiKey != "" {
		cfg.Database.Qdrant.APIKey = apiKey
	}

	// ClickHouse password
	if password := os.Getenv("ADP_DATABASE_CLICKHOUSE_PASSWORD"); password != "" {
		cfg.Database.ClickHouse.Password = password
	}

	// Vault token
	if token := os.Getenv("ADP_SECRETS_VAULT_TOKEN"); token != "" {
		cfg.Secrets.Vault.Token = token
	}

	// JWKS URL (allow override)
	if jwksURL := os.Getenv("ADP_AUTH_JWKS_URL"); jwksURL != "" {
		cfg.Auth.JWKSURL = jwksURL
	}

	// Auth issuer (allow override)
	if issuer := os.Getenv("ADP_AUTH_ISSUER"); issuer != "" {
		cfg.Auth.Issuer = issuer
	}

	// Auth audience (allow override)
	if audience := os.Getenv("ADP_AUTH_AUDIENCE"); audience != "" {
		cfg.Auth.Audience = audience
	}

	// API key (allow override)
	if apiKey := os.Getenv("ADP_API_KEY"); apiKey != "" {
		cfg.Auth.APIKey = apiKey
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	var errs []string

	// Validate server configuration
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, "server.port must be between 1 and 65535")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[c.Log.Level] {
		errs = append(errs, "log.level must be one of: debug, info, warn, error")
	}

	// Validate database configuration in production
	// Skip SSL mode check when DATABASE_URL is set (connection string handles it)
	if c.Environment == "production" && c.Database.Postgres.DatabaseURL == "" {
		if c.Database.Postgres.SSLMode == "disable" {
			errs = append(errs, "database.postgres.ssl_mode should not be 'disable' in production (use 'require' or 'verify-full')")
		}
	}

	// Validate auth configuration in production
	if c.Environment == "production" {
		if c.Auth.JWKSURL == "" && c.Auth.APIKey == "" {
			errs = append(errs, "auth.jwks_url or auth.api_key is required in production")
		}
		if c.Auth.JWKSURL != "" && c.Auth.Issuer == "" {
			errs = append(errs, "auth.issuer is required when jwks_url is set")
		}
	}

	// Validate rate limit configuration
	if c.RateLimit.Enabled {
		if c.RateLimit.RequestsPerSecond <= 0 {
			errs = append(errs, "rate_limit.requests_per_second must be positive")
		}
		if c.RateLimit.BurstSize <= 0 {
			errs = append(errs, "rate_limit.burst_size must be positive")
		}
	}

	// Validate secrets provider
	validProviders := map[string]bool{
		"env": true, "vault": true, "aws-secrets-manager": true,
	}
	if !validProviders[c.Secrets.Provider] {
		errs = append(errs, "secrets.provider must be one of: env, vault, aws-secrets-manager")
	}

	// Validate Vault configuration if using Vault
	if c.Secrets.Provider == "vault" {
		if c.Secrets.Vault.Address == "" {
			errs = append(errs, "secrets.vault.address is required when using vault provider")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

// PostgresDSN returns the PostgreSQL connection string
func (c *Config) PostgresDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Postgres.Host,
		c.Database.Postgres.Port,
		c.Database.Postgres.Username,
		c.Database.Postgres.Password,
		c.Database.Postgres.Database,
		c.Database.Postgres.SSLMode,
	)
}

// RedisAddr returns the Redis address
func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Database.Redis.Host, c.Database.Redis.Port)
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// IsDevelopment returns true if running in development environment
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// MaskSecrets returns a copy of the config with sensitive values masked
func (c *Config) MaskSecrets() Config {
	masked := *c
	if masked.Database.Neo4j.Password != "" {
		masked.Database.Neo4j.Password = "***MASKED***"
	}
	if masked.Database.Postgres.Password != "" {
		masked.Database.Postgres.Password = "***MASKED***"
	}
	if masked.Database.Redis.Password != "" {
		masked.Database.Redis.Password = "***MASKED***"
	}
	if masked.Database.Qdrant.APIKey != "" {
		masked.Database.Qdrant.APIKey = "***MASKED***"
	}
	if masked.Database.ClickHouse.Password != "" {
		masked.Database.ClickHouse.Password = "***MASKED***"
	}
	if masked.Secrets.Vault.Token != "" {
		masked.Secrets.Vault.Token = "***MASKED***"
	}
	if masked.Auth.APIKey != "" {
		masked.Auth.APIKey = "***MASKED***"
	}
	return masked
}
