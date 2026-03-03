# Contributing to AgentBrain

## Development Setup

### Prerequisites

- Go 1.22 or later
- [golangci-lint](https://golangci-lint.run/welcome/install/) for linting
- Docker (optional, for LocalStack integration testing and container builds)
- AWS CLI (optional, for manual S3 inspection)

### Clone and Build

```bash
git clone https://github.com/agentbrain/agentbrain.git
cd agentbrain
make build
```

### Run Tests

```bash
make test          # Unit tests with -race
make test-cover    # Coverage report → coverage.html
make lint          # Static analysis
```

### Local Development with LocalStack

For local S3 testing without AWS credentials:

```bash
docker run -d -p 4566:4566 localstack/localstack
```

Then configure your `configs/agentbrain.yaml`:

```yaml
storage:
  bucket: test-bucket
  region: us-east-1
  endpoint: "http://localhost:4566"
```

Create the bucket:

```bash
aws --endpoint-url=http://localhost:4566 s3 mb s3://test-bucket
```

Run a single sync:

```bash
./bin/agentbrain --config configs/agentbrain.yaml --once
```

## Project Structure

```
AgentBrain/
├── cmd/agentbrain/main.go          # Entry point
├── internal/
│   ├── config/                     # YAML config loading
│   ├── connector/                  # Connector interface + registry
│   │   └── salesforce/             # Salesforce implementation
│   ├── storage/                    # S3, Parquet, layout
│   │   └── delta/                  # Delta Lake protocol
│   ├── schema/                     # Schema types, evolution, Parquet mapping
│   ├── sync/                       # Engine, planner, state
│   ├── scheduler/                  # Cron scheduling
│   └── observability/              # Logging, health endpoints
├── configs/                        # Configuration files
├── docs/                           # Documentation
├── Makefile
├── Dockerfile
└── go.mod
```

All application code is in `internal/` to keep the API surface private. The only public entry point is `cmd/agentbrain/main.go`.

## Code Style

### Go Conventions

- Follow standard Go conventions: `gofmt`, `go vet`, effective Go
- Use `context.Context` as the first parameter for all I/O operations
- Wrap errors with `fmt.Errorf("descriptive context: %w", err)`
- Use `log/slog` for structured logging (not `log` or `fmt.Printf`)
- Export types and functions that need to be used across packages; keep everything else unexported
- Write doc comments on all exported identifiers

### Naming

- Packages use lowercase single words: `config`, `schema`, `delta`
- Interfaces describe behavior: `Connector`, `S3Store`
- Constructor functions follow `NewX` pattern: `NewEngine`, `NewClient`
- Test files are `*_test.go` in the same package

### Error Handling

```go
// Wrap errors with context
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something for %s: %w", name, err)
}

// Check specific error types when needed
var nf *types.NotFound
if errors.As(err, &nf) {
    return nil // not found is ok
}
```

### Concurrency

- Thread sync via goroutines + channels (for streaming data) or `sync.Mutex` (for shared state)
- Semaphore pattern for bounded parallelism:
  ```go
  sem := make(chan struct{}, concurrency)
  sem <- struct{}{}        // acquire
  defer func() { <-sem }() // release
  ```
- All shared state access protected by mutex (`sync.RWMutex` preferred)

## Architecture

### Core Abstractions

**Connector Interface** (`internal/connector/connector.go`)

The central abstraction. Every data source implements this interface:

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

Data flows through channels (`<-chan RecordBatch`) so large datasets stream without buffering the entire result set in memory.

**Delta Lake Protocol** (`internal/storage/delta/`)

Custom Go implementation of a subset of the Delta Lake protocol spec. No mature Go Delta library exists, so we implement:

- Protocol v1 (minReaderVersion=1, minWriterVersion=2)
- Actions: `protocol`, `metaData`, `add`, `remove`, `commitInfo`
- Transaction log: newline-delimited JSON, zero-padded 20-digit version numbers
- Time-travel: replay log to any version for historical snapshots
- Checkpoints: periodic snapshots every 10 versions

**Schema Evolution** (`internal/schema/`)

Schemas are hashed deterministically (sorted field names + types). On each sync:

- Hash compared to stored version
- Additive changes (new columns): safe for incremental sync
- Breaking changes (removed columns, type changes): triggers full resync
- Schema versions stored in S3 for audit trail

### Data Flow

```
Scheduler → Engine.Run()
  → Connect (OAuth)
  → Load sync state from S3
  → Discover available objects
  → Plan each object (full/incremental/skip)
  → Extract in parallel (channel-based streaming)
  → Write Parquet files to S3
  → Commit Delta log entries
  → Update sync state with new watermarks
```

### S3 Storage Layout

All S3 key paths are centralized in `storage.Layout`:

```
data/{source}/{object}/                    Parquet data files + Delta log
state/{source}/sync_state.json             Watermarks and sync metadata
state/{source}/schema_history/{object}/    Versioned schemas
metadata/{source}/catalog.json             Discovered object catalog
```

## Adding a New Connector

This is the primary extension point. To add a new SaaS source (e.g., SAP, HubSpot):

### 1. Create the Package

```
internal/connector/{name}/
├── types.go       # API response structs
├── client.go      # HTTP client, auth, retries
├── metadata.go    # Object/field discovery
├── connector.go   # Implements connector.Connector
└── ...            # Additional files as needed
```

### 2. Implement the Interface

```go
package myconnector

import (
    "github.com/agentbrain/agentbrain/internal/config"
    "github.com/agentbrain/agentbrain/internal/connector"
)

type MyConnector struct {
    // ...
}

func NewConnector(cfg *config.SourceConfig) (connector.Connector, error) {
    // Build from cfg.Auth and cfg.Options
}

func (c *MyConnector) Name() string { return "myconnector" }

func (c *MyConnector) Connect(ctx context.Context) error {
    // Authenticate with the service
}

func (c *MyConnector) Close() error { return nil }

func (c *MyConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
    // Return all available objects with their metadata
}

func (c *MyConnector) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
    // Return detailed metadata including schema (fields, types)
}

func (c *MyConnector) GetIncrementalChanges(ctx context.Context, objectName, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
    records := make(chan connector.RecordBatch, 4)
    errs := make(chan error, 1)
    go func() {
        defer close(records)
        defer close(errs)
        // Fetch changed records since watermark, send as batches
    }()
    return records, errs
}

func (c *MyConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
    // Same pattern, fetch all records
}

func Register(registry *connector.Registry) {
    registry.Register("myconnector", func(cfg *config.SourceConfig) (connector.Connector, error) {
        return NewConnector(cfg)
    })
}
```

### 3. Register in main.go

```go
import "github.com/agentbrain/agentbrain/internal/connector/myconnector"

// In main():
myconnector.Register(registry)
```

### 4. Add Config

```yaml
sources:
  my_source:
    type: myconnector
    enabled: true
    auth:
      api_key: "${MY_API_KEY}"
```

### Key Requirements for Connectors

- **Streaming:** Use channels for `GetIncrementalChanges` and `GetFullSnapshot`. Send `RecordBatch` chunks rather than buffering everything.
- **Schema discovery:** `DescribeObject` must return `ObjectMetadata` with a populated `Schema` field. Map source types to `schema.FieldType` constants.
- **Watermark field:** Set `ObjectMetadata.WatermarkField` to the timestamp field used for incremental queries (e.g., `SystemModstamp`, `updated_at`).
- **Error handling:** Return errors through the error channel. The engine handles retries at the object level.
- **Context:** Respect context cancellation throughout. Check `ctx.Done()` in long-running loops.

## Writing Tests

### Unit Tests

Place test files next to the code they test (`*_test.go` in the same package).

Use the in-memory `mockS3Store` pattern from `delta/log_test.go` for testing storage-dependent code:

```go
type mockS3Store struct {
    mu   sync.Mutex
    data map[string][]byte
}

func newMockS3Store() *mockS3Store {
    return &mockS3Store{data: make(map[string][]byte)}
}

// Implement all S3Store interface methods...
```

Use `testify/assert` and `testify/require`:

```go
func TestMyFeature(t *testing.T) {
    result, err := doSomething()
    require.NoError(t, err)            // Fail immediately if error
    assert.Equal(t, expected, result)   // Report failure but continue
}
```

### Integration Tests

Tag integration tests with `//go:build integration`:

```go
//go:build integration

package mypackage_test

func TestIntegration_FullSync(t *testing.T) {
    // Requires LocalStack or real S3
}
```

Run with: `make test-integration`

### Test Coverage

```bash
make test-cover
open coverage.html
```

## Commit Messages

Use conventional commit style:

```
feat: add HubSpot connector with OAuth2 support
fix: handle null values in Salesforce bulk CSV parsing
refactor: extract S3 path logic into Layout type
test: add schema evolution tests for type changes
docs: update connector guide with SAP example
```

## Pull Request Checklist

- [ ] Code compiles: `make build`
- [ ] Tests pass: `make test`
- [ ] No lint issues: `make lint`
- [ ] New code has tests
- [ ] Exported identifiers have doc comments
- [ ] Errors are wrapped with context
- [ ] Context is threaded through I/O operations
- [ ] No secrets in committed code (use `${ENV_VAR}` in config)
- [ ] Documentation updated if behavior changes

## Dependencies

Add new dependencies deliberately. The current stack:

| Package | Purpose |
|---------|---------|
| `aws-sdk-go-v2` | S3 operations |
| `parquet-go/parquet-go` | Parquet file writing |
| `robfig/cron/v3` | Cron scheduling |
| `gopkg.in/yaml.v3` | Config parsing |
| `google/uuid` | UUID generation |
| `stretchr/testify` | Test assertions |

Avoid adding dependencies for functionality that can be implemented in < 50 lines of Go. Prefer standard library where practical.
