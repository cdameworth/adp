// Package cache provides Redis cluster support for ADP.
package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisMode represents the Redis deployment mode
type RedisMode string

const (
	ModeStandalone RedisMode = "standalone"
	ModeCluster    RedisMode = "cluster"
	ModeSentinel   RedisMode = "sentinel"
)

// RedisConfig holds configuration for Redis connections
type RedisConfig struct {
	// Mode is the Redis deployment mode
	Mode RedisMode
	// Addresses is the list of Redis addresses
	Addresses []string
	// Password for authentication
	Password string
	// DB number (standalone mode only)
	DB int
	// MaxRetries before giving up
	MaxRetries int
	// DialTimeout for establishing connections
	DialTimeout time.Duration
	// ReadTimeout for read operations
	ReadTimeout time.Duration
	// WriteTimeout for write operations
	WriteTimeout time.Duration
	// PoolSize is the maximum number of connections
	PoolSize int
	// MinIdleConns is the minimum number of idle connections
	MinIdleConns int
	// MaxIdleTime is the maximum idle time before closing
	MaxIdleTime time.Duration
	// TLS configuration
	TLS *TLSConfig
	// Sentinel configuration (sentinel mode only)
	Sentinel *SentinelConfig
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled            bool
	InsecureSkipVerify bool
	CertFile           string
	KeyFile            string
	CAFile             string
}

// SentinelConfig holds Sentinel-specific configuration
type SentinelConfig struct {
	MasterName string
	Password   string
}

// DefaultRedisConfig returns default Redis configuration
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Mode:         ModeStandalone,
		Addresses:    []string{"localhost:6379"},
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxIdleTime:  5 * time.Minute,
	}
}

// RedisClient wraps Redis client with support for different modes
type RedisClient struct {
	client redis.UniversalClient
	config RedisConfig
}

// NewRedisClient creates a new Redis client based on configuration
func NewRedisClient(ctx context.Context, cfg RedisConfig) (*RedisClient, error) {
	var client redis.UniversalClient

	switch cfg.Mode {
	case ModeCluster:
		client = newClusterClient(cfg)
	case ModeSentinel:
		client = newSentinelClient(cfg)
	default:
		client = newStandaloneClient(cfg)
	}

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisClient{
		client: client,
		config: cfg,
	}, nil
}

func newStandaloneClient(cfg RedisConfig) *redis.Client {
	addr := "localhost:6379"
	if len(cfg.Addresses) > 0 {
		addr = cfg.Addresses[0]
	}

	opts := &redis.Options{
		Addr:            addr,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      cfg.MaxRetries,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
	}

	if cfg.TLS != nil && cfg.TLS.Enabled {
		opts.TLSConfig = createTLSConfig(cfg.TLS)
	}

	return redis.NewClient(opts)
}

func newClusterClient(cfg RedisConfig) *redis.ClusterClient {
	opts := &redis.ClusterOptions{
		Addrs:           cfg.Addresses,
		Password:        cfg.Password,
		MaxRetries:      cfg.MaxRetries,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
		// Enable read from replicas for better read performance
		ReadOnly: false,
		// Route read commands to replicas when possible
		RouteByLatency: true,
	}

	if cfg.TLS != nil && cfg.TLS.Enabled {
		opts.TLSConfig = createTLSConfig(cfg.TLS)
	}

	return redis.NewClusterClient(opts)
}

func newSentinelClient(cfg RedisConfig) *redis.Client {
	masterName := "mymaster"
	if cfg.Sentinel != nil && cfg.Sentinel.MasterName != "" {
		masterName = cfg.Sentinel.MasterName
	}

	opts := &redis.FailoverOptions{
		MasterName:      masterName,
		SentinelAddrs:   cfg.Addresses,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      cfg.MaxRetries,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
	}

	if cfg.Sentinel != nil && cfg.Sentinel.Password != "" {
		opts.SentinelPassword = cfg.Sentinel.Password
	}

	if cfg.TLS != nil && cfg.TLS.Enabled {
		opts.TLSConfig = createTLSConfig(cfg.TLS)
	}

	return redis.NewFailoverClient(opts)
}

func createTLSConfig(cfg *TLSConfig) *tls.Config {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	// Note: Loading certificates from files would require additional implementation
	return tlsConfig
}

// Client returns the underlying Redis client
func (c *RedisClient) Client() redis.UniversalClient {
	return c.client
}

// Close closes the Redis connection
func (c *RedisClient) Close() error {
	return c.client.Close()
}

// Ping checks Redis connectivity
func (c *RedisClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Common operations

// Get retrieves a value by key
func (c *RedisClient) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

// Set sets a key-value pair with expiration
func (c *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(ctx, key, value, expiration).Err()
}

// SetNX sets a key only if it doesn't exist (for distributed locking)
func (c *RedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, value, expiration).Result()
}

// Del deletes one or more keys
func (c *RedisClient) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// Exists checks if keys exist
func (c *RedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.client.Exists(ctx, keys...).Result()
}

// Expire sets expiration on a key
func (c *RedisClient) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	return c.client.Expire(ctx, key, expiration).Result()
}

// TTL returns the remaining time to live of a key
func (c *RedisClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	return c.client.TTL(ctx, key).Result()
}

// Incr increments a key
func (c *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

// IncrBy increments a key by a value
func (c *RedisClient) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	return c.client.IncrBy(ctx, key, value).Result()
}

// Hash operations

// HSet sets hash field(s)
func (c *RedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.client.HSet(ctx, key, values...).Err()
}

// HGet gets a hash field
func (c *RedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	return c.client.HGet(ctx, key, field).Result()
}

// HGetAll gets all hash fields
func (c *RedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

// HDel deletes hash fields
func (c *RedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	return c.client.HDel(ctx, key, fields...).Err()
}

// HIncrBy increments a hash field by value
func (c *RedisClient) HIncrBy(ctx context.Context, key, field string, incr int64) (int64, error) {
	return c.client.HIncrBy(ctx, key, field, incr).Result()
}

// List operations

// LPush prepends values to a list
func (c *RedisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	return c.client.LPush(ctx, key, values...).Err()
}

// RPush appends values to a list
func (c *RedisClient) RPush(ctx context.Context, key string, values ...interface{}) error {
	return c.client.RPush(ctx, key, values...).Err()
}

// LRange gets a range of list elements
func (c *RedisClient) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return c.client.LRange(ctx, key, start, stop).Result()
}

// LLen returns list length
func (c *RedisClient) LLen(ctx context.Context, key string) (int64, error) {
	return c.client.LLen(ctx, key).Result()
}

// LTrim trims a list to the specified range
func (c *RedisClient) LTrim(ctx context.Context, key string, start, stop int64) error {
	return c.client.LTrim(ctx, key, start, stop).Err()
}

// Set operations

// SAdd adds members to a set
func (c *RedisClient) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return c.client.SAdd(ctx, key, members...).Err()
}

// SMembers returns all members of a set
func (c *RedisClient) SMembers(ctx context.Context, key string) ([]string, error) {
	return c.client.SMembers(ctx, key).Result()
}

// SIsMember checks if a member exists in a set
func (c *RedisClient) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	return c.client.SIsMember(ctx, key, member).Result()
}

// SRem removes members from a set
func (c *RedisClient) SRem(ctx context.Context, key string, members ...interface{}) error {
	return c.client.SRem(ctx, key, members...).Err()
}

// SCard returns the number of members in a set
func (c *RedisClient) SCard(ctx context.Context, key string) (int64, error) {
	return c.client.SCard(ctx, key).Result()
}

// Sorted set operations

// ZAdd adds members to a sorted set
func (c *RedisClient) ZAdd(ctx context.Context, key string, members ...redis.Z) error {
	return c.client.ZAdd(ctx, key, members...).Err()
}

// ZRangeWithScores returns a range of members with scores
func (c *RedisClient) ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return c.client.ZRangeWithScores(ctx, key, start, stop).Result()
}

// ZRevRangeWithScores returns a range of members with scores in reverse order
func (c *RedisClient) ZRevRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return c.client.ZRevRangeWithScores(ctx, key, start, stop).Result()
}

// ZScore returns the score of a member
func (c *RedisClient) ZScore(ctx context.Context, key, member string) (float64, error) {
	return c.client.ZScore(ctx, key, member).Result()
}

// ZRem removes members from a sorted set
func (c *RedisClient) ZRem(ctx context.Context, key string, members ...interface{}) error {
	return c.client.ZRem(ctx, key, members...).Err()
}

// Pub/Sub operations

// Publish publishes a message to a channel
func (c *RedisClient) Publish(ctx context.Context, channel string, message interface{}) error {
	return c.client.Publish(ctx, channel, message).Err()
}

// Subscribe returns a subscription to channels
func (c *RedisClient) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return c.client.Subscribe(ctx, channels...)
}

// Distributed lock support

// DistributedLock represents a distributed lock
type DistributedLock struct {
	client *RedisClient
	key    string
	value  string
	ttl    time.Duration
}

// NewDistributedLock creates a new distributed lock
func (c *RedisClient) NewDistributedLock(key, value string, ttl time.Duration) *DistributedLock {
	return &DistributedLock{
		client: c,
		key:    "lock:" + key,
		value:  value,
		ttl:    ttl,
	}
}

// Acquire tries to acquire the lock
func (l *DistributedLock) Acquire(ctx context.Context) (bool, error) {
	return l.client.SetNX(ctx, l.key, l.value, l.ttl)
}

// Release releases the lock (only if we own it)
func (l *DistributedLock) Release(ctx context.Context) error {
	// Use Lua script to atomically check and delete
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`)
	_, err := script.Run(ctx, l.client.client, []string{l.key}, l.value).Result()
	return err
}

// Extend extends the lock TTL (only if we own it)
func (l *DistributedLock) Extend(ctx context.Context, ttl time.Duration) (bool, error) {
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)
	result, err := script.Run(ctx, l.client.client, []string{l.key}, l.value, ttl.Milliseconds()).Int64()
	return result == 1, err
}

// Rate limiting support

// RateLimiter provides rate limiting using Redis
type RateLimiter struct {
	client *RedisClient
	prefix string
}

// NewRateLimiter creates a new rate limiter
func (c *RedisClient) NewRateLimiter(prefix string) *RateLimiter {
	return &RateLimiter{
		client: c,
		prefix: prefix,
	}
}

// Allow checks if an action is allowed under the rate limit using sliding window
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, int64, error) {
	now := time.Now().UnixMilli()
	windowStart := now - window.Milliseconds()
	fullKey := r.prefix + ":" + key

	// Use Lua script for atomic operation
	script := redis.NewScript(`
		local key = KEYS[1]
		local window_start = tonumber(ARGV[1])
		local now = tonumber(ARGV[2])
		local limit = tonumber(ARGV[3])
		local window_ms = tonumber(ARGV[4])

		-- Remove old entries
		redis.call("zremrangebyscore", key, "-inf", window_start)

		-- Count current entries
		local count = redis.call("zcard", key)

		if count < limit then
			-- Add new entry
			redis.call("zadd", key, now, now .. "-" .. math.random())
			redis.call("pexpire", key, window_ms)
			return {1, count + 1}
		else
			return {0, count}
		end
	`)

	result, err := script.Run(ctx, r.client.client, []string{fullKey}, windowStart, now, limit, window.Milliseconds()).Slice()
	if err != nil {
		return false, 0, err
	}

	allowed := result[0].(int64) == 1
	count := result[1].(int64)
	return allowed, count, nil
}

// HealthCheck returns health status of the Redis connection
func (c *RedisClient) HealthCheck(ctx context.Context) error {
	// Check basic connectivity
	if err := c.Ping(ctx); err != nil {
		return err
	}

	// For cluster mode, check cluster health
	if c.config.Mode == ModeCluster {
		if clusterClient, ok := c.client.(*redis.ClusterClient); ok {
			// Check cluster state
			info, err := clusterClient.ClusterInfo(ctx).Result()
			if err != nil {
				return fmt.Errorf("cluster info failed: %w", err)
			}
			if !strings.Contains(info, "cluster_state:ok") {
				return fmt.Errorf("cluster state is not ok")
			}
		}
	}

	return nil
}

// Stats returns Redis connection pool stats
func (c *RedisClient) Stats() *redis.PoolStats {
	return c.client.PoolStats()
}
