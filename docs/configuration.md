# Configuration Reference

AgentBrain uses a YAML configuration file with support for environment variable substitution.

## Loading

```bash
./bin/agentbrain --config path/to/config.yaml
```

## Environment Variable Substitution

Config values support `${VAR}` and `${VAR:-default}` syntax:

```yaml
storage:
  bucket: "${S3_BUCKET}"                      # Required env var
  region: "${AWS_REGION:-us-east-1}"          # With default value
  endpoint: "${S3_ENDPOINT:-}"                # Empty default
```

Variables are expanded before YAML parsing. If a variable is not set and has no default, the literal `${VAR}` string remains.

## Full Schema

```yaml
# Agent-level settings
agent:
  log_level: info           # debug | info | warn | error
  log_format: json          # json | text
  health_addr: ":8080"      # Health check HTTP listen address
  schedule: "@every 1h"     # Default sync schedule for all sources
  timeout: 30m              # Graceful shutdown timeout

# S3 storage settings
storage:
  bucket: my-bucket         # S3 bucket name (REQUIRED)
  region: us-east-1         # AWS region (REQUIRED)
  endpoint: ""              # Custom S3 endpoint (LocalStack, MinIO)
  prefix: ""                # Optional key prefix for all S3 operations

# Data sources to sync
sources:
  source_name:              # Arbitrary name for this source
    type: salesforce         # Connector type (REQUIRED)
    enabled: true            # Enable/disable this source
    schedule: "@every 1h"    # Override agent-level schedule
    concurrency: 4           # Max parallel object syncs
    batch_size: 10000        # Records per extraction batch
    objects:                 # Allow list of objects (empty = all)
      - Account
      - Contact
    auth:                    # Connector-specific auth credentials
      client_id: "..."
      client_secret: "..."
    options:                 # Connector-specific options
      key: "value"

# Backup and disaster recovery settings (optional)
backup:
  enabled: false             # Enable/disable backup system
  destination_bucket: ""     # S3 bucket for backup storage
  destination_region: ""     # AWS region for backup bucket  
  schedule: "@daily"         # Automated backup schedule
  retention_days: 30         # Days to retain backups
  cross_region: false        # Enable cross-region replication
  encryption_key: ""         # KMS key ID for backup encryption
  validation_mode: "checksum" # Validation mode: checksum, full, none
  concurrent_uploads: 4      # Concurrent file uploads during backup
  chunk_size_mb: 64         # File chunk size for large uploads
```

## Section Details

### agent

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `log_level` | string | `info` | Minimum log level. Options: `debug`, `info`, `warn`, `error` |
| `log_format` | string | `json` | Log output format. `json` for structured logs, `text` for human-readable |
| `health_addr` | string | `:8080` | HTTP listen address for `/healthz` and `/readyz` endpoints |
| `schedule` | string | `@every 1h` | Default cron schedule for sources that don't specify their own |
| `timeout` | duration | `30m` | Maximum time to wait for in-flight operations during shutdown |

### storage

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `bucket` | string | - | S3 bucket name. **Required.** |
| `region` | string | - | AWS region for the S3 bucket. **Required.** |
| `endpoint` | string | - | Custom S3 endpoint URL. Set for LocalStack (`http://localhost:4566`) or MinIO. Enables path-style addressing. |
| `prefix` | string | - | Optional prefix prepended to all S3 keys. Useful for sharing a bucket. |

### sources.{name}

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `type` | string | - | Connector type identifier. **Required.** Currently: `salesforce` |
| `enabled` | bool | `false` | Whether this source is active. Disabled sources are skipped entirely. |
| `schedule` | string | Agent schedule | Cron expression for this source. Overrides the agent-level schedule. |
| `concurrency` | int | `4` | Maximum number of objects synced in parallel. |
| `batch_size` | int | `10000` | Number of records per extraction batch. Affects memory usage and Parquet file size. |
| `objects` | []string | `[]` (all) | Allow list of object names to sync. Empty means sync all queryable objects discovered by the connector. |
| `auth` | map | - | Key-value pairs for authentication. Keys are connector-specific. |
| `options` | map | - | Key-value pairs for additional connector-specific settings. |
| `checkpoint` | map | - | Checkpoint management configuration. See below for options. |
| `validation` | map | - | Data validation and drift detection configuration. See below for options. |

### Data Validation Configuration

Each source can include comprehensive data validation settings:

```yaml
sources:
  salesforce_prod:
    validation:
      enabled: true
      error_threshold: 0.05      # Alert if >5% of records have errors
      drift_threshold: 0.10      # Alert if field patterns change >10%
      strict_mode: false         # Fail sync on validation errors
      custom_rules:              # Object-specific validation rules
        Account:
          - field: "AnnualRevenue"
            type: "range"
            min: 0
            max: 1000000000
          - field: "Type"
            type: "enum"
            values: ["Customer", "Partner", "Prospect"]
        Contact:
          - field: "Email"
            type: "format"
            pattern: "email"
          - field: "Age"
            type: "range"
            min: 0
            max: 150
            required: true
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable data validation framework |
| `error_threshold` | float | `0.05` | Maximum error rate before alerting (0-1) |
| `drift_threshold` | float | `0.10` | Maximum drift score before alerting (0-1) |
| `strict_mode` | bool | `false` | Fail sync on validation errors vs log warnings |
| `custom_rules` | map | `{}` | Object-specific validation rules |

#### Custom Validation Rules

Custom rules enforce business-specific data constraints per object:

**Range Rules** - Validate numeric fields are within bounds:
```yaml
- field: "Price"
  type: "range"
  min: 0.01
  max: 10000.00
```

**Enum Rules** - Validate string fields contain only allowed values:
```yaml
- field: "Status"
  type: "enum"
  values: ["pending", "shipped", "delivered", "cancelled"]
```

**Format Rules** - Validate string fields match regex patterns:
```yaml
- field: "CustomerID"
  type: "format"
  pattern: "^CUST-[0-9]{6}$"
```

The system also applies built-in validation rules automatically:
- **Type Consistency**: Field types match schema declarations
- **Null Constraints**: Non-nullable fields don't contain nulls
- **Format Detection**: Auto-validates emails, phones, URLs based on field names
- **Data Quality**: Detects placeholder values, whitespace issues, suspicious patterns

For detailed validation configuration, see [Data Validation Guide](data-validation.md).

### Checkpoint Configuration

Each source can include checkpoint management settings:

```yaml
sources:
  source_name:
    checkpoint:
      frequency: 10              # Commits between checkpoints
      retention_days: 30         # Days to retain old checkpoints
      max_checkpoints: 50        # Maximum checkpoints to keep
      validation_enabled: true   # Validate checkpoints after creation
      compaction_enabled: true   # Enable small file compaction
      adaptive_mode: true        # Use adaptive checkpoint frequency
      size_threshold_mb: 128     # Size threshold for adaptive mode
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `frequency` | int | `10` | Number of commits between checkpoints |
| `retention_days` | int | `30` | Days to retain old checkpoints |
| `max_checkpoints` | int | `50` | Maximum checkpoints to retain |
| `validation_enabled` | bool | `false` | Enable checkpoint validation |
| `compaction_enabled` | bool | `false` | Enable file compaction during checkpointing |
| `adaptive_mode` | bool | `false` | Adjust frequency based on data volume |
| `size_threshold_mb` | int64 | `128` | Data volume threshold for adaptive mode |

For detailed guidance on checkpoint configuration, see [Checkpoint Management Guide](checkpoint-management.md).

### backup

The backup system provides comprehensive data protection for Delta Lake tables. All backup settings are optional when `enabled` is `false`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable or disable the backup system |
| `destination_bucket` | string | - | S3 bucket for storing backups. **Required when enabled.** |
| `destination_region` | string | - | AWS region for the backup bucket. **Required when enabled.** |
| `schedule` | string | `@daily` | Cron expression for automated backup scheduling |
| `retention_days` | int | `30` | Number of days to retain backups before deletion |
| `cross_region` | bool | `false` | Enable cross-region replication for disaster recovery |
| `encryption_key` | string | - | KMS key ID for encrypting backup data |
| `validation_mode` | string | `checksum` | Backup validation mode: `checksum`, `full`, or `none` |
| `concurrent_uploads` | int | `4` | Number of concurrent file uploads during backup operations |
| `chunk_size_mb` | int | `64` | Chunk size in MB for uploading large files |

For detailed backup configuration and usage, see [Backup and Disaster Recovery Guide](backup-recovery.md).

### Salesforce Auth Keys

| Key | Description |
|-----|-------------|
| `client_id` | Connected App consumer key |
| `client_secret` | Connected App consumer secret |
| `username` | Salesforce user email |
| `password` | User password |
| `security_token` | User security token (appended to password in OAuth flow) |
| `login_url` | OAuth endpoint. `https://login.salesforce.com` for production, `https://test.salesforce.com` for sandboxes |

## Schedule Syntax

The scheduler uses [robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3) with the following formats:

| Expression | Description |
|-----------|-------------|
| `@every 1h` | Every hour |
| `@every 30m` | Every 30 minutes |
| `@every 6h` | Every 6 hours |
| `@daily` | Once a day at midnight |
| `@hourly` | Once an hour at minute 0 |
| `0 */2 * * *` | Every 2 hours (standard cron) |
| `30 9 * * 1-5` | 9:30 AM weekdays |
| `0 0 * * 0` | Midnight Sunday |

Seconds are optional: `*/30 * * * * *` runs every 30 seconds.

## Validation

The config loader validates:

1. `storage.bucket` is non-empty
2. `storage.region` is non-empty
3. At least one source is defined
4. Each source has a non-empty `type`

Additional validation should be added for connector-specific auth keys.

## Example: Minimal Config

```yaml
storage:
  bucket: my-data-lake
  region: us-east-1

sources:
  sf:
    type: salesforce
    enabled: true
    auth:
      client_id: "3MVG9..."
      client_secret: "ABC123..."
      username: "admin@mycompany.com"
      password: "secret"
      security_token: "XYZTOKEN"
```

## Example: LocalStack Development

```yaml
agent:
  log_level: debug
  log_format: text

storage:
  bucket: test-bucket
  region: us-east-1
  endpoint: "http://localhost:4566"

sources:
  sf_sandbox:
    type: salesforce
    enabled: true
    schedule: "@every 5m"
    concurrency: 2
    batch_size: 1000
    objects:
      - Account
    auth:
      client_id: "${SF_CLIENT_ID}"
      client_secret: "${SF_CLIENT_SECRET}"
      username: "${SF_USERNAME}"
      password: "${SF_PASSWORD}"
      security_token: "${SF_SECURITY_TOKEN}"
      login_url: "https://test.salesforce.com"
```

## Example: Multi-Source Production

```yaml
agent:
  log_level: info
  log_format: json
  health_addr: ":8080"
  timeout: 1h

storage:
  bucket: enterprise-datalake
  region: us-west-2
  prefix: agentbrain/v1

sources:
  salesforce_prod:
    type: salesforce
    enabled: true
    schedule: "@every 1h"
    concurrency: 8
    batch_size: 50000
    objects:
      - Account
      - Contact
      - Opportunity
      - Lead
      - Case
      - Task
      - Event
    auth:
      client_id: "${SF_PROD_CLIENT_ID}"
      client_secret: "${SF_PROD_CLIENT_SECRET}"
      username: "${SF_PROD_USERNAME}"
      password: "${SF_PROD_PASSWORD}"
      security_token: "${SF_PROD_SECURITY_TOKEN}"

  salesforce_sandbox:
    type: salesforce
    enabled: false
    schedule: "@every 6h"
    concurrency: 2
    objects:
      - Account
    auth:
      client_id: "${SF_SANDBOX_CLIENT_ID}"
      client_secret: "${SF_SANDBOX_CLIENT_SECRET}"
      username: "${SF_SANDBOX_USERNAME}"
      password: "${SF_SANDBOX_PASSWORD}"
      security_token: "${SF_SANDBOX_SECURITY_TOKEN}"
      login_url: "https://test.salesforce.com"
```
