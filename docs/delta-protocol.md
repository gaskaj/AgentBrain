# Delta Lake Protocol Implementation

AgentBrain implements a subset of the [Delta Lake protocol specification](https://github.com/delta-io/delta/blob/master/PROTOCOL.md) in pure Go. No mature Go Delta Lake library exists, so we implement the core features needed for append-only data ingestion with time-travel.

## Protocol Version

- `minReaderVersion`: 1
- `minWriterVersion`: 2

## Transaction Log

The transaction log is stored at `data/{source}/{object}/_delta_log/`. Each commit produces a new JSON file with a zero-padded 20-digit version number:

```
_delta_log/
├── 00000000000000000000.json    # Version 0 (table initialization)
├── 00000000000000000001.json    # Version 1
├── 00000000000000000002.json    # Version 2
├── 00000000000000000010.checkpoint.json
└── _last_checkpoint
```

Each version file contains newline-delimited JSON (one action per line):

```json
{"protocol":{"minReaderVersion":1,"minWriterVersion":2}}
{"metaData":{"id":"sf_Account","name":"Account","format":{"provider":"parquet"},"schemaString":"{...}","partitionColumns":[],"createdTime":1709500000000}}
{"commitInfo":{"timestamp":1709500000000,"operation":"CREATE TABLE","readVersion":-1,"isBlindAppend":true}}
```

## Action Types

### protocol

Declares the minimum reader and writer versions required.

```json
{
  "protocol": {
    "minReaderVersion": 1,
    "minWriterVersion": 2
  }
}
```

Written once in version 0.

### metaData

Describes the table: schema, format, partition columns.

```json
{
  "metaData": {
    "id": "salesforce_Account",
    "name": "Account",
    "format": {"provider": "parquet"},
    "schemaString": "{\"type\":\"struct\",\"fields\":[...]}",
    "partitionColumns": [],
    "createdTime": 1709500000000
  }
}
```

Written in version 0 and whenever the schema changes.

### add

Adds a data file to the table. Includes size, partition values, and optional statistics.

```json
{
  "add": {
    "path": "part-00000-abc123.snappy.parquet",
    "size": 1048576,
    "partitionValues": {},
    "modificationTime": 1709500100000,
    "dataChange": true,
    "stats": "{\"numRecords\":10000}"
  }
}
```

### remove

Logically deletes a data file. The file is no longer part of the active table state.

```json
{
  "remove": {
    "path": "part-00000-abc123.snappy.parquet",
    "deletionTimestamp": 1709500200000,
    "dataChange": true
  }
}
```

### commitInfo

Records metadata about the commit itself: timestamp, operation type, whether it was a blind append.

```json
{
  "commitInfo": {
    "timestamp": 1709500100000,
    "operation": "WRITE",
    "readVersion": 0,
    "isBlindAppend": true
  }
}
```

## Table Lifecycle

### Initialization (Version 0)

When a Delta table is first created:

```
Version 0:
  Line 1: protocol (minReaderVersion=1, minWriterVersion=2)
  Line 2: metaData (table ID, name, schema, format)
  Line 3: commitInfo (operation="CREATE TABLE")
```

### Data Commits (Version 1+)

Each sync cycle appends a new version:

```
Version 1:
  Line 1: add (part-00000-{uuid}.snappy.parquet, size, stats)
  Line 2: add (part-00001-{uuid}.snappy.parquet, size, stats)
  Line 3: commitInfo (operation="WRITE", readVersion=0)
```

### Schema Updates

When the schema changes, a new metaData action is included:

```
Version N:
  Line 1: metaData (updated schemaString)
  Line 2: add (new data file)
  Line 3: commitInfo (operation="WRITE (full sync)")
```

## Snapshots and Time-Travel

A snapshot represents the table state at a specific version. It is computed by replaying the log from version 0 to the target version:

1. Start with empty state
2. For each version 0..N:
   - `protocol` → set protocol
   - `metaData` → set metadata
   - `add` → add file to active set
   - `remove` → remove file from active set
3. Result: the set of active files at version N

```go
// Get the latest snapshot
snap, err := table.Snapshot(ctx, -1)

// Time-travel to version 5
snap, err := table.Snapshot(ctx, 5)

// Active files at that version
for path, addAction := range snap.Files {
    fmt.Println(path, addAction.Size)
}
```

## Checkpoints

To avoid replaying the entire log on every read, checkpoints capture the complete state at a version. AgentBrain implements comprehensive checkpoint management with the following features:

### Basic Checkpoints
- Created every 10 versions by default (configurable)
- Stored as `_delta_log/{version:020d}.checkpoint.json`
- Contains all active `protocol`, `metaData`, and `add` actions
- `_last_checkpoint` file records the latest checkpoint version

```json
// _last_checkpoint
{"version": 10, "size": 42}
```

### Enhanced Checkpoint Management

AgentBrain extends basic checkpointing with:

- **Adaptive Frequency**: Adjust checkpoint intervals based on data volume and performance
- **Validation & Recovery**: Detect corrupted checkpoints and fallback to previous valid versions
- **Automatic Cleanup**: Remove old checkpoints based on retention policies
- **File Compaction**: Optimize small files during checkpoint creation
- **Performance Monitoring**: Track checkpoint health and log replay improvements

### Configuration

Configure checkpoint behavior per source:

```yaml
sources:
  my_source:
    checkpoint:
      frequency: 10              # Commits between checkpoints
      retention_days: 30         # Days to retain old checkpoints
      validation_enabled: true   # Enable integrity checks
      adaptive_mode: true        # Dynamic frequency adjustment
      compaction_enabled: true   # Small file optimization
      size_threshold_mb: 128     # Size threshold for adaptive mode
```

For detailed configuration and operational procedures, see [Checkpoint Management Guide](checkpoint-management.md).

## Go Implementation

### Key Types

```go
// internal/storage/delta/actions.go
type Action struct {
    Protocol   *Protocol   `json:"protocol,omitempty"`
    MetaData   *Metadata   `json:"metaData,omitempty"`
    Add        *Add        `json:"add,omitempty"`
    Remove     *Remove     `json:"remove,omitempty"`
    CommitInfo *CommitInfo `json:"commitInfo,omitempty"`
}
```

### Transaction Log Operations

```go
// internal/storage/delta/log.go
log := delta.NewTransactionLog(s3Client, "data/sf/Account/_delta_log/")

// Write a version
log.WriteVersion(ctx, 0, actions)

// Read a version
actions, err := log.ReadVersion(ctx, 0)

// List all versions (sorted)
versions, err := log.ListVersions(ctx)  // [0, 1, 2, ...]

// Get latest version number
latest, err := log.LatestVersion(ctx)   // -1 if none
```

### DeltaTable Operations

```go
// internal/storage/delta/table.go
table := delta.NewDeltaTable(s3Client, "salesforce", "Account", logPrefix, logger)

// Initialize (idempotent)
table.Initialize(ctx, schemaString)

// Commit new actions
version, err := table.Commit(ctx, []Action{addAction1, addAction2}, "WRITE")

// Get snapshot at latest version
snap, err := table.Snapshot(ctx, -1)

// Time-travel to specific version
snap, err := table.Snapshot(ctx, 5)
```

### S3Store Interface

The delta package depends on this interface rather than the concrete S3 client, enabling testability:

```go
// internal/storage/delta/log.go
type S3Store interface {
    Upload(ctx context.Context, key string, data []byte, contentType string) error
    Download(ctx context.Context, key string) ([]byte, error)
    List(ctx context.Context, prefix string) ([]string, error)
    PutJSON(ctx context.Context, key string, v any) error
    GetJSON(ctx context.Context, key string, v any) error
    Exists(ctx context.Context, key string) (bool, error)
}
```

Tests use an in-memory `mockS3Store` (see `internal/storage/delta/log_test.go`).

## Limitations vs. Full Delta Lake

Our implementation covers the append-heavy data ingestion use case. It does not include:

- **Concurrent writers** - Single writer assumed. No conflict resolution or optimistic concurrency.
- **MERGE / UPDATE / DELETE DML** - Append-only with logical removes for full resyncs.
- **Partition pruning** - No partitioning support (all data in root directory).
- **Z-ordering / data skipping** - Stats are stored but not used for query optimization.
- **Parquet checkpoints** - Checkpoints use JSON instead of Parquet for simplicity.
- **Column mapping** - Not implemented.
- **Deletion vectors** - Not implemented.

These could be added incrementally if needed for downstream query engines.
