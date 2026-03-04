package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertStatus represents the current status of an alert
type AlertStatus string

const (
	AlertStatusFiring   AlertStatus = "firing"
	AlertStatusResolved AlertStatus = "resolved"
)

// Alert represents a health monitoring alert
type Alert struct {
	ID            string        `json:"id"`
	RuleName      string        `json:"rule_name"`
	Severity      AlertSeverity `json:"severity"`
	Status        AlertStatus   `json:"status"`
	Message       string        `json:"message"`
	Timestamp     time.Time     `json:"timestamp"`
	HealthStatus  HealthStatus  `json:"health_status"`
	ResolvedAt    *time.Time    `json:"resolved_at,omitempty"`
}

// AlertManager handles alert processing and notification dispatch
type AlertManager struct {
	config           NotificationsConfig
	channels         []NotificationChannel
	logger           *slog.Logger
	alertState       map[string]*AlertState
	mu               sync.RWMutex
	cooldownPeriod   time.Duration
}

// AlertState tracks the state of an alert for cooldown management
type AlertState struct {
	LastFired time.Time
	Count     int
}

// NewAlertManager creates a new alert management system
func NewAlertManager(config NotificationsConfig, logger *slog.Logger) (*AlertManager, error) {
	am := &AlertManager{
		config:         config,
		logger:         logger,
		alertState:     make(map[string]*AlertState),
		cooldownPeriod: 30 * time.Minute, // Default cooldown period
	}

	// Initialize notification channels
	channels, err := am.initializeChannels()
	if err != nil {
		return nil, fmt.Errorf("initialize notification channels: %w", err)
	}
	am.channels = channels

	return am, nil
}

// ProcessAlert processes an incoming alert and dispatches notifications if needed
func (am *AlertManager) ProcessAlert(ctx context.Context, alert Alert) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Check if we should suppress this alert due to cooldown
	if am.shouldSuppressAlert(alert) {
		am.logger.Debug("alert suppressed due to cooldown",
			"alert_id", alert.ID,
			"rule", alert.RuleName)
		return nil
	}

	// Update alert state
	state := am.alertState[alert.RuleName]
	if state == nil {
		state = &AlertState{}
		am.alertState[alert.RuleName] = state
	}
	state.LastFired = alert.Timestamp
	state.Count++

	am.logger.Info("processing alert",
		"alert_id", alert.ID,
		"rule", alert.RuleName,
		"severity", alert.Severity,
		"message", alert.Message)

	// Dispatch to all configured notification channels
	var errors []error
	for _, channel := range am.channels {
		if err := channel.SendAlert(ctx, alert); err != nil {
			am.logger.Error("failed to send alert via channel",
				"channel", channel.Name(),
				"alert_id", alert.ID,
				"error", err)
			errors = append(errors, err)
		}
	}

	// Return error if all channels failed
	if len(errors) == len(am.channels) && len(am.channels) > 0 {
		return fmt.Errorf("all notification channels failed")
	}

	return nil
}

// GetAlertHistory returns recent alert history
func (am *AlertManager) GetAlertHistory() map[string]*AlertState {
	am.mu.RLock()
	defer am.mu.RUnlock()
	
	history := make(map[string]*AlertState)
	for k, v := range am.alertState {
		history[k] = &AlertState{
			LastFired: v.LastFired,
			Count:     v.Count,
		}
	}
	return history
}

func (am *AlertManager) shouldSuppressAlert(alert Alert) bool {
	state := am.alertState[alert.RuleName]
	if state == nil {
		return false
	}

	// Suppress if within cooldown period
	return time.Since(state.LastFired) < am.cooldownPeriod
}

func (am *AlertManager) initializeChannels() ([]NotificationChannel, error) {
	var channels []NotificationChannel

	// Initialize Slack channel if configured
	if am.config.Slack.Enabled {
		slack, err := NewSlackChannel(am.config.Slack, am.logger)
		if err != nil {
			return nil, fmt.Errorf("create slack channel: %w", err)
		}
		channels = append(channels, slack)
	}

	// Initialize Email channel if configured
	if am.config.Email.Enabled {
		email, err := NewEmailChannel(am.config.Email, am.logger)
		if err != nil {
			return nil, fmt.Errorf("create email channel: %w", err)
		}
		channels = append(channels, email)
	}

	// Initialize Webhook channel if configured
	if am.config.Webhook.Enabled {
		webhook, err := NewWebhookChannel(am.config.Webhook, am.logger)
		if err != nil {
			return nil, fmt.Errorf("create webhook channel: %w", err)
		}
		channels = append(channels, webhook)
	}

	am.logger.Info("initialized notification channels", "count", len(channels))
	return channels, nil
}

