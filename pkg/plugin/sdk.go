package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
)

// SDK provides helper functions and interfaces for plugin development
type SDK struct {
	logger *slog.Logger
}

// NewSDK creates a new plugin SDK instance
func NewSDK(logger *slog.Logger) *SDK {
	return &SDK{
		logger: logger,
	}
}

// PluginInterface defines the interface that plugins must implement
type PluginInterface interface {
	// GetMetadata returns plugin metadata
	GetMetadata() *PluginMetadata
	
	// Initialize initializes the plugin
	Initialize(config map[string]interface{}) error
	
	// CreateConnector creates a new connector instance
	CreateConnector(cfg *config.SourceConfig) (connector.Connector, error)
	
	// Validate validates plugin configuration
	Validate(config map[string]interface{}) error
	
	// Shutdown gracefully shuts down the plugin
	Shutdown() error
}

// PluginMetadata contains information about a plugin
type PluginMetadata struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	License     string            `json:"license"`
	Homepage    string            `json:"homepage"`
	Tags        []string          `json:"tags"`
	Capabilities []string         `json:"capabilities"`
	Requirements *Requirements    `json:"requirements"`
	Config       *ConfigSchema    `json:"config"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// Requirements defines plugin requirements
type Requirements struct {
	MinAgentBrainVersion string            `json:"min_agentbrain_version"`
	MaxAgentBrainVersion string            `json:"max_agentbrain_version,omitempty"`
	GoVersion            string            `json:"go_version"`
	Dependencies         []Dependency      `json:"dependencies"`
	Resources            *ResourceRequirements `json:"resources"`
	Permissions          []Permission      `json:"permissions"`
}

// Dependency represents a plugin dependency
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"` // "go-module", "system-package", "plugin"
}

// ResourceRequirements defines resource requirements for the plugin
type ResourceRequirements struct {
	MinMemoryMB int     `json:"min_memory_mb"`
	MaxMemoryMB int     `json:"max_memory_mb"`
	MinCPUCores float64 `json:"min_cpu_cores"`
	DiskSpaceMB int     `json:"disk_space_mb"`
	NetworkAccess bool  `json:"network_access"`
	FileSystemAccess []string `json:"filesystem_access"`
}

// Permission represents a required permission
type Permission struct {
	Type        string   `json:"type"`
	Resource    string   `json:"resource"`
	Actions     []string `json:"actions"`
	Description string   `json:"description"`
}

// ConfigSchema defines the configuration schema for the plugin
type ConfigSchema struct {
	Type        string                    `json:"type"`
	Properties  map[string]*PropertySchema `json:"properties"`
	Required    []string                  `json:"required"`
	Description string                    `json:"description"`
}

// PropertySchema defines a configuration property schema
type PropertySchema struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
	Required    bool        `json:"required"`
	Format      string      `json:"format,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
	Minimum     *float64    `json:"minimum,omitempty"`
	Maximum     *float64    `json:"maximum,omitempty"`
	Items       *PropertySchema `json:"items,omitempty"`
	Enum        []interface{} `json:"enum,omitempty"`
}

// BaseConnector provides a base implementation for plugin connectors
type BaseConnector struct {
	name     string
	metadata *PluginMetadata
	config   *config.SourceConfig
	logger   *slog.Logger
	sdk      *SDK
}

// NewBaseConnector creates a new base connector
func NewBaseConnector(name string, metadata *PluginMetadata, cfg *config.SourceConfig, logger *slog.Logger) *BaseConnector {
	return &BaseConnector{
		name:     name,
		metadata: metadata,
		config:   cfg,
		logger:   logger,
		sdk:      NewSDK(logger),
	}
}

// Name returns the connector name
func (bc *BaseConnector) Name() string {
	return bc.name
}

// ValidateConfig provides a base implementation for configuration validation
func (bc *BaseConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	if bc.metadata == nil || bc.metadata.Config == nil {
		return nil // No validation schema available
	}

	// Combine auth and options for validation against the unified schema
	combined := make(map[string]interface{})
	for k, v := range auth {
		combined[k] = v
	}
	for k, v := range options {
		combined[k] = v
	}

	// Validate combined configuration
	if err := bc.validateProperties(combined, bc.metadata.Config.Properties, "config"); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

// ConfigSchema returns the configuration schema
func (bc *BaseConnector) ConfigSchema() map[string]interface{} {
	if bc.metadata == nil || bc.metadata.Config == nil {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		}
	}

	return map[string]interface{}{
		"type":        bc.metadata.Config.Type,
		"properties":  bc.metadata.Config.Properties,
		"required":    bc.metadata.Config.Required,
		"description": bc.metadata.Config.Description,
	}
}

// GetLogger returns the logger instance
func (bc *BaseConnector) GetLogger() *slog.Logger {
	return bc.logger
}

// GetConfig returns the source configuration
func (bc *BaseConnector) GetConfig() *config.SourceConfig {
	return bc.config
}

// GetSDK returns the SDK instance
func (bc *BaseConnector) GetSDK() *SDK {
	return bc.sdk
}

// validateProperties validates configuration properties against schema
func (bc *BaseConnector) validateProperties(data map[string]interface{}, schema map[string]*PropertySchema, context string) error {
	if schema == nil {
		return nil
	}
	
	for key, propSchema := range schema {
		value, exists := data[key]
		
		// Check required properties
		if propSchema.Required && !exists {
			return fmt.Errorf("required property %s.%s is missing", context, key)
		}
		
		if !exists {
			continue
		}

		// Validate property type and constraints
		if err := bc.validateProperty(key, value, propSchema); err != nil {
			return fmt.Errorf("property %s.%s: %w", context, key, err)
		}
	}

	return nil
}

// validateProperty validates a single property against its schema
func (bc *BaseConnector) validateProperty(key string, value interface{}, schema *PropertySchema) error {
	// Type validation
	switch schema.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		
		strValue := value.(string)
		
		// Pattern validation
		if schema.Pattern != "" {
			// In a full implementation, you would use regexp here
			// For now, we'll skip pattern validation
		}
		
		// Enum validation
		if len(schema.Enum) > 0 {
			found := false
			for _, enumValue := range schema.Enum {
				if strValue == enumValue {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("value must be one of %v", schema.Enum)
			}
		}

	case "number", "integer":
		var numValue float64
		switch v := value.(type) {
		case float64:
			numValue = v
		case float32:
			numValue = float64(v)
		case int:
			numValue = float64(v)
		case int64:
			numValue = float64(v)
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
		
		// Range validation
		if schema.Minimum != nil && numValue < *schema.Minimum {
			return fmt.Errorf("value must be >= %v", *schema.Minimum)
		}
		if schema.Maximum != nil && numValue > *schema.Maximum {
			return fmt.Errorf("value must be <= %v", *schema.Maximum)
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}

	case "array":
		slice, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
		
		// Validate array items if schema is provided
		if schema.Items != nil {
			for i, item := range slice {
				if err := bc.validateProperty(fmt.Sprintf("%s[%d]", key, i), item, schema.Items); err != nil {
					return err
				}
			}
		}

	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	}

	return nil
}

// TestHelper provides utilities for testing plugins
type TestHelper struct {
	sdk    *SDK
	logger *slog.Logger
}

// NewTestHelper creates a new test helper
func NewTestHelper() *TestHelper {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &TestHelper{
		sdk:    NewSDK(logger),
		logger: logger,
	}
}

// CreateMockConfig creates a mock source configuration for testing
func (th *TestHelper) CreateMockConfig(connectorType string, auth, options map[string]string) *config.SourceConfig {
	return &config.SourceConfig{
		Type:        connectorType,
		Enabled:     true,
		Concurrency: 1,
		BatchSize:   100,
		Objects:     []string{"test_object"},
		Auth:        auth,
		Options:     options,
	}
}

// CreateMockMetadata creates mock plugin metadata for testing
func (th *TestHelper) CreateMockMetadata(name, version string) *PluginMetadata {
	return &PluginMetadata{
		Name:        name,
		Version:     version,
		Description: "Mock plugin for testing",
		Author:      "Test Author",
		License:     "MIT",
		Tags:        []string{"test", "mock"},
		Capabilities: []string{"connector"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Requirements: &Requirements{
			MinAgentBrainVersion: "1.0.0",
			GoVersion:           "1.21",
			Resources: &ResourceRequirements{
				MinMemoryMB:   64,
				MaxMemoryMB:   512,
				MinCPUCores:   0.5,
				NetworkAccess: true,
			},
		},
		Config: &ConfigSchema{
			Type: "object",
			Properties: map[string]*PropertySchema{
				"api_key": {
					Type:        "string",
					Description: "API key for authentication",
					Required:    true,
				},
				"timeout": {
					Type:        "integer",
					Description: "Request timeout in seconds",
					Default:     30,
					Minimum:     &[]float64{1}[0],
					Maximum:     &[]float64{300}[0],
				},
			},
			Required:    []string{"api_key"},
			Description: "Configuration for mock connector",
		},
	}
}

// ValidatePlugin validates that a plugin implementation is correct
func (th *TestHelper) ValidatePlugin(plugin PluginInterface) error {
	// Test metadata
	metadata := plugin.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("plugin metadata is nil")
	}
	
	if metadata.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	
	if metadata.Version == "" {
		return fmt.Errorf("plugin version is required")
	}

	// Test initialization
	testConfig := map[string]interface{}{
		"test": "value",
	}
	
	if err := plugin.Initialize(testConfig); err != nil {
		return fmt.Errorf("plugin initialization failed: %w", err)
	}

	// Test configuration validation
	if err := plugin.Validate(testConfig); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	// Test connector creation
	mockConfig := th.CreateMockConfig("test", 
		map[string]string{"api_key": "test"}, 
		map[string]string{})
	
	conn, err := plugin.CreateConnector(mockConfig)
	if err != nil {
		return fmt.Errorf("connector creation failed: %w", err)
	}
	
	if conn == nil {
		return fmt.Errorf("connector is nil")
	}

	// Test shutdown
	if err := plugin.Shutdown(); err != nil {
		return fmt.Errorf("plugin shutdown failed: %w", err)
	}

	if th.logger != nil {
		th.logger.Info("Plugin validation successful", "name", metadata.Name)
	}
	return nil
}

// BenchmarkHelper provides utilities for benchmarking plugins
type BenchmarkHelper struct {
	sdk    *SDK
	logger *slog.Logger
}

// NewBenchmarkHelper creates a new benchmark helper
func NewBenchmarkHelper() *BenchmarkHelper {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &BenchmarkHelper{
		sdk:    NewSDK(logger),
		logger: logger,
	}
}

// BenchmarkConnector runs performance benchmarks on a connector
func (bh *BenchmarkHelper) BenchmarkConnector(connector connector.Connector, objectName string, recordCount int) (*BenchmarkResult, error) {
	ctx := context.Background()
	
	result := &BenchmarkResult{
		ConnectorName: connector.Name(),
		ObjectName:    objectName,
		RecordCount:   recordCount,
		StartTime:     time.Now(),
	}

	// Benchmark connection
	connectStart := time.Now()
	if err := connector.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	result.ConnectTime = time.Since(connectStart)

	// Benchmark metadata discovery
	metadataStart := time.Now()
	_, err := connector.DiscoverMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("metadata discovery failed: %w", err)
	}
	result.MetadataTime = time.Since(metadataStart)

	// Benchmark full snapshot
	snapshotStart := time.Now()
	recordsCh, errCh := connector.GetFullSnapshot(ctx, objectName)
	
	var recordsReceived int
	for {
		select {
		case batch, ok := <-recordsCh:
			if !ok {
				goto snapshotDone
			}
			recordsReceived += len(batch.Records)
		case err, ok := <-errCh:
			if !ok {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("snapshot failed: %w", err)
			}
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("snapshot timeout")
		}
	}
	
snapshotDone:
	result.SnapshotTime = time.Since(snapshotStart)
	result.RecordsReceived = recordsReceived
	
	// Calculate throughput
	if result.SnapshotTime > 0 {
		result.RecordsPerSecond = float64(recordsReceived) / result.SnapshotTime.Seconds()
	}

	// Clean up
	connector.Close()
	
	result.EndTime = time.Now()
	result.TotalTime = result.EndTime.Sub(result.StartTime)

	bh.logger.Info("Benchmark completed", 
		"connector", connector.Name(),
		"object", objectName,
		"records", recordsReceived,
		"duration", result.TotalTime,
		"records_per_sec", result.RecordsPerSecond)

	return result, nil
}

// BenchmarkResult contains benchmark results
type BenchmarkResult struct {
	ConnectorName    string        `json:"connector_name"`
	ObjectName       string        `json:"object_name"`
	RecordCount      int           `json:"record_count"`
	RecordsReceived  int           `json:"records_received"`
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	TotalTime        time.Duration `json:"total_time"`
	ConnectTime      time.Duration `json:"connect_time"`
	MetadataTime     time.Duration `json:"metadata_time"`
	SnapshotTime     time.Duration `json:"snapshot_time"`
	RecordsPerSecond float64       `json:"records_per_second"`
}