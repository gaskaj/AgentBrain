# AgentBrain

Enterprise SaaS data collection agent that extracts, organizes, and stores data from SaaS platforms into S3 using Parquet files and a custom Delta Lake protocol. Designed for schemaless, versioned, time-travel-capable data storage.

Salesforce is the first supported connector. The architecture supports adding SAP, HubSpot, and other connectors through a pluggable interface.

## Architecture

```
┌──────────────┐     ┌───────────────────┐     ┌──────────────────────┐
│  Scheduler   │────>│   Sync Engine     │────>│  Storage Layer       │
│ (robfig/cron)│     │  (orchestrator)   │     │  S3 + Parquet +      │
└──────────────┘     │                   │     │  Delta Transaction   │
                     │  ┌─────────────┐  │     │  Log                 │
                     │  │   Planner   │  │     └──────────────────────┘
                     │  └─────────────┘  │
                     │  ┌─────────────┐  │     ┌──────────────────────┐
                     │  │ Schema Mgr  │  │     │  State Store (S3)    │
                     │  └─────────────┘  │     │  watermarks, schema  │
                     └────────┬──────────┘     │  versions            │
                              │                └──────────────────────┘
                     ┌────────┴──────────┐
                     │  Connector        │
                     │  Interface        │
                     ├───────────────────┤
                     │ Salesforce  │ SAP │
                     └─────────────┴─────┘
```

## Quick Start

### Prerequisites

- Go 1.22+
- AWS credentials configured (or LocalStack for local development)
- Salesforce Connected App credentials (for Salesforce connector)

### Build

```bash
make build
```

### Configure

```bash
cp configs/agentbrain.example.yaml configs/agentbrain.yaml
# Edit configs/agentbrain.yaml with your S3 bucket and Salesforce credentials
```

Environment variables are supported with `${VAR:-default}` syntax:

```yaml
storage:
  bucket: "${S3_BUCKET:-my-datalake-bucket}"
  region: "${AWS_REGION:-us-east-1}"
```

### Run

Single sync cycle:

```bash
./bin/agentbrain --config configs/agentbrain.yaml --once
```

Daemon mode (long-running with scheduler):

```bash
./bin/agentbrain --config configs/agentbrain.yaml
```

Health check:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

### Docker

```bash
make docker-build
docker run -v $(pwd)/configs:/etc/agentbrain agentbrain:latest
```

## Configuration

See [`configs/agentbrain.example.yaml`](configs/agentbrain.example.yaml) for a complete example.

| Section | Key | Description | Default |
|---------|-----|-------------|---------|
| `agent.log_level` | Log verbosity | `debug`, `info`, `warn`, `error` | `info` |
| `agent.log_format` | Log output format | `json` or `text` | `json` |
| `agent.health_addr` | Health check listen address | `:port` | `:8080` |
| `agent.schedule` | Default sync schedule (cron) | Any cron expression | `@every 1h` |
| `agent.timeout` | Graceful shutdown timeout | Duration | `30m` |
| `storage.bucket` | S3 bucket name | Required | - |
| `storage.region` | AWS region | Required | - |
| `storage.endpoint` | Custom S3 endpoint (LocalStack) | URL | - |
| `storage.prefix` | Optional S3 key prefix | String | - |

Each source under `sources:` accepts:

| Key | Description | Default |
|-----|-------------|---------|
| `type` | Connector type (`salesforce`) | Required |
| `enabled` | Enable/disable this source | `false` |
| `schedule` | Override agent-level schedule | Agent schedule |
| `concurrency` | Parallel object syncs | `4` |
| `batch_size` | Records per batch | `10000` |
| `objects` | Allow list of objects to sync | All queryable |
| `auth` | Authentication credentials map | Required |

## S3 Data Layout

```
s3://bucket/
├── data/{source}/{object}/              # Delta tables
│   ├── _delta_log/
│   │   ├── 00000000000000000000.json    # Version 0 (init)
│   │   ├── 00000000000000000001.json    # Version 1
│   │   ├── 00000000000000000010.checkpoint.json
│   │   └── _last_checkpoint
│   ├── part-00000-{uuid}.snappy.parquet
│   └── part-00000-{uuid}.snappy.parquet
├── state/{source}/
│   ├── sync_state.json                  # Watermarks, run history
│   └── schema_history/{object}/
│       ├── v1.json                      # Schema version 1
│       └── v2.json                      # Schema version 2
└── metadata/{source}/
    └── catalog.json                     # Discovered object catalog
```

## Sync Flow

1. **Load state** - Read watermarks from `state/{source}/sync_state.json`
2. **Discover** - Call connector's DiscoverMetadata to list all available objects
3. **Plan** - For each object:
   - No prior state → full sync
   - Breaking schema change → full sync
   - Otherwise → incremental from last watermark
4. **Check schema** - Compare schema hash against stored version, persist new versions
5. **Extract** - Stream records through Bulk API 2.0 (large datasets) or REST (small)
6. **Write** - Stream RecordBatch → Parquet files on S3
7. **Commit** - Append to Delta transaction log
8. **Update state** - Advance watermark only on success (at-least-once semantics)

Objects sync in parallel with configurable concurrency.

## Development

### Test

```bash
make test          # Unit tests with race detector
make test-cover    # Tests with coverage report
make lint          # Run golangci-lint
```

### LocalStack Integration Testing

```bash
docker run -d -p 4566:4566 localstack/localstack
# Set endpoint in config: endpoint: "http://localhost:4566"
make test-integration
```

### Project Structure

```
AgentBrain/
├── cmd/agentbrain/main.go          # Entry point, CLI flags, daemon lifecycle
├── internal/
│   ├── config/                     # YAML config types + loading
│   ├── connector/                  # Connector interface + registry
│   │   └── salesforce/             # Salesforce connector implementation
│   ├── storage/                    # S3 client, Parquet writer, path layout
│   │   └── delta/                  # Custom Delta Lake protocol
│   ├── schema/                     # Schema representation, evolution, Parquet mapping
│   ├── sync/                       # Sync engine, planner, state management
│   ├── scheduler/                  # Cron-based job scheduling
│   └── observability/              # Logging + health endpoints
├── configs/                        # Configuration files
├── docs/                           # Documentation
├── Makefile
├── Dockerfile
└── go.mod
```

## Adding a New Connector

1. Create a package under `internal/connector/{name}/`
2. Implement the `connector.Connector` interface
3. Create a `Register(registry *connector.Registry)` function
4. Call `Register` in `cmd/agentbrain/main.go`

See [`docs/connector-guide.md`](docs/connector-guide.md) for the full guide.

## License

Proprietary. All rights reserved.
