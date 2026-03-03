# CLAUDE.md - AgentBrain Project Guide

This file provides context for AI assistants working on the AgentBrain codebase.

## What This Project Is

AgentBrain is a Go application that acts as an extensible agent for collecting enterprise SaaS data and storing it in S3 using Parquet files with a custom Delta Lake protocol. It runs as a long-lived daemon with cron-based scheduling. Salesforce is the first connector; the architecture supports adding others (SAP, HubSpot, etc).

## Module and Build

- **Module path:** `github.com/agentbrain/agentbrain`
- **Go version:** 1.22+ (go.mod tracks 1.24.9)
- **Build:** `make build` produces `bin/agentbrain`
- **Test:** `make test` runs `go test -race -count=1 ./...`
- **Lint:** `make lint` uses golangci-lint
- **Binary flags:** `--config <path>` (required), `--once` (single run, no daemon)

## Package Layout

```
cmd/agentbrain/main.go               Entry point. Parses flags, wires everything, runs daemon or single-shot.
internal/config/config.go             Config types (Config, AgentConfig, StorageConfig, SourceConfig).
                                      Loads YAML with ${ENV:-default} expansion.
internal/connector/connector.go       Core Connector interface (7 methods). ObjectMetadata, RecordBatch types.
internal/connector/registry.go        Factory-based Registry for connectors. Thread-safe.
internal/connector/salesforce/        Salesforce implementation:
  types.go                             API response structs (TokenResponse, DescribeResult, BulkJobResponse, etc.)
  client.go                            HTTP client with OAuth2 password flow, retries (3x exponential), rate limit handling.
  metadata.go                          DescribeGlobal + DescribeObject. Maps SF types to internal schema types.
  rest.go                              REST SOQL queries with pagination. Builds incremental/full SOQL.
  bulk.go                              Bulk API 2.0: create job → poll → stream CSV results as RecordBatch.
  connector.go                         SalesforceConnector implements connector.Connector. Register() function.
internal/storage/s3.go                S3Client wrapping aws-sdk-go-v2. Upload, Download, List, Delete, PutJSON, GetJSON.
                                      Supports custom endpoints (LocalStack) and key prefixes.
internal/storage/layout.go            Layout type with methods for all S3 key path conventions.
internal/storage/parquet.go           ParquetWriter: dynamic schema → Parquet files via parquet-go. Snappy compression.
internal/storage/delta/actions.go     Delta protocol action types: Protocol, Metadata, Add, Remove, CommitInfo.
                                      Constructor functions for each action type.
internal/storage/delta/log.go         TransactionLog: write/read newline-delimited JSON version files.
                                      S3Store interface (6 methods) used for dependency injection.
internal/storage/delta/table.go       DeltaTable: Initialize (v0), Commit (append), Snapshot (replay to version), time-travel.
internal/storage/delta/checkpoint.go  CheckpointManager: creates checkpoints every 10 versions. JSON format.
internal/schema/schema.go             Schema type with Fields, deterministic hashing (SHA256, order-independent).
                                      ToDeltaSchemaString() for Delta log metadata.
internal/schema/evolution.go          Diff(old, new) → SchemaDiff with ChangeNone/Additive/Breaking.
                                      Detects added fields, removed fields, type changes.
internal/schema/parquet_schema.go     ToParquetSchema() converts internal Schema → parquet.Schema.
internal/sync/state.go                ObjectState (watermark, schema hash, delta version), SyncState (per-source).
                                      StateStore: load/save from S3. Returns empty state if none exists.
internal/sync/planner.go              Planner.Plan(): no state → full; breaking change → full; else → incremental.
                                      SyncMode enum: Full, Incremental, Skip.
internal/sync/engine.go               Engine.Run(): connect → load state → discover → plan → extract → write → commit → save.
                                      Parallel object sync via goroutines + semaphore.
internal/scheduler/scheduler.go       Cron-based Scheduler using robfig/cron/v3. Supports @every syntax.
internal/observability/logging.go     SetupLogging(): configures slog (JSON/text, level).
internal/observability/health.go      HealthServer: /healthz (always 200), /readyz (200 or 503 based on ready state).
```

## Key Interfaces

### connector.Connector
```go
type Connector interface {
    Name() string
    Connect(ctx context.Context) error
    Close() error
    DiscoverMetadata(ctx context.Context) ([]ObjectMetadata, error)
    DescribeObject(ctx context.Context, objectName string) (*ObjectMetadata, error)
    GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan RecordBatch, <-chan error)
    GetFullSnapshot(ctx context.Context, objectName string) (<-chan RecordBatch, <-chan error)
}
```

Returns data via channels for streaming without full buffering.

### delta.S3Store
```go
type S3Store interface {
    Upload(ctx context.Context, key string, data []byte, contentType string) error
    Download(ctx context.Context, key string) ([]byte, error)
    List(ctx context.Context, prefix string) ([]string, error)
    PutJSON(ctx context.Context, key string, v any) error
    GetJSON(ctx context.Context, key string, v any) error
    Exists(ctx context.Context, key string) (bool, error)
}
```

Used for dependency injection. The `*storage.S3Client` satisfies this interface. Tests use an in-memory mock.

## Data Flow

```
Scheduler triggers Engine.Run()
  → Connector.Connect() (OAuth)
  → StateStore.Load() (S3 JSON)
  → Connector.DiscoverMetadata() → object list
  → For each object:
      → Connector.DescribeObject() → schema
      → Planner.Plan() → full/incremental/skip
  → For each plan (parallel, semaphore-limited):
      → DeltaTable.Initialize() (if first time)
      → Connector.GetFullSnapshot() or GetIncrementalChanges()
        → channel of RecordBatch
      → ParquetWriter.WriteRecords() → S3 upload + WrittenFile
      → DeltaTable.Commit() → new version in _delta_log
      → CheckpointManager.MaybeCheckpoint()
      → Update ObjectState with new watermark
  → StateStore.Save()
```

## Delta Lake Protocol

Custom Go implementation of a subset of the [Delta Lake protocol](https://github.com/delta-io/delta/blob/master/PROTOCOL.md):

- **Protocol version:** minReaderVersion=1, minWriterVersion=2
- **Action types:** `protocol`, `metaData`, `add`, `remove`, `commitInfo`
- **Transaction log:** Newline-delimited JSON files at `_delta_log/{version:020d}.json`
- **Time-travel:** Replay log entries from version 0 to target version
- **Checkpoints:** JSON snapshots every 10 versions for read performance

## Schema Evolution

- Schemas are hashed (SHA256 of sorted field name/type pairs, order-independent)
- On each sync, the new schema hash is compared to the stored hash
- **Additive changes** (new columns): incremental sync continues
- **Breaking changes** (removed columns, type changes): forces full resync
- Schema versions are stored in `state/{source}/schema_history/{object}/v{n}.json`
- Previous schema is stored in ObjectState for proper diff computation

## S3 Key Layout

All paths are generated by `storage.Layout` methods:

| Method | Pattern |
|--------|---------|
| `DataPrefix` | `data/{source}/{object}` |
| `DeltaLogPrefix` | `data/{source}/{object}/_delta_log/` |
| `DeltaLogEntry` | `data/{source}/{object}/_delta_log/{version:020d}.json` |
| `DeltaCheckpoint` | `data/{source}/{object}/_delta_log/{version:020d}.checkpoint.parquet` |
| `DeltaLastCheckpoint` | `data/{source}/{object}/_delta_log/_last_checkpoint` |
| `ParquetFile` | `data/{source}/{object}/{filename}` |
| `SyncState` | `state/{source}/sync_state.json` |
| `SchemaVersion` | `state/{source}/schema_history/{object}/v{n}.json` |
| `Catalog` | `metadata/{source}/catalog.json` |

## Dependencies

| Package | Purpose |
|---------|---------|
| `aws-sdk-go-v2` (s3, config, credentials) | S3 operations |
| `parquet-go/parquet-go` | Parquet writing with dynamic schemas |
| `robfig/cron/v3` | Cron scheduling |
| `gopkg.in/yaml.v3` | Config parsing |
| `google/uuid` | Parquet file naming |
| `stretchr/testify` | Test assertions |

## Testing

Tests use in-memory mocks (no external services required):

- `internal/config/config_test.go` - Config loading, env var expansion, validation
- `internal/schema/schema_test.go` - Hashing, field names, delta schema string
- `internal/schema/evolution_test.go` - Schema diff detection (none, additive, breaking)
- `internal/storage/delta/log_test.go` - Transaction log write/read, version listing
- `internal/storage/delta/table_test.go` - Table init, commit, snapshot, time-travel
- `internal/sync/planner_test.go` - Sync planning (first run, incremental, breaking, allow list)

The `mockS3Store` in `delta/log_test.go` is a thread-safe in-memory implementation of the `S3Store` interface.

## Conventions

- All exported functions and types have doc comments
- Errors are wrapped with `fmt.Errorf("context: %w", err)` for chain debugging
- Context is threaded through all operations for cancellation
- Structured logging via `log/slog` with JSON output by default
- Configuration supports `${ENV_VAR:-default}` environment variable substitution
- Parquet files use Snappy compression and UUID-based naming
- Delta log versions use zero-padded 20-digit numbers

## Adding a New Connector

1. Create `internal/connector/{name}/` package
2. Implement `connector.Connector` interface
3. Add a `Register(registry *connector.Registry)` function
4. Call `Register` in `cmd/agentbrain/main.go`
5. Add the connector type string to config validation if desired

## Common Tasks

- **Add a new SaaS source:** Implement connector.Connector, register in main.go
- **Change sync schedule:** Edit `schedule` in config YAML (cron or `@every` syntax)
- **Modify S3 layout:** Update `storage.Layout` methods
- **Change Parquet compression:** Modify `storage.ParquetWriter.WriteRecords()`
- **Add new schema field types:** Update `schema.FieldType` constants + `schema.ToParquetSchema()`
- **Adjust checkpoint frequency:** Change `checkpointInterval` in `delta/checkpoint.go`
