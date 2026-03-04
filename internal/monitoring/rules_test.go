package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAgentFailureRateRule(t *testing.T) {
	config := AgentFailureRateConfig{
		Enabled:   true,
		Threshold: 0.10, // 10%
		Severity:  "warning",
	}
	
	rule := NewAgentFailureRateRule(config)
	assert.Equal(t, "agent_failure_rate", rule.Name())
	assert.Equal(t, AlertSeverityWarning, rule.Severity())
	assert.True(t, rule.IsEnabled())

	ctx := context.Background()
	
	// Test with failure rate below threshold
	snapshot := MetricsSnapshot{
		AgentMetrics: AgentMetrics{
			FailureRate: 0.05, // 5%
		},
	}
	
	status, message, err := rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, message, "5.00%")
	assert.Contains(t, message, "within threshold")

	// Test with failure rate above threshold
	snapshot.AgentMetrics.FailureRate = 0.15 // 15%
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusCritical, status)
	assert.Contains(t, message, "15.00%")
	assert.Contains(t, message, "exceeds threshold")
}

func TestWorkflowCompletionRule(t *testing.T) {
	config := WorkflowCompletionConfig{
		Enabled:        true,
		MinSuccessRate: 0.80, // 80%
		Severity:       "critical",
	}
	
	rule := NewWorkflowCompletionRule(config)
	assert.Equal(t, "workflow_completion", rule.Name())
	assert.Equal(t, AlertSeverityCritical, rule.Severity())
	assert.True(t, rule.IsEnabled())

	ctx := context.Background()
	
	// Test with success rate above minimum
	snapshot := MetricsSnapshot{
		BusinessMetrics: BusinessMetrics{
			WorkflowSuccessRate: 0.90, // 90%
		},
	}
	
	status, message, err := rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, message, "90.00%")
	assert.Contains(t, message, "above minimum")

	// Test with success rate below minimum
	snapshot.BusinessMetrics.WorkflowSuccessRate = 0.70 // 70%
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusWarning, status)
	assert.Contains(t, message, "70.00%")
	assert.Contains(t, message, "below minimum")
}

func TestDiskUsageRule(t *testing.T) {
	config := DiskUsageConfig{
		Enabled:           true,
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		Severity:          "warning",
	}
	
	rule := NewDiskUsageRule(config)
	assert.Equal(t, "disk_usage", rule.Name())
	assert.Equal(t, AlertSeverityWarning, rule.Severity())
	assert.True(t, rule.IsEnabled())

	ctx := context.Background()
	
	// Test with usage below warning threshold
	snapshot := MetricsSnapshot{
		SystemMetrics: SystemMetrics{
			DiskUsagePercent: 75.0,
		},
	}
	
	status, message, err := rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, message, "75.0%")
	assert.Contains(t, message, "within acceptable limits")

	// Test with usage above warning threshold
	snapshot.SystemMetrics.DiskUsagePercent = 85.0
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusWarning, status)
	assert.Contains(t, message, "85.0%")
	assert.Contains(t, message, "warning threshold")

	// Test with usage above critical threshold
	snapshot.SystemMetrics.DiskUsagePercent = 95.0
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusCritical, status)
	assert.Contains(t, message, "95.0%")
	assert.Contains(t, message, "critical threshold")
}

func TestMemoryUsageRule(t *testing.T) {
	config := MemoryUsageConfig{
		Enabled:           true,
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		Severity:          "warning",
	}
	
	rule := NewMemoryUsageRule(config)
	assert.Equal(t, "memory_usage", rule.Name())
	assert.Equal(t, AlertSeverityWarning, rule.Severity())
	assert.True(t, rule.IsEnabled())

	ctx := context.Background()
	
	// Test normal memory usage
	snapshot := MetricsSnapshot{
		SystemMetrics: SystemMetrics{
			MemoryUsagePercent: 60.0,
		},
	}
	
	status, message, err := rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, message, "acceptable limits")
}

func TestAPIResponseTimeRule(t *testing.T) {
	config := APIResponseTimeConfig{
		Enabled:           true,
		WarningThreshold:  5 * time.Second,
		CriticalThreshold: 10 * time.Second,
		Severity:          "warning",
	}
	
	rule := NewAPIResponseTimeRule(config)
	assert.Equal(t, "api_response_time", rule.Name())
	assert.Equal(t, AlertSeverityWarning, rule.Severity())
	assert.True(t, rule.IsEnabled())

	ctx := context.Background()
	
	// Test with normal response time
	snapshot := MetricsSnapshot{
		SystemMetrics: SystemMetrics{
			APIResponseTime: 2 * time.Second,
		},
	}
	
	status, message, err := rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, message, "within acceptable limits")

	// Test with slow response time
	snapshot.SystemMetrics.APIResponseTime = 7 * time.Second
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusWarning, status)
	assert.Contains(t, message, "warning threshold")

	// Test with very slow response time
	snapshot.SystemMetrics.APIResponseTime = 15 * time.Second
	
	status, message, err = rule.Check(ctx, snapshot)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusCritical, status)
	assert.Contains(t, message, "critical threshold")
}

func TestRuleDisabled(t *testing.T) {
	config := AgentFailureRateConfig{
		Enabled:   false, // Disabled
		Threshold: 0.10,
		Severity:  "warning",
	}
	
	rule := NewAgentFailureRateRule(config)
	assert.False(t, rule.IsEnabled())
}

func TestRuleConfigDefaults(t *testing.T) {
	// Test with empty severity
	config := DiskUsageConfig{
		Enabled:           true,
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		// Severity not set - should default
	}
	
	rule := NewDiskUsageRule(config)
	assert.Equal(t, AlertSeverityWarning, rule.Severity())
}