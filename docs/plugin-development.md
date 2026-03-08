# Plugin Development Guide

This guide provides step-by-step instructions for developing connector plugins for AgentBrain. Plugins enable extending AgentBrain with custom connectors without modifying the core codebase.

## Prerequisites

- Go 1.21 or later
- AgentBrain development environment
- Basic understanding of the connector interface
- Familiarity with the target SaaS API

## Quick Start

### 1. Generate Plugin Template

Use the plugin-builder tool to generate a basic plugin structure:

```bash
# Install plugin-builder
go install github.com/agentbrain/agentbrain/cmd/plugin-builder@latest

# Generate basic template
plugin-builder -command=template -name=myconnector -template=basic

# This creates:
# myconnector/
# ├── main.go          # Main plugin implementation
# ├── go.mod           # Go module definition
# └── README.md        # Basic documentation
```

### 2. Implement Your Connector

Edit the generated `main.go` file to implement your connector logic:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/agentbrain/agentbrain/internal/config"
    "github.com/agentbrain/agentbrain/internal/connector"
    "github.com/agentbrain/agentbrain/pkg/plugin"
)

// PluginMetadata is required for all plugins
var PluginMetadata = map[string]interface{}{
    "name":        "myconnector",
    "version":     "1.0.0",
    "description": "Custom connector for My SaaS Platform",
    "author":      "Your Name",
    "license":     "MIT",
    "capabilities": []string{"connector"},
}

type MyConnector struct {
    *plugin.BaseConnector
    apiClient *MyAPIClient
}

// NewConnector creates a new connector instance (required export)
func NewConnector(cfg *config.SourceConfig) (connector.Connector, error) {
    metadata := &plugin.PluginMetadata{
        Name:        "myconnector",
        Version:     "1.0.0",
        Description: "Custom connector for My SaaS Platform",
    }

    base := plugin.NewBaseConnector("myconnector", metadata, cfg, nil)
    
    return &MyConnector{
        BaseConnector: base,
    }, nil
}

// Implement connector interface methods...
```

### 3. Build and Test

```bash
# Build the plugin
plugin-builder -command=build -path=./myconnector

# Validate the plugin
plugin-builder -command=validate -path=./myconnector

# Test the plugin
cd myconnector && go test ./...
```

### 4. Deploy and Use

```bash
# Package the plugin
plugin-builder -command=package -path=./myconnector

# Copy to AgentBrain plugins directory
sudo cp dist/package/myconnector.so /etc/agentbrain/plugins/
sudo cp dist/package/myconnector.json /etc/agentbrain/plugins/

# Configure in AgentBrain
# Edit your AgentBrain config to use the plugin
```

## Detailed Development

### Plugin Structure

A complete plugin consists of several components:

```
myconnector/
├── main.go              # Main plugin implementation
├── client.go            # API client implementation
├── config.go            # Configuration handling
├── metadata.go          # Object metadata handling
├── sync.go              # Data synchronization logic
├── main_test.go         # Unit tests
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── plugin.json          # Plugin metadata
└── README.md            # Documentation
```

### Plugin Interface Implementation

Your plugin must implement the `connector.Connector` interface:

```go
type Connector interface {
    Name() string
    Connect(ctx context.Context) error
    Close() error
    DiscoverMetadata(ctx context.Context) ([]ObjectMetadata, error)
    DescribeObject(ctx context.Context, objectName string) (*ObjectMetadata, error)
    GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan RecordBatch, <-chan error)
    GetFullSnapshot(ctx context.Context, objectName string) (<-chan RecordBatch, <-chan error)
    ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error
    ConfigSchema() map[string]interface{}
}
```

### Example Implementation

#### 1. Connection Management

```go
func (c *MyConnector) Connect(ctx context.Context) error {
    // Get configuration
    cfg := c.GetConfig()
    
    // Create API client
    client := &MyAPIClient{
        APIKey: cfg.Auth["api_key"],
        BaseURL: cfg.Options["base_url"],
        Timeout: time.Duration(30) * time.Second,
    }
    
    // Test connection
    if err := client.TestConnection(ctx); err != nil {
        return fmt.Errorf("connection test failed: %w", err)
    }
    
    c.apiClient = client
    c.GetLogger().Info("Connected successfully", "connector", c.Name())
    
    return nil
}
```

#### 2. Metadata Discovery

```go
func (c *MyConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
    if c.apiClient == nil {
        return nil, fmt.Errorf("not connected")
    }
    
    // Discover available objects
    objects, err := c.apiClient.ListObjects(ctx)
    if err != nil {
        return nil, fmt.Errorf("list objects: %w", err)
    }
    
    var metadata []connector.ObjectMetadata
    for _, obj := range objects {
        meta := connector.ObjectMetadata{
            Name:           obj.Name,
            Label:          obj.DisplayName,
            Queryable:      obj.Queryable,
            Retrievable:    obj.Retrievable,
            Replicateable:  obj.Replicateable,
            WatermarkField: obj.TimestampField,
        }
        
        // Get schema for object
        schema, err := c.buildObjectSchema(ctx, obj.Name)
        if err != nil {
            c.GetLogger().Warn("Failed to build schema", "object", obj.Name, "error", err)
            continue
        }
        meta.Schema = schema
        
        metadata = append(metadata, meta)
    }
    
    return metadata, nil
}
```

#### 3. Data Synchronization

```go
func (c *MyConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
    recordsCh := make(chan connector.RecordBatch)
    errCh := make(chan error)
    
    go func() {
        defer close(recordsCh)
        defer close(errCh)
        
        // Get configuration
        cfg := c.GetConfig()
        batchSize := cfg.BatchSize
        if batchSize <= 0 {
            batchSize = 1000
        }
        
        // Start data retrieval
        offset := 0
        for {
            // Fetch batch of records
            records, hasMore, err := c.apiClient.GetRecords(ctx, objectName, offset, batchSize)
            if err != nil {
                errCh <- fmt.Errorf("get records: %w", err)
                return
            }
            
            if len(records) == 0 {
                break
            }
            
            // Send batch
            batch := connector.RecordBatch{
                Records: records,
                Object:  objectName,
            }
            
            select {
            case recordsCh <- batch:
            case <-ctx.Done():
                return
            }
            
            if !hasMore {
                break
            }
            
            offset += batchSize
        }
    }()
    
    return recordsCh, errCh
}
```

#### 4. Configuration Validation

```go
func (c *MyConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
    // Validate required authentication parameters
    apiKey, ok := auth["api_key"].(string)
    if !ok || apiKey == "" {
        return fmt.Errorf("api_key is required in auth configuration")
    }
    
    // Validate optional parameters
    if baseURL, ok := options["base_url"].(string); ok {
        if !strings.HasPrefix(baseURL, "https://") {
            return fmt.Errorf("base_url must use HTTPS")
        }
    }
    
    // Validate timeout
    if timeoutStr, ok := options["timeout"].(string); ok {
        if _, err := time.ParseDuration(timeoutStr); err != nil {
            return fmt.Errorf("invalid timeout format: %w", err)
        }
    }
    
    return nil
}

func (c *MyConnector) ConfigSchema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "auth": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "api_key": map[string]interface{}{
                        "type":        "string",
                        "description": "API key for authentication",
                        "required":    true,
                    },
                },
                "required": []string{"api_key"},
            },
            "options": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "base_url": map[string]interface{}{
                        "type":        "string",
                        "description": "Base URL for API calls",
                        "format":      "uri",
                        "default":     "https://api.example.com",
                    },
                    "timeout": map[string]interface{}{
                        "type":        "string",
                        "description": "Request timeout (e.g., '30s', '1m')",
                        "default":     "30s",
                    },
                },
            },
        },
    }
}
```

### Testing Your Plugin

#### Unit Tests

Create comprehensive unit tests for your plugin:

```go
package main

import (
    "context"
    "testing"
    "github.com/agentbrain/agentbrain/pkg/plugin"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyConnector_Connect(t *testing.T) {
    helper := plugin.NewTestHelper()
    
    cfg := helper.CreateMockConfig("myconnector", 
        map[string]string{"api_key": "test-key"}, 
        map[string]string{"base_url": "https://api.test.com"})
    
    connector, err := NewConnector(cfg)
    require.NoError(t, err)
    require.NotNil(t, connector)
    
    // Test connection (you may need to mock the API client)
    ctx := context.Background()
    err = connector.Connect(ctx)
    // Add appropriate assertions based on your implementation
}

func TestMyConnector_ValidateConfig(t *testing.T) {
    cfg := &config.SourceConfig{Type: "myconnector"}
    connector, err := NewConnector(cfg)
    require.NoError(t, err)
    
    // Test valid configuration
    auth := map[string]interface{}{"api_key": "test-key"}
    options := map[string]interface{}{"base_url": "https://api.test.com"}
    
    err = connector.ValidateConfig(auth, options)
    assert.NoError(t, err)
    
    // Test invalid configuration
    invalidAuth := map[string]interface{}{}
    err = connector.ValidateConfig(invalidAuth, options)
    assert.Error(t, err)
}
```

#### Integration Tests

Test your plugin against real or mock API endpoints:

```go
//go:build integration

func TestMyConnector_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    // Set up test environment
    cfg := &config.SourceConfig{
        Type: "myconnector",
        Auth: map[string]string{
            "api_key": os.Getenv("TEST_API_KEY"),
        },
        Options: map[string]string{
            "base_url": "https://api-sandbox.example.com",
        },
    }
    
    connector, err := NewConnector(cfg)
    require.NoError(t, err)
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Test connection
    err = connector.Connect(ctx)
    require.NoError(t, err)
    defer connector.Close()
    
    // Test metadata discovery
    metadata, err := connector.DiscoverMetadata(ctx)
    require.NoError(t, err)
    assert.NotEmpty(t, metadata)
    
    // Test data retrieval
    if len(metadata) > 0 {
        recordsCh, errCh := connector.GetFullSnapshot(ctx, metadata[0].Name)
        
        recordCount := 0
        for {
            select {
            case batch, ok := <-recordsCh:
                if !ok {
                    goto done
                }
                recordCount += len(batch.Records)
                assert.Equal(t, metadata[0].Name, batch.Object)
            case err, ok := <-errCh:
                if !ok {
                    continue
                }
                if err != nil {
                    t.Errorf("Sync error: %v", err)
                    return
                }
            case <-ctx.Done():
                t.Error("Test timeout")
                return
            }
        }
        
        done:
        t.Logf("Retrieved %d records from %s", recordCount, metadata[0].Name)
    }
}
```

### Plugin Metadata

Create a comprehensive `plugin.json` file:

```json
{
  "name": "myconnector",
  "version": "1.0.0",
  "description": "Custom connector for My SaaS Platform",
  "author": "Your Name <your.email@company.com>",
  "license": "MIT",
  "homepage": "https://github.com/yourorg/myconnector-plugin",
  "tags": ["connector", "saas", "api"],
  "capabilities": ["connector", "incremental-sync", "full-sync"],
  "requirements": {
    "min_agentbrain_version": "1.0.0",
    "go_version": "1.21",
    "dependencies": [
      {
        "name": "github.com/yourorg/api-client",
        "version": "v2.1.0",
        "type": "go-module"
      }
    ],
    "resources": {
      "min_memory_mb": 64,
      "max_memory_mb": 256,
      "min_cpu_cores": 0.5,
      "disk_space_mb": 10,
      "network_access": true,
      "filesystem_access": ["/tmp/myconnector-cache"]
    },
    "permissions": [
      {
        "type": "network",
        "resource": "api.example.com",
        "actions": ["read"],
        "description": "Access to My SaaS API"
      }
    ]
  },
  "config": {
    "type": "object",
    "properties": {
      "api_key": {
        "type": "string",
        "description": "API key for authentication",
        "required": true
      },
      "base_url": {
        "type": "string",
        "description": "Base URL for API calls",
        "format": "uri",
        "default": "https://api.example.com"
      }
    },
    "required": ["api_key"]
  }
}
```

### Error Handling Best Practices

#### Graceful Error Handling

```go
func (c *MyConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
    recordsCh := make(chan connector.RecordBatch)
    errCh := make(chan error, 1) // Buffered to prevent goroutine leak
    
    go func() {
        defer close(recordsCh)
        defer close(errCh)
        
        // Use defer for cleanup
        defer func() {
            if r := recover(); r != nil {
                errCh <- fmt.Errorf("panic in sync: %v", r)
            }
        }()
        
        // Implement retry logic
        retryCount := 0
        maxRetries := 3
        
        for {
            err := c.syncData(ctx, objectName, recordsCh)
            if err == nil {
                return // Success
            }
            
            // Check for retryable errors
            if !isRetryable(err) {
                errCh <- err
                return
            }
            
            retryCount++
            if retryCount >= maxRetries {
                errCh <- fmt.Errorf("max retries exceeded: %w", err)
                return
            }
            
            // Exponential backoff
            backoff := time.Duration(retryCount) * time.Second
            select {
            case <-time.After(backoff):
                continue
            case <-ctx.Done():
                errCh <- ctx.Err()
                return
            }
        }
    }()
    
    return recordsCh, errCh
}
```

#### Rate Limiting

```go
type RateLimiter struct {
    requests chan struct{}
    interval time.Duration
}

func NewRateLimiter(requestsPerSecond int) *RateLimiter {
    rl := &RateLimiter{
        requests: make(chan struct{}, requestsPerSecond),
        interval: time.Second / time.Duration(requestsPerSecond),
    }
    
    // Fill initial bucket
    for i := 0; i < requestsPerSecond; i++ {
        rl.requests <- struct{}{}
    }
    
    // Refill ticker
    go func() {
        ticker := time.NewTicker(rl.interval)
        defer ticker.Stop()
        
        for range ticker.C {
            select {
            case rl.requests <- struct{}{}:
            default:
                // Bucket full, skip
            }
        }
    }()
    
    return rl
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
    select {
    case <-rl.requests:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Performance Optimization

#### Concurrent Processing

```go
func (c *MyConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
    recordsCh := make(chan connector.RecordBatch)
    errCh := make(chan error)
    
    go func() {
        defer close(recordsCh)
        defer close(errCh)
        
        // Use worker pool for concurrent processing
        workerCount := c.GetConfig().Concurrency
        if workerCount <= 0 {
            workerCount = 4
        }
        
        jobsCh := make(chan SyncJob, workerCount*2)
        resultsCh := make(chan SyncResult, workerCount*2)
        
        // Start workers
        for i := 0; i < workerCount; i++ {
            go c.syncWorker(ctx, jobsCh, resultsCh)
        }
        
        // Start job producer
        go c.produceJobs(ctx, objectName, jobsCh)
        
        // Process results
        c.processResults(ctx, resultsCh, recordsCh, errCh)
    }()
    
    return recordsCh, errCh
}
```

#### Memory Management

```go
func (c *MyConnector) processLargeDataset(ctx context.Context, objectName string) error {
    // Use streaming to avoid loading entire dataset into memory
    stream, err := c.apiClient.StreamRecords(ctx, objectName)
    if err != nil {
        return err
    }
    defer stream.Close()
    
    // Process records in batches
    batch := make([]map[string]any, 0, c.GetConfig().BatchSize)
    
    for {
        record, err := stream.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        batch = append(batch, record)
        
        if len(batch) >= c.GetConfig().BatchSize {
            if err := c.processBatch(ctx, batch); err != nil {
                return err
            }
            // Clear batch to free memory
            batch = batch[:0]
        }
    }
    
    // Process remaining records
    if len(batch) > 0 {
        return c.processBatch(ctx, batch)
    }
    
    return nil
}
```

### Deployment and Distribution

#### Building for Distribution

```bash
# Build for multiple architectures
GOOS=linux GOARCH=amd64 plugin-builder -command=build -path=./myconnector
GOOS=linux GOARCH=arm64 plugin-builder -command=build -path=./myconnector

# Create distribution packages
plugin-builder -command=package -path=./myconnector
```

#### Version Management

Use semantic versioning and maintain backward compatibility:

```go
const (
    PluginVersion = "1.2.0"
    MinAgentBrainVersion = "1.0.0"
    MaxAgentBrainVersion = "2.0.0"
)

func checkCompatibility(agentBrainVersion string) error {
    // Implement version compatibility checking
    return nil
}
```

### Debugging and Troubleshooting

#### Logging

```go
func (c *MyConnector) Connect(ctx context.Context) error {
    logger := c.GetLogger()
    logger.Info("Starting connection", "connector", c.Name())
    
    startTime := time.Now()
    defer func() {
        logger.Info("Connection completed", 
            "connector", c.Name(),
            "duration", time.Since(startTime))
    }()
    
    // Connection logic with detailed logging
    logger.Debug("Creating API client", "base_url", c.config.BaseURL)
    
    client, err := c.createAPIClient()
    if err != nil {
        logger.Error("Failed to create API client", "error", err)
        return fmt.Errorf("create API client: %w", err)
    }
    
    logger.Debug("Testing connection")
    if err := client.TestConnection(ctx); err != nil {
        logger.Error("Connection test failed", "error", err)
        return fmt.Errorf("connection test: %w", err)
    }
    
    c.apiClient = client
    logger.Info("Connected successfully")
    return nil
}
```

#### Metrics and Monitoring

```go
type ConnectorMetrics struct {
    RequestCount     int64
    ErrorCount       int64
    AverageLatency   time.Duration
    RecordsProcessed int64
}

func (c *MyConnector) recordMetrics(operation string, duration time.Duration, err error) {
    // Record operation metrics
    c.metrics.RequestCount++
    
    if err != nil {
        c.metrics.ErrorCount++
    }
    
    // Update average latency
    c.updateLatency(duration)
}
```

## Advanced Topics

### Custom Authentication

Implement custom authentication methods:

```go
type OAuthAuthenticator struct {
    clientID     string
    clientSecret string
    tokenURL     string
    accessToken  string
    refreshToken string
    expiresAt    time.Time
}

func (a *OAuthAuthenticator) GetAccessToken(ctx context.Context) (string, error) {
    if time.Now().Before(a.expiresAt) {
        return a.accessToken, nil
    }
    
    // Refresh token
    return a.refreshAccessToken(ctx)
}
```

### Schema Evolution

Handle schema changes gracefully:

```go
func (c *MyConnector) handleSchemaEvolution(oldSchema, newSchema *schema.Schema) error {
    // Compare schemas and handle changes
    changes := schema.Compare(oldSchema, newSchema)
    
    for _, change := range changes {
        switch change.Type {
        case schema.FieldAdded:
            c.GetLogger().Info("New field detected", "field", change.Field)
        case schema.FieldRemoved:
            c.GetLogger().Warn("Field removed", "field", change.Field)
        case schema.FieldTypeChanged:
            return fmt.Errorf("breaking change: field %s type changed", change.Field)
        }
    }
    
    return nil
}
```

### Plugin Communication

Implement plugin-to-plugin communication:

```go
type PluginMessage struct {
    Type    string      `json:"type"`
    Payload interface{} `json:"payload"`
}

func (c *MyConnector) sendMessage(target string, message PluginMessage) error {
    // Implement inter-plugin messaging
    return c.pluginManager.SendMessage(target, message)
}
```

## Best Practices Summary

1. **Error Handling**: Implement comprehensive error handling with retries and graceful degradation
2. **Resource Management**: Properly manage connections, memory, and other resources
3. **Testing**: Include unit tests, integration tests, and performance tests
4. **Logging**: Use structured logging with appropriate log levels
5. **Configuration**: Validate configuration and provide clear error messages
6. **Performance**: Implement efficient data processing and handle large datasets
7. **Security**: Validate inputs and handle authentication securely
8. **Documentation**: Provide clear documentation and examples
9. **Versioning**: Use semantic versioning and maintain compatibility
10. **Monitoring**: Include metrics and health checks for operational visibility

## Support and Resources

- **SDK Documentation**: See `pkg/plugin/` package documentation
- **Examples**: Check `examples/plugins/` for reference implementations
- **API Reference**: Full connector interface documentation
- **Community**: Join our Discord for plugin development support
- **Issues**: Report bugs and request features on GitHub