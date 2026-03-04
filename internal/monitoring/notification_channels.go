package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// NotificationChannel defines the interface for alert notification backends
type NotificationChannel interface {
	Name() string
	SendAlert(ctx context.Context, alert Alert) error
}

// SlackChannel sends alerts to Slack via webhooks
type SlackChannel struct {
	config SlackConfig
	client *http.Client
	logger *slog.Logger
}

// NewSlackChannel creates a new Slack notification channel
func NewSlackChannel(config SlackConfig, logger *slog.Logger) (*SlackChannel, error) {
	if config.WebhookURL == "" {
		return nil, fmt.Errorf("slack webhook URL is required")
	}

	return &SlackChannel{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}, nil
}

func (s *SlackChannel) Name() string {
	return "slack"
}

func (s *SlackChannel) SendAlert(ctx context.Context, alert Alert) error {
	payload := map[string]interface{}{
		"channel":    s.config.Channel,
		"username":   "AgentBrain Monitor",
		"icon_emoji": s.getEmojiForSeverity(alert.Severity),
		"attachments": []map[string]interface{}{
			{
				"color":     s.getColorForSeverity(alert.Severity),
				"title":     fmt.Sprintf("Health Alert: %s", alert.RuleName),
				"text":      alert.Message,
				"timestamp": alert.Timestamp.Unix(),
				"fields": []map[string]interface{}{
					{
						"title": "Severity",
						"value": string(alert.Severity),
						"short": true,
					},
					{
						"title": "Status",
						"value": string(alert.HealthStatus),
						"short": true,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook failed with status: %d", resp.StatusCode)
	}

	s.logger.Debug("sent alert to slack", 
		"alert_id", alert.ID,
		"channel", s.config.Channel)
	
	return nil
}

func (s *SlackChannel) getEmojiForSeverity(severity AlertSeverity) string {
	switch severity {
	case AlertSeverityCritical:
		return ":rotating_light:"
	case AlertSeverityWarning:
		return ":warning:"
	default:
		return ":information_source:"
	}
}

func (s *SlackChannel) getColorForSeverity(severity AlertSeverity) string {
	switch severity {
	case AlertSeverityCritical:
		return "danger"
	case AlertSeverityWarning:
		return "warning"
	default:
		return "good"
	}
}

// EmailChannel sends alerts via SMTP email
type EmailChannel struct {
	config EmailConfig
	logger *slog.Logger
}

// NewEmailChannel creates a new email notification channel
func NewEmailChannel(config EmailConfig, logger *slog.Logger) (*EmailChannel, error) {
	if config.SMTPHost == "" {
		return nil, fmt.Errorf("SMTP host is required")
	}
	if len(config.Recipients) == 0 {
		return nil, fmt.Errorf("at least one email recipient is required")
	}

	return &EmailChannel{
		config: config,
		logger: logger,
	}, nil
}

func (e *EmailChannel) Name() string {
	return "email"
}

func (e *EmailChannel) SendAlert(ctx context.Context, alert Alert) error {
	subject := fmt.Sprintf("[AgentBrain] %s Alert: %s", 
		strings.Title(string(alert.Severity)), alert.RuleName)
	
	body := fmt.Sprintf(`
Health Alert Notification

Rule: %s
Severity: %s
Status: %s
Time: %s

Message: %s

Alert ID: %s
`,
		alert.RuleName,
		alert.Severity,
		alert.HealthStatus,
		alert.Timestamp.Format(time.RFC3339),
		alert.Message,
		alert.ID,
	)

	msg := fmt.Sprintf("Subject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", subject, body)

	// Setup authentication if configured
	var auth smtp.Auth
	if e.config.Username != "" && e.config.Password != "" {
		auth = smtp.PlainAuth("", e.config.Username, e.config.Password, e.config.SMTPHost)
	}

	// Send email to all recipients
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)
	from := e.config.From
	if from == "" {
		from = e.config.Username
	}

	err := smtp.SendMail(addr, auth, from, e.config.Recipients, []byte(msg))
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	e.logger.Debug("sent alert via email",
		"alert_id", alert.ID,
		"recipients", len(e.config.Recipients))

	return nil
}

// WebhookChannel sends alerts to arbitrary HTTP endpoints
type WebhookChannel struct {
	config WebhookConfig
	client *http.Client
	logger *slog.Logger
}

// NewWebhookChannel creates a new webhook notification channel
func NewWebhookChannel(config WebhookConfig, logger *slog.Logger) (*WebhookChannel, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("webhook URL is required")
	}

	method := config.Method
	if method == "" {
		method = "POST"
	}

	return &WebhookChannel{
		config: WebhookConfig{
			Enabled: config.Enabled,
			URL:     config.URL,
			Method:  method,
		},
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}, nil
}

func (w *WebhookChannel) Name() string {
	return "webhook"
}

func (w *WebhookChannel) SendAlert(ctx context.Context, alert Alert) error {
	payload := map[string]interface{}{
		"alert_id":      alert.ID,
		"rule_name":     alert.RuleName,
		"severity":      alert.Severity,
		"status":        alert.Status,
		"health_status": alert.HealthStatus,
		"message":       alert.Message,
		"timestamp":     alert.Timestamp.Unix(),
		"source":        "agentbrain-monitor",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, w.config.Method, w.config.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agentbrain-monitor/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook failed with status: %d", resp.StatusCode)
	}

	w.logger.Debug("sent alert via webhook",
		"alert_id", alert.ID,
		"url", w.config.URL,
		"method", w.config.Method)

	return nil
}