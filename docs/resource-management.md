# Resource Management

AgentBrain provides comprehensive resource lifecycle management to ensure production reliability and graceful degradation under resource pressure.

## Overview

The resource management system tracks system resources, manages resource pools, and automatically applies degradation strategies when resources become constrained. This prevents resource exhaustion crashes and maintains system stability under load.

## Architecture

### Core Components

1. **Resource Manager** (`internal/resource/manager.go`)
   - Centralized resource tracking and coordination
   - Resource pool registration and management
   - Alert generation and callback system
   - Degradation strategy application

2. **Resource Pools** (`internal/resource/pools.go`)
   - HTTP client pooling for connector operations
   - Parquet writer pooling for storage operations
   - Configurable pool sizes, timeouts, and cleanup

3. **Degradation Controller** (`internal/resource/degradation.go`)
   - Threshold-based degradation triggers
   - Pluggable degradation strategies
   - Priority-based operation handling

4. **Resource Monitors** (`internal/resource/monitor.go`)
   - System resource usage tracking
   - Connection monitoring
   - Composite monitoring support

## Configuration

Add resource management configuration to your `agentbrain.yaml`:

```yaml
resources:
  enabled: true
  limits:
    max_memory_mb: 2048
    max_goroutines: 1000
    max_file_handles: 500
    max_connections: 100
    max_disk_usage_mb: 10240
  pools:
    http_clients:
      max_size: 20
      initial_size: 5
      idle_timeout: 5m
      cleanup_interval: 1m
    parquet_writers:
      max_size: 10
      idle_timeout: 2m
      cleanup_interval: 30s
  degradation:
    memory_threshold: 0.8
    goroutine_threshold: 0.9
    disk_threshold: 0.85
    connection_threshold: 0.8
    strategies:
      - type: reduce_concurrency
        trigger_threshold: 0.8
        enabled: true
      - type: skip_validation
        trigger_threshold: 0.9
        enabled: true
      - type: emergency_stop
        trigger_threshold: 0.95
        enabled: true
```

### Configuration Options

#### Resource Limits

- `max_memory_mb`: Maximum memory usage in megabytes
- `max_goroutines`: Maximum number of goroutines
- `max_file_handles`: Maximum file handles
- `max_connections`: Maximum network connections
- `max_disk_usage_mb`: Maximum disk usage in megabytes

#### Pool Configuration

- `max_size`: Maximum pool size
- `initial_size`: Initial pool size (default: max_size/4)
- `idle_timeout`: Timeout for idle resources
- `max_lifetime`: Maximum resource lifetime
- `cleanup_interval`: Cleanup frequency

#### Degradation Thresholds

- `memory_threshold`: Memory usage threshold (0.0-1.0)
- `goroutine_threshold`: Goroutine usage threshold (0.0-1.0)
- `disk_threshold`: Disk usage threshold (0.0-1.0)
- `connection_threshold`: Connection usage threshold (0.0-1.0)

## Usage

### Basic Setup

```go
import (
    "github.com/agentbrain/agentbrain/internal/resource"
    "github.com/agentbrain/agentbrain/internal/config"
)

// Create resource manager
limits := resource.ResourceLimits{
    MaxMemoryMB:    2048,
    MaxGoroutines:  1000,
    MaxConnections: 100,
}

manager := resource.NewManager(limits, logger)

// Start resource monitoring
ctx := context.Background()
err := manager.Start(ctx)
if err != nil {
    log.Fatal("Failed to start resource manager:", err)
}
defer manager.Stop(ctx)
```

### HTTP Client Pooling

```go
// Create HTTP client pool
poolConfig := resource.PoolConfig{
    MaxSize:         20,
    IdleTimeout:     5 * time.Minute,
    CleanupInterval: 1 * time.Minute,
}

httpPool, err := resource.NewHTTPClientPool(poolConfig, logger)
if err != nil {
    log.Fatal("Failed to create HTTP pool:", err)
}

// Register pool with resource manager
err = manager.RegisterPool("http_clients", httpPool)
if err != nil {
    log.Fatal("Failed to register pool:", err)
}

// Acquire HTTP client
client, err := httpPool.AcquireHTTPClient(ctx, resource.PriorityNormal)
if err != nil {
    log.Error("Failed to acquire HTTP client:", err)
    return
}
defer httpPool.ReleaseHTTPClient(client)

// Use client for requests
resp, err := client.Get("https://api.example.com/data")
```

### Resource Monitoring

```go
// Check current resource usage
usage, err := manager.GetCurrentUsage(ctx)
if err != nil {
    log.Error("Failed to get usage:", err)
    return
}

log.Printf("Memory: %.1f%%, Goroutines: %d", 
    usage.MemoryPercent, usage.GoroutineCount)

// Check resource availability
requirements := resource.ResourceRequirements{
    Memory:      100,  // 100MB
    Goroutines:  5,    // 5 goroutines
    Connections: 2,    // 2 connections
}

err = manager.CheckResourceAvailability(ctx, requirements)
if err != nil {
    log.Error("Insufficient resources:", err)
    return
}
```

### Custom Degradation Strategies

```go
// Implement custom degradation strategy
type CustomStrategy struct {
    name         string
    resourceType resource.ResourceType
    threshold    float64
}

func (s *CustomStrategy) ShouldDegrade(usage resource.ResourceUsage, limits resource.ResourceLimits) bool {
    return usage.MemoryPercent > s.threshold*100
}

func (s *CustomStrategy) ApplyDegradation(ctx context.Context, operation resource.Operation) resource.Operation {
    // Apply custom degradation logic
    return &DegradedOperation{
        originalOp: operation,
        strategy:   s.name,
        // ... custom degradation parameters
    }
}

func (s *CustomStrategy) GetResourceType() resource.ResourceType {
    return s.resourceType
}

func (s *CustomStrategy) GetName() string {
    return s.name
}

// Register custom strategy
degrader := resource.NewDegradationController(thresholds, logger)
customStrategy := &CustomStrategy{
    name:         "custom_batch_reduction",
    resourceType: resource.ResourceTypeMemory,
    threshold:    0.85,
}
degrader.RegisterStrategy(resource.ResourceTypeMemory, customStrategy)
manager.SetDegradationController(degrader)
```

## Degradation Strategies

### Built-in Strategies

1. **Reduce Concurrency**
   - Reduces operation concurrency by a configurable factor
   - Triggers at 80% resource usage
   - Applies to both memory and goroutine pressure

2. **Skip Validation**
   - Skips non-critical validation steps
   - Triggers at 90% memory usage
   - Maintains data integrity for critical operations

3. **Emergency Stop**
   - Stops new operations to prevent crashes
   - Triggers at 95% resource usage
   - Last resort protection mechanism

### Strategy Behavior

- **Priority-based**: Higher priority operations get preferential treatment
- **Progressive**: Multiple strategies can be applied simultaneously
- **Reversible**: Strategies are removed when resource pressure decreases
- **Configurable**: Thresholds and behavior can be customized

## Integration Points

### Sync Engine

The sync engine automatically integrates with resource management:

- Resource-aware concurrency adjustment
- Pre-sync resource availability checks
- Adaptive batch sizing during extraction
- Graceful degradation of validation phases

```go
// Resource-aware sync engine
engine := sync.NewEngine(connector, s3, source, concurrency, objects, logger)
engine.SetResourceManager(manager, resourceConfig)
```

### Storage Layer

Storage operations benefit from resource management:

- S3 operation retries with circuit breakers
- Parquet writer pooling and reuse
- Adaptive batch sizes based on memory usage

```go
// Resource-aware storage
s3Client.SetResourceManager(manager)
parquetWriter.SetResourceManager(manager)
```

### Connector Registry

HTTP connections are pooled and managed:

- Shared HTTP client pools across connectors
- Connection limits and health monitoring
- Priority-based connection acquisition

```go
// Resource-aware connector registry
registry.SetResourceManager(manager)
```

## Monitoring and Alerts

### Resource Alerts

The system generates alerts for resource conditions:

```go
// Register alert callback
manager.RegisterAlertCallback(func(alert resource.ResourceAlert) {
    log.Printf("Resource alert: %s - %s (%.1f%% > %.1f%%)",
        alert.Type, alert.Message, alert.CurrentValue, alert.Threshold)
    
    // Send to monitoring system
    metrics.RecordResourceAlert(alert)
})
```

### Health Monitoring Integration

Resource metrics integrate with the health monitoring system:

```go
healthMonitor.SetResourceManager(manager)
```

### Pool Health Metrics

Monitor pool performance and health:

```go
health := manager.GetPoolHealth()
for poolName, poolHealth := range health {
    log.Printf("Pool %s: Active=%d, Idle=%d, Hits=%d, Misses=%d",
        poolName, poolHealth.Active, poolHealth.Idle, 
        poolHealth.Hits, poolHealth.Misses)
}
```

## Best Practices

### Production Configuration

1. **Set appropriate limits** based on your infrastructure
2. **Monitor resource usage** patterns over time
3. **Test degradation strategies** under load
4. **Adjust thresholds** based on application behavior

### Performance Optimization

1. **Use connection pooling** for frequent HTTP operations
2. **Configure pool sizes** based on expected concurrency
3. **Set reasonable timeouts** to prevent resource leaks
4. **Enable cleanup routines** to manage resource lifecycle

### Error Handling

1. **Handle pool exhaustion** gracefully
2. **Implement fallback strategies** for resource constraints
3. **Log resource-related errors** for troubleshooting
4. **Monitor degradation frequency** to tune thresholds

## Troubleshooting

### Common Issues

1. **Pool Exhaustion**
   ```
   Error: timeout acquiring HTTP client after 10s
   Solution: Increase pool size or reduce operation timeout
   ```

2. **Memory Pressure**
   ```
   Warning: Memory usage critical: 92.1%
   Solution: Check for memory leaks, increase limits, or tune batch sizes
   ```

3. **Degradation Too Aggressive**
   ```
   Warning: Frequent degradation events
   Solution: Adjust thresholds or increase resource limits
   ```

### Debugging

Enable detailed logging to troubleshoot resource issues:

```yaml
agent:
  log_level: debug
```

Monitor resource usage patterns:

```go
// Log detailed usage periodically
usage, _ := manager.GetCurrentUsage(ctx)
logger.Info("Resource usage detail",
    "memory_mb", usage.MemoryUsedMB,
    "memory_percent", usage.MemoryPercent,
    "goroutines", usage.GoroutineCount,
    "connections", usage.ConnectionCount)
```

## Advanced Topics

### Custom Resource Monitors

Implement application-specific monitoring:

```go
type CustomMonitor struct {
    // Custom monitoring logic
}

func (m *CustomMonitor) Monitor(ctx context.Context) (resource.ResourceUsage, error) {
    // Return custom usage metrics
}

func (m *CustomMonitor) SetThresholds(thresholds map[resource.ResourceType]float64) error {
    // Configure custom thresholds
}

manager.RegisterMonitor(customMonitor)
```

### Dynamic Configuration

Update resource configuration at runtime:

```go
// Update pool configuration
newConfig := resource.PoolConfig{
    MaxSize:     30,  // Increased pool size
    IdleTimeout: 10 * time.Minute,
}

// Recreate pool with new configuration
// Note: This requires careful coordination to avoid disruption
```

### Integration with External Monitoring

Export metrics to external systems:

```go
manager.RegisterAlertCallback(func(alert resource.ResourceAlert) {
    // Send to Prometheus, DataDog, etc.
    prometheusClient.RecordResourceAlert(alert)
})
```