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

func TestNewAlertManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := NotificationsConfig{
		Slack:   SlackConfig{Enabled: false},
		Email:   EmailConfig{Enabled: false},
		Webhook: WebhookConfig{Enabled: false},
	}

	am, err := NewAlertManager(config, logger)
	require.NoError(t, err)
	assert.NotNil(t, am)
	assert.Equal(t, 30*time.Minute, am.cooldownPeriod)
	assert.Empty(t, am.channels) // No channels enabled
}

func TestAlertManager_ProcessAlert(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := NotificationsConfig{
		// No channels configured - alerts should be processed but not sent
	}

	am, err := NewAlertManager(config, logger)
	require.NoError(t, err)

	alert := Alert{
		ID:           "test-alert-1",
		RuleName:     "test_rule",
		Severity:     AlertSeverityWarning,
		Status:       AlertStatusFiring,
		Message:      "Test alert message",
		Timestamp:    time.Now(),
		HealthStatus: HealthStatusWarning,
	}

	ctx := context.Background()
	err = am.ProcessAlert(ctx, alert)
	require.NoError(t, err)

	// Check that alert state was recorded
	history := am.GetAlertHistory()
	assert.Contains(t, history, "test_rule")
	assert.Equal(t, 1, history["test_rule"].Count)
}

func TestAlertManager_AlertCooldown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	am := &AlertManager{
		logger:         logger,
		alertState:     make(map[string]*AlertState),
		cooldownPeriod: 1 * time.Second, // Short cooldown for testing
		channels:       []NotificationChannel{}, // No channels
	}

	alert := Alert{
		ID:        "test-alert-1",
		RuleName:  "test_rule",
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// First alert should not be suppressed
	assert.False(t, am.shouldSuppressAlert(alert))

	// Process the alert to update state
	err := am.ProcessAlert(ctx, alert)
	require.NoError(t, err)

	// Immediate second alert should be suppressed
	alert.ID = "test-alert-2"
	alert.Timestamp = time.Now()
	assert.True(t, am.shouldSuppressAlert(alert))

	// Wait for cooldown period to expire
	time.Sleep(1100 * time.Millisecond)
	alert.ID = "test-alert-3"
	alert.Timestamp = time.Now()
	assert.False(t, am.shouldSuppressAlert(alert))
}

func TestAlertManager_GetAlertHistory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	am := &AlertManager{
		logger:     logger,
		alertState: make(map[string]*AlertState),
		channels:   []NotificationChannel{},
	}

	// Initially empty
	history := am.GetAlertHistory()
	assert.Empty(t, history)

	// Add some alert states
	now := time.Now()
	am.alertState["rule1"] = &AlertState{
		LastFired: now,
		Count:     3,
	}
	am.alertState["rule2"] = &AlertState{
		LastFired: now.Add(-1 * time.Hour),
		Count:     1,
	}

	history = am.GetAlertHistory()
	assert.Len(t, history, 2)
	assert.Contains(t, history, "rule1")
	assert.Contains(t, history, "rule2")
	assert.Equal(t, 3, history["rule1"].Count)
	assert.Equal(t, 1, history["rule2"].Count)

	// Verify it returns a copy (modifying returned history shouldn't affect internal state)
	history["rule1"].Count = 99
	assert.Equal(t, 3, am.alertState["rule1"].Count) // Original unchanged
}

func TestAlert_Struct(t *testing.T) {
	now := time.Now()
	resolvedAt := now.Add(5 * time.Minute)

	alert := Alert{
		ID:           "alert-123",
		RuleName:     "disk_usage",
		Severity:     AlertSeverityCritical,
		Status:       AlertStatusResolved,
		Message:      "Disk usage exceeded critical threshold",
		Timestamp:    now,
		HealthStatus: HealthStatusCritical,
		ResolvedAt:   &resolvedAt,
	}

	assert.Equal(t, "alert-123", alert.ID)
	assert.Equal(t, "disk_usage", alert.RuleName)
	assert.Equal(t, AlertSeverityCritical, alert.Severity)
	assert.Equal(t, AlertStatusResolved, alert.Status)
	assert.Equal(t, HealthStatusCritical, alert.HealthStatus)
	assert.NotNil(t, alert.ResolvedAt)
	assert.Equal(t, resolvedAt, *alert.ResolvedAt)
}

func TestAlertSeverity_Constants(t *testing.T) {
	assert.Equal(t, AlertSeverity("info"), AlertSeverityInfo)
	assert.Equal(t, AlertSeverity("warning"), AlertSeverityWarning)
	assert.Equal(t, AlertSeverity("critical"), AlertSeverityCritical)
}

func TestAlertStatus_Constants(t *testing.T) {
	assert.Equal(t, AlertStatus("firing"), AlertStatusFiring)
	assert.Equal(t, AlertStatus("resolved"), AlertStatusResolved)
}

func TestAlertManager_InitializeChannels_AllDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	config := NotificationsConfig{
		Slack:   SlackConfig{Enabled: false},
		Email:   EmailConfig{Enabled: false},
		Webhook: WebhookConfig{Enabled: false},
	}

	am := &AlertManager{
		config: config,
		logger: logger,
	}

	channels, err := am.initializeChannels()
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestAlertManager_ProcessAlert_NoChannels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	am := &AlertManager{
		logger:     logger,
		alertState: make(map[string]*AlertState),
		channels:   []NotificationChannel{}, // No channels
	}

	alert := Alert{
		ID:        "test-alert",
		RuleName:  "test_rule",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := am.ProcessAlert(ctx, alert)
	
	// Should not return error even with no channels
	require.NoError(t, err)
	
	// State should still be updated
	assert.Contains(t, am.alertState, "test_rule")
}