// Package observability provides health checks and operational monitoring for ADP.
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HealthStatus represents the overall health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// ComponentHealth represents the health of a single component
type ComponentHealth struct {
	Name        string                 `json:"name"`
	Status      HealthStatus           `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Latency     time.Duration          `json:"latency,omitempty"`
	LastChecked time.Time              `json:"last_checked"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// OverallHealth represents the overall system health
type OverallHealth struct {
	Status     HealthStatus               `json:"status"`
	Version    string                     `json:"version"`
	Uptime     time.Duration              `json:"uptime"`
	StartTime  time.Time                  `json:"start_time"`
	Timestamp  time.Time                  `json:"timestamp"`
	Components map[string]ComponentHealth `json:"components"`
}

// HealthChecker performs health checks on various system components
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) ComponentHealth
	IsRequired() bool // If true, failure marks the system as unhealthy
}

// HealthService manages health checks and status
type HealthService struct {
	checkers  []HealthChecker
	startTime time.Time
	version   string
	cache     *healthCache
	checkMu   sync.RWMutex
}

type healthCache struct {
	health    *OverallHealth
	timestamp time.Time
	ttl       time.Duration
}

// HealthServiceConfig holds configuration for the health service
type HealthServiceConfig struct {
	Version      string
	CacheTTL     time.Duration
	CheckTimeout time.Duration
}

// NewHealthService creates a new health service
func NewHealthService(cfg HealthServiceConfig) *HealthService {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Second
	}
	return &HealthService{
		checkers:  []HealthChecker{},
		startTime: time.Now(),
		version:   cfg.Version,
		cache: &healthCache{
			ttl: cfg.CacheTTL,
		},
	}
}

// RegisterChecker registers a health checker
func (s *HealthService) RegisterChecker(checker HealthChecker) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	s.checkers = append(s.checkers, checker)
}

// Check performs health checks on all registered components
func (s *HealthService) Check(ctx context.Context) *OverallHealth {
	// Check cache first
	s.checkMu.RLock()
	if s.cache.health != nil && time.Since(s.cache.timestamp) < s.cache.ttl {
		health := *s.cache.health
		s.checkMu.RUnlock()
		return &health
	}
	s.checkMu.RUnlock()

	// Perform checks
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	// Double-check cache after acquiring write lock
	if s.cache.health != nil && time.Since(s.cache.timestamp) < s.cache.ttl {
		health := *s.cache.health
		return &health
	}

	health := &OverallHealth{
		Status:     HealthStatusHealthy,
		Version:    s.version,
		Uptime:     time.Since(s.startTime),
		StartTime:  s.startTime,
		Timestamp:  time.Now(),
		Components: make(map[string]ComponentHealth),
	}

	// Run all health checks concurrently
	var wg sync.WaitGroup
	results := make(chan ComponentHealth, len(s.checkers))

	for _, checker := range s.checkers {
		wg.Add(1)
		go func(c HealthChecker) {
			defer wg.Done()
			// Create timeout context for individual check
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			results <- c.Check(checkCtx)
		}(checker)
	}

	// Close results channel when all checks complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	hasRequiredFailure := false
	hasDegradation := false

	for result := range results {
		health.Components[result.Name] = result

		// Find if this checker is required
		var isRequired bool
		for _, c := range s.checkers {
			if c.Name() == result.Name {
				isRequired = c.IsRequired()
				break
			}
		}

		switch result.Status {
		case HealthStatusUnhealthy:
			if isRequired {
				hasRequiredFailure = true
			} else {
				hasDegradation = true
			}
		case HealthStatusDegraded:
			hasDegradation = true
		}
	}

	// Determine overall status
	if hasRequiredFailure {
		health.Status = HealthStatusUnhealthy
	} else if hasDegradation {
		health.Status = HealthStatusDegraded
	}

	// Update cache
	s.cache.health = health
	s.cache.timestamp = time.Now()

	return health
}

// LivenessHandler returns an HTTP handler for liveness probes
func (s *HealthService) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "alive",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// ReadinessHandler returns an HTTP handler for readiness probes
func (s *HealthService) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := s.Check(r.Context())

		w.Header().Set("Content-Type", "application/json")

		if health.Status == HealthStatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     health.Status,
			"components": health.Components,
			"timestamp":  time.Now().Format(time.RFC3339),
		})
	}
}

// HealthHandler returns an HTTP handler for detailed health information
func (s *HealthService) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := s.Check(r.Context())

		w.Header().Set("Content-Type", "application/json")

		switch health.Status {
		case HealthStatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		case HealthStatusDegraded:
			w.WriteHeader(http.StatusOK) // Still serving, but degraded
		default:
			w.WriteHeader(http.StatusOK)
		}

		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		encoder.Encode(health)
	}
}

// PostgresHealthChecker checks PostgreSQL connectivity
type PostgresHealthChecker struct {
	name     string
	required bool
	pinger   Pinger
}

// Pinger interface for database ping operations
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewPostgresHealthChecker creates a PostgreSQL health checker
func NewPostgresHealthChecker(name string, required bool, pinger Pinger) *PostgresHealthChecker {
	return &PostgresHealthChecker{
		name:     name,
		required: required,
		pinger:   pinger,
	}
}

func (c *PostgresHealthChecker) Name() string     { return c.name }
func (c *PostgresHealthChecker) IsRequired() bool { return c.required }

func (c *PostgresHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.pinger.Ping(ctx); err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("ping failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "connected"
	}

	result.Latency = time.Since(start)
	return result
}

// RedisHealthChecker checks Redis connectivity
type RedisHealthChecker struct {
	name     string
	required bool
	pinger   Pinger
}

// NewRedisHealthChecker creates a Redis health checker
func NewRedisHealthChecker(name string, required bool, pinger Pinger) *RedisHealthChecker {
	return &RedisHealthChecker{
		name:     name,
		required: required,
		pinger:   pinger,
	}
}

func (c *RedisHealthChecker) Name() string     { return c.name }
func (c *RedisHealthChecker) IsRequired() bool { return c.required }

func (c *RedisHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.pinger.Ping(ctx); err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("ping failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "connected"
	}

	result.Latency = time.Since(start)
	return result
}

// Neo4jHealthChecker checks Neo4j connectivity
type Neo4jHealthChecker struct {
	name     string
	required bool
	checker  func(ctx context.Context) error
}

// NewNeo4jHealthChecker creates a Neo4j health checker
func NewNeo4jHealthChecker(name string, required bool, checker func(ctx context.Context) error) *Neo4jHealthChecker {
	return &Neo4jHealthChecker{
		name:     name,
		required: required,
		checker:  checker,
	}
}

func (c *Neo4jHealthChecker) Name() string     { return c.name }
func (c *Neo4jHealthChecker) IsRequired() bool { return c.required }

func (c *Neo4jHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.checker(ctx); err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "connected"
	}

	result.Latency = time.Since(start)
	return result
}

// QdrantHealthChecker checks Qdrant connectivity
type QdrantHealthChecker struct {
	name     string
	required bool
	checker  func(ctx context.Context) error
}

// NewQdrantHealthChecker creates a Qdrant health checker
func NewQdrantHealthChecker(name string, required bool, checker func(ctx context.Context) error) *QdrantHealthChecker {
	return &QdrantHealthChecker{
		name:     name,
		required: required,
		checker:  checker,
	}
}

func (c *QdrantHealthChecker) Name() string     { return c.name }
func (c *QdrantHealthChecker) IsRequired() bool { return c.required }

func (c *QdrantHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.checker(ctx); err != nil {
		result.Status = HealthStatusDegraded // Qdrant may not be required for all operations
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "connected"
	}

	result.Latency = time.Since(start)
	return result
}

// ClickHouseHealthChecker checks ClickHouse connectivity
type ClickHouseHealthChecker struct {
	name     string
	required bool
	checker  func(ctx context.Context) error
}

// NewClickHouseHealthChecker creates a ClickHouse health checker
func NewClickHouseHealthChecker(name string, required bool, checker func(ctx context.Context) error) *ClickHouseHealthChecker {
	return &ClickHouseHealthChecker{
		name:     name,
		required: required,
		checker:  checker,
	}
}

func (c *ClickHouseHealthChecker) Name() string     { return c.name }
func (c *ClickHouseHealthChecker) IsRequired() bool { return c.required }

func (c *ClickHouseHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.checker(ctx); err != nil {
		result.Status = HealthStatusDegraded // ClickHouse only affects analytics
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "connected"
	}

	result.Latency = time.Since(start)
	return result
}

// OPAHealthChecker checks OPA policy engine connectivity
type OPAHealthChecker struct {
	name     string
	required bool
	checker  func(ctx context.Context) error
}

// NewOPAHealthChecker creates an OPA health checker
func NewOPAHealthChecker(name string, required bool, checker func(ctx context.Context) error) *OPAHealthChecker {
	return &OPAHealthChecker{
		name:     name,
		required: required,
		checker:  checker,
	}
}

func (c *OPAHealthChecker) Name() string     { return c.name }
func (c *OPAHealthChecker) IsRequired() bool { return c.required }

func (c *OPAHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
	}

	if err := c.checker(ctx); err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "ready"
	}

	result.Latency = time.Since(start)
	return result
}

// DiskSpaceHealthChecker checks available disk space
type DiskSpaceHealthChecker struct {
	name          string
	required      bool
	path          string
	minFreeBytes  uint64
	warnFreeBytes uint64
	checker       func(path string) (uint64, error)
}

// NewDiskSpaceHealthChecker creates a disk space health checker
func NewDiskSpaceHealthChecker(name string, required bool, path string, minFreeGB, warnFreeGB float64, checker func(path string) (uint64, error)) *DiskSpaceHealthChecker {
	return &DiskSpaceHealthChecker{
		name:          name,
		required:      required,
		path:          path,
		minFreeBytes:  uint64(minFreeGB * 1024 * 1024 * 1024),
		warnFreeBytes: uint64(warnFreeGB * 1024 * 1024 * 1024),
		checker:       checker,
	}
}

func (c *DiskSpaceHealthChecker) Name() string     { return c.name }
func (c *DiskSpaceHealthChecker) IsRequired() bool { return c.required }

func (c *DiskSpaceHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
		Details:     make(map[string]interface{}),
	}

	freeBytes, err := c.checker(c.path)
	if err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		result.Details["free_bytes"] = freeBytes
		result.Details["free_gb"] = float64(freeBytes) / 1024 / 1024 / 1024

		if freeBytes < c.minFreeBytes {
			result.Status = HealthStatusUnhealthy
			result.Message = fmt.Sprintf("disk space critically low: %.2f GB free", float64(freeBytes)/1024/1024/1024)
		} else if freeBytes < c.warnFreeBytes {
			result.Status = HealthStatusDegraded
			result.Message = fmt.Sprintf("disk space low: %.2f GB free", float64(freeBytes)/1024/1024/1024)
		} else {
			result.Status = HealthStatusHealthy
			result.Message = fmt.Sprintf("%.2f GB free", float64(freeBytes)/1024/1024/1024)
		}
	}

	result.Latency = time.Since(start)
	return result
}

// MemoryHealthChecker checks memory usage
type MemoryHealthChecker struct {
	name           string
	required       bool
	maxUsageRatio  float64
	warnUsageRatio float64
	checker        func() (used, total uint64, err error)
}

// NewMemoryHealthChecker creates a memory health checker
func NewMemoryHealthChecker(name string, required bool, maxUsageRatio, warnUsageRatio float64, checker func() (used, total uint64, err error)) *MemoryHealthChecker {
	return &MemoryHealthChecker{
		name:           name,
		required:       required,
		maxUsageRatio:  maxUsageRatio,
		warnUsageRatio: warnUsageRatio,
		checker:        checker,
	}
}

func (c *MemoryHealthChecker) Name() string     { return c.name }
func (c *MemoryHealthChecker) IsRequired() bool { return c.required }

func (c *MemoryHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	result := ComponentHealth{
		Name:        c.name,
		LastChecked: time.Now(),
		Details:     make(map[string]interface{}),
	}

	used, total, err := c.checker()
	if err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("check failed: %v", err)
	} else {
		usageRatio := float64(used) / float64(total)
		result.Details["used_bytes"] = used
		result.Details["total_bytes"] = total
		result.Details["usage_ratio"] = usageRatio

		if usageRatio > c.maxUsageRatio {
			result.Status = HealthStatusUnhealthy
			result.Message = fmt.Sprintf("memory usage critical: %.1f%%", usageRatio*100)
		} else if usageRatio > c.warnUsageRatio {
			result.Status = HealthStatusDegraded
			result.Message = fmt.Sprintf("memory usage high: %.1f%%", usageRatio*100)
		} else {
			result.Status = HealthStatusHealthy
			result.Message = fmt.Sprintf("memory usage: %.1f%%", usageRatio*100)
		}
	}

	result.Latency = time.Since(start)
	return result
}
