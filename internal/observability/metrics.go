package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for ADP
type Metrics struct {
	// Session metrics
	SessionsActive  prometheus.Gauge
	SessionsTotal   *prometheus.CounterVec
	SessionDuration *prometheus.HistogramVec

	// Decision metrics
	DecisionsTotal         *prometheus.CounterVec
	DecisionConfidence     *prometheus.HistogramVec
	DecisionsLowConfidence prometheus.Counter

	// Policy metrics
	PolicyEvaluationsTotal    *prometheus.CounterVec
	PolicyEvaluationDuration  *prometheus.HistogramVec
	PolicyFalsePositivesTotal *prometheus.CounterVec

	// Escalation metrics
	EscalationsTotal         *prometheus.CounterVec
	EscalationResolutionTime *prometheus.HistogramVec
	EscalationsPending       prometheus.Gauge

	// Context metrics
	ContextRequestsTotal     *prometheus.CounterVec
	ContextTokensDelivered   *prometheus.CounterVec
	ContextRetrievalDuration *prometheus.HistogramVec
	ContextCacheHits         prometheus.Counter
	ContextCacheMisses       prometheus.Counter

	// Enforcement metrics
	CommitsPreparedTotal *prometheus.CounterVec
	CommitsVerifiedTotal prometheus.Counter
	CommitsRejectedTotal *prometheus.CounterVec

	// API metrics
	APIRequestsTotal   *prometheus.CounterVec
	APIRequestDuration *prometheus.HistogramVec

	// Compliance metrics
	AuditTrailCompleteness   prometheus.Gauge
	SensitivePathAccessTotal *prometheus.CounterVec
	OffHoursActivityTotal    *prometheus.CounterVec

	mu sync.RWMutex
}

// NewMetrics creates a new Metrics instance with all metrics registered
func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "adp"
	}

	m := &Metrics{
		// Session metrics
		SessionsActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "sessions_active",
			Help:      "Number of currently active agent sessions",
		}),
		SessionsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "sessions_total",
			Help:      "Total number of sessions by agent tool and trust level",
		}, []string{"agent_tool", "trust_level"}),
		SessionDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "session_duration_seconds",
			Help:      "Duration of agent sessions",
			Buckets:   []float64{60, 300, 600, 1800, 3600, 7200, 14400},
		}, []string{"agent_tool"}),

		// Decision metrics
		DecisionsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "decisions_total",
			Help:      "Total decisions by type, result, and agent tool",
		}, []string{"type", "result", "agent_tool"}),
		DecisionConfidence: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "decision_confidence_score",
			Help:      "Distribution of decision confidence scores",
			Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		}, []string{"agent_tool"}),
		DecisionsLowConfidence: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "decisions_low_confidence_total",
			Help:      "Total decisions with confidence below 0.7",
		}),

		// Policy metrics
		PolicyEvaluationsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "policy_evaluations_total",
			Help:      "Total policy evaluations by policy and result",
		}, []string{"policy", "result"}),
		PolicyEvaluationDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "policy_evaluation_duration_seconds",
			Help:      "Time to evaluate policies",
			Buckets:   prometheus.DefBuckets,
		}, []string{"policy"}),
		PolicyFalsePositivesTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "policy_false_positives_total",
			Help:      "Denials that were later approved on escalation",
		}, []string{"policy"}),

		// Escalation metrics
		EscalationsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "escalations_total",
			Help:      "Total escalations by policy and status",
		}, []string{"policy", "status"}),
		EscalationResolutionTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "escalation_resolution_duration_seconds",
			Help:      "Time to resolve escalations",
			Buckets:   []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800},
		}, []string{"policy"}),
		EscalationsPending: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "escalations_pending",
			Help:      "Number of pending escalations",
		}),

		// Context metrics
		ContextRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "context_requests_total",
			Help:      "Total context requests by layer",
		}, []string{"layer"}),
		ContextTokensDelivered: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "context_tokens_delivered_total",
			Help:      "Total tokens delivered by layer",
		}, []string{"layer"}),
		ContextRetrievalDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "context_retrieval_duration_seconds",
			Help:      "Time to retrieve context",
			Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"layer"}),
		ContextCacheHits: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "context_cache_hits_total",
			Help:      "Total context cache hits",
		}),
		ContextCacheMisses: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "context_cache_misses_total",
			Help:      "Total context cache misses",
		}),

		// Enforcement metrics
		CommitsPreparedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "commits_prepared_total",
			Help:      "Total commits prepared by status",
		}, []string{"status"}),
		CommitsVerifiedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "commits_verified_total",
			Help:      "Total commits verified",
		}),
		CommitsRejectedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "commits_rejected_total",
			Help:      "Total commits rejected by reason",
		}, []string{"reason"}),

		// API metrics
		APIRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "api_requests_total",
			Help:      "Total API requests by endpoint and status",
		}, []string{"endpoint", "method", "status"}),
		APIRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "api_request_duration_seconds",
			Help:      "API request duration by endpoint",
			Buckets:   prometheus.DefBuckets,
		}, []string{"endpoint", "method"}),

		// Compliance metrics
		AuditTrailCompleteness: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "audit_trail_completeness_ratio",
			Help:      "Ratio of commits with complete audit trails",
		}),
		SensitivePathAccessTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "sensitive_path_access_total",
			Help:      "Access to sensitive paths by pattern",
		}, []string{"path_pattern"}),
		OffHoursActivityTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "off_hours_activity_total",
			Help:      "Agent activity outside business hours by environment",
		}, []string{"environment"}),
	}

	return m
}

// RecordSessionStart records a new session starting
func (m *Metrics) RecordSessionStart(agentTool string, trustLevel int) {
	m.SessionsActive.Inc()
	m.SessionsTotal.WithLabelValues(agentTool, string(rune('0'+trustLevel))).Inc()
}

// RecordSessionEnd records a session ending
func (m *Metrics) RecordSessionEnd(agentTool string, duration time.Duration) {
	m.SessionsActive.Dec()
	m.SessionDuration.WithLabelValues(agentTool).Observe(duration.Seconds())
}

// RecordDecision records a decision
func (m *Metrics) RecordDecision(decisionType, result, agentTool string, confidence float64) {
	m.DecisionsTotal.WithLabelValues(decisionType, result, agentTool).Inc()
	m.DecisionConfidence.WithLabelValues(agentTool).Observe(confidence)
	if confidence < 0.7 {
		m.DecisionsLowConfidence.Inc()
	}
}

// RecordPolicyEvaluation records a policy evaluation
func (m *Metrics) RecordPolicyEvaluation(policy, result string, duration time.Duration) {
	m.PolicyEvaluationsTotal.WithLabelValues(policy, result).Inc()
	m.PolicyEvaluationDuration.WithLabelValues(policy).Observe(duration.Seconds())
}

// RecordPolicyFalsePositive records a policy false positive
func (m *Metrics) RecordPolicyFalsePositive(policy string) {
	m.PolicyFalsePositivesTotal.WithLabelValues(policy).Inc()
}

// RecordEscalation records an escalation
func (m *Metrics) RecordEscalation(policy, status string) {
	m.EscalationsTotal.WithLabelValues(policy, status).Inc()
	if status == "pending" {
		m.EscalationsPending.Inc()
	}
}

// RecordEscalationResolution records an escalation resolution
func (m *Metrics) RecordEscalationResolution(policy string, duration time.Duration) {
	m.EscalationResolutionTime.WithLabelValues(policy).Observe(duration.Seconds())
	m.EscalationsPending.Dec()
}

// RecordContextRequest records a context request
func (m *Metrics) RecordContextRequest(layer string, tokens int, duration time.Duration, cacheHit bool) {
	m.ContextRequestsTotal.WithLabelValues(layer).Inc()
	m.ContextTokensDelivered.WithLabelValues(layer).Add(float64(tokens))
	m.ContextRetrievalDuration.WithLabelValues(layer).Observe(duration.Seconds())
	if cacheHit {
		m.ContextCacheHits.Inc()
	} else {
		m.ContextCacheMisses.Inc()
	}
}

// RecordCommitPrepared records a commit preparation
func (m *Metrics) RecordCommitPrepared(status string) {
	m.CommitsPreparedTotal.WithLabelValues(status).Inc()
}

// RecordCommitVerified records a verified commit
func (m *Metrics) RecordCommitVerified() {
	m.CommitsVerifiedTotal.Inc()
}

// RecordCommitRejected records a rejected commit
func (m *Metrics) RecordCommitRejected(reason string) {
	m.CommitsRejectedTotal.WithLabelValues(reason).Inc()
}

// RecordAPIRequest records an API request
func (m *Metrics) RecordAPIRequest(endpoint, method string, status int, duration time.Duration) {
	statusClass := fmt.Sprintf("%dxx", status/100)
	m.APIRequestsTotal.WithLabelValues(endpoint, method, statusClass).Inc()
	m.APIRequestDuration.WithLabelValues(endpoint, method).Observe(duration.Seconds())
}

// RecordSensitiveAccess records access to sensitive paths
func (m *Metrics) RecordSensitiveAccess(pathPattern string) {
	m.SensitivePathAccessTotal.WithLabelValues(pathPattern).Inc()
}

// RecordOffHoursActivity records activity outside business hours
func (m *Metrics) RecordOffHoursActivity(environment string) {
	m.OffHoursActivityTotal.WithLabelValues(environment).Inc()
}

// SetAuditCompleteness sets the audit trail completeness ratio
func (m *Metrics) SetAuditCompleteness(ratio float64) {
	m.AuditTrailCompleteness.Set(ratio)
}

// SetPendingEscalations sets the pending escalations count
func (m *Metrics) SetPendingEscalations(count int) {
	m.EscalationsPending.Set(float64(count))
}

// MetricsMiddleware provides HTTP middleware for recording API metrics
type MetricsMiddleware struct {
	metrics *Metrics
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metrics *Metrics) *MetricsMiddleware {
	return &MetricsMiddleware{metrics: metrics}
}

// MetricsCollector periodically collects aggregate metrics
type MetricsCollector struct {
	metrics  *Metrics
	interval time.Duration
	stopCh   chan struct{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(metrics *Metrics, interval time.Duration) *MetricsCollector {
	return &MetricsCollector{
		metrics:  metrics,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins collecting metrics periodically
func (c *MetricsCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.collect(ctx)
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop stops the metrics collector
func (c *MetricsCollector) Stop() {
	close(c.stopCh)
}

func (c *MetricsCollector) collect(ctx context.Context) {
	// This would typically query the database for aggregate metrics
	// and update the Prometheus gauges
	// Implementation depends on access to data stores
}

// DefaultMetrics is the global metrics instance
var DefaultMetrics *Metrics

func init() {
	DefaultMetrics = NewMetrics("adp")
}

// GetMetrics returns the default metrics instance
func GetMetrics() *Metrics {
	return DefaultMetrics
}
