package monitoring

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHealthMonitor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled:       true,
		CheckInterval: 1 * time.Minute,
		AlertCooldown: 5 * time.Minute,
		Rules: RulesConfig{
			AgentFailureRate: AgentFailureRateConfig{
				Enabled:   true,
				Threshold: 0.10,
				Severity:  "warning",
			},
		},
		Notifications: NotificationsConfig{
			Slack: SlackConfig{Enabled: false},
			Email: EmailConfig{Enabled: false},
			Webhook: WebhookConfig{Enabled: false},
		},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)
	assert.NotNil(t, monitor)
	assert.Equal(t, config.Enabled, monitor.config.Enabled)
	assert.Equal(t, config.CheckInterval, monitor.config.CheckInterval)
	assert.Len(t, monitor.rules, 5) // Should have 5 built-in rules
}

func TestHealthMonitor_GetHealthStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled:       true,
		CheckInterval: 1 * time.Minute,
		Rules: RulesConfig{
			AgentFailureRate: AgentFailureRateConfig{
				Enabled:   true,
				Threshold: 0.05, // Low threshold to trigger alert
				Severity:  "warning",
			},
		},
		Notifications: NotificationsConfig{},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	status, details, err := monitor.GetHealthStatus(ctx)
	require.NoError(t, err)
	assert.NotNil(t, details)
	
	// With mock data having 5% failure rate and threshold of 5%, should be OK
	// (the rule checks for > threshold, so 5% == 5% should be OK)
	assert.Equal(t, HealthStatusOK, status)
}

func TestHealthMonitor_DisabledMonitoring(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled: false, // Disabled
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	status, details, err := monitor.GetHealthStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusOK, status)
	assert.Contains(t, details, "monitoring")
	assert.Equal(t, "disabled", details["monitoring"])
}

func TestHealthMonitor_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond, // Fast interval for testing
		Rules:         RulesConfig{},
		Notifications: NotificationsConfig{},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start monitoring
	err = monitor.Start(ctx)
	require.NoError(t, err)

	// Should be running
	monitor.mu.RLock()
	isRunning := monitor.isRunning
	monitor.mu.RUnlock()
	assert.True(t, isRunning)

	// Stop monitoring
	err = monitor.Stop()
	require.NoError(t, err)

	// Should be stopped
	monitor.mu.RLock()
	isRunning = monitor.isRunning
	monitor.mu.RUnlock()
	assert.False(t, isRunning)
}

func TestHealthMonitor_CollectMetrics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled: true,
		Rules:   RulesConfig{},
		Notifications: NotificationsConfig{},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	snapshot, err := monitor.collectMetrics(ctx)
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.False(t, snapshot.Timestamp.IsZero())
	assert.GreaterOrEqual(t, snapshot.AgentMetrics.ActiveAgents, 0)
	assert.GreaterOrEqual(t, snapshot.SystemMetrics.DiskUsagePercent, 0.0)
	assert.GreaterOrEqual(t, snapshot.BusinessMetrics.WorkflowSuccessRate, 0.0)
}

func TestHealthMonitor_EnabledRules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled: true,
		Rules: RulesConfig{
			AgentFailureRate: AgentFailureRateConfig{
				Enabled: true,
			},
			WorkflowCompletion: WorkflowCompletionConfig{
				Enabled: false, // Disabled
			},
		},
		Notifications: NotificationsConfig{},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	enabledRules := monitor.enabledRules()
	
	// Count enabled rules (some built-in rules have default enabled states)
	enabledCount := 0
	for _, rule := range monitor.rules {
		if rule.IsEnabled() {
			enabledCount++
		}
	}
	
	assert.Equal(t, enabledCount, len(enabledRules))
}

func TestHealthMonitor_PerformHealthCheck(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := MonitoringConfig{
		Enabled:       true,
		CheckInterval: 1 * time.Minute,
		Rules: RulesConfig{
			AgentFailureRate: AgentFailureRateConfig{
				Enabled:   true,
				Threshold: 0.01, // Very low threshold to trigger alert
				Severity:  "warning",
			},
		},
		Notifications: NotificationsConfig{
			// No notifications configured, so alerts won't be sent
		},
	}

	monitor, err := NewHealthMonitor(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = monitor.performHealthCheck(ctx)
	require.NoError(t, err)

	// Check that last check time was updated
	monitor.mu.RLock()
	lastCheck := monitor.lastCheck
	monitor.mu.RUnlock()
	
	assert.False(t, lastCheck.IsZero())
	assert.True(t, time.Since(lastCheck) < 1*time.Second)
}