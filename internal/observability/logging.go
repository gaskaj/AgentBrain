package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
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
