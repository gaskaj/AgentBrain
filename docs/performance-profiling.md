# Performance Profiling Guide

This guide covers the performance profiling and resource optimization framework in AgentBrain, designed to help identify bottlenecks, optimize resource usage, and ensure production-ready performance.

## Overview

The profiling framework provides comprehensive performance monitoring and analysis capabilities:

- **CPU Profiling**: Track CPU-intensive operations and identify hotspots
- **Memory Profiling**: Monitor memory usage patterns and detect leaks
- **Resource Monitoring**: Track goroutine counts, GC metrics, and system resources
- **Operation Analytics**: Analyze performance trends across different operations
- **Bottleneck Detection**: Automatically identify performance bottlenecks
- **Optimization Recommendations**: Get actionable insights for performance improvements

## Configuration

### Enable Profiling

Add the profiler configuration to your `config.yaml`:

```yaml
profiler:
  enabled: true                      # Enable performance profiling
  sample_rate: 0.1                  # Sample 10% of operations
  output_dir: "./profiles"          # Profile output directory
  cpu_profile_duration: "30s"      # CPU profiling duration
  memory_profile_interval: "5m"    # Memory profile interval
  goroutine_threshold: 1000         # Alert when goroutines exceed threshold
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable/disable profiling |
| `sample_rate` | float64 | 0.1 | Fraction of operations to profile (0.0-1.0) |
| `output_dir` | string | "./profiles" | Directory for profile output files |
| `cpu_profile_duration` | duration | 30s | How long to run CPU profiling |
| `memory_profile_interval` | duration | 5m | Interval between memory snapshots |
| `goroutine_threshold` | int | 1000 | Goroutine count threshold for alerts |

## Using the Profiler

### Basic Usage

```go
import "github.com/agentbrain/agentbrain/internal/profiler"

// Create profiler from config
profilerConfig := config.ProfilerConfig{
    Enabled:               true,
    SampleRate:            0.1,
    OutputDir:             "./profiles",
    CPUProfileDuration:    30 * time.Second,
    MemoryProfileInterval: 5 * time.Minute,
    GoroutineThreshold:    1000,
}

profiler, err := profiler.New(profilerConfig)
if err != nil {
    log.Fatal(err)
}

// Start profiling
ctx := context.Background()
if err := profiler.Start(ctx); err != nil {
    log.Fatal(err)
}
defer profiler.Stop()
```

### Middleware Integration

The profiling framework includes middleware for easy integration with existing operations:

```go
import "github.com/agentbrain/agentbrain/internal/profiler"

middleware := profiler.NewMiddleware(profiler)

// Track generic operations
err := middleware.TrackOperation("database_query", func() error {
    // Your operation here
    return database.Query()
})

// Track LLM API calls
err := middleware.TrackLLMCall("claude", messageCount, func() error {
    return claudeClient.SendMessage(messages)
})

// Track workspace operations
err := middleware.TrackWorkspaceOperation("setup", func() error {
    return workspace.Setup()
})

// Track git operations
err := middleware.TrackGitOperation("clone", repoSize, func() error {
    return git.Clone(url)
})
```

### Transaction Tracking

For complex operations spanning multiple steps:

```go
transaction := middleware.StartTransaction("workflow_execution")
transaction.AddMetadata("workflow_id", workflowID)
transaction.AddMetadata("steps", stepCount)

// Perform operations...
err := performWorkflow()

transaction.Finish(err)
```

## Profile Types

### CPU Profiling

CPU profiles help identify computational bottlenecks:

```go
// Manual CPU profiling
if err := profiler.StartCPUProfile(); err != nil {
    log.Error(err)
}

// Run your operations...
time.Sleep(30 * time.Second)

if err := profiler.StopCPUProfile(); err != nil {
    log.Error(err)
}
```

Analyze CPU profiles using Go's pprof tool:

```bash
go tool pprof profiles/cpu-1609459200.prof
```

### Memory Profiling

Memory profiles show allocation patterns and potential leaks:

```go
// Capture memory snapshot
if err := profiler.CaptureMemProfile(); err != nil {
    log.Error(err)
}
```

Analyze memory profiles:

```bash
go tool pprof profiles/mem-1609459200.prof
```

### Goroutine Profiling

Monitor goroutine usage and detect goroutine leaks:

```go
// Capture goroutine profile
if err := profiler.CaptureGoroutineProfile(); err != nil {
    log.Error(err)
}
```

Analyze goroutine profiles:

```bash
go tool pprof profiles/goroutine-1609459200.prof
```

## Resource Monitoring

### Real-time Metrics

Get current resource usage:

```go
monitor := profiler.GetResourceMonitor()
metrics := monitor.GetCurrentMetrics()

fmt.Printf("Memory Usage: %d bytes\n", metrics.MemoryUsage.HeapAlloc)
fmt.Printf("Goroutines: %d\n", metrics.GoroutineCount)
fmt.Printf("Last GC: %v\n", metrics.MemoryUsage.LastGC)
```

### Memory Trend Analysis

Analyze memory usage trends over time:

```go
// Get memory trend for last hour
trend := monitor.GetMemoryTrend(1 * time.Hour)

fmt.Printf("Memory trend: %s\n", trend.Direction)
fmt.Printf("Growth rate: %.2f MB/s\n", trend.Rate/(1024*1024))
fmt.Printf("Peak usage: %d MB\n", trend.Peak/(1024*1024))
```

### Memory Leak Detection

Automatically detect potential memory leaks:

```go
// Check for memory leaks over last 30 minutes
// Threshold: 1MB/s sustained growth
isLeak := monitor.DetectMemoryLeak(30*time.Minute, 1024*1024)
if isLeak {
    log.Warn("Potential memory leak detected")
}
```

## Performance Analytics

### Operation Metrics

Get detailed metrics for specific operations:

```go
analytics := profiler.GetAnalytics()
metrics, exists := analytics.GetOperationMetrics("llm_call_claude")
if exists {
    fmt.Printf("Operation: %s\n", metrics.Name)
    fmt.Printf("Count: %d\n", metrics.Count)
    fmt.Printf("Average time: %v\n", metrics.AvgTime)
    fmt.Printf("P95 time: %v\n", metrics.P95Time)
    fmt.Printf("Error rate: %.2f%%\n", 
        float64(metrics.Errors)/float64(metrics.Count)*100)
}
```

### Performance Reports

Generate comprehensive performance reports:

```go
// Generate report for last hour
report := analytics.GeneratePerformanceReport(monitor, 1*time.Hour)

fmt.Printf("Report generated: %v\n", report.GeneratedAt)
fmt.Printf("Operations analyzed: %d\n", len(report.Operations))
fmt.Printf("Bottlenecks found: %d\n", len(report.Bottlenecks))

// Print bottlenecks
for _, bottleneck := range report.Bottlenecks {
    fmt.Printf("Bottleneck: %s (%s severity)\n", 
        bottleneck.Description, bottleneck.Severity)
    fmt.Printf("Suggestion: %s\n", bottleneck.Suggestion)
}

// Print recommendations
for _, rec := range report.Recommendations {
    fmt.Printf("Recommendation: %s\n", rec)
}
```

## Integration Examples

### Claude Client Integration

```go
// In your Claude client
func (c *Client) SendMessageWithTools(ctx context.Context, messages []Message) error {
    if c.profilerMiddleware != nil {
        return c.profilerMiddleware.TrackLLMCall("claude", len(messages), func() error {
            return c.sendMessageInternal(ctx, messages)
        })
    }
    return c.sendMessageInternal(ctx, messages)
}
```

### Workspace Manager Integration

```go
// In your workspace manager
func (w *WorkspaceManager) SetupWorkspace(ctx context.Context, issueID string) error {
    if w.profilerMiddleware != nil {
        return w.profilerMiddleware.TrackWorkspaceOperation("setup", func() error {
            return w.setupWorkspaceInternal(ctx, issueID)
        })
    }
    return w.setupWorkspaceInternal(ctx, issueID)
}
```

### File Operations Integration

```go
// Track file operations
func (f *FileManager) ReadLargeFile(path string) ([]byte, error) {
    var data []byte
    var err error
    
    if f.profilerMiddleware != nil {
        stat, _ := os.Stat(path)
        size := stat.Size()
        
        err = f.profilerMiddleware.TrackFileOperation("read", path, size, func() error {
            data, err = os.ReadFile(path)
            return err
        })
    } else {
        data, err = os.ReadFile(path)
    }
    
    return data, err
}
```

## Best Practices

### 1. Sampling Strategy

- Use appropriate sample rates to balance overhead with insight
- Higher sample rates (0.5-1.0) for debugging, lower (0.01-0.1) for production
- Adjust based on operation frequency and performance requirements

### 2. Profile Storage Management

- Regularly clean up old profile files to prevent disk space issues
- Consider automated archival for long-term trend analysis
- Use compressed storage for historical profiles

### 3. Performance Impact

- Profiling adds minimal overhead when disabled
- CPU profiling has higher overhead than memory profiling
- Monitor profiler resource usage in production environments

### 4. Alert Thresholds

- Set goroutine thresholds based on your application's normal behavior
- Adjust memory leak detection thresholds for your workload
- Configure appropriate alert cooldowns to prevent spam

### 5. Analysis Workflow

1. **Baseline Establishment**: Run profiling on normal workloads to establish baselines
2. **Continuous Monitoring**: Use low sample rates for ongoing performance monitoring
3. **Deep Analysis**: Increase sample rates during performance investigation
4. **Bottleneck Resolution**: Focus on highest-impact bottlenecks first

## Production Considerations

### Resource Overhead

- Disabled profiling: Negligible overhead (function call checks only)
- Enabled profiling: 1-5% CPU overhead depending on sample rate
- Memory profiles: Temporary heap allocation increase during capture
- Disk usage: Plan for profile file storage (10-100MB per profile)

### Security

- Profile files may contain sensitive information
- Restrict access to profile output directories
- Consider encryption for profiles containing sensitive data
- Implement proper cleanup policies for temporary profiles

### Monitoring Integration

The profiler integrates with the existing monitoring system:

```yaml
monitoring:
  rules:
    memory_usage:
      warning_threshold: 80.0
      critical_threshold: 90.0
      
    goroutine_count:
      warning_threshold: 1000
      critical_threshold: 5000
```

## Troubleshooting

### Common Issues

1. **Profiles not generated**
   - Check if profiling is enabled in configuration
   - Verify output directory permissions
   - Ensure sufficient disk space

2. **High memory usage**
   - Reduce memory profile interval
   - Lower sample rate
   - Implement profile cleanup policies

3. **Performance impact**
   - Reduce sample rate
   - Disable CPU profiling in production
   - Use targeted profiling for specific operations only

### Debug Information

Enable debug logging to troubleshoot profiler issues:

```go
// Add debug output
fmt.Printf("Profiler enabled: %v\n", profiler.IsEnabled())
fmt.Printf("Profiler running: %v\n", profiler.IsRunning())
fmt.Printf("Monitor running: %v\n", monitor.IsRunning())
```

## Advanced Usage

### Custom Metrics Collection

Extend the profiler with custom metrics:

```go
// Custom operation tracking
profiler.TrackOperation("custom_algorithm", duration, map[string]interface{}{
    "input_size":    inputSize,
    "algorithm":     "quicksort",
    "optimization":  "enabled",
    "cache_hits":    cacheHits,
})
```

### Integration with External Tools

- Export metrics to Prometheus
- Send alerts to external monitoring systems
- Integrate with APM tools like New Relic or DataDog

### Automated Performance Testing

Use the profiler in automated performance tests:

```go
func BenchmarkWithProfiling(b *testing.B) {
    profiler, _ := profiler.New(profilerConfig)
    profiler.Start(context.Background())
    defer profiler.Stop()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Your benchmark code
        performOperation()
    }
    
    // Generate performance report
    report := profiler.GetAnalytics().GeneratePerformanceReport(
        profiler.GetResourceMonitor(), 
        time.Duration(b.Elapsed()),
    )
    
    // Assert performance requirements
    if len(report.Bottlenecks) > 0 {
        b.Errorf("Performance bottlenecks detected: %v", report.Bottlenecks)
    }
}
```

## Conclusion

The performance profiling framework provides comprehensive insights into your application's performance characteristics. Use it to:

- Identify and resolve performance bottlenecks
- Monitor resource usage trends
- Detect memory leaks and goroutine proliferation
- Optimize API call patterns and I/O operations
- Ensure production performance and scalability

Regular profiling and performance monitoring are essential for maintaining a high-performance, production-ready system.