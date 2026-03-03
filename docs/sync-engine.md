# Sync Engine

The sync engine (`internal/sync/`) is the core orchestrator that coordinates data extraction, transformation, and storage. It ties together connectors, the storage layer, and the Delta Lake protocol.

## Components

```
internal/sync/
├── engine.go    # Engine: orchestrates the full sync lifecycle
├── planner.go   # Planner: decides full vs incremental vs skip per object
└── state.go     # StateStore: persists watermarks and schema state to S3
```

## Engine

The `Engine` struct orchestrates one sync cycle for a single source.

### Construction

```go
engine := sync.NewEngine(
    connector,    // connector.Connector implementation
    s3Client,     // *storage.S3Client
    "salesforce", // source name
    4,            // concurrency (parallel objects)
    []string{},   // allowed objects (empty = all)
    logger,       // *slog.Logger
)
```

### Run() Lifecycle

`Engine.Run(ctx)` executes a complete sync cycle:

```
1. Connect
   └── connector.Connect(ctx) — authenticate with source

2. Load State
   └── stateStore.Load(ctx, source) — read sync_state.json from S3
       └── Returns empty state if first run

3. Discover Metadata
   └── connector.DiscoverMetadata(ctx) — list all objects
   └── Save catalog to metadata/{source}/catalog.json

4. Plan Each Object
   └── For each discovered object:
       ├── connector.DescribeObject(ctx, name) — get schema
       └── planner.Plan(metadata, state, allowList) — determine mode

5. Execute Plans (parallel)
   └── For each plan where mode != Skip:
       ├── Initialize Delta table (if first time)
       ├── Extract: connector.Get{Full,Incremental}()
       │   └── Receives <-chan RecordBatch
       ├── Write: parquetWriter.WriteRecords() per batch
       │   └── Uploads Parquet file to S3
       ├── Commit: deltaTable.Commit() with Add actions
       ├── Update ObjectState with new watermark
       ├── Save schema version (if changed)
       └── Maybe create checkpoint

6. Save State
   └── stateStore.Save(ctx, state) — write updated sync_state.json
```

### Concurrency

Objects sync in parallel, bounded by a semaphore:

```go
sem := make(chan struct{}, e.concurrency)

for _, plan := range plans {
    go func(p *ObjectPlan) {
        sem <- struct{}{}        // Acquire
        defer func() { <-sem }() // Release

        e.syncObject(ctx, state, p)
    }(plan)
}
```

A `sync.Mutex` protects the shared `SyncState` map when parallel goroutines update their object states.

### Error Handling

- Individual object failures are logged but don't stop other objects
- The engine collects all errors and reports the total count
- Watermarks advance only after successful Delta commit (at-least-once)
- Non-critical failures (catalog save, checkpoint) are logged as warnings

## Planner

The `Planner` determines the sync strategy for each object.

### Decision Logic

```
Input: ObjectMetadata (from connector) + ObjectState (from prior run) + AllowList

┌─────────────────────────────────────────────────┐
│ Is object in allow list?                         │
│   No  → SKIP ("not in allowed objects list")     │
│   Yes → continue                                 │
├─────────────────────────────────────────────────┤
│ Does prior ObjectState exist?                    │
│   No  → FULL ("no prior sync state")             │
│   Yes → continue                                 │
├─────────────────────────────────────────────────┤
│ Has schema hash changed?                         │
│   No  → INCREMENTAL ("incremental from wmark")   │
│   Yes → continue                                 │
├─────────────────────────────────────────────────┤
│ Is previous schema available for diffing?        │
│   No  → FULL ("schema changed, no prior schema") │
│   Yes → continue                                 │
├─────────────────────────────────────────────────┤
│ Is the schema change breaking?                   │
│   Yes → FULL ("breaking schema change detected")  │
│   No  → INCREMENTAL (additive change is safe)     │
└─────────────────────────────────────────────────┘
```

### Sync Modes

| Mode | Description | When |
|------|-------------|------|
| `Full` | Extract all records | First sync, breaking schema change, no prior schema for diffing |
| `Incremental` | Extract records changed since watermark | Has prior state, no breaking changes |
| `Skip` | Do nothing | Not in allow list |

### Schema Change Detection

Schema changes are detected by comparing deterministic hashes:

1. Connector returns `ObjectMetadata.Schema` with current fields
2. Planner compares `schema.ComputeHash()` against `ObjectState.SchemaHash`
3. If hashes differ:
   - If `ObjectState.PreviousSchema` is available: run `schema.Diff()` to classify change
   - If not: treat as breaking (safe default)
4. `schema.Diff()` categorizes changes:
   - **Additive** (new columns only): incremental sync continues
   - **Breaking** (removed columns or type changes): full resync

## State Store

The `StateStore` manages sync state persistence in S3.

### SyncState Structure

```json
{
  "source": "salesforce",
  "objects": {
    "Account": {
      "lastSyncTime": "2024-03-03T12:00:00Z",
      "watermarkField": "SystemModstamp",
      "watermarkValue": "2024-03-03T12:00:00Z",
      "schemaHash": "a1b2c3d4e5f6g7h8",
      "schemaVersion": 2,
      "previousSchema": { ... },
      "deltaVersion": 5,
      "totalRecords": 50000,
      "lastSyncRecords": 1200,
      "syncType": "incremental"
    },
    "Contact": { ... }
  },
  "lastRunAt": "2024-03-03T12:05:00Z",
  "runCount": 42
}
```

### S3 Locations

| Key | Content |
|-----|---------|
| `state/{source}/sync_state.json` | Full sync state with all object watermarks |
| `state/{source}/schema_history/{object}/v{n}.json` | Versioned schema snapshots |
| `metadata/{source}/catalog.json` | Last discovered object catalog |

### First-Run Behavior

If `sync_state.json` does not exist, `StateStore.Load()` returns a new empty state:

```go
&SyncState{
    Source:  source,
    Objects: make(map[string]ObjectState),
}
```

This triggers full syncs for all objects (the planner sees no prior state).

### State Update Guarantees

- Watermarks advance **only after successful Delta commit**
- If a sync fails mid-way, the watermark stays at its previous value
- Next run will re-extract the same records (at-least-once semantics)
- State is saved once at the end of the full run, not per-object

## Integration with Delta Lake

Each synced object gets its own Delta table at `data/{source}/{object}/`.

### Table Initialization

On first sync of an object:

```go
table := delta.NewDeltaTable(s3, source, object, logPrefix, logger)
table.Initialize(ctx, schema.ToDeltaSchemaString())
```

This creates version 0 with `protocol` and `metaData` actions.

### Data Commits

Each sync batch produces:
1. Parquet file uploaded to `data/{source}/{object}/part-00000-{uuid}.snappy.parquet`
2. `Add` action recording the file path, size, and record count
3. All `Add` actions committed as a new Delta version

### Checkpoints

After commit, `CheckpointManager.MaybeCheckpoint()` creates a checkpoint if the new version is a multiple of 10.
