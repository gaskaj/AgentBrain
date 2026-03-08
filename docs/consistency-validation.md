# Consistency Validation

AgentBrain includes comprehensive cross-object consistency validation to ensure data integrity across related objects in your data lake. This system helps detect and recover from various types of consistency issues that can arise during sync operations.

## Overview

The consistency validation system operates as a post-sync validation phase that analyzes relationships between objects, checks for staleness violations, and ensures data freshness across dependent objects. It provides both automated detection and CLI-based recovery tools.

## Configuration

Enable consistency validation by adding a `consistency` section to your source configuration:

```yaml
sources:
  salesforce:
    type: salesforce
    enabled: true
    objects:
      - Account
      - Contact
      - Opportunity
      - Case
    consistency:
      enabled: true
      relationships:
        Account:
          - Contact
          - Opportunity
        Contact:
          - Case
      staleness_windows:
        Account: "2h"
        Contact: "1h"
        Opportunity: "4h"
        Case: "30m"
      max_staleness: "24h"
      required_objects:
        - Account
        - Contact
      fail_on_violation: false
```

### Configuration Options

- **`enabled`**: Enable/disable consistency validation for this source
- **`relationships`**: Define parent-child relationships between objects
- **`staleness_windows`**: Maximum allowed age for each object before considered stale
- **`max_staleness`**: Default staleness threshold for objects not specified in windows
- **`required_objects`**: Objects that must always be present in sync operations
- **`fail_on_violation`**: Whether to fail the entire sync on critical violations

## Violation Types

### 1. Staleness Violations

Objects that haven't been synced within their configured staleness window.

```yaml
# Example: Account object stale for 3 hours when max allowed is 2 hours
Type: staleness
Objects: [Account]
Severity: high
Description: "Object Account is stale (last sync: 3h ago, max allowed: 2h)"
SuggestedAction: "Trigger immediate sync for Account"
```

### 2. Missing Object Violations

Required dependent objects are missing when their parent was successfully synced.

```yaml
# Example: Account synced but Contact is missing
Type: missing_object
Objects: [Account, Contact]
Severity: high
Description: "Parent object Account was synced but dependent objects are missing: Contact"
SuggestedAction: "Sync missing dependent objects: Contact"
```

### 3. Watermark Drift Violations

Related objects have significantly different watermark timestamps, indicating temporal inconsistency.

```yaml
# Example: Account watermark is 6 hours ahead of Contact
Type: watermark_drift
Objects: [Account, Contact]
Severity: medium
Description: "Watermark drift between Account and Contact: 6h"
SuggestedAction: "Sync Contact to catch up with Account"
```

### 4. Transaction Boundary Violations

Related objects were synced too far apart in time, violating transaction consistency.

```yaml
# Example: Account and Contact synced 8 hours apart
Type: transaction_boundary
Objects: [Account, Contact]
Severity: medium
Description: "Sync time gap between related objects Account and Contact: 8h"
SuggestedAction: "Consider syncing related objects Account and Contact closer together"
```

## Severity Levels

### Critical
- System is in an inconsistent state requiring immediate attention
- May indicate data corruption or sync failures
- Staleness > 4x configured threshold
- Watermark drift > 24 hours

### High
- Significant consistency issues affecting data quality
- Missing required dependent objects
- Staleness > 2x configured threshold
- Watermark drift > 12 hours

### Medium
- Moderate consistency issues that should be addressed
- Transaction boundary violations
- Staleness > 1.5x configured threshold
- Watermark drift > 6 hours

### Low
- Minor consistency issues
- Staleness within configured thresholds but elevated
- Small watermark drift < 6 hours

## Recovery Commands

### Analyze Consistency

Analyze consistency violations across a time range:

```bash
# Analyze violations in the last 24 hours
./bin/agentbrain recover analyze --source salesforce --since 24h --config config.yaml

# Output as JSON for programmatic processing
./bin/agentbrain recover analyze --source salesforce --since 24h --format json --config config.yaml
```

### Repair Inconsistencies

Repair specific consistency issues using different strategies:

```bash
# Resync specific objects to fix inconsistencies
./bin/agentbrain recover repair --source salesforce --objects Account,Contact --strategy resync --config config.yaml

# Validate current state without making changes
./bin/agentbrain recover repair --source salesforce --strategy validate --config config.yaml

# Dry run to see what would be repaired
./bin/agentbrain recover repair --source salesforce --objects Account,Contact --strategy resync --dry-run --config config.yaml
```

### Validate Current State

Check current consistency status:

```bash
# Validate all objects for a source
./bin/agentbrain recover validate --source salesforce --config config.yaml
```

## Monitoring Integration

Consistency reports are automatically stored in S3 for historical analysis:

```
s3://your-bucket/consistency/{source}/reports/{sync-id}.json
```

Each report contains:
- Sync ID and timestamp
- Object sync results
- Detected violations with details
- Overall consistency status
- Suggested remediation actions

## Best Practices

### Relationship Modeling

1. **Model True Dependencies**: Only define relationships between objects that have actual business dependencies
2. **Avoid Circular Dependencies**: Ensure relationships form a directed acyclic graph
3. **Consider Sync Patterns**: Align relationships with your actual sync patterns and requirements

### Staleness Configuration

1. **Business Requirements**: Set staleness windows based on business needs, not technical constraints
2. **Graduated Thresholds**: Use shorter windows for critical objects, longer for less critical ones
3. **Monitoring Alignment**: Align with your monitoring and alerting thresholds

### Recovery Strategy

1. **Automated vs Manual**: Use `fail_on_violation: false` with monitoring for most cases
2. **Critical Objects**: Set `fail_on_violation: true` for business-critical data
3. **Recovery Planning**: Have documented procedures for common violation types

## Integration Examples

### Monitoring Dashboard

Monitor consistency metrics in your observability platform:

```yaml
# Example Grafana dashboard query
consistency_violations_total{source="salesforce", severity="critical"} > 0
```

### Alerting Rules

Set up alerts for critical violations:

```yaml
# Example alert rule
- alert: CriticalConsistencyViolation
  expr: consistency_violations_total{severity="critical"} > 0
  for: 5m
  annotations:
    summary: "Critical consistency violation detected in {{ $labels.source }}"
    description: "Source {{ $labels.source }} has {{ $value }} critical consistency violations"
```

### Automated Recovery

Implement automated recovery for common patterns:

```bash
#!/bin/bash
# Example automated recovery script
SOURCE="salesforce"
CONFIG="config.yaml"

# Check for violations
./bin/agentbrain recover validate --source $SOURCE --config $CONFIG
if [ $? -ne 0 ]; then
    echo "Violations detected, attempting repair..."
    ./bin/agentbrain recover repair --source $SOURCE --strategy resync --config $CONFIG
fi
```

## Troubleshooting

### Common Issues

**High Violation Rates**
- Check sync schedules and frequencies
- Review object dependencies and relationships
- Validate staleness window configurations

**False Positive Violations**
- Adjust staleness windows based on actual sync patterns
- Review relationship definitions for accuracy
- Consider business requirements vs technical constraints

**Performance Impact**
- Consistency validation runs post-sync and typically adds < 1% overhead
- Large numbers of objects may increase validation time
- Consider disabling for non-critical sources if needed

### Debug Mode

Enable debug logging for detailed consistency validation information:

```bash
./bin/agentbrain --config config.yaml --log-level debug
```

This provides detailed information about:
- Relationship resolution
- Violation detection logic
- Staleness calculations
- Report generation

## API Reference

### ConsistencyTracker

```go
type ConsistencyTracker struct {
    s3Store       *storage.S3Client
    source        string
    relationships map[string][]string
    windows       map[string]time.Duration
    maxStaleness  time.Duration
    logger        *slog.Logger
}

func NewConsistencyTracker(s3Store *storage.S3Client, source string, config ConsistencyConfig, logger *slog.Logger) *ConsistencyTracker

func (ct *ConsistencyTracker) ValidateSync(ctx context.Context, plans []*ObjectPlan, state *SyncState) *SyncConsistencyReport

func (ct *ConsistencyTracker) StoreReport(ctx context.Context, report *SyncConsistencyReport) error
```

### SyncConsistencyReport

```go
type SyncConsistencyReport struct {
    SyncID        string                    `json:"sync_id"`
    Timestamp     time.Time                 `json:"timestamp"`
    Source        string                    `json:"source"`
    ObjectResults map[string]ObjectResult   `json:"object_results"`
    Violations    []ConsistencyViolation    `json:"violations"`
    OverallStatus ConsistencyStatus         `json:"overall_status"`
}

func (r *SyncConsistencyReport) HasViolations() bool
func (r *SyncConsistencyReport) HasCriticalViolations() bool
```

### ConsistencyViolation

```go
type ConsistencyViolation struct {
    Type            ViolationType `json:"type"`
    Objects         []string      `json:"objects"`
    Description     string        `json:"description"`
    Severity        Severity      `json:"severity"`
    SuggestedAction string        `json:"suggested_action"`
    DetectedAt      time.Time     `json:"detected_at"`
}
```