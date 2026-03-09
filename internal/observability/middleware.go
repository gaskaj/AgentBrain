package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware provides HTTP middleware for automatic instrumentation.
type HTTPMiddleware struct {
	tracingManager *TracingManager
	metricsManager *MetricsManager
	logger         *slog.Logger
}

// NewHTTPMiddleware creates a new HTTP middleware with observability instrumentation.
func NewHTTPMiddleware(tracingManager *TracingManager, metricsManager *MetricsManager, logger *slog.Logger) *HTTPMiddleware {
	return &HTTPMiddleware{
		tracingManager: tracingManager,
		metricsManager: metricsManager,
		logger:         logger,
	}
}

// Handler wraps an HTTP handler with observability instrumentation.
func (m *HTTPMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate correlation ID
		correlationID := uuid.New().String()
		
		// Add correlation ID to context
		ctx := context.WithValue(r.Context(), "correlation_id", correlationID)
		r = r.WithContext(ctx)
		
		// Add correlation ID to response headers
		w.Header().Set("X-Correlation-ID", correlationID)
		
		// Start tracing span
		spanName := fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path)
		ctx, span := m.tracingManager.StartSpan(ctx, spanName,
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.route", r.URL.Path),
				attribute.String("http.user_agent", r.UserAgent()),
				attribute.String("correlation_id", correlationID),
			),
		)
		defer span.End()
		
		r = r.WithContext(ctx)
		
		// Create response writer wrapper for status tracking
		ww := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		
		// Track request start time
		startTime := time.Now()
		
		// Execute request
		next.ServeHTTP(ww, r)
		
		// Calculate duration
		duration := time.Since(startTime)
		
		// Add final attributes to span
		span.SetAttributes(
			attribute.Int("http.status_code", ww.statusCode),
			attribute.Int64("http.response_size", int64(ww.responseSize)),
			attribute.Int64("duration_ms", duration.Milliseconds()),
		)
		
		// Set span status based on HTTP status
		if ww.statusCode >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", ww.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
		
		// Record metrics if metrics manager is available
		if m.metricsManager != nil {
			m.metricsManager.RecordHTTPRequest(r.Method, r.URL.Path, ww.statusCode, duration)
		}
		
		// Log request
		m.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"duration_ms", duration.Milliseconds(),
			"correlation_id", correlationID,
		)
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code and response size.
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode   int
	responseSize int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.responseSize += n
	return n, err
}

// SyncMiddleware provides middleware for sync operations.
type SyncMiddleware struct {
	tracingManager *TracingManager
	metricsManager *MetricsManager
	logger         *slog.Logger
}

// NewSyncMiddleware creates a new sync middleware.
func NewSyncMiddleware(tracingManager *TracingManager, metricsManager *MetricsManager, logger *slog.Logger) *SyncMiddleware {
	return &SyncMiddleware{
		tracingManager: tracingManager,
		metricsManager: metricsManager,
		logger:         logger,
	}
}

// WrapOperation wraps a sync operation with observability instrumentation.
func (m *SyncMiddleware) WrapOperation(operation string) func(func(context.Context) error) func(context.Context) error {
	return func(fn func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) error {
			// Generate correlation ID if not present
			correlationID, ok := ctx.Value("correlation_id").(string)
			if !ok {
				correlationID = uuid.New().String()
				ctx = context.WithValue(ctx, "correlation_id", correlationID)
			}
			
			// Start tracing span
			spanName := fmt.Sprintf("sync.%s", operation)
			ctx, span := m.tracingManager.StartSpan(ctx, spanName,
				trace.WithAttributes(
					attribute.String("sync.operation", operation),
					attribute.String("correlation_id", correlationID),
				),
			)
			defer span.End()
			
			// Track operation start time
			startTime := time.Now()
			
			// Execute operation
			err := fn(ctx)
			
			// Calculate duration
			duration := time.Since(startTime)
			
			// Add duration to span
			span.SetAttributes(attribute.Int64("duration_ms", duration.Milliseconds()))
			
			if err != nil {
				// Record error
				m.tracingManager.RecordError(span, err)
				
				// Record metrics
				if m.metricsManager != nil {
					m.metricsManager.RecordSyncOperation(operation, false, duration)
				}
				
				// Log error
				m.logger.Error("sync operation failed",
					"operation", operation,
					"duration_ms", duration.Milliseconds(),
					"error", err.Error(),
					"correlation_id", correlationID,
				)
				
				return err
			}
			
			// Record success
			m.tracingManager.FinishSpan(span)
			
			// Record metrics
			if m.metricsManager != nil {
				m.metricsManager.RecordSyncOperation(operation, true, duration)
			}
			
			// Log success
			m.logger.Info("sync operation completed",
				"operation", operation,
				"duration_ms", duration.Milliseconds(),
				"correlation_id", correlationID,
			)
			
			return nil
		}
	}
}

// ConnectorMiddleware provides middleware for connector operations.
type ConnectorMiddleware struct {
	tracingManager *TracingManager
	metricsManager *MetricsManager
	connectorName  string
	logger         *slog.Logger
}

// NewConnectorMiddleware creates a new connector middleware.
func NewConnectorMiddleware(tracingManager *TracingManager, metricsManager *MetricsManager, connectorName string, logger *slog.Logger) *ConnectorMiddleware {
	return &ConnectorMiddleware{
		tracingManager: tracingManager,
		metricsManager: metricsManager,
		connectorName:  connectorName,
		logger:         logger,
	}
}

// WrapAPICall wraps a connector API call with observability instrumentation.
func (m *ConnectorMiddleware) WrapAPICall(operation, method string) func(func(context.Context) error) func(context.Context) error {
	return func(fn func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) error {
			// Get correlation ID from context
			correlationID, ok := ctx.Value("correlation_id").(string)
			if !ok {
				correlationID = uuid.New().String()
				ctx = context.WithValue(ctx, "correlation_id", correlationID)
			}
			
			// Start tracing span
			ctx, span := m.tracingManager.StartConnectorSpan(ctx, operation, m.connectorName, method)
			defer span.End()
			
			// Track operation start time
			startTime := time.Now()
			
			// Execute operation
			err := fn(ctx)
			
			// Calculate duration
			duration := time.Since(startTime)
			
			// Add duration to span
			span.SetAttributes(attribute.Int64("duration_ms", duration.Milliseconds()))
			
			if err != nil {
				// Record error
				m.tracingManager.RecordError(span, err)
				
				// Record metrics
				if m.metricsManager != nil {
					m.metricsManager.RecordConnectorOperation(m.connectorName, operation, false, duration)
				}
				
				// Log error
				m.logger.Error("connector operation failed",
					"connector", m.connectorName,
					"operation", operation,
					"method", method,
					"duration_ms", duration.Milliseconds(),
					"error", err.Error(),
					"correlation_id", correlationID,
				)
				
				return err
			}
			
			// Record success
			m.tracingManager.FinishSpan(span)
			
			// Record metrics
			if m.metricsManager != nil {
				m.metricsManager.RecordConnectorOperation(m.connectorName, operation, true, duration)
			}
			
			// Log success
			m.logger.Debug("connector operation completed",
				"connector", m.connectorName,
				"operation", operation,
				"method", method,
				"duration_ms", duration.Milliseconds(),
				"correlation_id", correlationID,
			)
			
			return nil
		}
	}
}

// GetCorrelationID extracts correlation ID from context.
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value("correlation_id").(string); ok {
		return id
	}
	return ""
}

// WithCorrelationID adds a correlation ID to the context.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, "correlation_id", correlationID)
}