package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	t.Run("starts with full bucket", func(t *testing.T) {
		bucket := newTokenBucket(10, 1)
		allowed, remaining, _ := bucket.allow()
		if !allowed {
			t.Error("expected first request to be allowed")
		}
		if remaining != 9 {
			t.Errorf("expected 9 remaining, got %v", remaining)
		}
	})

	t.Run("drains bucket", func(t *testing.T) {
		bucket := newTokenBucket(3, 0.001) // Very slow refill

		for i := 0; i < 3; i++ {
			allowed, _, _ := bucket.allow()
			if !allowed {
				t.Errorf("request %d should be allowed", i)
			}
		}

		// Fourth request should be denied
		allowed, _, retryAfter := bucket.allow()
		if allowed {
			t.Error("expected fourth request to be denied")
		}
		if retryAfter <= 0 {
			t.Error("expected positive retry after")
		}
	})

	t.Run("refills over time", func(t *testing.T) {
		bucket := newTokenBucket(2, 10) // 10 tokens per second

		// Drain the bucket
		bucket.allow()
		bucket.allow()
		allowed, _, _ := bucket.allow()
		if allowed {
			t.Error("expected bucket to be drained")
		}

		// Wait for refill
		time.Sleep(150 * time.Millisecond)

		// Should have refilled at least 1 token
		allowed, _, _ = bucket.allow()
		if !allowed {
			t.Error("expected bucket to have refilled")
		}
	})

	t.Run("caps at max tokens", func(t *testing.T) {
		bucket := newTokenBucket(5, 100) // Fast refill

		// Wait a bit to accumulate tokens
		time.Sleep(100 * time.Millisecond)

		// Should still only have 5 tokens max
		for i := 0; i < 5; i++ {
			allowed, _, _ := bucket.allow()
			if !allowed {
				t.Errorf("request %d should be allowed", i)
			}
		}

		// Sixth should be denied
		allowed, _, _ := bucket.allow()
		if allowed {
			t.Error("expected sixth request to be denied")
		}
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("creates separate buckets per key", func(t *testing.T) {
		config := RateLimitConfig{
			RequestsPerSecond: 0.001, // Very slow
			BurstSize:         2,
			KeyFunc:           IPKeyFunc,
			CleanupInterval:   1 * time.Hour,
			BucketTTL:         1 * time.Hour,
		}
		limiter := NewRateLimiter(config)
		defer limiter.Stop()

		// Drain bucket for key1
		limiter.Allow("key1")
		limiter.Allow("key1")
		allowed1, _, _ := limiter.Allow("key1")

		// key2 should have its own full bucket
		allowed2, remaining, _ := limiter.Allow("key2")

		if allowed1 {
			t.Error("key1 should be rate limited")
		}
		if !allowed2 {
			t.Error("key2 should have its own bucket")
		}
		if remaining != 1 {
			t.Errorf("expected 1 remaining for key2, got %v", remaining)
		}
	})

	t.Run("cleans up stale buckets", func(t *testing.T) {
		config := RateLimitConfig{
			RequestsPerSecond: 1,
			BurstSize:         10,
			KeyFunc:           IPKeyFunc,
			CleanupInterval:   10 * time.Millisecond,
			BucketTTL:         20 * time.Millisecond,
		}
		limiter := NewRateLimiter(config)
		defer limiter.Stop()

		// Create some buckets
		limiter.Allow("key1")
		limiter.Allow("key2")

		// Wait for cleanup
		time.Sleep(50 * time.Millisecond)

		// Buckets should be cleaned up
		limiter.mu.RLock()
		count := len(limiter.buckets)
		limiter.mu.RUnlock()

		if count != 0 {
			t.Errorf("expected 0 buckets after cleanup, got %d", count)
		}
	})
}

func TestRateLimiterMiddleware(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 0.001, // Very slow refill
		BurstSize:         2,
		KeyFunc:           IPKeyFunc,
		CleanupInterval:   1 * time.Hour,
		BucketTTL:         1 * time.Hour,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("allows requests under limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		limiter.Middleware(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}

		// Check rate limit headers
		if rr.Header().Get("X-RateLimit-Limit") != "2" {
			t.Errorf("expected X-RateLimit-Limit=2, got %s", rr.Header().Get("X-RateLimit-Limit"))
		}
	})

	t.Run("returns 429 when rate limited", func(t *testing.T) {
		// Use a unique IP to get a fresh bucket
		ip := "10.0.0.1:12345"

		// Drain the bucket
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = ip
			rr := httptest.NewRecorder()
			limiter.Middleware(handler).ServeHTTP(rr, req)
		}

		// Third request should be rate limited
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()

		limiter.Middleware(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429, got %d", rr.Code)
		}

		// Check Retry-After header
		if rr.Header().Get("Retry-After") == "" {
			t.Error("expected Retry-After header")
		}
	})

	t.Run("respects exclude function", func(t *testing.T) {
		excludeConfig := RateLimitConfig{
			RequestsPerSecond: 0.001,
			BurstSize:         1,
			KeyFunc:           IPKeyFunc,
			ExcludeFunc: func(r *http.Request) bool {
				return r.URL.Path == "/health"
			},
			CleanupInterval: 1 * time.Hour,
			BucketTTL:       1 * time.Hour,
		}
		excludeLimiter := NewRateLimiter(excludeConfig)
		defer excludeLimiter.Stop()

		// First request to /api drains bucket
		req := httptest.NewRequest(http.MethodGet, "/api", nil)
		req.RemoteAddr = "172.16.0.1:12345"
		rr := httptest.NewRecorder()
		excludeLimiter.Middleware(handler).ServeHTTP(rr, req)

		// Second request to /api should be rate limited
		req = httptest.NewRequest(http.MethodGet, "/api", nil)
		req.RemoteAddr = "172.16.0.1:12345"
		rr = httptest.NewRecorder()
		excludeLimiter.Middleware(handler).ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429 for /api, got %d", rr.Code)
		}

		// But /health should be excluded
		req = httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "172.16.0.1:12345"
		rr = httptest.NewRecorder()
		excludeLimiter.Middleware(handler).ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for excluded /health, got %d", rr.Code)
		}
	})
}

func TestIPKeyFunc(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		expectedPrefix string
	}{
		{
			name:           "uses RemoteAddr when no headers",
			remoteAddr:     "192.168.1.1:12345",
			expectedPrefix: "192.168.1.1:12345",
		},
		{
			name:           "uses X-Forwarded-For when present",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "10.0.0.1, 10.0.0.2",
			expectedPrefix: "10.0.0.1",
		},
		{
			name:           "uses X-Real-IP when X-Forwarded-For absent",
			remoteAddr:     "192.168.1.1:12345",
			xRealIP:        "10.0.0.5",
			expectedPrefix: "10.0.0.5",
		},
		{
			name:           "prefers X-Forwarded-For over X-Real-IP",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "10.0.0.1",
			xRealIP:        "10.0.0.5",
			expectedPrefix: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			key := IPKeyFunc(req)
			if key != tt.expectedPrefix {
				t.Errorf("expected %s, got %s", tt.expectedPrefix, key)
			}
		})
	}
}

func TestPerEndpointRateLimiter(t *testing.T) {
	defaultConfig := RateLimitConfig{
		RequestsPerSecond: 0.001, // Very slow
		BurstSize:         1,
		KeyFunc:           IPKeyFunc,
		CleanupInterval:   1 * time.Hour,
		BucketTTL:         1 * time.Hour,
	}

	endpoints := []EndpointLimit{
		{Pattern: "/api/fast", RequestsPerSecond: 0.001, BurstSize: 5},
		{Pattern: "POST /api/special", RequestsPerSecond: 0.001, BurstSize: 3},
	}

	perl := NewPerEndpointRateLimiter(defaultConfig, endpoints)
	defer perl.Stop()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("uses default for unmatched endpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/other", nil)
		req.RemoteAddr = "1.1.1.1:1"
		rr := httptest.NewRecorder()

		perl.Middleware(handler).ServeHTTP(rr, req)

		if rr.Header().Get("X-RateLimit-Limit") != "1" {
			t.Errorf("expected default burst size 1, got %s", rr.Header().Get("X-RateLimit-Limit"))
		}
	})

	t.Run("uses endpoint-specific limit for matched path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/fast", nil)
		req.RemoteAddr = "2.2.2.2:1"
		rr := httptest.NewRecorder()

		perl.Middleware(handler).ServeHTTP(rr, req)

		if rr.Header().Get("X-RateLimit-Limit") != "5" {
			t.Errorf("expected burst size 5 for /api/fast, got %s", rr.Header().Get("X-RateLimit-Limit"))
		}
	})

	t.Run("uses endpoint-specific limit for method+path match", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/special", nil)
		req.RemoteAddr = "3.3.3.3:1"
		rr := httptest.NewRecorder()

		perl.Middleware(handler).ServeHTTP(rr, req)

		if rr.Header().Get("X-RateLimit-Limit") != "3" {
			t.Errorf("expected burst size 3 for POST /api/special, got %s", rr.Header().Get("X-RateLimit-Limit"))
		}
	})
}

func TestRateLimiterConcurrency(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         100,
		KeyFunc:           IPKeyFunc,
		CleanupInterval:   1 * time.Hour,
		BucketTTL:         1 * time.Hour,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Allow("concurrent-key")
		}()
	}
	wg.Wait()

	// Test should complete without race conditions
	// Use -race flag when running tests to verify
}
