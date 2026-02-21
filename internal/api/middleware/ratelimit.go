package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitConfig holds configuration for rate limiting
type RateLimitConfig struct {
	// RequestsPerSecond is the rate at which tokens are added to the bucket
	RequestsPerSecond float64
	// BurstSize is the maximum number of tokens in the bucket
	BurstSize int
	// KeyFunc extracts the rate limit key from the request (e.g., IP, user ID)
	KeyFunc func(r *http.Request) string
	// ExcludeFunc returns true if the request should be excluded from rate limiting
	ExcludeFunc func(r *http.Request) bool
	// CleanupInterval is how often to clean up stale buckets
	CleanupInterval time.Duration
	// BucketTTL is how long an unused bucket is kept before cleanup
	BucketTTL time.Duration
}

// DefaultRateLimitConfig returns sensible defaults
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: 10.0,
		BurstSize:         20,
		KeyFunc:           IPKeyFunc,
		ExcludeFunc:       nil,
		CleanupInterval:   5 * time.Minute,
		BucketTTL:         10 * time.Minute,
	}
}

// IPKeyFunc extracts the client IP for rate limiting
func IPKeyFunc(r *http.Request) string {
	// Check X-Forwarded-For first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain (original client)
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// UserKeyFunc extracts the user ID for rate limiting (requires auth middleware)
func UserKeyFunc(r *http.Request) string {
	if userID, ok := GetUserFromContext(r.Context()); ok {
		return "user:" + userID
	}
	// Fall back to IP if no user
	return IPKeyFunc(r)
}

// EndpointKeyFunc creates a composite key of IP + endpoint
func EndpointKeyFunc(r *http.Request) string {
	return fmt.Sprintf("%s:%s:%s", IPKeyFunc(r), r.Method, r.URL.Path)
}

// tokenBucket implements a token bucket rate limiter
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// newTokenBucket creates a new token bucket
func newTokenBucket(maxTokens float64, refillRate float64) *tokenBucket {
	return &tokenBucket{
		tokens:     maxTokens, // Start full
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// allow checks if a request should be allowed and consumes a token if so
func (b *tokenBucket) allow() (bool, float64, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()

	// Refill tokens based on elapsed time
	b.tokens = min(b.maxTokens, b.tokens+elapsed*b.refillRate)
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true, b.tokens, 0
	}

	// Calculate retry time
	tokensNeeded := 1 - b.tokens
	retryAfter := time.Duration(tokensNeeded/b.refillRate*1000) * time.Millisecond

	return false, b.tokens, retryAfter
}

// RateLimiter manages rate limiting across multiple clients
type RateLimiter struct {
	config  RateLimitConfig
	buckets map[string]*bucketEntry
	mu      sync.RWMutex
	stopCh  chan struct{}
}

type bucketEntry struct {
	bucket   *tokenBucket
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config:  config,
		buckets: make(map[string]*bucketEntry),
		stopCh:  make(chan struct{}),
	}

	// Start cleanup goroutine
	if config.CleanupInterval > 0 {
		go rl.cleanupLoop()
	}

	return rl
}

// cleanupLoop periodically removes stale buckets
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// cleanup removes buckets that haven't been used recently
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.config.BucketTTL)
	for key, entry := range rl.buckets {
		if entry.lastSeen.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
}

// Stop stops the rate limiter cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// getBucket returns or creates a bucket for the given key
func (rl *RateLimiter) getBucket(key string) *tokenBucket {
	rl.mu.RLock()
	entry, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if exists {
		entry.lastSeen = time.Now()
		return entry.bucket
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := rl.buckets[key]; exists {
		entry.lastSeen = time.Now()
		return entry.bucket
	}

	bucket := newTokenBucket(float64(rl.config.BurstSize), rl.config.RequestsPerSecond)
	rl.buckets[key] = &bucketEntry{
		bucket:   bucket,
		lastSeen: time.Now(),
	}
	return bucket
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(key string) (bool, float64, time.Duration) {
	bucket := rl.getBucket(key)
	return bucket.allow()
}

// Middleware returns the HTTP middleware handler
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check exclusion
		if rl.config.ExcludeFunc != nil && rl.config.ExcludeFunc(r) {
			next.ServeHTTP(w, r)
			return
		}

		key := rl.config.KeyFunc(r)
		allowed, remaining, retryAfter := rl.Allow(key)

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.config.BurstSize))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatFloat(remaining, 'f', 0, 64))

		if !allowed {
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(retryAfter).Unix(), 10))
			w.Header().Set("Retry-After", strconv.FormatFloat(retryAfter.Seconds(), 'f', 0, 64))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":"rate_limit_exceeded","message":"Too many requests","retry_after_seconds":%d}`, int(retryAfter.Seconds()))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// PerEndpointRateLimiter provides different rate limits per endpoint
type PerEndpointRateLimiter struct {
	defaultLimiter *RateLimiter
	endpointLimits map[string]*RateLimiter
	mu             sync.RWMutex
}

// EndpointLimit defines rate limits for a specific endpoint pattern
type EndpointLimit struct {
	// Pattern is the endpoint pattern (e.g., "/v1/sessions", "POST /v1/governance/check")
	Pattern string
	// RequestsPerSecond for this endpoint
	RequestsPerSecond float64
	// BurstSize for this endpoint
	BurstSize int
}

// NewPerEndpointRateLimiter creates a rate limiter with per-endpoint limits
func NewPerEndpointRateLimiter(defaultConfig RateLimitConfig, endpoints []EndpointLimit) *PerEndpointRateLimiter {
	perl := &PerEndpointRateLimiter{
		defaultLimiter: NewRateLimiter(defaultConfig),
		endpointLimits: make(map[string]*RateLimiter),
	}

	for _, ep := range endpoints {
		config := defaultConfig
		config.RequestsPerSecond = ep.RequestsPerSecond
		config.BurstSize = ep.BurstSize
		perl.endpointLimits[ep.Pattern] = NewRateLimiter(config)
	}

	return perl
}

// Stop stops all rate limiters
func (perl *PerEndpointRateLimiter) Stop() {
	perl.defaultLimiter.Stop()
	for _, rl := range perl.endpointLimits {
		rl.Stop()
	}
}

// getLimiterForRequest returns the appropriate rate limiter for the request
func (perl *PerEndpointRateLimiter) getLimiterForRequest(r *http.Request) *RateLimiter {
	perl.mu.RLock()
	defer perl.mu.RUnlock()

	// Check method + path pattern first
	methodPath := r.Method + " " + r.URL.Path
	if limiter, exists := perl.endpointLimits[methodPath]; exists {
		return limiter
	}

	// Check path only
	if limiter, exists := perl.endpointLimits[r.URL.Path]; exists {
		return limiter
	}

	return perl.defaultLimiter
}

// Middleware returns the HTTP middleware handler
func (perl *PerEndpointRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiter := perl.getLimiterForRequest(r)
		limiter.Middleware(next).ServeHTTP(w, r)
	})
}

// DistributedRateLimiter interface for Redis-backed rate limiting
type DistributedRateLimiter interface {
	Allow(ctx context.Context, key string) (bool, float64, time.Duration, error)
}

// RedisRateLimiterConfig holds configuration for Redis-backed rate limiting
type RedisRateLimiterConfig struct {
	// KeyPrefix for Redis keys
	KeyPrefix string
	// RequestsPerSecond rate limit
	RequestsPerSecond float64
	// BurstSize maximum burst
	BurstSize int
	// KeyFunc extracts the rate limit key from the request
	KeyFunc func(r *http.Request) string
}

// RedisRateLimiter implements distributed rate limiting using Redis
// This is a placeholder interface - actual implementation requires Redis client
type RedisRateLimiter struct {
	config RedisRateLimiterConfig
	// client redis.Client would be added here
}

// NewRedisRateLimiter creates a new Redis-backed rate limiter
// Note: Actual implementation requires a Redis client dependency
func NewRedisRateLimiter(config RedisRateLimiterConfig) *RedisRateLimiter {
	return &RedisRateLimiter{
		config: config,
	}
}

// Allow checks if a request should be allowed using Redis EVAL script
// The script implements token bucket algorithm atomically in Redis
func (rrl *RedisRateLimiter) Allow(ctx context.Context, key string) (bool, float64, time.Duration, error) {
	// Redis Lua script for token bucket:
	// KEYS[1] = bucket key
	// ARGV[1] = max tokens (burst)
	// ARGV[2] = refill rate (tokens per second)
	// ARGV[3] = current timestamp (seconds with decimals)
	//
	// Script returns: [allowed (0/1), remaining tokens, retry_after_ms]
	//
	// local key = KEYS[1]
	// local max_tokens = tonumber(ARGV[1])
	// local refill_rate = tonumber(ARGV[2])
	// local now = tonumber(ARGV[3])
	//
	// local bucket = redis.call('HMGET', key, 'tokens', 'last_update')
	// local tokens = tonumber(bucket[1]) or max_tokens
	// local last_update = tonumber(bucket[2]) or now
	//
	// local elapsed = now - last_update
	// tokens = math.min(max_tokens, tokens + elapsed * refill_rate)
	//
	// local allowed = 0
	// local retry_after = 0
	//
	// if tokens >= 1 then
	//     tokens = tokens - 1
	//     allowed = 1
	// else
	//     retry_after = math.ceil((1 - tokens) / refill_rate * 1000)
	// end
	//
	// redis.call('HMSET', key, 'tokens', tokens, 'last_update', now)
	// redis.call('EXPIRE', key, 3600)
	//
	// return {allowed, tokens, retry_after}

	// Placeholder: actual implementation would execute the above script
	return true, float64(rrl.config.BurstSize), 0, nil
}

// Middleware returns the HTTP middleware handler for Redis-backed rate limiting
func (rrl *RedisRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := rrl.config.KeyPrefix + ":" + rrl.config.KeyFunc(r)

		allowed, remaining, retryAfter, err := rrl.Allow(r.Context(), key)
		if err != nil {
			// On Redis error, fail open (allow request) but log
			// In production, this should be configurable
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rrl.config.BurstSize))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatFloat(remaining, 'f', 0, 64))

		if !allowed {
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(retryAfter).Unix(), 10))
			w.Header().Set("Retry-After", strconv.FormatFloat(retryAfter.Seconds(), 'f', 0, 64))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":"rate_limit_exceeded","message":"Too many requests","retry_after_seconds":%d}`, int(retryAfter.Seconds()))
			return
		}

		next.ServeHTTP(w, r)
	})
}
