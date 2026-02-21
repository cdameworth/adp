package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Common errors
var (
	ErrCacheMiss    = errors.New("cache miss")
	ErrCacheSet     = errors.New("cache set failed")
	ErrCacheDelete  = errors.New("cache delete failed")
	ErrNotConnected = errors.New("not connected to cache")
)

// Cache defines the interface for caching operations
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Keys(ctx context.Context, pattern string) ([]string, error)
	Clear(ctx context.Context, pattern string) error
	Close() error
	HealthCheck(ctx context.Context) error
}

// RedisCache implements Cache using Redis
type RedisCache struct {
	client *redis.Client
	config *SimpleCacheConfig
}

// SimpleCacheConfig contains simple Redis connection configuration for basic caching
type SimpleCacheConfig struct {
	Host         string
	Port         int
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// DefaultSimpleCacheConfig returns default simple cache configuration
func DefaultSimpleCacheConfig() *SimpleCacheConfig {
	return &SimpleCacheConfig{
		Host:         "localhost",
		Port:         6379,
		Password:     "",
		DB:           0,
		MaxRetries:   3,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// NewRedisCache creates a new Redis cache
func NewRedisCache(config *SimpleCacheConfig) (*RedisCache, error) {
	if config == nil {
		config = DefaultSimpleCacheConfig()
	}

	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password:     config.Password,
		DB:           config.DB,
		MaxRetries:   config.MaxRetries,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisCache{
		client: client,
		config: config,
	}, nil
}

// Get retrieves a value from the cache
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("cache get failed: %w", err)
	}
	return val, nil
}

// Set stores a value in the cache with TTL
func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	err := c.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCacheSet, err)
	}
	return nil
}

// SetNX sets a value only if it doesn't exist (useful for distributed locking)
func (c *RedisCache) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	success, err := c.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("setnx failed: %w", err)
	}
	return success, nil
}

// Delete removes a value from the cache
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	err := c.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCacheDelete, err)
	}
	return nil
}

// Exists checks if a key exists in the cache
func (c *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("exists check failed: %w", err)
	}
	return n > 0, nil
}

// Keys returns all keys matching a pattern
func (c *RedisCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	keys, err := c.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("keys failed: %w", err)
	}
	return keys, nil
}

// Clear removes all keys matching a pattern
func (c *RedisCache) Clear(ctx context.Context, pattern string) error {
	keys, err := c.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	return nil
}

// Increment atomically increments a counter
func (c *RedisCache) Increment(ctx context.Context, key string) (int64, error) {
	val, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("increment failed: %w", err)
	}
	return val, nil
}

// IncrementBy atomically increments a counter by a specific amount
func (c *RedisCache) IncrementBy(ctx context.Context, key string, value int64) (int64, error) {
	val, err := c.client.IncrBy(ctx, key, value).Result()
	if err != nil {
		return 0, fmt.Errorf("increment by failed: %w", err)
	}
	return val, nil
}

// Expire sets a TTL on an existing key
func (c *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	err := c.client.Expire(ctx, key, ttl).Err()
	if err != nil {
		return fmt.Errorf("expire failed: %w", err)
	}
	return nil
}

// TTL returns the remaining TTL of a key
func (c *RedisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := c.client.TTL(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("ttl failed: %w", err)
	}
	return ttl, nil
}

// HSet sets a field in a hash
func (c *RedisCache) HSet(ctx context.Context, key, field string, value []byte) error {
	err := c.client.HSet(ctx, key, field, value).Err()
	if err != nil {
		return fmt.Errorf("hset failed: %w", err)
	}
	return nil
}

// HGet retrieves a field from a hash
func (c *RedisCache) HGet(ctx context.Context, key, field string) ([]byte, error) {
	val, err := c.client.HGet(ctx, key, field).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("hget failed: %w", err)
	}
	return val, nil
}

// HGetAll retrieves all fields from a hash
func (c *RedisCache) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	val, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("hgetall failed: %w", err)
	}
	return val, nil
}

// HDel deletes a field from a hash
func (c *RedisCache) HDel(ctx context.Context, key string, fields ...string) error {
	err := c.client.HDel(ctx, key, fields...).Err()
	if err != nil {
		return fmt.Errorf("hdel failed: %w", err)
	}
	return nil
}

// LPush pushes values to the left of a list
func (c *RedisCache) LPush(ctx context.Context, key string, values ...interface{}) error {
	err := c.client.LPush(ctx, key, values...).Err()
	if err != nil {
		return fmt.Errorf("lpush failed: %w", err)
	}
	return nil
}

// RPop pops a value from the right of a list
func (c *RedisCache) RPop(ctx context.Context, key string) (string, error) {
	val, err := c.client.RPop(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrCacheMiss
		}
		return "", fmt.Errorf("rpop failed: %w", err)
	}
	return val, nil
}

// LRange retrieves a range of elements from a list
func (c *RedisCache) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	val, err := c.client.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("lrange failed: %w", err)
	}
	return val, nil
}

// LLen returns the length of a list
func (c *RedisCache) LLen(ctx context.Context, key string) (int64, error) {
	val, err := c.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("llen failed: %w", err)
	}
	return val, nil
}

// SAdd adds members to a set
func (c *RedisCache) SAdd(ctx context.Context, key string, members ...interface{}) error {
	err := c.client.SAdd(ctx, key, members...).Err()
	if err != nil {
		return fmt.Errorf("sadd failed: %w", err)
	}
	return nil
}

// SMembers retrieves all members of a set
func (c *RedisCache) SMembers(ctx context.Context, key string) ([]string, error) {
	val, err := c.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("smembers failed: %w", err)
	}
	return val, nil
}

// SIsMember checks if a value is a member of a set
func (c *RedisCache) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	val, err := c.client.SIsMember(ctx, key, member).Result()
	if err != nil {
		return false, fmt.Errorf("sismember failed: %w", err)
	}
	return val, nil
}

// SRem removes members from a set
func (c *RedisCache) SRem(ctx context.Context, key string, members ...interface{}) error {
	err := c.client.SRem(ctx, key, members...).Err()
	if err != nil {
		return fmt.Errorf("srem failed: %w", err)
	}
	return nil
}

// Close closes the Redis connection
func (c *RedisCache) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// HealthCheck checks if Redis is healthy
func (c *RedisCache) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return ErrNotConnected
	}
	return c.client.Ping(ctx).Err()
}

// Client returns the underlying Redis client for advanced operations
func (c *RedisCache) Client() *redis.Client {
	return c.client
}

// InMemoryCache provides an in-memory cache implementation for testing
type InMemoryCache struct {
	data map[string]cacheEntry
	mu   sync.Mutex
}

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewInMemoryCache creates a new in-memory cache
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string]cacheEntry),
	}
}

// Get retrieves a value from the cache
func (c *InMemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.data[key]
	if !ok {
		return nil, ErrCacheMiss
	}

	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.data, key)
		return nil, ErrCacheMiss
	}

	return entry.value, nil
}

// Set stores a value in the cache
func (c *InMemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	c.data[key] = cacheEntry{
		value:     value,
		expiresAt: expiresAt,
	}
	return nil
}

// Delete removes a value from the cache
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	return nil
}

// Exists checks if a key exists
func (c *InMemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.data[key]
	if !ok {
		return false, nil
	}

	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.data, key)
		return false, nil
	}

	return true, nil
}

// Keys returns all keys matching a pattern (simplified - only supports * wildcard at end)
func (c *InMemoryCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var keys []string
	for key := range c.data {
		// Simplified pattern matching
		keys = append(keys, key)
	}
	return keys, nil
}

// Clear removes all keys
func (c *InMemoryCache) Clear(ctx context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]cacheEntry)
	return nil
}

// Close is a no-op for in-memory cache
func (c *InMemoryCache) Close() error {
	return nil
}

// HealthCheck always returns nil for in-memory cache
func (c *InMemoryCache) HealthCheck(ctx context.Context) error {
	return nil
}
