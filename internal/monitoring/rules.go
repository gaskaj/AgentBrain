package monitoring

import (
	"context"
	"fmt"
)

// AgentFailureRateRule monitors agent failure rates
type AgentFailureRateRule struct {
	config AgentFailureRateConfig
}

func NewAgentFailureRateRule(config AgentFailureRateConfig) *AgentFailureRateRule {
	return &AgentFailureRateRule{config: config}
}

func (r *AgentFailureRateRule) Name() string {
	return "agent_failure_rate"
}

func (r *AgentFailureRateRule) Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error) {
	if snapshot.AgentMetrics.FailureRate > r.config.Threshold {
		return HealthStatusCritical, 
			fmt.Sprintf("Agent failure rate %.2f%% exceeds threshold %.2f%%", 
				snapshot.AgentMetrics.FailureRate*100, r.config.Threshold*100), 
			nil
	}
	
	return HealthStatusOK, 
		fmt.Sprintf("Agent failure rate %.2f%% is within threshold", 
			snapshot.AgentMetrics.FailureRate*100), 
		nil
}

func (r *AgentFailureRateRule) Severity() AlertSeverity {
	return AlertSeverity(r.config.Severity)
}

func (r *AgentFailureRateRule) IsEnabled() bool {
	return r.config.Enabled
}

// WorkflowCompletionRule monitors workflow completion rates
type WorkflowCompletionRule struct {
	config WorkflowCompletionConfig
}

func NewWorkflowCompletionRule(config WorkflowCompletionConfig) *WorkflowCompletionRule {
	return &WorkflowCompletionRule{config: config}
}

func (r *WorkflowCompletionRule) Name() string {
	return "workflow_completion"
}

func (r *WorkflowCompletionRule) Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error) {
	if snapshot.BusinessMetrics.WorkflowSuccessRate < r.config.MinSuccessRate {
		return HealthStatusWarning,
			fmt.Sprintf("Workflow success rate %.2f%% is below minimum %.2f%%",
				snapshot.BusinessMetrics.WorkflowSuccessRate*100, r.config.MinSuccessRate*100),
			nil
	}
	
	return HealthStatusOK,
		fmt.Sprintf("Workflow success rate %.2f%% is above minimum",
			snapshot.BusinessMetrics.WorkflowSuccessRate*100),
		nil
}

func (r *WorkflowCompletionRule) Severity() AlertSeverity {
	return AlertSeverity(r.config.Severity)
}

func (r *WorkflowCompletionRule) IsEnabled() bool {
	return r.config.Enabled
}

// DiskUsageRule monitors disk space usage
type DiskUsageRule struct {
	config DiskUsageConfig
}

func NewDiskUsageRule(config DiskUsageConfig) *DiskUsageRule {
	return &DiskUsageRule{config: config}
}

func (r *DiskUsageRule) Name() string {
	return "disk_usage"
}

func (r *DiskUsageRule) Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error) {
	usage := snapshot.SystemMetrics.DiskUsagePercent
	
	if usage > r.config.CriticalThreshold {
		return HealthStatusCritical,
			fmt.Sprintf("Disk usage %.1f%% exceeds critical threshold %.1f%%",
				usage, r.config.CriticalThreshold),
			nil
	} else if usage > r.config.WarningThreshold {
		return HealthStatusWarning,
			fmt.Sprintf("Disk usage %.1f%% exceeds warning threshold %.1f%%",
				usage, r.config.WarningThreshold),
			nil
	}
	
	return HealthStatusOK,
		fmt.Sprintf("Disk usage %.1f%% is within acceptable limits", usage),
		nil
}

func (r *DiskUsageRule) Severity() AlertSeverity {
	if r.config.Severity != "" {
		return AlertSeverity(r.config.Severity)
	}
	return AlertSeverityWarning
}

func (r *DiskUsageRule) IsEnabled() bool {
	return r.config.Enabled
}

// MemoryUsageRule monitors memory usage
type MemoryUsageRule struct {
	config MemoryUsageConfig
}

func NewMemoryUsageRule(config MemoryUsageConfig) *MemoryUsageRule {
	return &MemoryUsageRule{config: config}
}

func (r *MemoryUsageRule) Name() string {
	return "memory_usage"
}

func (r *MemoryUsageRule) Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error) {
	usage := snapshot.SystemMetrics.MemoryUsagePercent
	
	if usage > r.config.CriticalThreshold {
		return HealthStatusCritical,
			fmt.Sprintf("Memory usage %.1f%% exceeds critical threshold %.1f%%",
				usage, r.config.CriticalThreshold),
			nil
	} else if usage > r.config.WarningThreshold {
		return HealthStatusWarning,
			fmt.Sprintf("Memory usage %.1f%% exceeds warning threshold %.1f%%",
				usage, r.config.WarningThreshold),
			nil
	}
	
	return HealthStatusOK,
		fmt.Sprintf("Memory usage %.1f%% is within acceptable limits", usage),
		nil
}

func (r *MemoryUsageRule) Severity() AlertSeverity {
	if r.config.Severity != "" {
		return AlertSeverity(r.config.Severity)
	}
	return AlertSeverityWarning
}

func (r *MemoryUsageRule) IsEnabled() bool {
	return r.config.Enabled
}

// APIResponseTimeRule monitors API response times
type APIResponseTimeRule struct {
	config APIResponseTimeConfig
}

func NewAPIResponseTimeRule(config APIResponseTimeConfig) *APIResponseTimeRule {
	return &APIResponseTimeRule{config: config}
}

func (r *APIResponseTimeRule) Name() string {
	return "api_response_time"
}

func (r *APIResponseTimeRule) Check(ctx context.Context, snapshot MetricsSnapshot) (HealthStatus, string, error) {
	responseTime := snapshot.SystemMetrics.APIResponseTime
	
	if responseTime > r.config.CriticalThreshold {
		return HealthStatusCritical,
			fmt.Sprintf("API response time %v exceeds critical threshold %v",
				responseTime, r.config.CriticalThreshold),
			nil
	} else if responseTime > r.config.WarningThreshold {
		return HealthStatusWarning,
			fmt.Sprintf("API response time %v exceeds warning threshold %v",
				responseTime, r.config.WarningThreshold),
			nil
	}
	
	return HealthStatusOK,
		fmt.Sprintf("API response time %v is within acceptable limits", responseTime),
		nil
}

func (r *APIResponseTimeRule) Severity() AlertSeverity {
	if r.config.Severity != "" {
		return AlertSeverity(r.config.Severity)
	}
	return AlertSeverityWarning
}

func (r *APIResponseTimeRule) IsEnabled() bool {
	return r.config.Enabled
}

