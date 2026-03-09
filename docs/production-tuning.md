# Production Tuning Guide

This guide covers performance optimization and resource management for production deployments of AgentBrain.

## Resource Management

### Memory Management

#### Optimal Memory Limits

Set memory limits based on your deployment environment:

```yaml
resources:
  limits:
    max_memory_mb: 4096  # For 8GB containers
    # max_memory_mb: 2048  # For 4GB containers
    # max_memory_mb: 1024  # For 2GB containers
```

#### Memory Pressure Thresholds

Configure degradation thresholds based on your application's memory usage patterns:

```yaml
resources:
  degradation:
    memory_threshold: 0.75  # Conservative for production
    # memory_threshold: 0.8   # Balanced approach
    # memory_threshold: 0.85  # Aggressive optimization
```

#### Batch Size Optimization

Adjust batch sizes based on memory constraints:

```yaml
sources:
  salesforce:
    batch_size: 5000   # Conservative for limited memory
    # batch_size: 10000  # Default
    # batch_size: 20000  # For high-memory deployments
```

### Connection Management

#### HTTP Client Pooling

Configure HTTP pools based on concurrency requirements:

```yaml
resources:
  pools:
    http_clients:
      max_size: 50        # High-throughput deployments
      initial_size: 10    # Warm pool startup
      idle_timeout: 10m   # Longer for persistent connections
      cleanup_interval: 2m
```

#### Connection Limits

Set connection limits based on external API constraints:

```yaml
resources:
  limits:
    max_connections: 200  # Based on API rate limits
sources:
  salesforce:
    concurrency: 8        # Limited by Salesforce API limits
```

### Goroutine Management

#### Concurrency Tuning

Balance concurrency with resource constraints:

```yaml
resources:
  limits:
    max_goroutines: 2000  # High-throughput deployments
  degradation:
    goroutine_threshold: 0.8  # Reduce concurrency at 80%

sources:
  source1:
    concurrency: 10  # Per-source concurrency
  source2:
    concurrency: 5   # Lower for resource-intensive sources
```

## Performance Optimization

### Sync Performance

#### Incremental Sync Optimization

Configure checkpointing for optimal incremental syncs:

```yaml
sources:
  salesforce:
    checkpoint:
      frequency: 1000        # Checkpoint every 1000 records
      validation_enabled: false  # Disable for performance
      adaptive_mode: true    # Auto-adjust based on load
```

#### Parallel Processing

Optimize parallel processing based on source characteristics:

```yaml
sources:
  # High-volume, low-latency source
  fast_api:
    concurrency: 20
    batch_size: 50000
    
  # Low-volume, high-latency source  
  slow_api:
    concurrency: 3
    batch_size: 1000
```

### Storage Performance

#### Parquet Optimization

Configure Parquet settings for optimal storage performance:

```yaml
resources:
  pools:
    parquet_writers:
      max_size: 20          # More writers for high throughput
      idle_timeout: 5m      # Longer retention for reuse
```

#### S3 Performance

Optimize S3 operations:

```yaml
storage:
  # Use regional endpoints for better performance
  region: us-west-2
  # Consider dedicated endpoints for high-throughput
  
retry:
  operation_policies:
    s3_upload:
      max_attempts: 5
      base_delay: 1s        # Faster retries
      max_delay: 30s
      backoff_strategy: exponential_jitter
```

### Monitoring Integration

#### Resource Monitoring

Configure comprehensive monitoring:

```yaml
monitoring:
  enabled: true
  check_interval: 1m      # Frequent checks for production
  rules:
    memory_usage:
      enabled: true
      warning_threshold: 75.0
      critical_threshold: 90.0
    disk_usage:
      enabled: true
      warning_threshold: 80.0
      critical_threshold: 95.0
```

#### Performance Profiling

Enable profiling for performance analysis:

```yaml
profiler:
  enabled: true
  sample_rate: 0.01       # 1% sampling in production
  output_dir: /var/log/agentbrain/profiles
  cpu_profile_duration: 60s
  memory_profile_interval: 10m
```

## Deployment Configurations

### High-Throughput Configuration

For high-volume data synchronization:

```yaml
resources:
  enabled: true
  limits:
    max_memory_mb: 8192
    max_goroutines: 3000
    max_connections: 300
  pools:
    http_clients:
      max_size: 100
      initial_size: 20
    parquet_writers:
      max_size: 30
  degradation:
    memory_threshold: 0.8
    goroutine_threshold: 0.85

sources:
  salesforce:
    concurrency: 25
    batch_size: 25000
    checkpoint:
      frequency: 500
      adaptive_mode: true

retry:
  default_policy:
    max_attempts: 3
    base_delay: 500ms
```

### Resource-Constrained Configuration

For limited resource environments:

```yaml
resources:
  enabled: true
  limits:
    max_memory_mb: 1024
    max_goroutines: 500
    max_connections: 50
  pools:
    http_clients:
      max_size: 10
      initial_size: 2
    parquet_writers:
      max_size: 5
  degradation:
    memory_threshold: 0.7
    goroutine_threshold: 0.8
    strategies:
      - type: reduce_concurrency
        trigger_threshold: 0.7
        enabled: true
      - type: skip_validation
        trigger_threshold: 0.85
        enabled: true

sources:
  salesforce:
    concurrency: 3
    batch_size: 5000
    checkpoint:
      frequency: 100
      validation_enabled: false
```

### High-Reliability Configuration

For mission-critical deployments:

```yaml
resources:
  enabled: true
  limits:
    max_memory_mb: 4096
    max_goroutines: 1500
    max_connections: 150
  degradation:
    memory_threshold: 0.75  # Conservative thresholds
    goroutine_threshold: 0.8
    strategies:
      - type: reduce_concurrency
        trigger_threshold: 0.75
        enabled: true
      - type: emergency_stop
        trigger_threshold: 0.9
        enabled: true

backup:
  enabled: true
  schedule: "@every 6h"
  validation_mode: full
  cross_region: true

retry:
  default_policy:
    max_attempts: 5
    base_delay: 2s
    max_delay: 60s
  circuit_breakers:
    s3_operations:
      failure_threshold: 3
      reset_timeout: 300s
```

## Monitoring and Alerting

### Key Metrics

Monitor these critical metrics in production:

1. **Resource Utilization**
   - Memory usage percentage
   - Goroutine count
   - Connection pool utilization
   - Disk usage

2. **Performance Metrics**
   - Sync completion times
   - Records processed per minute
   - Error rates by phase
   - Retry attempt frequency

3. **Pool Health**
   - Pool hit/miss ratios
   - Active vs idle resource counts
   - Timeout frequencies
   - Cleanup effectiveness

### Alert Thresholds

Configure alerts for production monitoring:

```yaml
monitoring:
  rules:
    memory_usage:
      warning_threshold: 80.0   # 80% memory usage
      critical_threshold: 95.0  # 95% memory usage
    
    sync_error_rate:
      warning_threshold: 0.05   # 5% error rate
      critical_threshold: 0.15  # 15% error rate
    
    pool_exhaustion:
      warning_threshold: 0.9    # 90% pool utilization
      critical_threshold: 0.98  # 98% pool utilization
```

### Log Analysis

Key log patterns to monitor:

```bash
# Resource pressure events
grep "applying degradation strategies" /var/log/agentbrain/app.log

# Pool exhaustion
grep "timeout acquiring.*client" /var/log/agentbrain/app.log

# Memory pressure
grep "Memory usage.*%" /var/log/agentbrain/app.log

# Sync failures
grep "sync failed" /var/log/agentbrain/app.log
```

## Troubleshooting

### Common Performance Issues

#### High Memory Usage

**Symptoms:**
- Frequent degradation events
- Out of memory errors
- Slow sync performance

**Solutions:**
1. Reduce batch sizes
2. Increase memory limits
3. Enable more aggressive cleanup
4. Check for memory leaks

```yaml
sources:
  salesforce:
    batch_size: 5000  # Reduce from default 10000

resources:
  pools:
    parquet_writers:
      idle_timeout: 1m  # Faster cleanup
```

#### Connection Pool Exhaustion

**Symptoms:**
- Timeout errors acquiring connections
- High connection wait times
- Sync delays

**Solutions:**
1. Increase pool sizes
2. Reduce idle timeouts
3. Optimize connection reuse

```yaml
resources:
  pools:
    http_clients:
      max_size: 50      # Increase pool size
      idle_timeout: 2m  # Faster turnover
```

#### Sync Performance Degradation

**Symptoms:**
- Increasing sync times
- Higher error rates
- Resource alerts

**Solutions:**
1. Analyze sync patterns
2. Adjust concurrency levels
3. Optimize checkpoint frequency

```yaml
sources:
  salesforce:
    concurrency: 5     # Reduce from higher value
    checkpoint:
      frequency: 1000  # More frequent checkpoints
```

### Performance Tuning Workflow

1. **Baseline Measurement**
   ```bash
   # Enable profiling
   curl -X POST http://localhost:8080/debug/profiling/start
   
   # Run sync cycle
   # Collect metrics
   
   # Stop profiling
   curl -X POST http://localhost:8080/debug/profiling/stop
   ```

2. **Resource Analysis**
   ```bash
   # Check current usage
   curl http://localhost:8080/health | jq '.system_metrics'
   
   # Pool health
   curl http://localhost:8080/pools/health | jq
   ```

3. **Configuration Adjustment**
   - Modify configuration based on analysis
   - Deploy changes incrementally
   - Monitor impact

4. **Performance Validation**
   - Compare before/after metrics
   - Verify stability over time
   - Document optimal settings

## Capacity Planning

### Resource Estimation

#### Memory Requirements

Base memory calculation:
```
Base Memory = 500MB (application overhead)
Batch Memory = batch_size × record_size × concurrency × 2 (buffer)
Pool Memory = pool_size × average_resource_size
Total Memory = Base + Batch + Pool + 20% (safety margin)
```

Example calculation:
```
batch_size = 10,000
record_size = 2KB
concurrency = 10
pool_size = 20 × 50KB = 1MB

Base = 500MB
Batch = 10,000 × 2KB × 10 × 2 = 400MB
Pool = 1MB
Safety = 180MB
Total = ~1.1GB → Set max_memory_mb: 1536
```

#### Connection Requirements

Estimate connection needs:
```
Connections = sources × concurrency + pools + overhead
Example: 5 sources × 10 concurrency + 20 pool + 10 overhead = 80
→ Set max_connections: 120 (50% margin)
```

### Scaling Guidelines

#### Horizontal Scaling

For multiple instances:
- Coordinate resource limits across instances
- Use shared storage for state management
- Implement leader election for exclusive operations

#### Vertical Scaling

When scaling resources:
- Increase limits proportionally
- Adjust pool sizes based on new capacity
- Update degradation thresholds accordingly

### Load Testing

Test configuration under realistic load:

```bash
# Simulate high load
for i in {1..10}; do
  curl -X POST http://localhost:8080/sync/trigger &
done

# Monitor during load test
while true; do
  curl -s http://localhost:8080/health | jq '.system_metrics.memory_usage_percent'
  sleep 5
done
```

Monitor key metrics during load testing:
- Response times
- Error rates  
- Resource utilization
- Degradation frequency
- Recovery times

Adjust configuration based on load test results to ensure stable production performance.