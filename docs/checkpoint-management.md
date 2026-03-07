# Delta Lake Checkpoint Management

This guide covers comprehensive checkpoint lifecycle management in AgentBrain's Delta Lake implementation.

## Overview

Checkpoints in Delta Lake provide periodic snapshots of table state to optimize log replay performance. AgentBrain implements a sophisticated checkpoint management system with the following features:

- **Adaptive checkpointing**: Adjust frequency based on data volume and performance
- **Validation and recovery**: Detect and recover from corrupted checkpoints
- **Automatic cleanup**: Remove old checkpoints based on retention policies
- **File compaction**: Optimize small files during checkpoint creation
- **Comprehensive monitoring**: Track checkpoint health and performance

## Configuration

### Basic Configuration

Configure checkpoint settings in your source configuration:

```yaml
sources:
  salesforce_prod:
    type: salesforce
    checkpoint:
      frequency: 10              # Commits between checkpoints
      retention_days: 30         # Days to retain old checkpoints
      max_checkpoints: 50        # Maximum checkpoints to keep
      validation_enabled: true   # Validate checkpoints after creation
      compaction_enabled: true   # Enable file compaction
      adaptive_mode: true        # Use adaptive frequency
      size_threshold_mb: 128     # Size threshold for adaptive mode
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `frequency` | int | 10 | Number of commits between checkpoints |
| `retention_days` | int | 30 | Days to retain old checkpoints |
| `max_checkpoints` | int | 50 | Maximum number of checkpoints to keep |
| `validation_enabled` | bool | false | Enable checkpoint validation |
| `compaction_enabled` | bool | false | Enable small file compaction |
| `adaptive_mode` | bool | false | Use adaptive checkpoint frequency |
| `size_threshold_mb` | int64 | 128 | Data volume threshold for adaptive mode |

## Checkpoint Lifecycle

### 1. Creation Triggers

Checkpoints are created when:

- **Fixed frequency**: Every N commits (configurable)
- **Adaptive mode**: Based on data volume and performance metrics
- **Manual trigger**: Explicitly requested via API

### 2. Creation Process

1. **Snapshot generation**: Create current table state snapshot
2. **Action compilation**: Build list of protocol, metadata, and add actions
3. **File compaction** (optional): Merge small files for optimization
4. **Checkpoint writing**: Save checkpoint to storage with atomic operations
5. **Validation** (optional): Verify checkpoint integrity
6. **Marker update**: Update last checkpoint reference
7. **Cleanup**: Remove old checkpoints per retention policy

### 3. Validation

When validation is enabled, each checkpoint is verified for:

- **Structure**: Proper protocol and metadata actions
- **Completeness**: All expected files are referenced
- **Consistency**: File metadata matches expectations
- **Integrity**: Referenced files exist in storage

### 4. Recovery

The system can recover from checkpoint failures by:

- **Fallback**: Use previous valid checkpoint
- **Log replay**: Fall back to full transaction log replay
- **Automatic detection**: Find most recent valid checkpoint

## Adaptive Checkpointing

### Algorithm

Adaptive mode adjusts checkpoint frequency based on:

1. **Minimum frequency**: Always checkpoint after max(2 × frequency) commits
2. **Data volume**: Checkpoint when accumulated data exceeds threshold
3. **Performance impact**: Consider log replay time and file count
4. **Backoff strategy**: Reduce frequency for failed checkpoints

### Benefits

- **Performance optimization**: Faster log replay for heavy workloads
- **Storage efficiency**: Avoid unnecessary checkpoints for light workloads
- **Automatic tuning**: Adapts to changing data patterns

## File Compaction

### Overview

Small file compaction runs during checkpoint creation to:

- Merge files smaller than optimal size
- Reduce file count for better query performance
- Optimize storage layout

### Configuration

```yaml
checkpoint:
  compaction_enabled: true
  size_threshold_mb: 128  # Files smaller than this may be compacted
```

### Process

1. **Analysis**: Identify files below size threshold
2. **Grouping**: Group compatible files for merging
3. **Rewrite**: Create new optimally-sized files
4. **Update**: Add remove/add actions to checkpoint

## Monitoring and Metrics

### Key Metrics

- **Checkpoint frequency**: Rate of checkpoint creation
- **Validation success rate**: Percentage of successful validations
- **Storage savings**: Space reclaimed by cleanup and compaction
- **Log replay performance**: Time to replay transaction log
- **Checkpoint health score**: Overall checkpoint system health (0-100)

### Health Score Calculation

The checkpoint health score considers:

- Validation failure rate (up to -30 points)
- Checkpoint staleness (up to -20 points)
- Recent checkpoint activity

### Alerting

Monitor these conditions:

- **Validation failures**: Indicates checkpoint corruption
- **Stale checkpoints**: No recent checkpoint creation
- **Storage growth**: Cleanup not working properly
- **Performance degradation**: Slow log replay times

## Troubleshooting

### Common Issues

#### 1. Checkpoint Validation Failures

**Symptoms**: Validation error logs, checkpoint health score drops

**Causes**:
- Referenced files missing from storage
- Corrupted checkpoint files
- Schema evolution issues

**Solutions**:
```bash
# Check checkpoint integrity
tail -f logs/agent.log | grep "checkpoint validation"

# Manually validate specific checkpoint
# (Use admin API or debugging tools)
```

#### 2. Storage Growth

**Symptoms**: Increasing storage usage, old checkpoints not cleaned up

**Causes**:
- Retention policy too long
- Cleanup process failures
- S3 deletion permissions missing

**Solutions**:
```yaml
# Adjust retention settings
checkpoint:
  retention_days: 7
  max_checkpoints: 10
```

#### 3. Performance Issues

**Symptoms**: Slow log replay, query performance degradation

**Causes**:
- Checkpoint frequency too low
- File fragmentation
- Large transaction logs

**Solutions**:
```yaml
# Enable adaptive mode
checkpoint:
  adaptive_mode: true
  size_threshold_mb: 64
  compaction_enabled: true
```

### Recovery Procedures

#### Manual Checkpoint Recovery

1. **Identify issue**: Check logs for checkpoint errors
2. **Find valid checkpoint**: Use recovery tools to locate valid checkpoint
3. **Force recovery**: Manually trigger recovery from specific version
4. **Verify integrity**: Validate recovered state

#### Emergency Procedures

If checkpoint system fails completely:

1. **Disable checkpointing**: Set `frequency: 0` temporarily
2. **Clear corrupted checkpoints**: Remove invalid checkpoint files
3. **Rebuild from logs**: Let system replay full transaction log
4. **Gradually re-enable**: Start with conservative settings

## Performance Tuning

### Optimization Guidelines

1. **Frequency tuning**:
   - High-volume tables: Lower frequency (5-10 commits)
   - Low-volume tables: Higher frequency (20-50 commits)

2. **Adaptive mode**:
   - Enable for variable workloads
   - Set appropriate size thresholds

3. **Validation**:
   - Enable in production for critical tables
   - Disable for performance-critical scenarios

4. **Retention**:
   - Balance storage costs vs. recovery options
   - Consider compliance requirements

### Example Configurations

#### High-Volume Production Table
```yaml
checkpoint:
  frequency: 5
  retention_days: 14
  validation_enabled: true
  compaction_enabled: true
  adaptive_mode: true
  size_threshold_mb: 256
```

#### Low-Volume Reference Table
```yaml
checkpoint:
  frequency: 50
  retention_days: 90
  validation_enabled: false
  compaction_enabled: false
  adaptive_mode: false
```

#### Development/Testing
```yaml
checkpoint:
  frequency: 10
  retention_days: 3
  max_checkpoints: 5
  validation_enabled: false
  compaction_enabled: false
```

## API Reference

### Configuration Schema

```go
type DeltaCheckpointConfig struct {
    Frequency         int           `yaml:"frequency"`
    RetentionDays     int           `yaml:"retention_days"`
    MaxCheckpoints    int           `yaml:"max_checkpoints"`
    ValidationEnabled bool          `yaml:"validation_enabled"`
    CompactionEnabled bool          `yaml:"compaction_enabled"`
    SizeThresholdMB   int64         `yaml:"size_threshold_mb"`
    AdaptiveMode      bool          `yaml:"adaptive_mode"`
    CreationTimeout   time.Duration `yaml:"creation_timeout"`
}
```

### Metrics

```go
type CheckpointMetrics struct {
    TotalCheckpoints   int64                  `json:"total_checkpoints"`
    LastCheckpointTime time.Time              `json:"last_checkpoint_time"`
    TotalReplayTime    time.Duration          `json:"total_replay_time"`
    Config             DeltaCheckpointConfig  `json:"config"`
}
```

## Best Practices

1. **Enable validation in production** for critical data integrity
2. **Use adaptive mode** for workloads with varying data volumes
3. **Monitor checkpoint health score** and set up alerting
4. **Test recovery procedures** regularly
5. **Tune retention policies** based on storage costs and recovery needs
6. **Enable compaction** for tables with many small files
7. **Use conservative settings initially** and tune based on monitoring
8. **Document checkpoint configurations** and operational procedures