package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TracingConfig holds the configuration for distributed tracing.
type TracingConfig struct {
	Enabled    bool    `yaml:"enabled"`
	Exporter   string  `yaml:"exporter"`   // "jaeger", "zipkin", "otlp"
	Endpoint   string  `yaml:"endpoint"`
	SampleRate float64 `yaml:"sample_rate"`
	ServiceName string  `yaml:"service_name"`
	ServiceVersion string `yaml:"service_version"`
}

// TracingManager manages OpenTelemetry distributed tracing.
type TracingManager struct {
	config       TracingConfig
	tracer       trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	logger       *slog.Logger
	mu           sync.RWMutex
	shutdown     func(context.Context) error
}

// NewTracingManager creates a new tracing manager with the given configuration.
func NewTracingManager(config TracingConfig, logger *slog.Logger) (*TracingManager, error) {
	if !config.Enabled {
		return &TracingManager{
			config: config,
			logger: logger,
			tracer: trace.NewNoopTracerProvider().Tracer("noop"),
		}, nil
	}

	// Set default values
	if config.ServiceName == "" {
		config.ServiceName = "agentbrain"
	}
	if config.ServiceVersion == "" {
		config.ServiceVersion = "unknown"
	}
	if config.SampleRate == 0 {
		config.SampleRate = 0.1
	}

	tm := &TracingManager{
		config: config,
		logger: logger,
	}

	if err := tm.initializeTracing(); err != nil {
		return nil, fmt.Errorf("initialize tracing: %w", err)
	}

	return tm, nil
}

// initializeTracing sets up the OpenTelemetry tracing pipeline.
func (tm *TracingManager) initializeTracing() error {
	// Create resource with service information
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", tm.config.ServiceName),
			attribute.String("service.version", tm.config.ServiceVersion),
		),
	)
	if err != nil {
		return fmt.Errorf("create resource: %w", err)
	}

	// Create exporter based on configuration
	var exporter sdktrace.SpanExporter
	switch tm.config.Exporter {
	case "jaeger":
		exporter, err = tm.createJaegerExporter()
	case "zipkin":
		exporter, err = tm.createZipkinExporter()
	case "otlp":
		exporter, err = tm.createOTLPExporter()
	default:
		return fmt.Errorf("unsupported exporter: %s", tm.config.Exporter)
	}

	if err != nil {
		return fmt.Errorf("create exporter: %w", err)
	}

	// Create tracer provider with sampling
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(tm.config.SampleRate)),
	)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tm.tracerProvider = tp
	tm.tracer = tp.Tracer("agentbrain")
	tm.shutdown = tp.Shutdown

	tm.logger.Info("tracing initialized",
		"exporter", tm.config.Exporter,
		"endpoint", tm.config.Endpoint,
		"sample_rate", tm.config.SampleRate,
	)

	return nil
}

// createJaegerExporter creates a Jaeger exporter.
func (tm *TracingManager) createJaegerExporter() (sdktrace.SpanExporter, error) {
	endpoint := tm.config.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:14268/api/traces"
	}

	return jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(endpoint)))
}

// createZipkinExporter creates a Zipkin exporter.
func (tm *TracingManager) createZipkinExporter() (sdktrace.SpanExporter, error) {
	endpoint := tm.config.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:9411/api/v2/spans"
	}

	return zipkin.New(endpoint)
}

// createOTLPExporter creates an OTLP exporter.
func (tm *TracingManager) createOTLPExporter() (sdktrace.SpanExporter, error) {
	endpoint := tm.config.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:4318/v1/traces"
	}

	return otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
}

// StartSpan starts a new span with the given name and options.
func (tm *TracingManager) StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tm.tracer.Start(ctx, spanName, opts...)
}

// StartSyncSpan starts a span for a sync operation with common attributes.
func (tm *TracingManager) StartSyncSpan(ctx context.Context, operation, source, object string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("sync.%s", operation)
	ctx, span := tm.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("sync.operation", operation),
			attribute.String("sync.source", source),
			attribute.String("sync.object", object),
			attribute.String("component", "sync_engine"),
		),
	)
	return ctx, span
}

// StartConnectorSpan starts a span for a connector operation.
func (tm *TracingManager) StartConnectorSpan(ctx context.Context, operation, connector, method string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("connector.%s.%s", connector, operation)
	ctx, span := tm.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("connector.name", connector),
			attribute.String("connector.operation", operation),
			attribute.String("connector.method", method),
			attribute.String("component", "connector"),
		),
	)
	return ctx, span
}

// StartStorageSpan starts a span for a storage operation.
func (tm *TracingManager) StartStorageSpan(ctx context.Context, operation, storageType string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("storage.%s", operation)
	ctx, span := tm.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("storage.operation", operation),
			attribute.String("storage.type", storageType),
			attribute.String("component", "storage"),
		),
	)
	return ctx, span
}

// AddSpanAttributes adds attributes to the current span.
func (tm *TracingManager) AddSpanAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	span.SetAttributes(attrs...)
}

// AddSpanEvent adds an event to the current span.
func (tm *TracingManager) AddSpanEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// RecordError records an error on the span and marks it as failed.
func (tm *TracingManager) RecordError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// FinishSpan finishes the span with success status.
func (tm *TracingManager) FinishSpan(span trace.Span) {
	span.SetStatus(codes.Ok, "")
	span.End()
}

// FinishSpanWithError finishes the span with error status.
func (tm *TracingManager) FinishSpanWithError(span trace.Span, err error) {
	tm.RecordError(span, err)
	span.End()
}

// InjectTraceContext injects trace context into a map (for HTTP headers, etc.).
func (tm *TracingManager) InjectTraceContext(ctx context.Context, carrier map[string]string) {
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.MapCarrier(carrier))
}

// ExtractTraceContext extracts trace context from a map.
func (tm *TracingManager) ExtractTraceContext(ctx context.Context, carrier map[string]string) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.MapCarrier(carrier))
}

// GetTracer returns the configured tracer.
func (tm *TracingManager) GetTracer() trace.Tracer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tracer
}

// IsEnabled returns whether tracing is enabled.
func (tm *TracingManager) IsEnabled() bool {
	return tm.config.Enabled
}

// Shutdown gracefully shuts down the tracing system.
func (tm *TracingManager) Shutdown(ctx context.Context) error {
	if tm.shutdown != nil {
		tm.logger.Info("shutting down tracing system")
		return tm.shutdown(ctx)
	}
	return nil
}

// TraceOperation wraps an operation with automatic span creation and error handling.
func (tm *TracingManager) TraceOperation(ctx context.Context, operationName string, operation func(ctx context.Context, span trace.Span) error) error {
	ctx, span := tm.StartSpan(ctx, operationName)
	defer span.End()

	if err := operation(ctx, span); err != nil {
		tm.RecordError(span, err)
		return err
	}

	tm.FinishSpan(span)
	return nil
}

// TraceSyncOperation wraps a sync operation with automatic span creation.
func (tm *TracingManager) TraceSyncOperation(ctx context.Context, operation, source, object string, fn func(ctx context.Context, span trace.Span) error) error {
	ctx, span := tm.StartSyncSpan(ctx, operation, source, object)
	defer span.End()

	startTime := time.Now()
	defer func() {
		span.SetAttributes(attribute.Int64("duration_ms", time.Since(startTime).Milliseconds()))
	}()

	if err := fn(ctx, span); err != nil {
		tm.RecordError(span, err)
		return err
	}

	tm.FinishSpan(span)
	return nil
}

// TraceConnectorOperation wraps a connector operation with automatic span creation.
func (tm *TracingManager) TraceConnectorOperation(ctx context.Context, operation, connector, method string, fn func(ctx context.Context, span trace.Span) error) error {
	ctx, span := tm.StartConnectorSpan(ctx, operation, connector, method)
	defer span.End()

	startTime := time.Now()
	defer func() {
		span.SetAttributes(attribute.Int64("duration_ms", time.Since(startTime).Milliseconds()))
	}()

	if err := fn(ctx, span); err != nil {
		tm.RecordError(span, err)
		return err
	}

	tm.FinishSpan(span)
	return nil
}

// TraceStorageOperation wraps a storage operation with automatic span creation.
func (tm *TracingManager) TraceStorageOperation(ctx context.Context, operation, storageType string, fn func(ctx context.Context, span trace.Span) error) error {
	ctx, span := tm.StartStorageSpan(ctx, operation, storageType)
	defer span.End()

	startTime := time.Now()
	defer func() {
		span.SetAttributes(attribute.Int64("duration_ms", time.Since(startTime).Milliseconds()))
	}()

	if err := fn(ctx, span); err != nil {
		tm.RecordError(span, err)
		return err
	}

	tm.FinishSpan(span)
	return nil
}