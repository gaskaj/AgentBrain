# Connector Development Guide

This guide covers how to add a new data source connector to AgentBrain.

## Overview

Connectors are the integration layer between AgentBrain and external SaaS platforms. Each connector implements the `connector.Connector` interface and is registered with the connector registry at startup.

The connector is responsible for:
- Authenticating with the source system
- Discovering available objects and their schemas
- Extracting data (full snapshots and incremental changes)
- Streaming results via Go channels

The sync engine handles everything else: planning, writing Parquet files, managing the Delta log, and persisting state.

## The Connector Interface

Defined in `internal/connector/connector.go`:

```go
type Connector interface {
    // Name returns the connector identifier (e.g., "salesforce", "sap").
    Name() string

    // Connect establishes a connection and authenticates with the source.
    Connect(ctx context.Context) error

    // Close releases any resources.
    Close() error

    // DiscoverMetadata returns metadata for all available objects.
    DiscoverMetadata(ctx context.Context) ([]ObjectMetadata, error)

    // DescribeObject returns detailed metadata including field schema.
    DescribeObject(ctx context.Context, objectName string) (*ObjectMetadata, error)

    // GetIncrementalChanges streams records changed since the given watermark.
    GetIncrementalChanges(ctx context.Context, objectName string,
        watermarkField string, since time.Time) (<-chan RecordBatch, <-chan error)

    // GetFullSnapshot streams all records for a full sync.
    GetFullSnapshot(ctx context.Context, objectName string) (<-chan RecordBatch, <-chan error)
}
```

## Supporting Types

### ObjectMetadata

```go
type ObjectMetadata struct {
    Name           string         `json:"name"`            // API name (e.g., "Account")
    Label          string         `json:"label"`           // Human-readable name
    Queryable      bool           `json:"queryable"`       // Can be queried
    Retrievable    bool           `json:"retrievable"`     // Can retrieve individual records
    Replicateable  bool           `json:"replicateable"`   // Supports incremental replication
    RecordCount    int64          `json:"recordCount"`     // Approximate row count (optional)
    WatermarkField string         `json:"watermarkField"`  // Timestamp field for incremental
    Schema         *schema.Schema `json:"schema"`          // Field definitions
}
```

The `WatermarkField` is critical - it tells the sync engine which timestamp column to use for incremental queries (e.g., `SystemModstamp`, `updated_at`, `modified_date`).

### RecordBatch

```go
type RecordBatch struct {
    Records []map[string]any  // Records as key-value maps
    Object  string            // Object name these records belong to
}
```

Records are untyped maps. The Parquet writer uses the schema to coerce types at write time. String values are the safest default for connector output.

### Schema

```go
// From internal/schema/schema.go
type Schema struct {
    ObjectName string
    Fields     []Field
    Version    int
    Hash       string   // Computed automatically
}

type Field struct {
    Name     string
    Type     FieldType   // string, integer, long, double, boolean, date, datetime, binary
    Nullable bool
}
```

## Step-by-Step Implementation

### 1. Create the Package

```
internal/connector/myservice/
├── types.go       # API response structs
├── client.go      # HTTP client with auth, retries
├── metadata.go    # Object/field discovery
└── connector.go   # Connector interface implementation + Register()
```

### 2. Define API Types (`types.go`)

Map the external API's JSON responses to Go structs:

```go
package myservice

type AuthResponse struct {
    AccessToken string `json:"access_token"`
    ExpiresIn   int    `json:"expires_in"`
}

type ObjectListResponse struct {
    Objects []ObjectInfo `json:"objects"`
}

type ObjectInfo struct {
    Name   string      `json:"name"`
    Fields []FieldInfo `json:"fields"`
}

type FieldInfo struct {
    Name     string `json:"name"`
    Type     string `json:"type"`
    Required bool   `json:"required"`
}
```

### 3. Build the HTTP Client (`client.go`)

Handle authentication, retries, and rate limiting:

```go
package myservice

import (
    "context"
    "net/http"
    "time"
)

type Client struct {
    httpClient  *http.Client
    baseURL     string
    accessToken string
}

func NewClient(baseURL string) *Client {
    return &Client{
        httpClient: &http.Client{Timeout: 2 * time.Minute},
        baseURL:    baseURL,
    }
}

func (c *Client) Authenticate(ctx context.Context, apiKey string) error {
    // POST to auth endpoint, store token
}

func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
    // Authenticated GET with retry logic
    // Retry on 429 (rate limit) and 5xx (server error)
    // Exponential backoff: 1s, 2s, 4s
}
```

**Retry pattern from the Salesforce client:**

```go
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        delay := retryBaseDelay * time.Duration(1<<(attempt-1))
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(delay):
        }
    }
    // Make request...
    if resp.StatusCode == 429 || resp.StatusCode >= 500 {
        continue  // Retry
    }
}
```

### 4. Implement Metadata Discovery (`metadata.go`)

```go
func (c *Client) ListObjects(ctx context.Context) ([]connector.ObjectMetadata, error) {
    // Call API to list available objects
    // Filter to queryable objects
    // Set WatermarkField appropriately
}

func (c *Client) DescribeObject(ctx context.Context, name string) (*connector.ObjectMetadata, error) {
    // Call API to get field-level metadata
    // Map API types to schema.FieldType
    // Return ObjectMetadata with populated Schema
}
```

**Type mapping example** (from Salesforce):

```go
func mapFieldType(apiType string) schema.FieldType {
    switch apiType {
    case "string", "text", "email", "url":
        return schema.FieldTypeString
    case "integer", "int32":
        return schema.FieldTypeInt
    case "long", "int64":
        return schema.FieldTypeLong
    case "decimal", "float", "double", "currency":
        return schema.FieldTypeDouble
    case "boolean":
        return schema.FieldTypeBoolean
    case "date":
        return schema.FieldTypeDate
    case "datetime", "timestamp":
        return schema.FieldTypeDatetime
    default:
        return schema.FieldTypeString  // Safe fallback
    }
}
```

### 5. Implement the Connector (`connector.go`)

```go
package myservice

import (
    "context"
    "log/slog"
    "time"

    "github.com/agentbrain/agentbrain/internal/config"
    "github.com/agentbrain/agentbrain/internal/connector"
)

type MyConnector struct {
    client    *Client
    cfg       *config.SourceConfig
    logger    *slog.Logger
    batchSize int
}

func NewConnector(cfg *config.SourceConfig) (connector.Connector, error) {
    logger := slog.Default().With("connector", "myservice")
    client := NewClient(cfg.Auth["base_url"])

    return &MyConnector{
        client:    client,
        cfg:       cfg,
        logger:    logger,
        batchSize: cfg.BatchSize,
    }, nil
}

func (c *MyConnector) Name() string { return "myservice" }

func (c *MyConnector) Connect(ctx context.Context) error {
    return c.client.Authenticate(ctx, c.cfg.Auth["api_key"])
}

func (c *MyConnector) Close() error { return nil }

func (c *MyConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
    return c.client.ListObjects(ctx)
}

func (c *MyConnector) DescribeObject(ctx context.Context, name string) (*connector.ObjectMetadata, error) {
    return c.client.DescribeObject(ctx, name)
}
```

### 6. Implement Streaming Extraction

This is the most important part. Use goroutines and channels:

```go
func (c *MyConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
    records := make(chan connector.RecordBatch, 4)  // Buffer a few batches
    errs := make(chan error, 1)

    go func() {
        defer close(records)
        defer close(errs)

        var cursor string
        for {
            // Fetch a page of results
            page, nextCursor, err := c.client.FetchPage(ctx, objectName, cursor)
            if err != nil {
                errs <- err
                return
            }

            if len(page) > 0 {
                select {
                case records <- connector.RecordBatch{
                    Records: page,
                    Object:  objectName,
                }:
                case <-ctx.Done():
                    errs <- ctx.Err()
                    return
                }
            }

            if nextCursor == "" {
                return  // No more pages
            }
            cursor = nextCursor
        }
    }()

    return records, errs
}

func (c *MyConnector) GetIncrementalChanges(ctx context.Context, objectName, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
    // Same pattern, but add a filter: WHERE watermarkField > since
    records := make(chan connector.RecordBatch, 4)
    errs := make(chan error, 1)

    go func() {
        defer close(records)
        defer close(errs)
        // Fetch with timestamp filter...
    }()

    return records, errs
}
```

**Critical rules for streaming:**

1. Always `defer close(records)` and `defer close(errs)` in the goroutine
2. Check `ctx.Done()` in the select when sending to channels
3. Send at most one error to `errs` then return
4. Buffer the records channel (e.g., capacity 4) to avoid blocking the producer

### 7. Register the Connector

```go
func Register(registry *connector.Registry) {
    registry.Register("myservice", func(cfg *config.SourceConfig) (connector.Connector, error) {
        return NewConnector(cfg)
    })
}
```

Then in `cmd/agentbrain/main.go`:

```go
import "github.com/agentbrain/agentbrain/internal/connector/myservice"

func main() {
    // ...
    registry := connector.NewRegistry()
    salesforce.Register(registry)
    myservice.Register(registry)  // Add this
    // ...
}
```

### 8. Add Configuration

```yaml
sources:
  my_source:
    type: myservice
    enabled: true
    schedule: "@every 2h"
    concurrency: 4
    batch_size: 5000
    objects:
      - Users
      - Orders
    auth:
      base_url: "https://api.myservice.com"
      api_key: "${MY_SERVICE_API_KEY}"
```

## Testing Your Connector

### Unit Tests

Mock the HTTP layer:

```go
func TestMyConnector_DiscoverMetadata(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(ObjectListResponse{
            Objects: []ObjectInfo{{Name: "Users"}},
        })
    }))
    defer server.Close()

    cfg := &config.SourceConfig{
        Auth: map[string]string{"base_url": server.URL},
    }
    conn, err := NewConnector(cfg)
    require.NoError(t, err)

    objects, err := conn.DiscoverMetadata(context.Background())
    require.NoError(t, err)
    assert.Len(t, objects, 1)
}
```

### Integration Tests

Test against a real or sandbox environment:

```go
//go:build integration

func TestMyConnector_FullSync(t *testing.T) {
    cfg := &config.SourceConfig{
        Auth: map[string]string{
            "base_url": os.Getenv("MY_SERVICE_URL"),
            "api_key":  os.Getenv("MY_SERVICE_KEY"),
        },
        BatchSize: 100,
    }
    // ...
}
```

## Reference: Salesforce Connector

The Salesforce connector in `internal/connector/salesforce/` is the reference implementation:

| File | Purpose |
|------|---------|
| `types.go` | 10 struct types mapping Salesforce API responses |
| `client.go` | OAuth2 password flow, GET/POST/GetStream with 3x exponential retry |
| `metadata.go` | DescribeGlobal (list objects), DescribeObject (field-level schema) |
| `rest.go` | REST SOQL queries with automatic pagination via `nextRecordsUrl` |
| `bulk.go` | Bulk API 2.0: create job → poll until complete → stream CSV results |
| `connector.go` | Wires everything together, implements Connector interface |

Key patterns to reuse:
- Exponential backoff retry on transient errors
- CSV streaming for large result sets (Bulk API)
- JSON pagination for small result sets (REST API)
- Watermark-based SOQL generation for incremental queries
