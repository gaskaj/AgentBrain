package security

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RuntimeMonitor monitors security events and metrics during application runtime
type RuntimeMonitor struct {
	config         RuntimeMonitoringConfig
	metrics        SecurityMetrics
	alertManager   AlertManager
	logger         *slog.Logger
	mu             sync.RWMutex
	isRunning      bool
	stopCh         chan struct{}
	doneCh         chan struct{}
	eventBuffer    []SecurityEvent
	lastHealthCheck time.Time
}

// NewRuntimeMonitor creates a new runtime security monitor
func NewRuntimeMonitor(config RuntimeMonitoringConfig) (*RuntimeMonitor, error) {
	monitor := &RuntimeMonitor{
		config:      config,
		metrics:     SecurityMetrics{
			MetricsByCategory: make(map[string]int64),
			TrendData:         make([]MetricDataPoint, 0),
		},
		eventBuffer: make([]SecurityEvent, 0),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		logger:      slog.Default(),
	}

	return monitor, nil
}

// Start begins runtime security monitoring
func (rm *RuntimeMonitor) Start(ctx context.Context) error {
	if !rm.config.Enabled {
		rm.logger.Info("runtime security monitoring is disabled")
		return nil
	}

	rm.mu.Lock()
	if rm.isRunning {
		rm.mu.Unlock()
		return fmt.Errorf("runtime monitor is already running")
	}
	rm.isRunning = true
	rm.mu.Unlock()

	rm.logger.Info("starting runtime security monitor")

	go rm.runMonitorLoop(ctx)
	return nil
}

// Stop gracefully stops runtime security monitoring
func (rm *RuntimeMonitor) Stop() error {
	rm.mu.Lock()
	if !rm.isRunning {
		rm.mu.Unlock()
		return nil
	}
	rm.mu.Unlock()

	rm.logger.Info("stopping runtime security monitor")
	close(rm.stopCh)
	<-rm.doneCh

	rm.mu.Lock()
	rm.isRunning = false
	rm.mu.Unlock()

	return nil
}

// GetMetrics returns current security metrics
func (rm *RuntimeMonitor) GetMetrics() SecurityMetrics {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Create a deep copy to avoid race conditions
	metrics := rm.metrics
	metrics.LastUpdated = time.Now()

	// Copy maps
	metrics.MetricsByCategory = make(map[string]int64)
	for k, v := range rm.metrics.MetricsByCategory {
		metrics.MetricsByCategory[k] = v
	}

	// Copy trend data
	metrics.TrendData = make([]MetricDataPoint, len(rm.metrics.TrendData))
	copy(metrics.TrendData, rm.metrics.TrendData)

	return metrics
}

// RecordEvent records a security event for monitoring and analysis
func (rm *RuntimeMonitor) RecordEvent(event SecurityEvent) error {
	if !rm.config.Enabled {
		return nil
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Add timestamp if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Add ID if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	// Update metrics based on event type
	rm.updateMetricsFromEvent(event)

	// Add to event buffer
	rm.eventBuffer = append(rm.eventBuffer, event)

	// Keep only recent events (last 1000)
	if len(rm.eventBuffer) > 1000 {
		rm.eventBuffer = rm.eventBuffer[len(rm.eventBuffer)-1000:]
	}

	// Log security event
	rm.logger.Warn("security event recorded",
		"event_id", event.ID,
		"type", event.Type,
		"severity", event.Severity,
		"source", event.Source,
		"description", event.Description)

	// Send alert for high-severity events
	if rm.shouldAlert(event) && rm.alertManager != nil {
		alert := rm.createAlertFromEvent(event)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := rm.alertManager.SendSecurityAlert(ctx, alert); err != nil {
				rm.logger.Error("failed to send security alert",
					"event_id", event.ID,
					"error", err)
			}
		}()
	}

	return nil
}

// IsHealthy returns whether the runtime monitor is healthy
func (rm *RuntimeMonitor) IsHealthy() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if !rm.isRunning {
		return false
	}

	// Check if monitoring is stuck (no health check in last 5 minutes)
	if !rm.lastHealthCheck.IsZero() && time.Since(rm.lastHealthCheck) > 5*time.Minute {
		return false
	}

	return true
}

// runMonitorLoop runs the main monitoring loop
func (rm *RuntimeMonitor) runMonitorLoop(ctx context.Context) {
	defer close(rm.doneCh)

	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	rm.logger.Info("runtime security monitor started")

	for {
		select {
		case <-ctx.Done():
			rm.logger.Info("runtime monitor stopped due to context cancellation")
			return
		case <-rm.stopCh:
			rm.logger.Info("runtime monitor stopped")
			return
		case <-ticker.C:
			rm.performSecurityChecks(ctx)
		}
	}
}

// performSecurityChecks performs various security checks
func (rm *RuntimeMonitor) performSecurityChecks(ctx context.Context) {
	rm.mu.Lock()
	rm.lastHealthCheck = time.Now()
	rm.mu.Unlock()

	// Check memory anomalies
	if rm.config.MemoryAnomalyDetection {
		rm.checkMemoryAnomalies()
	}

	// Check process anomalies
	if rm.config.ProcessMonitoring {
		rm.checkProcessAnomalies()
	}

	// Update trend data
	rm.updateTrendData()

	// Cleanup old data
	rm.cleanupOldData()
}

// checkMemoryAnomalies monitors for memory-related security issues
func (rm *RuntimeMonitor) checkMemoryAnomalies() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Check for potential memory leaks or excessive memory usage
	const maxMemoryMB = 1024 // 1GB threshold
	currentMemoryMB := m.Alloc / 1024 / 1024

	if currentMemoryMB > maxMemoryMB {
		event := SecurityEvent{
			Type:        "memory_anomaly",
			Severity:    "high",
			Source:      "runtime_monitor",
			Description: fmt.Sprintf("High memory usage detected: %d MB", currentMemoryMB),
			Metadata: map[string]interface{}{
				"memory_mb":     currentMemoryMB,
				"heap_objects":  m.HeapObjects,
				"gc_cycles":     m.NumGC,
			},
			RemediationSuggestion: "Investigate potential memory leaks or optimize memory usage",
		}
		rm.RecordEvent(event)
	}

	// Check for rapid memory growth
	rm.mu.Lock()
	previousMemory, exists := rm.metrics.MetricsByCategory["memory_usage_mb"]
	rm.metrics.MetricsByCategory["memory_usage_mb"] = int64(currentMemoryMB)
	rm.mu.Unlock()

	if exists && int64(currentMemoryMB) > previousMemory*2 {
		event := SecurityEvent{
			Type:        "memory_anomaly",
			Severity:    "medium",
			Source:      "runtime_monitor",
			Description: fmt.Sprintf("Rapid memory growth detected: %d MB to %d MB", previousMemory, currentMemoryMB),
			Metadata: map[string]interface{}{
				"previous_memory_mb": previousMemory,
				"current_memory_mb":  currentMemoryMB,
				"growth_factor":      float64(currentMemoryMB) / float64(previousMemory),
			},
			RemediationSuggestion: "Review recent changes that might cause memory usage increases",
		}
		rm.RecordEvent(event)
	}
}

// checkProcessAnomalies monitors for process-related security issues
func (rm *RuntimeMonitor) checkProcessAnomalies() {
	numGoroutines := runtime.NumGoroutine()
	
	// Check for goroutine leaks
	const maxGoroutines = 1000
	if numGoroutines > maxGoroutines {
		event := SecurityEvent{
			Type:        "process_anomaly",
			Severity:    "medium",
			Source:      "runtime_monitor",
			Description: fmt.Sprintf("High number of goroutines detected: %d", numGoroutines),
			Metadata: map[string]interface{}{
				"goroutine_count": numGoroutines,
				"cpu_count":       runtime.NumCPU(),
			},
			RemediationSuggestion: "Check for goroutine leaks or excessive concurrent operations",
		}
		rm.RecordEvent(event)
	}

	rm.mu.Lock()
	rm.metrics.MetricsByCategory["goroutine_count"] = int64(numGoroutines)
	rm.mu.Unlock()
}

// updateMetricsFromEvent updates security metrics based on an event
func (rm *RuntimeMonitor) updateMetricsFromEvent(event SecurityEvent) {
	switch event.Type {
	case "auth_failure":
		rm.metrics.FailedAuthAttempts++
		rm.metrics.MetricsByCategory["auth_failures"]++
	case "network_anomaly":
		rm.metrics.UnexpectedNetworkIO++
		rm.metrics.MetricsByCategory["network_anomalies"]++
	case "file_access":
		rm.metrics.SuspiciousFileAccess++
		rm.metrics.MetricsByCategory["file_access_violations"]++
	case "memory_anomaly":
		rm.metrics.MemoryAnomalies++
		rm.metrics.MetricsByCategory["memory_anomalies"]++
	case "process_anomaly":
		rm.metrics.ProcessAnomalies++
		rm.metrics.MetricsByCategory["process_anomalies"]++
	case "network_connection":
		rm.metrics.UnauthorizedNetworkConns++
		rm.metrics.MetricsByCategory["unauthorized_connections"]++
	case "tls_violation":
		rm.metrics.TLSViolations++
		rm.metrics.MetricsByCategory["tls_violations"]++
	case "credential_exposure":
		rm.metrics.CredentialExposures++
		rm.metrics.MetricsByCategory["credential_exposures"]++
	case "data_integrity":
		rm.metrics.DataIntegrityViolations++
		rm.metrics.MetricsByCategory["data_integrity_violations"]++
	case "access_control":
		rm.metrics.AccessControlViolations++
		rm.metrics.MetricsByCategory["access_control_violations"]++
	}

	// Update category counter
	if categoryCount, exists := rm.metrics.MetricsByCategory[event.Type]; exists {
		rm.metrics.MetricsByCategory[event.Type] = categoryCount + 1
	} else {
		rm.metrics.MetricsByCategory[event.Type] = 1
	}
}

// shouldAlert determines if an event should trigger an alert
func (rm *RuntimeMonitor) shouldAlert(event SecurityEvent) bool {
	highSeverityEvents := []string{"critical", "high"}
	for _, severity := range highSeverityEvents {
		if event.Severity == severity {
			return true
		}
	}

	// Alert on auth failures if threshold exceeded
	if event.Type == "auth_failure" && rm.config.AuthFailureThreshold > 0 {
		return rm.metrics.FailedAuthAttempts >= int64(rm.config.AuthFailureThreshold)
	}

	return false
}

// createAlertFromEvent creates a security alert from a security event
func (rm *RuntimeMonitor) createAlertFromEvent(event SecurityEvent) SecurityAlert {
	return SecurityAlert{
		ID:          uuid.New().String(),
		Timestamp:   event.Timestamp,
		Severity:    event.Severity,
		Title:       fmt.Sprintf("Security Event: %s", event.Type),
		Description: event.Description,
		Source:      "runtime_monitor",
		Remediation: event.RemediationSuggestion,
		Tags:        []string{"runtime", event.Type},
		Metadata: map[string]interface{}{
			"original_event_id": event.ID,
			"event_source":      event.Source,
			"event_metadata":    event.Metadata,
		},
	}
}

// updateTrendData adds current metrics to trend data
func (rm *RuntimeMonitor) updateTrendData() {
	now := time.Now()
	
	// Add trend data points for key metrics
	trendMetrics := []struct {
		category string
		value    int64
	}{
		{"auth_failures", rm.metrics.FailedAuthAttempts},
		{"network_anomalies", rm.metrics.UnexpectedNetworkIO},
		{"memory_anomalies", rm.metrics.MemoryAnomalies},
		{"process_anomalies", rm.metrics.ProcessAnomalies},
	}

	for _, metric := range trendMetrics {
		dataPoint := MetricDataPoint{
			Timestamp: now,
			Value:     metric.value,
			Category:  metric.category,
		}
		rm.metrics.TrendData = append(rm.metrics.TrendData, dataPoint)
	}

	// Keep only last 24 hours of trend data (assuming 30-second intervals = 2880 points)
	maxTrendPoints := 2880
	if len(rm.metrics.TrendData) > maxTrendPoints {
		rm.metrics.TrendData = rm.metrics.TrendData[len(rm.metrics.TrendData)-maxTrendPoints:]
	}
}

// cleanupOldData removes old data to prevent memory leaks
func (rm *RuntimeMonitor) cleanupOldData() {
	// Clean up event buffer (keep only events from last hour)
	cutoff := time.Now().Add(-1 * time.Hour)
	var recentEvents []SecurityEvent
	
	for _, event := range rm.eventBuffer {
		if event.Timestamp.After(cutoff) {
			recentEvents = append(recentEvents, event)
		}
	}
	
	rm.eventBuffer = recentEvents
}

// GetRecentEvents returns recent security events
func (rm *RuntimeMonitor) GetRecentEvents(limit int) []SecurityEvent {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if limit <= 0 || limit > len(rm.eventBuffer) {
		limit = len(rm.eventBuffer)
	}

	// Return most recent events
	events := make([]SecurityEvent, limit)
	copy(events, rm.eventBuffer[len(rm.eventBuffer)-limit:])
	
	return events
}

// GetSecurityScore calculates an overall security score based on current metrics
func (rm *RuntimeMonitor) GetSecurityScore() float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	score := 100.0

	// Penalize based on various security metrics
	penalties := []struct {
		value   int64
		weight  float64
		max     int64
	}{
		{rm.metrics.FailedAuthAttempts, 2.0, 50},
		{rm.metrics.UnexpectedNetworkIO, 1.5, 20},
		{rm.metrics.SuspiciousFileAccess, 1.8, 10},
		{rm.metrics.MemoryAnomalies, 1.2, 30},
		{rm.metrics.ProcessAnomalies, 1.0, 25},
		{rm.metrics.UnauthorizedNetworkConns, 3.0, 5},
		{rm.metrics.TLSViolations, 4.0, 3},
		{rm.metrics.CredentialExposures, 10.0, 1},
		{rm.metrics.DataIntegrityViolations, 8.0, 2},
		{rm.metrics.AccessControlViolations, 5.0, 5},
	}

	for _, penalty := range penalties {
		if penalty.value > penalty.max {
			score -= penalty.weight * float64(penalty.max)
		} else {
			score -= penalty.weight * float64(penalty.value)
		}
	}

	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// GenerateSecurityReport generates a comprehensive runtime security report
func (rm *RuntimeMonitor) GenerateSecurityReport() *SecurityReport {
	metrics := rm.GetMetrics()
	recentEvents := rm.GetRecentEvents(100)

	report := &SecurityReport{
		ID:             uuid.New().String(),
		Timestamp:      time.Now(),
		Status:         "completed",
		RuntimeMetrics: metrics,
		GeneratedBy:    "runtime-monitor",
	}

	// Calculate summary
	summary := SecuritySummary{
		SecurityScore: rm.GetSecurityScore(),
	}

	// Count issues by severity based on recent events
	summary.IssuesBySeverity = make(map[string]int)
	for _, event := range recentEvents {
		summary.IssuesBySeverity[event.Severity]++
		if event.Severity == "high" || event.Severity == "critical" {
			summary.HighSeverityIssues++
		}
		if event.Severity == "critical" {
			summary.CriticalIssues++
		}
	}

	summary.TotalIssues = len(recentEvents)
	report.Summary = summary
	report.RiskScore = 100.0 - summary.SecurityScore

	// Generate recommendations based on metrics
	report.Recommendations = rm.generateRecommendations(metrics, recentEvents)

	return report
}

// generateRecommendations generates security recommendations based on runtime data
func (rm *RuntimeMonitor) generateRecommendations(metrics SecurityMetrics, events []SecurityEvent) []Recommendation {
	var recommendations []Recommendation

	// Check for high auth failure rate
	if metrics.FailedAuthAttempts > 10 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Title:       "Investigate authentication failures",
			Description: fmt.Sprintf("High number of authentication failures detected (%d)", metrics.FailedAuthAttempts),
			Severity:    "high",
			Category:    "authentication",
			Fix:         "Review authentication logs, implement rate limiting, and consider multi-factor authentication",
			Effort:      "medium",
			Impact:      "high",
			Tags:        []string{"authentication", "security"},
		})
	}

	// Check for memory anomalies
	if metrics.MemoryAnomalies > 5 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Title:       "Address memory anomalies",
			Description: "Multiple memory anomalies detected indicating potential security issues",
			Severity:    "medium",
			Category:    "memory",
			Fix:         "Profile memory usage, check for memory leaks, and implement memory monitoring",
			Effort:      "high",
			Impact:      "medium",
			Tags:        []string{"memory", "performance", "security"},
		})
	}

	// Check for credential exposures
	if metrics.CredentialExposures > 0 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Title:       "Immediate credential exposure response",
			Description: "Credential exposures detected - immediate action required",
			Severity:    "critical",
			Category:    "credentials",
			Fix:         "Rotate affected credentials immediately, review code for hardcoded secrets, implement secret scanning",
			Effort:      "high",
			Impact:      "critical",
			Tags:        []string{"credentials", "incident-response"},
		})
	}

	// General recommendation if there are many recent events
	if len(events) > 50 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Title:       "Review security monitoring configuration",
			Description: "High volume of security events may indicate over-sensitive monitoring or actual security issues",
			Severity:    "medium",
			Category:    "monitoring",
			Fix:         "Review monitoring thresholds and investigate patterns in security events",
			Effort:      "medium",
			Impact:      "medium",
			Tags:        []string{"monitoring", "tuning"},
		})
	}

	return recommendations
}

// SetAlertManager sets the alert manager for sending security alerts
func (rm *RuntimeMonitor) SetAlertManager(alertManager AlertManager) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.alertManager = alertManager
}

// SetLogger sets the logger for the runtime monitor
func (rm *RuntimeMonitor) SetLogger(logger *slog.Logger) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.logger = logger
}