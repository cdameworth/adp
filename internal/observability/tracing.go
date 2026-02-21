// Package observability provides distributed tracing with OpenTelemetry for ADP.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingConfig holds configuration for distributed tracing
type TracingConfig struct {
	// ServiceName is the name of this service
	ServiceName string
	// ServiceVersion is the version of this service
	ServiceVersion string
	// Environment is the deployment environment (development, staging, production)
	Environment string
	// Enabled controls whether tracing is enabled
	Enabled bool
	// ExporterType is the type of exporter (otlp-grpc, otlp-http, stdout)
	ExporterType string
	// Endpoint is the OTLP collector endpoint
	Endpoint string
	// Insecure disables TLS for the exporter
	Insecure bool
	// SampleRate is the sampling rate (0.0 to 1.0)
	SampleRate float64
	// Headers for OTLP exporter authentication
	Headers map[string]string
	// Timeout for exporter operations
	Timeout time.Duration
}

// DefaultTracingConfig returns default tracing configuration
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{
		ServiceName:    "adp",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		Enabled:        true,
		ExporterType:   "stdout",
		SampleRate:     1.0,
		Timeout:        10 * time.Second,
	}
}

// TracingProvider manages the OpenTelemetry tracing provider
type TracingProvider struct {
	provider *sdktrace.TracerProvider
	config   TracingConfig
}

// InitTracing initializes the tracing provider
func InitTracing(ctx context.Context, cfg TracingConfig) (*TracingProvider, error) {
	if !cfg.Enabled {
		return &TracingProvider{config: cfg}, nil
	}

	// Create exporter
	exporter, err := createExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
			attribute.String("service.namespace", "adp"),
		),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create sampler
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global provider and propagator
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracingProvider{
		provider: provider,
		config:   cfg,
	}, nil
}

// createExporter creates the appropriate exporter based on configuration
func createExporter(ctx context.Context, cfg TracingConfig) (sdktrace.SpanExporter, error) {
	switch cfg.ExporterType {
	case "otlp-grpc":
		return createOTLPGRPCExporter(ctx, cfg)
	case "otlp-http":
		return createOTLPHTTPExporter(ctx, cfg)
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	default:
		return nil, fmt.Errorf("unknown exporter type: %s", cfg.ExporterType)
	}
}

func createOTLPGRPCExporter(ctx context.Context, cfg TracingConfig) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.Timeout),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}

	return otlptracegrpc.New(ctx, opts...)
}

func createOTLPHTTPExporter(ctx context.Context, cfg TracingConfig) (*otlptrace.Exporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithTimeout(cfg.Timeout),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}

	return otlptracehttp.New(ctx, opts...)
}

// Shutdown gracefully shuts down the tracing provider
func (p *TracingProvider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

// Tracer returns a tracer for the given name
func (p *TracingProvider) Tracer(name string) trace.Tracer {
	if p.provider == nil {
		return otel.Tracer(name)
	}
	return p.provider.Tracer(name)
}

// SpanOptions holds options for creating spans
type SpanOptions struct {
	Kind       trace.SpanKind
	Attributes []attribute.KeyValue
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...SpanOptions) (context.Context, trace.Span) {
	spanOpts := []trace.SpanStartOption{}

	for _, opt := range opts {
		if opt.Kind != 0 {
			spanOpts = append(spanOpts, trace.WithSpanKind(opt.Kind))
		}
		if len(opt.Attributes) > 0 {
			spanOpts = append(spanOpts, trace.WithAttributes(opt.Attributes...))
		}
	}

	return tracer.Start(ctx, name, spanOpts...)
}

// EndSpanWithError ends a span and records an error if present
func EndSpanWithError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// AddSpanAttributes adds attributes to the current span
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// HTTPTracingMiddleware returns middleware that traces HTTP requests
func HTTPTracingMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract context from incoming request
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span
			ctx, span := tracer.Start(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPMethod(r.Method),
					semconv.HTTPURL(r.URL.String()),
					semconv.HTTPRoute(r.URL.Path),
					semconv.NetHostName(r.Host),
					attribute.String("http.user_agent", r.UserAgent()),
					attribute.String("http.client_ip", r.RemoteAddr),
				),
			)
			defer span.End()

			// Wrap response writer to capture status code
			rw := &responseWriterWithStatus{ResponseWriter: w, statusCode: http.StatusOK}

			// Serve request
			next.ServeHTTP(rw, r.WithContext(ctx))

			// Record response attributes
			span.SetAttributes(
				semconv.HTTPStatusCode(rw.statusCode),
			)

			if rw.statusCode >= 400 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", rw.statusCode))
			}
		})
	}
}

type responseWriterWithStatus struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWithStatus) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Common ADP span attributes

// SessionSpanAttributes returns common attributes for session-related spans
func SessionSpanAttributes(sessionID, orgID, userID, tool string, trustLevel int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("adp.session.id", sessionID),
		attribute.String("adp.organization.id", orgID),
		attribute.String("adp.user.id", userID),
		attribute.String("adp.agent.tool", tool),
		attribute.Int("adp.trust.level", trustLevel),
	}
}

// PolicySpanAttributes returns common attributes for policy-related spans
func PolicySpanAttributes(policyName, result string, evaluationTime time.Duration) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("adp.policy.name", policyName),
		attribute.String("adp.policy.result", result),
		attribute.Int64("adp.policy.evaluation_ms", evaluationTime.Milliseconds()),
	}
}

// DecisionSpanAttributes returns common attributes for decision-related spans
func DecisionSpanAttributes(decisionID, decisionType, result string, confidence float64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("adp.decision.id", decisionID),
		attribute.String("adp.decision.type", decisionType),
		attribute.String("adp.decision.result", result),
		attribute.Float64("adp.decision.confidence", confidence),
	}
}

// ContextSpanAttributes returns common attributes for context-related spans
func ContextSpanAttributes(layer string, tokens int, retrievalTime time.Duration, cacheHit bool) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("adp.context.layer", layer),
		attribute.Int("adp.context.tokens", tokens),
		attribute.Int64("adp.context.retrieval_ms", retrievalTime.Milliseconds()),
		attribute.Bool("adp.context.cache_hit", cacheHit),
	}
}

// EscalationSpanAttributes returns common attributes for escalation-related spans
func EscalationSpanAttributes(escalationID, policyName, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("adp.escalation.id", escalationID),
		attribute.String("adp.escalation.policy", policyName),
		attribute.String("adp.escalation.status", status),
	}
}

// DatabaseSpanAttributes returns common attributes for database operations
func DatabaseSpanAttributes(operation, table string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("db.operation", operation),
		attribute.String("db.table", table),
	}
}

// SpanFromContext returns the span from the given context
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan returns a new context with the given span
func ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}

// TraceID returns the trace ID from the context as a string
func TraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// SpanID returns the span ID from the context as a string
func SpanID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasSpanID() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}
