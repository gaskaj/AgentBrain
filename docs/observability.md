# AgentBrain Observability Framework

## Overview

The AgentBrain observability framework provides comprehensive distributed tracing, metrics collection, and structured logging for production operations. This framework enables operators to monitor sync operations end-to-end, identify performance bottlenecks, and troubleshoot issues across the distributed sync pipeline.

## Features

### Distributed Tracing
- **OpenTelemetry Integration**: Standards-based distributed tracing with multiple exporter support
- **End-to-End Visibility**: Trace sync operations from connector APIs through storage operations
- **Correlation IDs**: Automatic correlation of operations across components
- **Multiple Exporters**: Support for Jaeger, Zipkin, and OTLP exporters

### Enhanced Metrics
- **Business Metrics**: Data volume, sync success rates, schema changes, connector health
- **Operational Metrics**: API latency, storage performance, Delta Lake operations
- **System Metrics**: Circuit breaker states, retry attempts, resource utilization
- **HTTP Metrics**: Request/response patterns for health endpoints

### Structured Logging
- **Contextual Logging**: Automatic correlation ID injection and structured context
- **Component-Specific Loggers**: Dedicated loggers for sync, connector, and storage operations
- **Trace Integration**: Correlation between logs and distributed traces

## Configuration

### Basic Configuration

```yaml
observability:
  tracing:
    enabled: true
    exporter: "jaeger"
    endpoint: "http://localhost:14268/api/traces"
    sample_rate: 0.1
    service_name: "agentbrain"
    service_version: "1.0.0"
  
  metrics:
    enabled: true
    exporter: "prometheus"
    endpoint: ":9090"
    collection_interval: 30s
    business_metrics: true
    http_metrics: true
    
  logging:
    correlation_ids: true
    structured_context: true
    trace_integration: true
```

### Exporter Configuration

#### Jaeger
```yaml
observability:
  tracing:
    exporter: "jaeger"
    endpoint: "http://jaeger-collector:14268/api/traces"
```

#### Zipkin
```yaml
observability:
  tracing:
    exporter: "zipkin" 
    endpoint: "http://zipkin:9411/api/v2/spans"
```

#### OTLP (OpenTelemetry Protocol)
```yaml
observability:
  tracing:
    exporter: "otlp"
    endpoint: "http://otel-collector:4318/v1/traces"
```

#### Prometheus Metrics
```yaml
observability:
  metrics:
    exporter: "prometheus"
    endpoint: ":9090"
    business_metrics: true
    http_metrics: true
```

## Trace Structure

### Sync Operation Traces
```
sync.connect (source: salesforce)
├── connector.salesforce.authenticate
│   ├── http.POST /services/oauth2/token
│   └── duration: 250ms
├── sync.discover (source: salesforce)  
│   ├── connector.salesforce.discover_metadata
│   │   ├── http.GET /services/data/v59.0/sobjects
│   │   └── objects_discovered: 15
│   └── duration: 1.2s
├── sync.extract (object: Account)
│   ├── connector.salesforce.extract_data
│   │   ├── http.GET /services/data/v59.0/query
│   │   └── records_extracted: 10000
│   ├── storage.upload (type: s3)
│   │   ├── s3.put_object
│   │   └── size_bytes: 2048000
│   └── storage.commit (type: delta)
│       ├── delta.commit
│       └── version: 125
└── total_duration: 45s
```

### Connector API Traces
```
connector.salesforce.extract_data
├── span_attributes:
│   ├── connector.name: salesforce
│   ├── connector.operation: extract_data
│   ├── connector.method: bulk_api
│   ├── sobject: Account
│   └── correlation_id: sync_1234567890
├── connector.salesforce.authenticate
├── connector.salesforce.create_job
├── connector.salesforce.add_batch
├── connector.salesforce.poll_batch
└── connector.salesforce.get_results
```

### Storage Operation Traces  
```
storage.upload
├── span_attributes:
│   ├── storage.operation: upload
│   ├── storage.type: s3
│   ├── s3.bucket: agentbrain-data
│   ├── s3.key: sync/salesforce/Account/data.parquet
│   └── s3.content_length: 2048000
└── duration: 500ms

storage.commit
├── span_attributes:
│   ├── storage.operation: commit
│   ├── storage.type: delta
│   ├── delta.table: salesforce_Account
│   └── delta.version: 125
└── duration: 150ms
```

## Metrics Catalog

### Sync Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `sync_operations_total` | Counter | Total number of sync operations by source |
| `sync_operations_duration_seconds` | Histogram | Sync operation duration by source |
| `sync_operations_success_rate` | Gauge | Success rate per source (0-1) |
| `sync_records_processed_total` | Counter | Total records processed by source and object |
| `sync_data_volume_bytes_total` | Counter | Total data volume synced by source |

### Connector Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `connector_api_calls_total` | Counter | API calls by connector and operation |
| `connector_api_duration_seconds` | Histogram | API call duration by connector |
| `connector_auth_failures_total` | Counter | Authentication failures by connector |
| `connector_rate_limit_hits_total` | Counter | Rate limit hits by connector |
| `connector_health_status` | Gauge | Connector health status (1=healthy, 0=unhealthy) |

### Storage Metrics  
| Metric | Type | Description |
|--------|------|-------------|
| `storage_operations_total` | Counter | Storage operations by type and operation |
| `storage_operations_duration_seconds` | Histogram | Storage operation duration |
| `storage_operations_size_bytes` | Histogram | Storage operation data size |
| `delta_commits_total` | Counter | Delta Lake commits by table |
| `delta_commit_duration_seconds` | Histogram | Delta commit duration |

### Business Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `data_freshness_seconds` | Gauge | Time since last successful sync by source |
| `schema_changes_total` | Counter | Schema changes detected by source and object |
| `sync_frequency_per_hour` | Gauge | Sync frequency per source |
| `data_volume_growth_rate` | Gauge | Data volume growth rate by source |

## Troubleshooting with Observability

### Performance Issues

1. **Identify Slow Operations**
   ```
   Query: sync_operations_duration_seconds > 30s
   Result: Identifies syncs taking longer than 30 seconds
   ```

2. **Analyze Trace Spans**
   - Look for long-running spans in Jaeger UI
   - Identify bottlenecks in connector API calls or storage operations
   - Check for retry patterns or circuit breaker activations

3. **Monitor Resource Usage**
   ```
   Query: storage_operations_duration_seconds{operation="upload"} > 5s
   Result: Identifies slow S3 uploads
   ```

### Error Diagnosis

1. **Correlation ID Tracking**
   - Extract correlation ID from error logs
   - Search for all log entries with the same correlation ID
   - Follow the complete operation flow across components

2. **Circuit Breaker Analysis**
   ```
   Query: circuit_breaker_state{state="open"}
   Result: Shows which circuit breakers are currently open
   ```

3. **Retry Pattern Analysis**
   - Check retry metrics for specific operations
   - Identify operations with high failure rates
   - Analyze backoff patterns and success rates

### Data Quality Issues

1. **Schema Change Detection**
   ```
   Query: schema_changes_total
   Result: Shows sources with recent schema changes
   ```

2. **Data Volume Anomalies**
   ```
   Query: rate(sync_records_processed_total[1h]) 
   Result: Shows record processing rate changes
   ```

## Dashboard and Alerting

### Recommended Grafana Dashboards

#### Sync Operations Dashboard
- Sync success rate over time
- Sync duration percentiles (P50, P95, P99)
- Records processed per hour by source
- Data volume trends

#### Connector Health Dashboard  
- API call success rates by connector
- Authentication failure rates
- Rate limiting incidents
- Connector response time distributions

#### Storage Performance Dashboard
- S3 operation latencies
- Delta Lake commit rates and durations
- Storage operation error rates
- Data transfer volumes

### Alerting Rules

#### Critical Alerts
```yaml
# Sync failure rate above 10%
- alert: SyncFailureRateHigh
  expr: sync_operations_success_rate < 0.9
  for: 5m
  labels:
    severity: critical
    
# No successful syncs in 2 hours
- alert: SyncStalled  
  expr: time() - sync_last_success_timestamp > 7200
  for: 0m
  labels:
    severity: critical
```

#### Warning Alerts
```yaml
# Slow sync operations
- alert: SyncDurationHigh
  expr: sync_operations_duration_seconds > 300
  for: 10m
  labels:
    severity: warning
    
# High connector error rate
- alert: ConnectorErrorRateHigh
  expr: rate(connector_api_failures_total[5m]) > 0.1
  for: 5m
  labels:
    severity: warning
```

## Production Deployment

### Resource Requirements

#### Jaeger Deployment
```yaml
# Minimal Jaeger deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
spec:
  template:
    spec:
      containers:
      - name: jaeger
        image: jaegertracing/all-in-one:latest
        resources:
          requests:
            memory: 512Mi
            cpu: 200m
          limits:
            memory: 1Gi
            cpu: 500m
```

#### Prometheus Configuration
```yaml
# Prometheus scrape configuration
global:
  scrape_interval: 15s
  
scrape_configs:
- job_name: 'agentbrain'
  static_configs:
  - targets: ['agentbrain:9090']
  scrape_interval: 30s
```

### Performance Considerations

1. **Trace Sampling**
   - Use appropriate sample rates (0.01-0.1 for production)
   - Consider adaptive sampling for high-volume environments
   - Monitor trace ingestion rates and storage costs

2. **Metrics Storage** 
   - Plan for metrics retention (default 15 days)
   - Consider high-availability Prometheus setup
   - Implement metrics federation for large deployments

3. **Log Volume Management**
   - Configure appropriate log levels for production
   - Use structured logging consistently
   - Implement log aggregation and retention policies

### Security Considerations

1. **Endpoint Security**
   - Secure Jaeger and Prometheus endpoints with authentication
   - Use TLS for trace and metrics transmission
   - Implement network policies for observability components

2. **Sensitive Data**
   - Avoid including sensitive data in trace attributes
   - Redact credentials and personal data from logs
   - Configure data retention policies for compliance

## Best Practices

### Instrumentation Guidelines

1. **Span Creation**
   - Create spans for logical units of work
   - Include relevant attributes for context
   - Use consistent naming conventions

2. **Error Handling**
   - Record errors on spans with appropriate status codes
   - Include error messages and stack traces when helpful
   - Differentiate between retryable and non-retryable errors

3. **Metrics Recording**
   - Use appropriate metric types (Counter, Gauge, Histogram)
   - Include relevant labels for filtering and grouping
   - Avoid high-cardinality label values

### Monitoring Strategy

1. **SLA Definition**
   - Define sync operation SLAs (success rate, latency)
   - Establish data freshness requirements
   - Set up alerts for SLA violations

2. **Capacity Planning**
   - Monitor resource usage trends
   - Track data volume growth rates
   - Plan for scale-out scenarios

3. **Incident Response**
   - Use correlation IDs for incident correlation
   - Implement runbooks for common failure scenarios
   - Establish escalation procedures for critical alerts

## Integration Examples

### Custom Middleware
```go
// Custom sync middleware with business context
func (e *Engine) WithBusinessContext(operation string) *observability.SyncMiddleware {
    return observability.NewSyncMiddleware(
        e.tracingManager,
        e.metricsManager, 
        e.logger,
    ).WithContext(map[string]interface{}{
        "business_unit": "sales",
        "data_classification": "confidential",
        "sync_schedule": "hourly",
    })
}
```

### Custom Metrics
```go
// Record business-specific metrics
func (e *Engine) recordBusinessMetrics(source string, records int64, revenue float64) {
    e.metricsManager.RecordDataVolume(source, records)
    
    // Custom business metric
    businessValue := observability.NewGauge("business_value_synced_total")
    businessValue.Set(revenue, map[string]string{
        "source": source,
        "currency": "USD",
    })
}
```

This observability framework provides the foundation for production-ready monitoring, alerting, and troubleshooting of AgentBrain sync operations.