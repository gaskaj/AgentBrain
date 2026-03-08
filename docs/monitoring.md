# Monitoring and Alerting

This document describes the monitoring and alerting system that provides health monitoring, rule-based alerts, metrics analysis, and data validation monitoring.

## Overview

The monitoring system continuously assesses system health by:

1. **Collecting system and business metrics** from various sources
2. **Evaluating health rules** against current metrics
3. **Monitoring data validation results** and schema drift
4. **Triggering alerts** when thresholds are exceeded
5. **Tracking trends** for proactive monitoring
6. **Providing health endpoints** for external monitoring systems

## Architecture

### Core Components

#### Health Monitor (`internal/monitoring/health_monitor.go`)
- Orchestrates periodic health checks
- Collects metrics snapshots
- Evaluates monitoring rules
- Manages alert cooldowns and notifications

#### Monitoring Rules (`internal/monitoring/rules.go`)
- **System Rules**: Disk usage, memory usage, API response time
- **Business Rules**: Agent failure rate, workflow completion rate  
- **Validation Rules**: Data validation error rates, schema drift detection

#### Metrics Analyzer (`internal/monitoring/metrics_analyzer.go`)
- Provides trend analysis capabilities
- Detects anomalies in metric patterns
- Calculates statistical measures over time windows
- Supports proactive alerting

#### Alert System (`internal/monitoring/alerting.go`)
- Manages alert lifecycle and state
- Implements alert cooldown periods
- Routes alerts to configured notification channels
- Provides alert history and acknowledgment

## Configuration

### Basic Monitoring Configuration

```yaml
monitoring:
  enabled: true
  check_interval: 5m           # How often to run health checks
  alert_cooldown: 30m          # Minimum time between duplicate alerts
  notification_channels:       # Alert destinations
    - type: "log"
      config:
        level: "warn"
    - type: "webhook"
      config:
        url: "https://hooks.slack.com/services/..."
        
  rules:                       # Health monitoring rules
    disk_usage:
      enabled: true
      warning_threshold: 80.0   # Warn at 80% disk usage
      critical_threshold: 90.0  # Critical at 90% disk usage
      severity: "warning"
      
    memory_usage:
      enabled: true
      warning_threshold: 80.0
      critical_threshold: 90.0
      severity: "warning"
      
    api_response_time:
      enabled: true
      warning_threshold: 5s
      critical_threshold: 10s
      severity: "warning"
      
    agent_failure_rate:
      enabled: true
      threshold: 0.10           # Alert if >10% of agent operations fail
      window: 1h               # Over the last hour
      severity: "warning"
      
    workflow_completion:
      enabled: true
      min_success_rate: 0.80    # Alert if <80% workflows complete successfully
      window: 6h               # Over the last 6 hours
      severity: "critical"
      
    # Data validation monitoring rules
    validation_error_rate:
      enabled: true
      threshold: 0.05          # Alert if >5% validation errors
      critical_threshold: 0.15 # Critical if >15% validation errors
      severity: "warning"
      
    schema_drift:
      enabled: true
      drift_threshold: 0.10    # Alert if drift score >0.10
      critical_threshold: 0.25 # Critical if drift score >0.25
      severity: "warning"
```

### Rule Configuration Reference

#### System Rules

**Disk Usage Rule**
```yaml
disk_usage:
  enabled: true
  warning_threshold: 80.0    # Percentage
  critical_threshold: 90.0   # Percentage
  severity: "warning"        # Alert severity level
```

**Memory Usage Rule**  
```yaml
memory_usage:
  enabled: true
  warning_threshold: 80.0    # Percentage
  critical_threshold: 90.0   # Percentage
  severity: "warning"
```

**API Response Time Rule**
```yaml
api_response_time:
  enabled: true
  warning_threshold: 5s      # Duration
  critical_threshold: 10s    # Duration
  severity: "warning"
```

#### Business Rules

**Agent Failure Rate Rule**
```yaml
agent_failure_rate:
  enabled: true
  threshold: 0.10           # 10% failure rate threshold
  window: 1h               # Time window for calculation
  severity: "warning"
```

**Workflow Completion Rule**
```yaml
workflow_completion:
  enabled: true
  min_success_rate: 0.80    # 80% minimum success rate
  window: 6h               # Time window for calculation
  severity: "critical"
```

#### Data Validation Rules

**Validation Error Rate Rule**
```yaml
validation_error_rate:
  enabled: true
  threshold: 0.05          # 5% error rate threshold
  critical_threshold: 0.15  # 15% critical threshold
  severity: "warning"
```

**Schema Drift Rule**
```yaml
schema_drift:
  enabled: true
  drift_threshold: 0.10    # Drift score threshold
  critical_threshold: 0.25 # Critical drift threshold
  severity: "warning"
```

## Data Validation Monitoring

The monitoring system integrates with the data validation framework to provide comprehensive data quality monitoring.

### Validation Metrics

The system tracks the following validation metrics:

- **Error Rates**: Percentage of records with validation errors per object
- **Warning Rates**: Percentage of records with validation warnings
- **Field Population Rates**: Percentage of records with non-null values per field
- **Type Consistency**: Distribution of data types found vs expected
- **Processing Time**: Time spent on validation operations

### Schema Drift Detection

Schema drift monitoring tracks changes in data patterns over time:

- **Population Drift**: Changes in field population rates
- **Type Drift**: Changes in field type distributions
- **Statistical Drift**: Changes in numeric field statistics (mean, range)
- **Pattern Drift**: Changes in value patterns and formats

### Validation Alerts

The system generates alerts for validation issues:

#### Error Threshold Alerts
Triggered when validation error rates exceed configured thresholds:
```json
{
  "type": "validation_error_rate",
  "severity": "warning",
  "object_name": "Account",
  "message": "Validation error rate 7.5% exceeds threshold 5.0%",
  "threshold": 0.05,
  "actual_value": 0.075,
  "timestamp": "2023-01-01T12:00:00Z"
}
```

#### Drift Detection Alerts
Triggered when schema drift is detected:
```json
{
  "type": "schema_drift",
  "severity": "medium",
  "object_name": "Contact",
  "message": "Schema drift detected for object Contact (score: 0.15)",
  "actual_value": 0.15,
  "timestamp": "2023-01-01T12:00:00Z"
}
```

#### Data Quality Alerts
Triggered for data quality issues:
```json
{
  "type": "data_quality",
  "severity": "low",
  "object_name": "Lead",
  "field_name": "Email",
  "message": "Data quality issue in Lead.Email: Invalid email format detected",
  "timestamp": "2023-01-01T12:00:00Z"
}
```

## Health Endpoints

The system exposes HTTP endpoints for external monitoring:

### `/healthz`
Returns basic health status:
```json
{
  "status": "healthy",
  "timestamp": "2023-01-01T12:00:00Z",
  "version": "1.0.0"
}
```

### `/readyz`
Returns readiness status including dependency checks:
```json
{
  "status": "ready",
  "checks": {
    "database": "ok",
    "s3": "ok",
    "external_apis": "ok"
  },
  "timestamp": "2023-01-01T12:00:00Z"
}
```

### `/metrics`
Returns detailed metrics for external monitoring systems:
```json
{
  "system_metrics": {
    "disk_usage_percent": 75.2,
    "memory_usage_percent": 68.5,
    "api_response_time": "2.3s"
  },
  "business_metrics": {
    "workflow_success_rate": 0.85,
    "token_usage_percent": 45.2
  },
  "validation_metrics": {
    "error_rate": 0.02,
    "warning_rate": 0.05,
    "drift_score": 0.08
  }
}
```

## Notification Channels

The system supports multiple notification channels for alerts:

### Log Channel
Writes alerts to application logs:
```yaml
notification_channels:
  - type: "log"
    config:
      level: "warn"    # Log level for alerts
```

### Webhook Channel
Sends alerts to HTTP endpoints (Slack, Teams, etc.):
```yaml
notification_channels:
  - type: "webhook"
    config:
      url: "https://hooks.slack.com/services/..."
      method: "POST"
      headers:
        Content-Type: "application/json"
      template: |
        {
          "text": "Alert: {{.Message}}",
          "channel": "#data-engineering",
          "username": "AgentBrain"
        }
```

### Email Channel
Sends email notifications:
```yaml
notification_channels:
  - type: "email"
    config:
      smtp_host: "smtp.company.com"
      smtp_port: 587
      username: "alerts@company.com"
      password: "${EMAIL_PASSWORD}"
      from: "alerts@company.com"
      to: ["team@company.com"]
      subject: "AgentBrain Alert: {{.Type}}"
```

## Metrics Collection

### System Metrics
Collected from the host system:
- CPU usage percentage
- Memory usage percentage  
- Disk usage percentage
- Network I/O rates
- API response times

### Business Metrics
Collected from application operations:
- Sync success/failure rates
- Data processing volumes
- Workflow completion rates
- Token/resource usage

### Validation Metrics
Collected from data validation operations:
- Record validation error rates
- Field population rates
- Schema drift scores
- Data quality scores
- Validation processing times

## Alert Management

### Alert States
Alerts progress through states:
- **Triggered**: Alert condition detected
- **Active**: Alert is active and notifications sent
- **Acknowledged**: Alert acknowledged by operator
- **Resolved**: Alert condition no longer exists

### Cooldown Periods
Prevents alert spam with configurable cooldown periods:
- Duplicate alerts suppressed during cooldown
- Escalation after prolonged alert conditions
- Different cooldowns per alert type/severity

### Alert History
The system maintains alert history for:
- Trend analysis and pattern detection
- Incident response and post-mortems
- Compliance and audit requirements
- System optimization insights

## Trend Analysis

### Metrics History
The system maintains historical metrics for trend analysis:
- Configurable retention periods
- Statistical analysis over time windows
- Anomaly detection based on historical patterns
- Proactive alerting for degrading trends

### Drift Analysis
Specialized analysis for schema drift:
- Field-level drift tracking
- Population rate trend analysis
- Type distribution changes
- Correlation with external events

## Best Practices

### Threshold Tuning
- Start with conservative thresholds
- Monitor for false positives/negatives
- Adjust based on historical patterns
- Different thresholds for different environments

### Alert Management
- Use appropriate severity levels
- Configure meaningful alert messages
- Set reasonable cooldown periods
- Implement alert escalation procedures

### Monitoring Coverage
- Monitor all critical system components
- Include business-specific metrics
- Add validation monitoring for data quality
- Cover both reactive and proactive scenarios

### Performance Considerations
- Balance monitoring frequency with system load
- Use sampling for high-volume metrics
- Implement efficient metric storage
- Consider monitoring overhead in capacity planning

## Troubleshooting

### High Alert Volume
1. Check alert cooldown settings
2. Review threshold configurations
3. Analyze alert patterns for root causes
4. Consider alert grouping/aggregation

### Missing Alerts
1. Verify rule configurations are enabled
2. Check metric collection is working
3. Validate notification channel configs
4. Review alert history for patterns

### False Positives
1. Analyze historical metric patterns
2. Adjust thresholds based on normal ranges
3. Consider time-of-day or seasonal patterns
4. Implement smart alerting with context

### Performance Impact
1. Monitor system resource usage
2. Optimize metric collection frequency
3. Use efficient storage for metrics history
4. Consider async processing for heavy operations

## Integration Examples

### Grafana Integration
Export metrics to Grafana for visualization:
```yaml
monitoring:
  exporters:
    - type: "prometheus"
      config:
        endpoint: ":9090/metrics"
        interval: 30s
```

### Datadog Integration  
Send metrics to Datadog:
```yaml
monitoring:
  exporters:
    - type: "datadog"
      config:
        api_key: "${DATADOG_API_KEY}"
        site: "datadoghq.com"
```

### PagerDuty Integration
Route critical alerts to PagerDuty:
```yaml
notification_channels:
  - type: "pagerduty"
    config:
      integration_key: "${PD_INTEGRATION_KEY}"
      severity_mapping:
        critical: "error"
        warning: "warning"
        info: "info"
```