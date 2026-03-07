# Architecture

## Overview

AgentBrain is a long-running Go daemon that periodically syncs data from enterprise SaaS platforms into S3. Data is stored as Parquet files managed by a custom Delta Lake transaction log, enabling versioned, schemaless, time-travel-capable storage.

## System Diagram

```
                     ┌──────────────────────────────────────────────────────┐
                     │                   AgentBrain Process                 │
                     │                                                      │
                     │  ┌─────────┐    ┌───────────────────────────────┐   │
   SIGINT/SIGTERM ──>│  │ main.go │───>│  Scheduler (robfig/cron)      │   │
                     │  └────┬────┘    │  - Per-source cron jobs       │   │
                     │       │         │  - @every or cron expressions │   │
                     │       │         └───────────────┬───────────────┘   │
                     │       │                         │                    │
                     │  ┌────┴──────────────┐  ┌──────┴──────────────┐    │
                     │  │ Health Server     │  │  Sync Engine (x N)  │    │
                     │  │ /healthz /readyz  │  │  One per source     │    │
                     │  └───────────────────┘  └──────┬──────────────┘    │
                     │                                │                    │
                     └────────────────────────────────┼────────────────────┘
                                                      │
                     ┌────────────────────────────────┼────────────────────┐
                     │          Sync Engine.Run()      │                    │
                     │                                │                    │
                     │  1. Connect ──── Connector.Connect() (OAuth)        │
                     │  2. Load ─────── StateStore.Load() (S3)             │
                     │  3. Discover ─── Connector.DiscoverMetadata()       │
                     │  4. Plan ─────── Planner.Plan() per object          │
                     │  5. Extract ──── Connector.Get{Full,Incremental}()  │
                     │  6. Write ────── ParquetWriter.WriteRecords() (S3)  │
                     │  7. Commit ───── DeltaTable.Commit() (S3)           │
                     │  8. Save ─────── StateStore.Save() (S3)             │
                     │                                                      │
                     │  Steps 5-7 run in parallel per object               │
                     │  (bounded by concurrency semaphore)                  │
                     └──────────────────────────────────────────────────────┘
                                          │
                     ┌────────────────────┼─────────────────────┐
                     │                    ▼                     │
                     │              S3 Bucket                   │
                     │                                          │
                     │  data/{source}/{object}/                 │
                     │    ├── _delta_log/                       │
                     │    │   ├── 00000000000000000000.json     │
                     │    │   └── ...                           │
                     │    └── part-00000-{uuid}.snappy.parquet  │
                     │                                          │
                     │  state/{source}/sync_state.json          │
                     │  state/{source}/schema_history/...       │
                     │  metadata/{source}/catalog.json          │
                     └──────────────────────────────────────────┘
```

## Package Dependency Graph

```
cmd/agentbrain/main.go
├── internal/config
├── internal/connector
│   └── internal/connector/salesforce
│       ├── internal/connector (interface)
│       └── internal/schema
├── internal/observability
├── internal/scheduler
├── internal/storage
│   ├── internal/schema
│   └── internal/storage/delta
└── internal/sync
    ├── internal/connector (interface)
    ├── internal/schema
    ├── internal/storage
    └── internal/storage/delta
```

Dependencies flow inward. `internal/connector` defines the interface; `internal/connector/salesforce` implements it. `internal/sync` orchestrates everything. `internal/storage` handles persistence. `internal/schema` is shared for schema representation.

## Operating Modes

### Daemon Mode (default)

```bash
./bin/agentbrain --config configs/agentbrain.yaml
```

- Starts health check HTTP server
- Registers cron jobs per source
- Runs until SIGINT/SIGTERM
- Graceful shutdown: stops scheduler, waits for in-flight syncs, shuts down health server

### Single-Run Mode

```bash
./bin/agentbrain --config configs/agentbrain.yaml --once
```

- Executes one sync cycle per enabled source
- Exits with code 0 on success, 1 on failure
- Useful for batch jobs, testing, and CI/CD

## Concurrency Model

- Each source gets its own `sync.Engine`
- Within an engine, objects sync concurrently up to `concurrency` limit
- Concurrency is controlled via a semaphore (buffered channel)
- A `sync.Mutex` protects shared state updates during parallel object syncs
- The scheduler can run multiple sources on overlapping schedules

## Error Handling Strategy

- **Object-level isolation:** A failed object does not block other objects
- **At-least-once semantics:** Watermarks advance only after successful commit
- **Retry with backoff:** HTTP client retries on 429/5xx (3 attempts, exponential backoff)
- **Graceful degradation:** Individual failures are logged and counted; the engine reports the total failure count
- **Context propagation:** All operations accept `context.Context` for cancellation

## Configuration

YAML-based with environment variable substitution (`${VAR:-default}`). Three levels:

1. **Agent** - Global settings (log level, health port, default schedule, timeout)
2. **Storage** - S3 bucket, region, endpoint, prefix
3. **Sources** - Per-source connector type, auth, schedule, objects, concurrency

Defaults are applied for optional fields. Validation ensures required fields are present.

## Storage Architecture

### Parquet Files

- Written via `parquet-go/parquet-go`
- Snappy compression
- Dynamic schemas derived from connector metadata at runtime
- Files named `part-00000-{uuid}.snappy.parquet`
- One file per RecordBatch (typically 10,000 records)

### Delta Lake Transaction Log

Custom implementation of the Delta protocol spec with enhanced checkpoint management:

- **Version files:** `_delta_log/{version:020d}.json` (newline-delimited JSON)
- **Action types:** `protocol`, `metaData`, `add`, `remove`, `commitInfo`
- **Time-travel:** Replay log from version 0 to reconstruct state at any point
- **Enhanced checkpoints:** Comprehensive checkpoint lifecycle management

#### Enhanced Checkpoint Architecture

```
┌─────────────────────────────────────────────────┐
│             CheckpointManager                   │
├─────────────────┬───────────────────────────────┤
│ Configuration   │  Lifecycle Management         │
│ • Frequency     │  • Adaptive scheduling        │
│ • Retention     │  • Validation & recovery      │
│ • Thresholds    │  • Cleanup & compaction       │
├─────────────────┼───────────────────────────────┤
│ Metrics         │  CheckpointValidator          │
│ • Health score  │  • Integrity verification     │
│ • Performance   │  • Corruption detection       │
│ • Storage saved │  • Fallback mechanisms        │
└─────────────────┴───────────────────────────────┘
```

The enhanced checkpoint system provides:
- **Adaptive frequency** based on data volume and performance
- **Validation and recovery** from corrupted checkpoints
- **Automatic cleanup** with configurable retention policies
- **File compaction** during checkpoint creation
- **Performance monitoring** and health scoring

### Sync State

- `sync_state.json`: Watermarks, schema hashes, delta versions, run counters
- `schema_history/`: Versioned schema snapshots for audit and diffing
- `catalog.json`: Last discovered object catalog from the connector

## Schema Evolution

The system tracks schemas over time and reacts to changes:

| Change Type | Examples | Action |
|------------|---------|--------|
| None | No field changes | Incremental sync |
| Additive | New columns added | Incremental sync (new columns nullable in Parquet) |
| Breaking | Column removed, type changed | Full resync |

Schema hashing is deterministic and order-independent (sorted field name+type pairs, SHA256).
