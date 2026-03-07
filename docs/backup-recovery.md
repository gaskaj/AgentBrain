# Backup and Disaster Recovery

This document describes AgentBrain's comprehensive backup and disaster recovery system for Delta Lake tables.

## Overview

The backup system provides enterprise-grade data protection capabilities including:

- Point-in-time backups of Delta tables
- Cross-region disaster recovery
- Automated backup scheduling and retention
- Backup integrity validation
- Granular restore capabilities

## Architecture

### Components

The backup system consists of several integrated components:

- **BackupEngine**: Creates consistent snapshots of Delta tables
- **RestoreEngine**: Recovers tables from backup snapshots  
- **BackupScheduler**: Automated backup scheduling with retention policies
- **BackupValidator**: Integrity validation and corruption detection
- **BackupManager**: Unified interface coordinating all backup operations

### Backup Format

Backups are stored in a structured format that preserves Delta Lake semantics:

```
backups/{source}/{table}/{timestamp}/
├── manifest.json              # Backup metadata and file inventory
├── _delta_log/               # Complete transaction log history
│   ├── 00000000000000000000.json
│   ├── 00000000000000000001.json
│   └── _last_checkpoint
├── data/                     # All referenced Parquet files
│   ├── part-00000-*.parquet
│   └── part-00001-*.parquet
└── checksum.sha256          # Integrity verification
```

### Storage Strategy

- **Isolation**: Backups stored separately from source data to prevent cascading failures
- **Cross-region**: Optional replication to different AWS regions for disaster recovery
- **Versioning**: Multiple backup versions retained based on retention policies
- **Encryption**: Support for at-rest encryption of backup data

## Configuration

### Basic Configuration

Add backup configuration to your `agentbrain.yaml`:

```yaml
backup:
  enabled: true
  destination_bucket: "my-backup-bucket"
  destination_region: "us-west-2"
  schedule: "@daily"
  retention_days: 30
  validation_mode: "checksum"
  concurrent_uploads: 4
```

### Advanced Configuration

```yaml
backup:
  enabled: true
  destination_bucket: "my-backup-bucket"
  destination_region: "us-west-2"
  schedule: "@daily"
  retention_days: 90
  cross_region: true
  encryption_key: "${BACKUP_ENCRYPTION_KEY}"
  validation_mode: "full"  # checksum, full, none
  concurrent_uploads: 8
  chunk_size_mb: 128
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `false` | Enable/disable backup system |
| `destination_bucket` | - | S3 bucket for backup storage (required) |
| `destination_region` | - | AWS region for backup bucket (required) |
| `schedule` | `@daily` | Cron expression for automated backups |
| `retention_days` | `30` | Days to retain backups before deletion |
| `cross_region` | `false` | Enable cross-region replication |
| `encryption_key` | - | KMS key ID for backup encryption |
| `validation_mode` | `checksum` | Backup validation mode |
| `concurrent_uploads` | `4` | Concurrent file uploads during backup |
| `chunk_size_mb` | `64` | File chunk size for large uploads |

### Schedule Formats

The scheduler supports standard cron expressions:

- `@daily` - Daily at midnight
- `@hourly` - Every hour  
- `@every 6h` - Every 6 hours
- `0 2 * * *` - Daily at 2:00 AM
- `0 */4 * * *` - Every 4 hours

## Usage

### Command Line Interface

#### Create Backup

Create an immediate backup of a table:

```bash
agentbrain --backup-create source/table
```

#### List Backups

List all backups for a source/table:

```bash
agentbrain --backup-list source/table
```

#### Validate Backup

Validate backup integrity:

```bash
agentbrain --backup-validate backup-id-12345
```

#### Restore from Backup

Restore a table from backup:

```bash
agentbrain --backup-restore backup-id-12345:target-source/target-table
```

### Programmatic API

#### Creating Backups

```go
// Create backup manager
manager := backup.NewManager(s3Client, backupConfig, logger)

// Create immediate backup
metadata, err := manager.Engine().CreateBackup(ctx, "source", "table", -1)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Backup created: %s\n", metadata.BackupID)
```

#### Scheduling Backups

```go
// Start scheduler
err := manager.Start(ctx)
if err != nil {
    log.Fatal(err)
}

// Schedule daily backups
err = manager.Scheduler().ScheduleBackup("source", "table", "@daily")
if err != nil {
    log.Fatal(err)
}
```

#### Restoring Data

```go
// Get restore preview
preview, err := manager.Restore().GetRestorePreview(ctx, "backup-id")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Will restore %d files (%.2f MB)\n", 
    len(preview.FilesToRestore), 
    float64(preview.TotalSize)/(1024*1024))

// Perform restore
err = manager.Restore().RestoreFromBackup(ctx, "backup-id", "target-source", "target-table")
if err != nil {
    log.Fatal(err)
}
```

## Disaster Recovery Procedures

### Recovery Scenarios

#### Complete Data Loss

If your primary S3 bucket is lost:

1. **Assess Scope**: Determine which tables are affected
2. **Identify Backups**: List available backups for affected tables
3. **Prioritize Recovery**: Restore most critical tables first
4. **Validate Results**: Verify restored data integrity

```bash
# List all backups
agentbrain --backup-list source/critical-table

# Restore to new location
agentbrain --backup-restore backup-123:recovered/critical-table

# Validate restored table
agentbrain --backup-validate backup-123
```

#### Partial Corruption

For selective restoration:

1. **Identify Corruption**: Determine affected tables/versions
2. **Find Clean Backup**: Locate backup before corruption
3. **Selective Restore**: Restore only affected components
4. **Merge Changes**: Apply any post-backup changes

#### Cross-Region Failover

If primary region fails:

1. **Switch Regions**: Update configuration to backup region
2. **Restore Services**: Deploy AgentBrain in backup region
3. **Restore Data**: Restore tables from cross-region backups
4. **Resume Operations**: Restart data ingestion

### Recovery Time Objectives (RTO)

| Scenario | Target RTO | Factors |
|----------|------------|---------|
| Single table restore | < 30 minutes | Table size, network bandwidth |
| Multiple table restore | < 2 hours | Number of tables, parallelization |
| Complete environment restore | < 4 hours | Total data size, resource availability |
| Cross-region failover | < 1 hour | Cross-region latency, automation level |

### Recovery Point Objectives (RPO)

- **Scheduled Backups**: Up to backup frequency (daily = 24h RPO)
- **Immediate Backups**: Near-zero RPO with manual backup
- **Transaction-Level**: Not supported (use Delta Lake's built-in features)

## Monitoring and Alerting

### Backup Success/Failure

Monitor backup operations through:

- **Logs**: Structured logs with backup status
- **Metrics**: Success rates, durations, file counts
- **Alerts**: Notifications on backup failures

### Validation Monitoring

Continuous validation ensures backup integrity:

- **Checksum Validation**: Fast file integrity checks
- **Full Validation**: Complete backup restorability tests
- **Automated Alerts**: Proactive notification of issues

### Retention Monitoring

Track backup storage and cleanup:

- **Storage Usage**: Monitor backup storage growth
- **Retention Compliance**: Verify old backups are cleaned up
- **Cost Optimization**: Balance retention vs. storage costs

## Best Practices

### Backup Strategy

1. **Regular Schedule**: Use automated daily or hourly backups
2. **Immediate Backups**: Create backups before major changes
3. **Cross-Region**: Enable for critical data protection
4. **Validation**: Regularly validate backup integrity

### Storage Management

1. **Separate Buckets**: Use different buckets for backups and source data
2. **Lifecycle Policies**: Configure S3 lifecycle rules for cost optimization
3. **Access Control**: Limit backup access to authorized users only
4. **Encryption**: Enable encryption for sensitive data

### Testing

1. **Regular Drills**: Perform periodic restore tests
2. **Documentation**: Document recovery procedures
3. **Automation**: Script common recovery scenarios
4. **Validation**: Test backup integrity regularly

### Security

1. **IAM Roles**: Use minimal permissions for backup operations
2. **Encryption**: Enable encryption at rest and in transit
3. **Access Logs**: Monitor backup access and modifications
4. **Key Management**: Rotate encryption keys regularly

## Troubleshooting

### Common Issues

#### Backup Failures

**Symptoms**: Backup creation fails with errors
**Causes**: 
- Insufficient S3 permissions
- Network connectivity issues
- Source table corruption
- Insufficient storage space

**Solutions**:
- Verify IAM permissions include backup bucket access
- Check network connectivity to S3
- Validate source table structure
- Monitor storage quotas

#### Restore Failures

**Symptoms**: Restore operations fail or produce incomplete results
**Causes**:
- Corrupted backup data
- Insufficient target permissions
- Target location conflicts
- Network interruptions

**Solutions**:
- Validate backup integrity first
- Verify target location permissions
- Clear existing target data if needed
- Retry with increased timeout

#### Performance Issues

**Symptoms**: Backup/restore operations are slow
**Causes**:
- Large file sizes
- Network bandwidth limitations
- High S3 request rates
- Insufficient concurrency

**Solutions**:
- Increase concurrent uploads
- Use larger chunk sizes for big files
- Implement request rate limiting
- Optimize S3 transfer acceleration

### Diagnostic Commands

```bash
# Check backup configuration
agentbrain --config configs/agentbrain.yaml --backup-list source/table

# Validate specific backup
agentbrain --backup-validate backup-id

# Test restore preview
agentbrain --backup-restore backup-id:test/table --dry-run
```

### Log Analysis

Backup operations generate structured logs:

```json
{
  "level": "info",
  "msg": "backup completed",
  "backup_id": "abc123",
  "source": "salesforce",
  "table": "accounts", 
  "duration": "45.2s",
  "files": 127,
  "size": 1073741824
}
```

Monitor for:
- Backup success/failure rates
- Duration trends
- File count changes
- Error patterns

## Performance Optimization

### Backup Performance

1. **Concurrent Uploads**: Increase `concurrent_uploads` for better throughput
2. **Chunk Size**: Use larger `chunk_size_mb` for big files
3. **Network**: Use S3 Transfer Acceleration for cross-region backups
4. **Scheduling**: Distribute backup schedules to avoid resource contention

### Restore Performance  

1. **Parallel Restoration**: Restore multiple tables simultaneously
2. **Incremental Restore**: Restore only changed files when possible
3. **Local Caching**: Use local storage for temporary restore staging
4. **Network Optimization**: Choose restore targets close to backup storage

### Storage Optimization

1. **Compression**: Enable S3 compression for backup storage
2. **Storage Classes**: Use appropriate S3 storage classes (IA, Glacier)
3. **Lifecycle Policies**: Automatically transition old backups to cheaper storage
4. **Deduplication**: Implement file-level deduplication for space savings

## Migration Guide

### Enabling Backups

To enable backups for an existing AgentBrain deployment:

1. **Update Configuration**: Add backup section to config
2. **Create Backup Bucket**: Set up dedicated S3 bucket
3. **Configure Permissions**: Add required IAM policies
4. **Test Operations**: Verify backup/restore functionality
5. **Schedule Backups**: Enable automated backup scheduling

### Upgrading Backup System

When upgrading the backup system:

1. **Backup Configuration**: Save current backup settings
2. **Stop Scheduler**: Gracefully stop backup operations
3. **Upgrade System**: Deploy new backup system version
4. **Validate Compatibility**: Ensure existing backups are readable
5. **Resume Operations**: Restart backup scheduling

## Security Considerations

### Access Control

- Use IAM roles with minimal required permissions
- Separate backup access from regular operations
- Implement multi-factor authentication for restore operations
- Audit backup access regularly

### Encryption

- Enable S3 server-side encryption for backup storage
- Use customer-managed KMS keys for sensitive data
- Encrypt data in transit during backup/restore operations
- Rotate encryption keys regularly

### Compliance

- Document backup procedures for compliance requirements
- Implement data retention policies matching regulatory needs
- Audit backup operations and access logs
- Ensure cross-border data transfer compliance

This backup and disaster recovery system provides comprehensive data protection for AgentBrain Delta tables while maintaining operational simplicity and enterprise-grade reliability.