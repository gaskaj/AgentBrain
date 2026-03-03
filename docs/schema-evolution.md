# Schema Evolution

AgentBrain tracks schemas over time and automatically handles schema changes during sync operations.

## Schema Representation

Schemas are represented as a list of typed fields:

```go
type Schema struct {
    ObjectName string   // e.g., "Account"
    Fields     []Field  // Ordered field definitions
    Version    int      // Monotonically increasing version number
    Hash       string   // Deterministic hash for change detection
}

type Field struct {
    Name     string    // Field name (e.g., "Id", "Name", "Amount")
    Type     FieldType // One of: string, integer, long, double, boolean, date, datetime, binary
    Nullable bool      // Whether the field can be null
}
```

## Field Types

| FieldType | Go Type | Parquet Type | Description |
|-----------|---------|-------------|-------------|
| `string` | `string` | `STRING` (byte array) | Text data, default for unknown types |
| `integer` | `int32` | `INT32` | 32-bit integers |
| `long` | `int64` | `INT64` | 64-bit integers |
| `double` | `float64` | `DOUBLE` | Floating-point numbers |
| `boolean` | `bool` | `BOOLEAN` | True/false |
| `date` | `string` | `STRING` | Date stored as ISO string |
| `datetime` | `string` | `STRING` | Datetime stored as ISO string |
| `binary` | `[]byte` | `BYTE_ARRAY` | Binary data |

Date and datetime are stored as strings in Parquet for maximum compatibility across query engines.

## Schema Hashing

Schemas are hashed deterministically for change detection:

1. Extract `(name, type)` pairs for each field
2. Sort pairs by field name (alphabetical)
3. JSON-encode the sorted pairs
4. SHA256 hash, truncated to first 8 bytes (16 hex chars)

```go
func (s *Schema) ComputeHash() string {
    // Sort fields by name
    // Marshal to JSON
    // SHA256 → first 8 bytes → hex string
}
```

The hash is **order-independent**: fields `[Id, Name]` and `[Name, Id]` produce the same hash. The hash does **not** consider nullability changes (only field names and types).

## Change Detection

On each sync, the current schema from the connector is compared against the stored schema:

```go
// In planner.go
newHash := objectMeta.Schema.ComputeHash()
if newHash != objectState.SchemaHash {
    // Schema changed!
}
```

## Schema Diffing

When a change is detected and the previous schema is available, a detailed diff is computed:

```go
diff := schema.Diff(oldSchema, newSchema)
```

The diff categorizes changes into three types:

### No Change (`ChangeNone`)

Fields are identical. Incremental sync continues normally.

### Additive Change (`ChangeAdditive`)

New fields were added, but no fields were removed or changed type.

```
Old: [Id:string, Name:string]
New: [Id:string, Name:string, Email:string]

Result: ChangeAdditive
  AddedFields: [Email:string]
```

**Action:** Incremental sync continues. New columns appear as nullable in subsequent Parquet files.

### Breaking Change (`ChangeBreaking`)

Fields were removed or types changed. This means existing Parquet files have incompatible schemas.

```
Old: [Id:string, Name:string, Amount:string]
New: [Id:string, Name:string, Amount:double]

Result: ChangeBreaking
  TypeChanges: [{Amount: string → double}]
```

```
Old: [Id:string, Name:string, OldField:string]
New: [Id:string, Name:string]

Result: ChangeBreaking
  RemovedFields: [OldField:string]
```

**Action:** Full resync. All data is re-extracted and new Parquet files are written.

## Version Management

Schema versions are monotonically increasing integers, stored in the sync state:

```json
{
  "schemaHash": "a1b2c3d4e5f6g7h8",
  "schemaVersion": 3,
  "previousSchema": { "objectName": "Account", "fields": [...], "version": 3, "hash": "..." }
}
```

When a schema change is detected, the version increments and the new schema is saved to the history:

```
state/{source}/schema_history/{object}/v1.json
state/{source}/schema_history/{object}/v2.json
state/{source}/schema_history/{object}/v3.json
```

This provides an audit trail of all schema changes over time.

## Delta Lake Integration

When the schema changes, the Delta table's `metaData` action is updated with the new schema string:

```go
schemaStr := schema.ToDeltaSchemaString()
// Produces: {"type":"struct","fields":[{"name":"Id","type":"string","nullable":false,"metadata":{}},...]}
```

This is stored in the `schemaString` field of the Delta `metaData` action. Downstream query engines (Spark, Trino, etc.) read this to understand the table schema.

## Parquet Schema Mapping

Internal schemas are converted to `parquet-go` schemas for file writing:

```go
pqSchema := schema.ToParquetSchema(s)
```

The mapping:

| Internal Type | Parquet Node |
|--------------|-------------|
| `string` | `parquet.String()` |
| `integer` | `parquet.Int(32)` |
| `long` | `parquet.Int(64)` |
| `double` | `parquet.Leaf(parquet.DoubleType)` |
| `boolean` | `parquet.Leaf(parquet.BooleanType)` |
| `binary` | `parquet.Leaf(parquet.ByteArrayType)` |
| `date` | `parquet.String()` |
| `datetime` | `parquet.String()` |

Nullable fields are wrapped with `parquet.Optional()`.

## Edge Cases

### No Previous Schema for Diffing

If the sync state has a schema hash but no `PreviousSchema` (e.g., from an older state format), any hash change triggers a full resync as the safe default. The planner cannot determine if the change is additive or breaking without the old field list.

### First Sync

No schema comparison is done. The schema from the connector becomes version 1.

### Schema Unchanged

If the hash matches, the schema version and hash in the state remain the same. No schema history entry is written.

### Connector Schema Discovery Failure

If `DescribeObject` fails for a specific object, the planner logs a warning and skips that object. Other objects continue syncing.
