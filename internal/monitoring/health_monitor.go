package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// HealthStatus represents the overall health state of a component
type HealthStatus string

const (
	HealthStatusOK       HealthStatus = "ok"
	HealthStatusWarning  HealthStatus = "warning"
	HealthStatusCritical HealthStatus = "critical"
	HealthStatusUnknown  HealthStatus = "unknown"
)

// MetricsSnapshot contains a point-in-time view of system metrics
type MetricsSnapshot struct {
	Timestamp       time.Time
	AgentMetrics    AgentMetrics
	SystemMetrics   SystemMetrics
	BusinessMetrics BusinessMetrics
	ErrorMetrics    ErrorMetrics
}

// AgentMetrics contains agent-specific health metrics
type AgentMetrics struct {
	ActiveAgents     int
	FailureRate      float64
	CompletionRate   float64
	AvgProcessingTime time.Duration
	ErrorCount       int64
	SuccessCount     int64
}

// SystemMetrics contains system resource metrics
type SystemMetrics struct {
	DiskUsagePercent   float64
	MemoryUsagePercent float64
	CPUUsagePercent    float64
	APIResponseTime    time.Duration
	CircuitBreakerTrips int64
}

// BusinessMetrics contains business logic metrics
type BusinessMetrics struct {
	WorkflowSuccessRate float64
	PRCreationRate      float64
	IssueProcessingRate float64
	TokenUsagePercent   float64
}

// ErrorMetrics contains error pattern tracking metrics
type ErrorMetrics struct {
	SyncErrorRate        float64                 `json:"sync_error_rate"`
	RecoverySuccessRate  float64                 `json:"recovery_success_rate"`
	CircuitBreakerTrips  int64                   `json:"circuit_breaker_trips"`
	ErrorsByPhase        map[string]int64        `json:"errors_by_phase"`
	ErrorsByObject       map[string]int64        `json:"errors_by_object"`
	RetryPatterns        map[string]RetryMetrics `json:"retry_patterns"`
}

// RetryMetrics tracks retry patterns for specific operations
type RetryMetrics struct {
	TotalAttempts    int64   `json:"total_attempts"`
	SuccessfulRetries int64   `json:"successful_retries"`
	FailedRetries    int64   `json:"failed_retries"`
	AverageRetryDelay float64 `json:"average_retry_delay_ms"`
}

// HealthRule defines an interface for health check rules
type HealthRule interface {
	Name() string
	Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error)
	Severity() AlertSeverity
	IsEnabled() bool
}

// HealthMonitor orchestrates health monitoring and alerting
type HealthMonitor struct {
	config       MonitoringConfig
	rules        []HealthRule
	alerting     *AlertManager
	logger       *slog.Logger
	mu           sync.RWMutex
	lastCheck    time.Time
	isRunning    bool
	stopCh       chan struct{}
	doneCh       chan struct{}
	errorTracker *ErrorPatternTracker
}

// ErrorPatternTracker tracks and analyzes error patterns
type ErrorPatternTracker struct {
	errorCounts   map[string]int64
	phaseCounts   map[string]int64
	objectCounts  map[string]int64
	retryMetrics  map[string]*RetryMetrics
	mu            sync.RWMutex
}

// NewErrorPatternTracker creates a new error pattern tracker
func NewErrorPatternTracker() *ErrorPatternTracker {
	return &ErrorPatternTracker{
		errorCounts:  make(map[string]int64),
		phaseCounts:  make(map[string]int64),
		objectCounts: make(map[string]int64),
		retryMetrics: make(map[string]*RetryMetrics),
	}
}

// RecordError records an error occurrence for pattern analysis
func (ept *ErrorPatternTracker) RecordError(phase, object, errorType string) {
	ept.mu.Lock()
	defer ept.mu.Unlock()
	
	ept.errorCounts[errorType]++
	ept.phaseCounts[phase]++
	ept.objectCounts[object]++
}

// RecordRetry records a retry attempt
func (ept *ErrorPatternTracker) RecordRetry(operation string, success bool, delayMs float64) {
	ept.mu.Lock()
	defer ept.mu.Unlock()
	
	if ept.retryMetrics[operation] == nil {
		ept.retryMetrics[operation] = &RetryMetrics{}
	}
	
	metrics := ept.retryMetrics[operation]
	metrics.TotalAttempts++
	
	if success {
		metrics.SuccessfulRetries++
	} else {
		metrics.FailedRetries++
	}
	
	// Update average delay (simple moving average)
	if metrics.TotalAttempts == 1 {
		metrics.AverageRetryDelay = delayMs
	} else {
		metrics.AverageRetryDelay = (metrics.AverageRetryDelay*float64(metrics.TotalAttempts-1) + delayMs) / float64(metrics.TotalAttempts)
	}
}

// GetErrorMetrics returns current error metrics
func (ept *ErrorPatternTracker) GetErrorMetrics() ErrorMetrics {
	ept.mu.RLock()
	defer ept.mu.RUnlock()
	
	// Calculate error rates and copy metrics
	errorsByPhase := make(map[string]int64)
	for phase, count := range ept.phaseCounts {
		errorsByPhase[phase] = count
	}
	
	errorsByObject := make(map[string]int64)
	for object, count := range ept.objectCounts {
		errorsByObject[object] = count
	}
	
	retryPatterns := make(map[string]RetryMetrics)
	for operation, metrics := range ept.retryMetrics {
		retryPatterns[operation] = *metrics
	}
	
	// Calculate rates (simplified - in production you'd use time windows)
	totalErrors := int64(0)
	for _, count := range ept.errorCounts {
		totalErrors += count
	}
	
	totalRetries := int64(0)
	successfulRetries := int64(0)
	for _, metrics := range ept.retryMetrics {
		totalRetries += metrics.TotalAttempts
		successfulRetries += metrics.SuccessfulRetries
	}
	
	var recoverySuccessRate float64
	if totalRetries > 0 {
		recoverySuccessRate = float64(successfulRetries) / float64(totalRetries)
	}
	
	return ErrorMetrics{
		SyncErrorRate:       float64(totalErrors) / 100.0, // Simplified calculation
		RecoverySuccessRate: recoverySuccessRate,
		ErrorsByPhase:       errorsByPhase,
		ErrorsByObject:      errorsByObject,
		RetryPatterns:       retryPatterns,
	}
}

// NewHealthMonitor creates a new health monitoring system
func NewHealthMonitor(config MonitoringConfig, logger *slog.Logger) (*HealthMonitor, error) {
	alertManager, err := NewAlertManager(config.Notifications, logger)
	if err != nil {
		return nil, fmt.Errorf("create alert manager: %w", err)
	}

	hm := &HealthMonitor{
		config:       config,
		alerting:     alertManager,
		logger:       logger,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		errorTracker: NewErrorPatternTracker(),
	}

	// Initialize built-in health rules
	hm.rules = []HealthRule{
		NewAgentFailureRateRule(config.Rules.AgentFailureRate),
		NewWorkflowCompletionRule(config.Rules.WorkflowCompletion),
		NewDiskUsageRule(config.Rules.DiskUsage),
		NewMemoryUsageRule(config.Rules.MemoryUsage),
		NewAPIResponseTimeRule(config.Rules.APIResponseTime),
	}

	return hm, nil
}

// Start begins health monitoring in the background
func (hm *HealthMonitor) Start(ctx context.Context) error {
	if !hm.config.Enabled {
		hm.logger.Info("health monitoring is disabled")
		return nil
	}

	hm.mu.Lock()
	if hm.isRunning {
		hm.mu.Unlock()
		return fmt.Errorf("health monitor is already running")
	}
	hm.isRunning = true
	hm.mu.Unlock()

	hm.logger.Info("starting health monitor", 
		"check_interval", hm.config.CheckInterval,
		"rules_count", len(hm.rules))

	go hm.runMonitorLoop(ctx)
	return nil
}

// Stop gracefully stops the health monitor
func (hm *HealthMonitor) Stop() error {
	hm.mu.Lock()
	if !hm.isRunning {
		hm.mu.Unlock()
		return nil
	}
	hm.mu.Unlock()

	hm.logger.Info("stopping health monitor")
	close(hm.stopCh)
	<-hm.doneCh
	
	hm.mu.Lock()
	hm.isRunning = false
	hm.mu.Unlock()
	
	return nil
}

// GetHealthStatus returns the current overall health status
func (hm *HealthMonitor) GetHealthStatus(ctx context.Context) (HealthStatus, map[string]interface{}, error) {
	if !hm.config.Enabled {
		return HealthStatusOK, map[string]interface{}{
			"monitoring": "disabled",
		}, nil
	}

	// Get current metrics snapshot
	snapshot, err := hm.collectMetrics(ctx)
	if err != nil {
		return HealthStatusUnknown, nil, fmt.Errorf("collect metrics: %w", err)
	}

	// Check all rules
	status := HealthStatusOK
	results := make(map[string]interface{})
	
	for _, rule := range hm.rules {
		if !rule.IsEnabled() {
			continue
		}

		ruleStatus, message, err := rule.Check(ctx, *snapshot)
		if err != nil {
			hm.logger.Warn("health rule check failed", 
				"rule", rule.Name(),
				"error", err)
			continue
		}

		results[rule.Name()] = map[string]interface{}{
			"status":  ruleStatus,
			"message": message,
		}

		// Escalate overall status based on rule severity
		if ruleStatus != HealthStatusOK {
			if rule.Severity() == AlertSeverityCritical || ruleStatus == HealthStatusCritical {
				status = HealthStatusCritical
			} else if status != HealthStatusCritical && ruleStatus == HealthStatusWarning {
				status = HealthStatusWarning
			}
		}
	}

	results["overall_status"] = status
	results["last_check"] = hm.lastCheck
	results["enabled_rules"] = len(hm.enabledRules())

	return status, results, nil
}

func (hm *HealthMonitor) runMonitorLoop(ctx context.Context) {
	defer close(hm.doneCh)

	ticker := time.NewTicker(hm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			hm.logger.Info("health monitor stopped due to context cancellation")
			return
		case <-hm.stopCh:
			hm.logger.Info("health monitor stopped")
			return
		case <-ticker.C:
			if err := hm.performHealthCheck(ctx); err != nil {
				hm.logger.Error("health check failed", "error", err)
			}
		}
	}
}

func (hm *HealthMonitor) performHealthCheck(ctx context.Context) error {
	hm.mu.Lock()
	hm.lastCheck = time.Now()
	hm.mu.Unlock()

	snapshot, err := hm.collectMetrics(ctx)
	if err != nil {
		return fmt.Errorf("collect metrics: %w", err)
	}

	enabledRules := hm.enabledRules()
	hm.logger.Debug("performing health check", 
		"enabled_rules", len(enabledRules),
		"timestamp", snapshot.Timestamp)

	var alerts []Alert
	for _, rule := range enabledRules {
		status, message, err := rule.Check(ctx, *snapshot)
		if err != nil {
			hm.logger.Warn("rule check failed", 
				"rule", rule.Name(),
				"error", err)
			continue
		}

		if status != HealthStatusOK {
			alert := Alert{
				ID:          fmt.Sprintf("%s-%d", rule.Name(), time.Now().Unix()),
				RuleName:    rule.Name(),
				Severity:    rule.Severity(),
				Status:      AlertStatusFiring,
				Message:     message,
				Timestamp:   snapshot.Timestamp,
				HealthStatus: status,
			}
			alerts = append(alerts, alert)
		}
	}

	// Dispatch alerts if any were triggered
	if len(alerts) > 0 {
		for _, alert := range alerts {
			if err := hm.alerting.ProcessAlert(ctx, alert); err != nil {
				hm.logger.Error("failed to process alert", 
					"alert_id", alert.ID,
					"rule", alert.RuleName,
					"error", err)
			}
		}
	}

	return nil
}

func (hm *HealthMonitor) collectMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	// TODO: Integrate with actual metrics collection
	// For now, return mock data to demonstrate the interface
	return &MetricsSnapshot{
		Timestamp: time.Now(),
		AgentMetrics: AgentMetrics{
			ActiveAgents:      3,
			FailureRate:       0.05,
			CompletionRate:    0.92,
			AvgProcessingTime: 2 * time.Minute,
			ErrorCount:        12,
			SuccessCount:      245,
		},
		SystemMetrics: SystemMetrics{
			DiskUsagePercent:    45.2,
			MemoryUsagePercent:  67.8,
			CPUUsagePercent:     23.4,
			APIResponseTime:     150 * time.Millisecond,
			CircuitBreakerTrips: 2,
		},
		BusinessMetrics: BusinessMetrics{
			WorkflowSuccessRate: 0.89,
			PRCreationRate:      0.78,
			IssueProcessingRate: 0.91,
			TokenUsagePercent:   72.5,
		},
		ErrorMetrics: hm.errorTracker.GetErrorMetrics(),
	}, nil
}

func (hm *HealthMonitor) enabledRules() []HealthRule {
	var enabled []HealthRule
	for _, rule := range hm.rules {
		if rule.IsEnabled() {
			enabled = append(enabled, rule)
		}
	}
	return enabled
}

// MonitoringConfig holds configuration for health monitoring
type MonitoringConfig struct {
	Enabled       bool                `yaml:"enabled"`
	CheckInterval time.Duration       `yaml:"check_interval"`
	AlertCooldown time.Duration       `yaml:"alert_cooldown"`
	Rules         RulesConfig         `yaml:"rules"`
	Notifications NotificationsConfig `yaml:"notifications"`
}

// RulesConfig holds configuration for all health check rules
type RulesConfig struct {
	AgentFailureRate    AgentFailureRateConfig    `yaml:"agent_failure_rate"`
	WorkflowCompletion  WorkflowCompletionConfig  `yaml:"workflow_completion"`
	DiskUsage           DiskUsageConfig           `yaml:"disk_usage"`
	MemoryUsage         MemoryUsageConfig         `yaml:"memory_usage"`
	APIResponseTime     APIResponseTimeConfig     `yaml:"api_response_time"`
}

// Configuration structs for health rules
type AgentFailureRateConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Threshold float64       `yaml:"threshold"`
	Window    time.Duration `yaml:"window"`
	Severity  string        `yaml:"severity"`
}

type WorkflowCompletionConfig struct {
	Enabled        bool          `yaml:"enabled"`
	MinSuccessRate float64       `yaml:"min_success_rate"`
	Window         time.Duration `yaml:"window"`
	Severity       string        `yaml:"severity"`
}

type DiskUsageConfig struct {
	Enabled           bool    `yaml:"enabled"`
	WarningThreshold  float64 `yaml:"warning_threshold"`
	CriticalThreshold float64 `yaml:"critical_threshold"`
	Severity          string  `yaml:"severity"`
}

type MemoryUsageConfig struct {
	Enabled           bool    `yaml:"enabled"`
	WarningThreshold  float64 `yaml:"warning_threshold"`
	CriticalThreshold float64 `yaml:"critical_threshold"`
	Severity          string  `yaml:"severity"`
}

type APIResponseTimeConfig struct {
	Enabled           bool          `yaml:"enabled"`
	WarningThreshold  time.Duration `yaml:"warning_threshold"`
	CriticalThreshold time.Duration `yaml:"critical_threshold"`
	Severity          string        `yaml:"severity"`
}

// NotificationsConfig holds configuration for all notification channels
type NotificationsConfig struct {
	Slack   SlackConfig   `yaml:"slack"`
	Email   EmailConfig   `yaml:"email"`
	Webhook WebhookConfig `yaml:"webhook"`
}

// SlackConfig holds Slack notification configuration
type SlackConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
	Channel    string `yaml:"channel"`
}

// EmailConfig holds email notification configuration
type EmailConfig struct {
	Enabled    bool     `yaml:"enabled"`
	SMTPHost   string   `yaml:"smtp_host"`
	SMTPPort   int      `yaml:"smtp_port"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	From       string   `yaml:"from"`
	Recipients []string `yaml:"recipients"`
}

// WebhookConfig holds webhook notification configuration
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Method  string `yaml:"method"`
}