package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
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
