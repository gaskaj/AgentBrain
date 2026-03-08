# Error Handling and Recovery

This document covers AgentBrain's comprehensive error handling and recovery system for sync operations.

## Overview

AgentBrain provides robust error handling and recovery capabilities to ensure reliable data synchronization even in the face of transient failures, network issues, or API rate limits. The system includes:

- **Rich Error Context**: Detailed error information with system state snapshots
- **Automatic Recovery**: Retry logic with exponential backoff and circuit breaker patterns
- **Partial Recovery**: Resume sync operations from the last successful checkpoint
- **Operational Visibility**: Comprehensive logging and monitoring integration
- **Manual Recovery Tools**: CLI tools for diagnosing and resolving sync issues

## Error Types and Context

### Sync Error Structure

All sync errors in AgentBrain use the `SyncError` type which provides rich context:

```go
type SyncError struct {
    Phase         SyncPhase     // Which phase of sync failed
    Object        string        // Object being processed
    BatchID       string        // Batch identifier if applicable
    RecordCount   int          // Records processed before failure
    Cause         error        // Root cause error
    Context       ErrorContext // System state at failure time
    Retryable     bool         // Whether operation can be retried
    Suggestion    string       // Actionable recovery suggestion
    StackTrace    string       // Stack trace for debugging
    Timestamp     time.Time    // When error occurred
    CorrelationID string       // Unique ID for tracing
}
```

### Sync Phases

Errors are categorized by the sync phase where they occurred:

- **Connect**: Authentication and connection establishment
- **Discover**: Object metadata discovery
- **Extract**: Data extraction from source system
- **Transform**: Data transformation and validation
- **Store**: Writing data to storage (S3/Parquet)
- **Commit**: Committing changes to Delta Lake log
- **Validate**: Data validation and consistency checks

### Error Context

Each error captures system state at failure time:

```go
type ErrorContext struct {
    ConnectorState map[string]interface{} // API client state
    StorageState   StorageMetrics         // S3 connection, disk space
    MemoryUsage    int64                  // Memory consumption (bytes)
    GoroutineCount int                    // Active goroutines
    Timestamp      time.Time              // Context capture time
    Source         string                 // Source being synced
}
```

## Recovery Patterns

### Exponential Backoff

The recovery system uses exponential backoff with jitter to handle transient failures:

- **Base delay**: 1 second (configurable)
- **Maximum delay**: 60 seconds (configurable)
- **Jitter**: ±10% random variation to prevent thundering herd
- **Max retries**: 3 attempts (configurable)

Example retry sequence:
1. First retry: ~1 second
2. Second retry: ~2 seconds  
3. Third retry: ~4 seconds

### Circuit Breaker

The circuit breaker pattern prevents cascade failures when upstream systems are unavailable:

- **Failure threshold**: 5 failures (configurable)
- **Open timeout**: 2 minutes (configurable)
- **States**: Closed → Open → Half-Open → Closed

When the circuit is open, operations fail immediately without attempting the call.

### Partial Recovery

For large objects, sync can resume from the last successful batch:

- **Checkpoint tracking**: Recovery state stored in S3
- **Batch-level resume**: Skip already processed batches
- **Watermark preservation**: Incremental sync resumes from last watermark
- **Cross-restart recovery**: Recovery state persists across agent restarts

## Configuration

### Error Handling Configuration

Configure error handling in your source configuration:

```yaml
sources:
  salesforce_prod:
    type: salesforce
    # ... other config ...
    error_handling:
      max_retries: 3
      base_delay: 1s
      max_delay: 60s
      circuit_breaker_threshold: 5
      circuit_breaker_timeout: 2m
      partial_recovery: true
      skip_failed_objects: false
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `max_retries` | Maximum retry attempts per operation | 3 |
| `base_delay` | Initial retry delay | 1s |
| `max_delay` | Maximum retry delay | 60s |
| `circuit_breaker_threshold` | Failures before circuit opens | 5 |
| `circuit_breaker_timeout` | How long circuit stays open | 2m |
| `partial_recovery` | Enable batch-level recovery | true |
| `skip_failed_objects` | Skip objects after max retries | false |

## CLI Recovery Tools

AgentBrain provides CLI tools for manual recovery operations.

### Recover Specific Object

Resume a failed sync for a specific object:

```bash
go run cmd/agentbrain/recover.go \
  -config config.yaml \
  -command recover \
  -source salesforce \
  -object Account
```

### Diagnose Error

Analyze a specific error by correlation ID:

```bash
go run cmd/agentbrain/recover.go \
  -config config.yaml \
  -command diagnose \
  -error-id sync_salesforce_1234567890
```

### Retry All Failed Operations

Attempt to retry all failed operations across all sources:

```bash
go run cmd/agentbrain/recover.go \
  -config config.yaml \
  -command retry-failed
```

### List Failed Syncs

List all current failed sync operations:

```bash
# List all failed syncs
go run cmd/agentbrain/recover.go \
  -config config.yaml \
  -command list-failed

# List failed syncs for specific source
go run cmd/agentbrain/recover.go \
  -config config.yaml \
  -command list-failed \
  -source salesforce
```

## Monitoring Integration

### Error Pattern Tracking

The health monitor tracks error patterns for operational insights:

- **Error rates by phase**: Which sync phases fail most often
- **Error rates by object**: Which objects are problematic
- **Recovery success rates**: How often retries succeed
- **Circuit breaker trips**: Upstream availability issues

### Alerts

Configure alerts for error patterns:

```yaml
monitoring:
  enabled: true
  rules:
    sync_error_rate:
      enabled: true
      threshold: 0.10  # 10% error rate
      window: 1h
      severity: warning
    
    recovery_failure_rate:
      enabled: true
      threshold: 0.20  # 20% of retries fail
      window: 1h
      severity: critical
```

### Metrics

Key metrics tracked:

- `sync_error_rate`: Percentage of sync operations that fail
- `recovery_success_rate`: Percentage of retry attempts that succeed
- `circuit_breaker_trips`: Number of times circuit breaker opens
- `errors_by_phase`: Error counts per sync phase
- `errors_by_object`: Error counts per object type

## Troubleshooting Guide

### Common Error Scenarios

#### Authentication Failures (Connect Phase)

**Symptoms:**
- Errors in connect phase
- Authentication-related error messages

**Causes:**
- Expired credentials
- Changed passwords/tokens
- Network connectivity issues

**Resolution:**
1. Verify credentials in configuration
2. Check network connectivity
3. Verify API endpoint availability
4. Rotate credentials if expired

#### Rate Limiting (Extract Phase)

**Symptoms:**
- 429 HTTP status codes
- Rate limit error messages
- Frequent retries in extract phase

**Causes:**
- API rate limits exceeded
- Too many concurrent requests
- Insufficient API quota

**Resolution:**
1. Reduce `concurrency` setting
2. Increase `base_delay` for retries
3. Request higher API limits from vendor
4. Implement request queuing

#### Storage Issues (Store Phase)

**Symptoms:**
- Errors in store phase
- S3 permission errors
- Disk space errors

**Causes:**
- S3 permission issues
- Network connectivity to S3
- Insufficient disk space
- S3 bucket policies

**Resolution:**
1. Verify S3 bucket permissions
2. Check AWS credentials
3. Monitor disk space usage
4. Review bucket policies

#### Data Validation Failures (Validate Phase)

**Symptoms:**
- Validation errors
- Schema mismatch errors
- Data quality issues

**Causes:**
- Source schema changes
- Data quality issues
- Validation rule changes

**Resolution:**
1. Review validation rules
2. Check for schema evolution
3. Update transformation logic
4. Adjust validation thresholds

### Error Code Reference

| Error Pattern | Phase | Retryable | Typical Cause |
|---------------|-------|-----------|---------------|
| `authentication_failed` | Connect | Yes | Invalid credentials |
| `rate_limit_exceeded` | Extract | Yes | API rate limiting |
| `network_timeout` | Any | Yes | Network connectivity |
| `permission_denied` | Store | No | S3 permissions |
| `schema_mismatch` | Transform | No | Schema evolution |
| `validation_failed` | Validate | No | Data quality |
| `disk_full` | Store | No | Insufficient storage |
| `memory_exhausted` | Any | No | Resource limits |

### Recovery State Management

Recovery states are stored in S3 at:
```
s3://bucket/sync/{source}/recovery/{object}.json
```

Each recovery state contains:
- Current sync phase
- Batch checkpoint information
- Retry attempt count
- Next retry time
- Error context snapshot

### Best Practices

1. **Monitor Error Patterns**: Set up alerts for unusual error rates
2. **Review Retry Configuration**: Adjust based on source system characteristics
3. **Implement Graceful Degradation**: Use `skip_failed_objects: true` for non-critical objects
4. **Regular Health Checks**: Monitor circuit breaker states
5. **Correlation ID Tracking**: Use correlation IDs for cross-system tracing
6. **Recovery Testing**: Regularly test recovery procedures
7. **Documentation**: Keep runbooks updated with common failure scenarios

### Performance Impact

The error handling system has minimal performance impact:
- **Memory**: ~1KB per active recovery state
- **Storage**: Recovery states stored compressed in S3
- **CPU**: Negligible overhead for error context capture
- **Network**: Additional S3 calls for recovery state management

For high-volume syncs, consider:
- Enabling partial recovery to minimize restart overhead
- Adjusting circuit breaker thresholds based on load
- Monitoring recovery state storage costs

## Advanced Topics

### Custom Error Handling

Implement custom error handling for connector-specific scenarios:

```go
type CustomSyncError struct {
    *sync.SyncError
    VendorErrorCode string
    RetryAfter      time.Duration
}

func (e *CustomSyncError) IsRetryable() bool {
    // Custom retry logic based on vendor error codes
    if e.VendorErrorCode == "TEMP_UNAVAILABLE" {
        return true
    }
    return e.SyncError.IsRetryable()
}
```

### Integration with External Monitoring

Export error metrics to external monitoring systems:

```go
// Export to Prometheus, DataDog, etc.
type MetricsExporter struct {
    errorCounter   prometheus.Counter
    retryHistogram prometheus.Histogram
}

func (m *MetricsExporter) RecordError(err *sync.SyncError) {
    m.errorCounter.WithLabelValues(
        string(err.Phase), 
        err.Object,
    ).Inc()
}
```

### Error Handling in Multi-tenant Environments

For multi-tenant deployments:
- Isolate recovery states by tenant
- Configure per-tenant error handling policies
- Implement tenant-specific alerting
- Use tenant-aware correlation IDs