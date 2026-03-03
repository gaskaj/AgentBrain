# Testing Guide

## Running Tests

```bash
make test           # Unit tests with race detector
make test-cover     # Coverage report (opens coverage.html)
make test-integration  # Integration tests (requires LocalStack)
```

## Test Organization

Tests live alongside the code they test in `*_test.go` files:

```
internal/config/config_test.go           # Config loading, env vars, validation
internal/schema/schema_test.go           # Schema hashing, field operations
internal/schema/evolution_test.go        # Schema diff detection
internal/storage/delta/log_test.go       # Transaction log read/write
internal/storage/delta/table_test.go     # DeltaTable operations, time-travel
internal/sync/planner_test.go            # Sync planning decisions
```

## Test Inventory

### Config Tests (`config_test.go`)

| Test | Description |
|------|-------------|
| `TestLoad` | Full config loading from YAML with all fields |
| `TestLoad_EnvVarExpansion` | `${VAR}` replaced with environment variable value |
| `TestLoad_EnvVarDefaults` | `${VAR:-default}` uses default when var is unset |
| `TestLoad_ValidationErrors` | Missing bucket, region, sources, type all produce errors |

### Schema Tests (`schema_test.go`)

| Test | Description |
|------|-------------|
| `TestNewSchema` | Schema creation with computed hash |
| `TestComputeHash_OrderIndependent` | Same fields in different order produce same hash |
| `TestComputeHash_DifferentTypes` | Same field name but different type produces different hash |
| `TestFieldNames` | Returns sorted field name list |
| `TestToDeltaSchemaString` | Produces valid Delta schema JSON |

### Evolution Tests (`evolution_test.go`)

| Test | Description |
|------|-------------|
| `TestDiff_NoChanges` | Identical schemas produce ChangeNone |
| `TestDiff_AdditiveChange` | New field only produces ChangeAdditive |
| `TestDiff_BreakingRemoval` | Removed field produces ChangeBreaking |
| `TestDiff_BreakingTypeChange` | Changed field type produces ChangeBreaking |

### Delta Log Tests (`log_test.go`)

| Test | Description |
|------|-------------|
| `TestTransactionLog_WriteAndRead` | Write actions then read them back |
| `TestTransactionLog_ListVersions` | List 5 versions, verify sorted order |
| `TestTransactionLog_LatestVersion` | Returns -1 when empty, 0 after first write |

### Delta Table Tests (`table_test.go`)

| Test | Description |
|------|-------------|
| `TestDeltaTable_InitializeAndSnapshot` | Create table, verify v0 snapshot has protocol + metadata |
| `TestDeltaTable_CommitAndSnapshot` | Commit 2 add actions, verify snapshot shows both files |
| `TestDeltaTable_TimeTravel` | v1: add A, v2: add B + remove A. Verify v1 has A, v2 has B |
| `TestDeltaTable_ActiveFiles` | Commit 2 files, verify ActiveFiles() returns both |

### Planner Tests (`planner_test.go`)

| Test | Description |
|------|-------------|
| `TestPlanner_FirstRun` | No prior state → SyncModeFull |
| `TestPlanner_IncrementalSync` | Matching hash → SyncModeIncremental |
| `TestPlanner_BreakingSchemaChange` | Type change with previous schema → SyncModeFull |
| `TestPlanner_SchemaChangedNoPriorSchema` | Hash mismatch without old schema → SyncModeFull |
| `TestPlanner_SkipNotInAllowList` | Object not in allow list → SyncModeSkip |

## Mock S3 Store

The `mockS3Store` in `delta/log_test.go` provides an in-memory implementation of the `S3Store` interface:

```go
type mockS3Store struct {
    mu   sync.Mutex
    data map[string][]byte
}
```

It implements all 6 methods of the `S3Store` interface:
- `Upload` — stores bytes in map
- `Download` — retrieves bytes from map (error if not found)
- `List` — returns keys matching prefix
- `PutJSON` — marshals to JSON and stores
- `GetJSON` — retrieves and unmarshals JSON
- `Exists` — checks if key exists in map

This mock is thread-safe (mutex-protected) and can be reused for testing any package that depends on `S3Store`.

## Writing New Tests

### Unit Test Pattern

```go
func TestMyFeature(t *testing.T) {
    // Setup
    store := newMockS3Store()
    logger := slog.Default()

    // Execute
    result, err := myFunction(store, logger)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, expected, result)
}
```

### Table-Driven Tests

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid input", "good", false},
        {"empty input", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validate(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Testing Connectors

Mock the HTTP layer with `httptest.Server`:

```go
func TestConnector_DescribeObject(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Return mock API response
        json.NewEncoder(w).Encode(DescribeResult{
            Name:   "Account",
            Fields: []FieldDescribe{{Name: "Id", Type: "id"}},
        })
    }))
    defer server.Close()

    client := NewClient(AuthConfig{LoginURL: server.URL})
    // Test against mock server...
}
```

## Integration Testing

Integration tests are tagged and run separately:

```go
//go:build integration

package storage_test

import (
    "context"
    "testing"

    "github.com/agentbrain/agentbrain/internal/config"
    "github.com/agentbrain/agentbrain/internal/storage"
)

func TestS3Client_Integration(t *testing.T) {
    cfg := config.StorageConfig{
        Bucket:   "test-bucket",
        Region:   "us-east-1",
        Endpoint: "http://localhost:4566",
    }

    ctx := context.Background()
    client, err := storage.NewS3Client(ctx, cfg)
    require.NoError(t, err)

    // Test upload, download, list, delete...
}
```

### LocalStack Setup

```bash
docker run -d -p 4566:4566 localstack/localstack
aws --endpoint-url=http://localhost:4566 s3 mb s3://test-bucket
make test-integration
```

## Verification Checklist

After making changes:

1. `go build ./...` — compiles without errors
2. `go vet ./...` — no static analysis issues
3. `make test` — all tests pass with race detector
4. `make lint` — no lint warnings (if golangci-lint installed)
