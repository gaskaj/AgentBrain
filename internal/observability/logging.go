package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// SetupLogging configures the global slog logger.
func SetupLogging(level, format string) *slog.Logger {
	lvl := parseLevel(level)

	var handler slog.Handler
	var w io.Writer = os.Stdout

	opts := &slog.HandlerOptions{Level: lvl}

	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		handler = slog.NewJSONHandler(w, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// GetMonitoringLogger returns a logger specifically for monitoring components
func GetMonitoringLogger() *slog.Logger {
	return slog.With("component", "monitoring")
}

// GetHealthLogger returns a logger specifically for health check operations
func GetHealthLogger() *slog.Logger {
	return slog.With("component", "health")
}

// GetAlertingLogger returns a logger specifically for alerting operations
func GetAlertingLogger() *slog.Logger {
	return slog.With("component", "alerting")
}

// GetRetryLogger returns a logger specifically for retry operations
func GetRetryLogger() *slog.Logger {
	return slog.With("component", "retry")
}

// LogRetryAttempt logs a retry attempt with structured context
func LogRetryAttempt(logger *slog.Logger, operation string, attempt int, err error, delay time.Duration) {
	logger.Warn("retry attempt failed, will retry",
		"operation", operation,
		"attempt", attempt,
		"error", err.Error(),
		"next_delay", delay,
		"retryable", true)
}

// LogRetrySuccess logs a successful retry operation
func LogRetrySuccess(logger *slog.Logger, operation string, totalAttempts int, totalDuration time.Duration) {
	logger.Info("retry operation succeeded",
		"operation", operation,
		"total_attempts", totalAttempts,
		"total_duration", totalDuration)
}

// LogRetryFailure logs a retry operation that failed after all attempts
func LogRetryFailure(logger *slog.Logger, operation string, totalAttempts int, totalDuration time.Duration, finalError error) {
	logger.Error("retry operation failed after all attempts",
		"operation", operation,
		"total_attempts", totalAttempts,
		"total_duration", totalDuration,
		"final_error", finalError.Error())
}

// LogCircuitBreakerStateChange logs circuit breaker state changes
func LogCircuitBreakerStateChange(logger *slog.Logger, name string, from, to string, requestCount int64, failureRate float64) {
	logger.Info("circuit breaker state changed",
		"name", name,
		"from_state", from,
		"to_state", to,
		"request_count", requestCount,
		"failure_rate", failureRate)
}

// LogCircuitBreakerTrip logs when a circuit breaker trips to open state
func LogCircuitBreakerTrip(logger *slog.Logger, name string, failureThreshold int, currentFailures int64) {
	logger.Warn("circuit breaker opened due to failures",
		"name", name,
		"failure_threshold", failureThreshold,
		"current_failures", currentFailures)
}

// LogCircuitBreakerReset logs when a circuit breaker resets to closed state
func LogCircuitBreakerReset(logger *slog.Logger, name string) {
	logger.Info("circuit breaker closed after successful execution",
		"name", name)
}

// LogRetryPolicyApplication logs when a retry policy is applied to an operation
func LogRetryPolicyApplication(logger *slog.Logger, operation, policyName string, maxAttempts int, baseDelay time.Duration) {
	logger.Debug("applying retry policy to operation",
		"operation", operation,
		"policy", policyName,
		"max_attempts", maxAttempts,
		"base_delay", baseDelay)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ContextualLogger provides structured logging with correlation IDs and sync context.
type ContextualLogger struct {
	logger        *slog.Logger
	correlationID string
	syncContext   map[string]interface{}
	mu            sync.RWMutex
}

// NewContextualLogger creates a new contextual logger.
func NewContextualLogger(logger *slog.Logger, correlationID string) *ContextualLogger {
	return &ContextualLogger{
		logger:        logger,
		correlationID: correlationID,
		syncContext:   make(map[string]interface{}),
	}
}

// WithCorrelationID creates a new logger with the given correlation ID.
func (cl *ContextualLogger) WithCorrelationID(correlationID string) *ContextualLogger {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	
	return &ContextualLogger{
		logger:        cl.logger,
		correlationID: correlationID,
		syncContext:   cl.syncContext,
	}
}

// WithSyncContext adds sync context to the logger.
func (cl *ContextualLogger) WithSyncContext(key string, value interface{}) *ContextualLogger {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	
	newContext := make(map[string]interface{})
	for k, v := range cl.syncContext {
		newContext[k] = v
	}
	newContext[key] = value
	
	return &ContextualLogger{
		logger:        cl.logger,
		correlationID: cl.correlationID,
		syncContext:   newContext,
	}
}

// buildArgs creates the base arguments with correlation ID and sync context.
func (cl *ContextualLogger) buildArgs(args ...interface{}) []interface{} {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	
	baseArgs := make([]interface{}, 0, len(args)+len(cl.syncContext)*2+2)
	baseArgs = append(baseArgs, "correlation_id", cl.correlationID)
	
	for key, value := range cl.syncContext {
		baseArgs = append(baseArgs, key, value)
	}
	
	baseArgs = append(baseArgs, args...)
	return baseArgs
}

// Debug logs a debug message with context.
func (cl *ContextualLogger) Debug(msg string, args ...interface{}) {
	cl.logger.Debug(msg, cl.buildArgs(args...)...)
}

// Info logs an info message with context.
func (cl *ContextualLogger) Info(msg string, args ...interface{}) {
	cl.logger.Info(msg, cl.buildArgs(args...)...)
}

// Warn logs a warning message with context.
func (cl *ContextualLogger) Warn(msg string, args ...interface{}) {
	cl.logger.Warn(msg, cl.buildArgs(args...)...)
}

// Error logs an error message with context.
func (cl *ContextualLogger) Error(msg string, args ...interface{}) {
	cl.logger.Error(msg, cl.buildArgs(args...)...)
}

// LogSyncPhase logs a sync phase event with structured context.
func (cl *ContextualLogger) LogSyncPhase(phase, source, object string, startTime time.Time, err error) {
	duration := time.Since(startTime)
	
	args := []interface{}{
		"sync_phase", phase,
		"source", source,
		"object", object,
		"duration_ms", duration.Milliseconds(),
	}
	
	if err != nil {
		args = append(args, "error", err.Error())
		cl.Error("sync phase failed", args...)
	} else {
		cl.Info("sync phase completed", args...)
	}
}

// LogConnectorOperation logs a connector operation with structured context.
func (cl *ContextualLogger) LogConnectorOperation(connector, operation, method string, startTime time.Time, err error) {
	duration := time.Since(startTime)
	
	args := []interface{}{
		"connector", connector,
		"operation", operation,
		"method", method,
		"duration_ms", duration.Milliseconds(),
	}
	
	if err != nil {
		args = append(args, "error", err.Error())
		cl.Error("connector operation failed", args...)
	} else {
		cl.Info("connector operation completed", args...)
	}
}

// LogStorageOperation logs a storage operation with structured context.
func (cl *ContextualLogger) LogStorageOperation(operation, storageType string, startTime time.Time, dataSize int64, err error) {
	duration := time.Since(startTime)
	
	args := []interface{}{
		"storage_operation", operation,
		"storage_type", storageType,
		"duration_ms", duration.Milliseconds(),
		"data_size_bytes", dataSize,
	}
	
	if err != nil {
		args = append(args, "error", err.Error())
		cl.Error("storage operation failed", args...)
	} else {
		cl.Info("storage operation completed", args...)
	}
}

// LogBusinessMetrics logs business metrics with structured context.
func (cl *ContextualLogger) LogBusinessMetrics(source string, recordsProcessed int64, dataVolumeBytes int64, schemaChanges int64) {
	cl.Info("business metrics recorded",
		"source", source,
		"records_processed", recordsProcessed,
		"data_volume_bytes", dataVolumeBytes,
		"schema_changes", schemaChanges,
	)
}

// GetLogger returns the underlying slog.Logger.
func (cl *ContextualLogger) GetLogger() *slog.Logger {
	return cl.logger
}
