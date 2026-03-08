package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// HealthChecker monitors plugin health and performs automatic recovery
type HealthChecker struct {
	logger    *slog.Logger
	mu        sync.RWMutex
	status    map[string]*HealthStatus
	stopCh    chan struct{}
	running   bool
}

// HealthStatus represents the health status of a plugin
type HealthStatus struct {
	PluginName       string                 `json:"plugin_name"`
	Healthy          bool                   `json:"healthy"`
	LastCheck        time.Time              `json:"last_check"`
	LastHealthy      time.Time              `json:"last_healthy"`
	FailureCount     int                    `json:"failure_count"`
	RecoveryAttempts int                    `json:"recovery_attempts"`
	Issues           []HealthIssue          `json:"issues"`
	Metrics          map[string]interface{} `json:"metrics"`
}

// HealthIssue represents a specific health problem
type HealthIssue struct {
	Type        HealthIssueType `json:"type"`
	Severity    IssueSeverity   `json:"severity"`
	Message     string          `json:"message"`
	FirstSeen   time.Time       `json:"first_seen"`
	LastSeen    time.Time       `json:"last_seen"`
	Count       int             `json:"count"`
	Resolved    bool            `json:"resolved"`
}

// HealthIssueType represents different types of health issues
type HealthIssueType string

const (
	IssueTypeLoadFailure    HealthIssueType = "load_failure"
	IssueTypeConnectFailure HealthIssueType = "connect_failure"
	IssueTypeMemoryLeak     HealthIssueType = "memory_leak"
	IssueTypeCPUSpike       HealthIssueType = "cpu_spike"
	IssueTypeResponseSlow   HealthIssueType = "response_slow"
	IssueTypeProcessCrash   HealthIssueType = "process_crash"
	IssueTypeResourceLimit  HealthIssueType = "resource_limit"
	IssueTypeTimeout        HealthIssueType = "timeout"
)

// IssueSeverity represents the severity level of health issues
type IssueSeverity string

const (
	SeverityLow      IssueSeverity = "low"
	SeverityMedium   IssueSeverity = "medium"
	SeverityHigh     IssueSeverity = "high"
	SeverityCritical IssueSeverity = "critical"
)

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		logger: logger,
		status: make(map[string]*HealthStatus),
		stopCh: make(chan struct{}),
	}
}

// Start begins health monitoring
func (hc *HealthChecker) Start(ctx context.Context) error {
	hc.mu.Lock()
	if hc.running {
		hc.mu.Unlock()
		return nil
	}
	hc.running = true
	hc.mu.Unlock()

	hc.logger.Info("Starting plugin health checker")

	// Start the monitoring loop
	go hc.monitoringLoop(ctx)

	return nil
}

// Stop stops health monitoring
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return
	}

	hc.logger.Info("Stopping plugin health checker")
	
	close(hc.stopCh)
	hc.running = false
}

// CheckPluginHealth performs a health check on a specific plugin
func (hc *HealthChecker) CheckPluginHealth(pluginName string, manager *Manager) *HealthStatus {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	status, exists := hc.status[pluginName]
	if !exists {
		status = &HealthStatus{
			PluginName: pluginName,
			Issues:     make([]HealthIssue, 0),
			Metrics:    make(map[string]interface{}),
		}
		hc.status[pluginName] = status
	}

	status.LastCheck = time.Now()
	previouslyHealthy := status.Healthy
	status.Healthy = true // Assume healthy until proven otherwise

	// Get plugin information
	plugin, err := manager.GetPlugin(pluginName)
	if err != nil {
		hc.recordIssue(status, HealthIssue{
			Type:      IssueTypeLoadFailure,
			Severity:  SeverityCritical,
			Message:   fmt.Sprintf("Plugin not found: %v", err),
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
		})
		status.Healthy = false
		status.FailureCount++
		return status
	}

	// Check plugin status
	if plugin.Status != PluginStatusActive {
		hc.recordIssue(status, HealthIssue{
			Type:      IssueTypeLoadFailure,
			Severity:  SeverityHigh,
			Message:   fmt.Sprintf("Plugin status is %s", plugin.Status),
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
		})
		status.Healthy = false
	}

	// Check error count
	if plugin.ErrorCount > 10 {
		severity := SeverityMedium
		if plugin.ErrorCount > 50 {
			severity = SeverityHigh
		}
		if plugin.ErrorCount > 100 {
			severity = SeverityCritical
		}

		hc.recordIssue(status, HealthIssue{
			Type:      IssueTypeConnectFailure,
			Severity:  severity,
			Message:   fmt.Sprintf("High error count: %d", plugin.ErrorCount),
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
		})
		
		if severity == SeverityCritical {
			status.Healthy = false
		}
	}

	// Check if plugin hasn't been used recently (possible issue)
	if !plugin.LastUsed.IsZero() {
		unusedDuration := time.Since(plugin.LastUsed)
		if unusedDuration > 2*time.Hour {
			hc.recordIssue(status, HealthIssue{
				Type:      IssueTypeResponseSlow,
				Severity:  SeverityLow,
				Message:   fmt.Sprintf("Plugin unused for %v", unusedDuration),
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				Count:     1,
			})
		}
	}

	// Update health status
	if status.Healthy && !previouslyHealthy {
		status.LastHealthy = time.Now()
		hc.logger.Info("Plugin health recovered", "plugin", pluginName)
	} else if !status.Healthy {
		status.FailureCount++
		hc.logger.Warn("Plugin health check failed", 
			"plugin", pluginName, 
			"failure_count", status.FailureCount)
	} else if status.Healthy {
		status.LastHealthy = time.Now()
	}

	// Update metrics
	status.Metrics["status"] = plugin.Status
	status.Metrics["load_time"] = plugin.LoadTime
	status.Metrics["last_used"] = plugin.LastUsed
	status.Metrics["error_count"] = plugin.ErrorCount
	status.Metrics["version"] = plugin.Version

	return status
}

// GetPluginHealth returns the current health status for a plugin
func (hc *HealthChecker) GetPluginHealth(pluginName string) *HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	status, exists := hc.status[pluginName]
	if !exists {
		return nil
	}

	// Return a copy to avoid concurrent access issues
	statusCopy := *status
	statusCopy.Issues = make([]HealthIssue, len(status.Issues))
	copy(statusCopy.Issues, status.Issues)
	
	statusCopy.Metrics = make(map[string]interface{})
	for k, v := range status.Metrics {
		statusCopy.Metrics[k] = v
	}

	return &statusCopy
}

// GetAllHealth returns health status for all monitored plugins
func (hc *HealthChecker) GetAllHealth() map[string]*HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]*HealthStatus)
	for name, status := range hc.status {
		// Return copies to avoid concurrent access issues
		statusCopy := *status
		statusCopy.Issues = make([]HealthIssue, len(status.Issues))
		copy(statusCopy.Issues, status.Issues)
		
		statusCopy.Metrics = make(map[string]interface{})
		for k, v := range status.Metrics {
			statusCopy.Metrics[k] = v
		}
		
		result[name] = &statusCopy
	}

	return result
}

// RecordRecoveryAttempt records an attempt to recover a plugin
func (hc *HealthChecker) RecordRecoveryAttempt(pluginName string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	status, exists := hc.status[pluginName]
	if !exists {
		return
	}

	status.RecoveryAttempts++
	hc.logger.Info("Plugin recovery attempted", 
		"plugin", pluginName, 
		"attempt", status.RecoveryAttempts)
}

// monitoringLoop runs the continuous health monitoring
func (hc *HealthChecker) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check health every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Health checking is triggered externally by the manager
			// This loop is mainly for cleanup and maintenance tasks
			hc.cleanupResolvedIssues()
		case <-hc.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// cleanupResolvedIssues removes old resolved issues
func (hc *HealthChecker) cleanupResolvedIssues() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour) // Remove issues older than 24 hours

	for pluginName, status := range hc.status {
		var activeIssues []HealthIssue
		for _, issue := range status.Issues {
			if !issue.Resolved || issue.LastSeen.After(cutoff) {
				activeIssues = append(activeIssues, issue)
			}
		}
		
		if len(activeIssues) < len(status.Issues) {
			status.Issues = activeIssues
			hc.logger.Debug("Cleaned up resolved issues", 
				"plugin", pluginName, 
				"removed", len(status.Issues)-len(activeIssues))
		}
	}
}

// recordIssue records a health issue for a plugin
func (hc *HealthChecker) recordIssue(status *HealthStatus, newIssue HealthIssue) {
	// Check if this issue already exists
	for i, existing := range status.Issues {
		if existing.Type == newIssue.Type && existing.Message == newIssue.Message {
			// Update existing issue
			status.Issues[i].Count++
			status.Issues[i].LastSeen = newIssue.LastSeen
			status.Issues[i].Resolved = false
			return
		}
	}

	// Add new issue
	status.Issues = append(status.Issues, newIssue)

	hc.logger.Warn("New plugin health issue recorded", 
		"plugin", status.PluginName,
		"type", newIssue.Type,
		"severity", newIssue.Severity,
		"message", newIssue.Message)
}

// resolveIssue marks an issue as resolved
func (hc *HealthChecker) resolveIssue(status *HealthStatus, issueType HealthIssueType, message string) {
	for i, issue := range status.Issues {
		if issue.Type == issueType && issue.Message == message && !issue.Resolved {
			status.Issues[i].Resolved = true
			hc.logger.Info("Plugin health issue resolved", 
				"plugin", status.PluginName,
				"type", issueType,
				"message", message)
			return
		}
	}
}