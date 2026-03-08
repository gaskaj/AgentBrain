package plugin

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockPlugin is a mock implementation of PluginInterface
type MockPlugin struct {
	mock.Mock
}

func (m *MockPlugin) GetMetadata() *PluginMetadata {
	args := m.Called()
	return args.Get(0).(*PluginMetadata)
}

func (m *MockPlugin) Initialize(config map[string]interface{}) error {
	args := m.Called(config)
	return args.Error(0)
}

func (m *MockPlugin) CreateConnector(cfg *config.SourceConfig) (connector.Connector, error) {
	args := m.Called(cfg)
	return args.Get(0).(connector.Connector), args.Error(1)
}

func (m *MockPlugin) Validate(config map[string]interface{}) error {
	args := m.Called(config)
	return args.Error(0)
}

func (m *MockPlugin) Shutdown() error {
	args := m.Called()
	return args.Error(0)
}

// MockConnectorForSDK is a simple mock connector for SDK testing
type MockConnectorForSDK struct {
	mock.Mock
}

func (m *MockConnectorForSDK) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConnectorForSDK) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockConnectorForSDK) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConnectorForSDK) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
	args := m.Called(ctx)
	return args.Get(0).([]connector.ObjectMetadata), args.Error(1)
}

func (m *MockConnectorForSDK) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	args := m.Called(ctx, objectName)
	return args.Get(0).(*connector.ObjectMetadata), args.Error(1)
}

func (m *MockConnectorForSDK) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
	args := m.Called(ctx, objectName, watermarkField, since)
	return args.Get(0).(<-chan connector.RecordBatch), args.Get(1).(<-chan error)
}

func (m *MockConnectorForSDK) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	args := m.Called(ctx, objectName)
	return args.Get(0).(<-chan connector.RecordBatch), args.Get(1).(<-chan error)
}

func (m *MockConnectorForSDK) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	args := m.Called(auth, options)
	return args.Error(0)
}

func (m *MockConnectorForSDK) ConfigSchema() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func TestPluginMetadata(t *testing.T) {
	metadata := &PluginMetadata{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Description: "Test plugin",
		Author:      "Test Author",
		License:     "MIT",
		Tags:        []string{"test", "mock"},
		Capabilities: []string{"connector"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	assert.Equal(t, "test-plugin", metadata.Name)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "Test plugin", metadata.Description)
	assert.Contains(t, metadata.Tags, "test")
	assert.Contains(t, metadata.Capabilities, "connector")
}

func TestRequirements(t *testing.T) {
	requirements := &Requirements{
		MinAgentBrainVersion: "1.0.0",
		GoVersion:           "1.21",
		Dependencies: []Dependency{
			{Name: "test-dep", Version: "1.0.0", Type: "go-module"},
		},
		Resources: &ResourceRequirements{
			MinMemoryMB:   64,
			MaxMemoryMB:   512,
			NetworkAccess: true,
		},
		Permissions: []Permission{
			{Type: "network", Resource: "api.example.com", Actions: []string{"read"}},
		},
	}

	assert.Equal(t, "1.0.0", requirements.MinAgentBrainVersion)
	assert.Equal(t, "1.21", requirements.GoVersion)
	assert.Len(t, requirements.Dependencies, 1)
	assert.Equal(t, "test-dep", requirements.Dependencies[0].Name)
	assert.Equal(t, 64, requirements.Resources.MinMemoryMB)
	assert.Len(t, requirements.Permissions, 1)
}

func TestConfigSchema(t *testing.T) {
	minValue := 1.0
	maxValue := 100.0

	schema := &ConfigSchema{
		Type: "object",
		Properties: map[string]*PropertySchema{
			"api_key": {
				Type:        "string",
				Description: "API key",
				Required:    true,
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds",
				Minimum:     &minValue,
				Maximum:     &maxValue,
				Default:     30,
			},
		},
		Required:    []string{"api_key"},
		Description: "Test configuration schema",
	}

	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Properties, "api_key")
	assert.Contains(t, schema.Properties, "timeout")
	assert.Equal(t, "string", schema.Properties["api_key"].Type)
	assert.Equal(t, "number", schema.Properties["timeout"].Type)
	assert.Equal(t, &minValue, schema.Properties["timeout"].Minimum)
	assert.Equal(t, &maxValue, schema.Properties["timeout"].Maximum)
	assert.Contains(t, schema.Required, "api_key")
}

func TestBaseConnector_ValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	// The validation checks both auth and options against the same schema properties
	metadata := &PluginMetadata{
		Config: &ConfigSchema{
			Type: "object",
			Properties: map[string]*PropertySchema{
				"api_key": {
					Type:     "string",
					Required: true,
				},
				"timeout": {
					Type:    "number",
					Default: 30,
				},
			},
		},
	}

	cfg := &config.SourceConfig{Type: "test"}
	baseConnector := NewBaseConnector("test", metadata, cfg, logger)

	// Test valid configuration - api_key in auth, timeout in options
	auth := map[string]interface{}{
		"api_key": "test-key",
	}
	options := map[string]interface{}{
		"timeout": 60,
	}

	err := baseConnector.ValidateConfig(auth, options)
	assert.NoError(t, err)

	// Test missing required field - api_key missing from auth
	authMissing := map[string]interface{}{}
	err = baseConnector.ValidateConfig(authMissing, options)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required property")
}

func TestBaseConnector_ValidateProperty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	baseConnector := NewBaseConnector("test", nil, nil, logger)

	// Test string validation
	stringSchema := &PropertySchema{Type: "string"}
	err := baseConnector.validateProperty("test", "valid string", stringSchema)
	assert.NoError(t, err)

	err = baseConnector.validateProperty("test", 123, stringSchema)
	assert.Error(t, err)

	// Test number validation
	min := 1.0
	max := 100.0
	numberSchema := &PropertySchema{
		Type:    "number",
		Minimum: &min,
		Maximum: &max,
	}
	
	err = baseConnector.validateProperty("test", 50, numberSchema)
	assert.NoError(t, err)
	
	err = baseConnector.validateProperty("test", 0, numberSchema)
	assert.Error(t, err)
	
	err = baseConnector.validateProperty("test", 150, numberSchema)
	assert.Error(t, err)

	// Test boolean validation
	boolSchema := &PropertySchema{Type: "boolean"}
	err = baseConnector.validateProperty("test", true, boolSchema)
	assert.NoError(t, err)
	
	err = baseConnector.validateProperty("test", "not a bool", boolSchema)
	assert.Error(t, err)

	// Test array validation
	arraySchema := &PropertySchema{
		Type: "array",
		Items: &PropertySchema{Type: "string"},
	}
	
	err = baseConnector.validateProperty("test", []interface{}{"a", "b", "c"}, arraySchema)
	assert.NoError(t, err)
	
	err = baseConnector.validateProperty("test", []interface{}{"a", 123, "c"}, arraySchema)
	assert.Error(t, err)

	// Test enum validation
	enumSchema := &PropertySchema{
		Type: "string",
		Enum: []interface{}{"option1", "option2", "option3"},
	}
	
	err = baseConnector.validateProperty("test", "option1", enumSchema)
	assert.NoError(t, err)
	
	err = baseConnector.validateProperty("test", "invalid", enumSchema)
	assert.Error(t, err)
}

func TestBaseConnector_ConfigSchema(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	metadata := &PluginMetadata{
		Config: &ConfigSchema{
			Type: "object",
			Properties: map[string]*PropertySchema{
				"key": {Type: "string"},
			},
			Description: "Test schema",
		},
	}

	cfg := &config.SourceConfig{Type: "test"}
	baseConnector := NewBaseConnector("test", metadata, cfg, logger)

	schema := baseConnector.ConfigSchema()
	
	assert.Equal(t, "object", schema["type"])
	assert.Contains(t, schema, "properties")
	assert.Equal(t, "Test schema", schema["description"])
}

func TestTestHelper_CreateMockConfig(t *testing.T) {
	helper := NewTestHelper()

	auth := map[string]string{"api_key": "test"}
	options := map[string]string{"timeout": "30"}

	config := helper.CreateMockConfig("test-connector", auth, options)

	assert.Equal(t, "test-connector", config.Type)
	assert.True(t, config.Enabled)
	assert.Equal(t, 1, config.Concurrency)
	assert.Equal(t, 100, config.BatchSize)
	assert.Contains(t, config.Objects, "test_object")
	assert.Equal(t, "test", config.Auth["api_key"])
	assert.Equal(t, "30", config.Options["timeout"])
}

func TestTestHelper_CreateMockMetadata(t *testing.T) {
	helper := NewTestHelper()

	metadata := helper.CreateMockMetadata("test-plugin", "1.0.0")

	assert.Equal(t, "test-plugin", metadata.Name)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "Mock plugin for testing", metadata.Description)
	assert.Contains(t, metadata.Tags, "test")
	assert.Contains(t, metadata.Tags, "mock")
	assert.Contains(t, metadata.Capabilities, "connector")
	assert.NotNil(t, metadata.Requirements)
	assert.NotNil(t, metadata.Config)
}

func TestTestHelper_ValidatePlugin(t *testing.T) {
	helper := NewTestHelper()
	mockPlugin := &MockPlugin{}

	metadata := helper.CreateMockMetadata("test-plugin", "1.0.0")
	mockConnector := &MockConnectorForSDK{}

	// Set up mock expectations
	mockPlugin.On("GetMetadata").Return(metadata)
	mockPlugin.On("Initialize", mock.AnythingOfType("map[string]interface {}")).Return(nil)
	mockPlugin.On("Validate", mock.AnythingOfType("map[string]interface {}")).Return(nil)
	mockPlugin.On("CreateConnector", mock.AnythingOfType("*config.SourceConfig")).Return(mockConnector, nil)
	mockPlugin.On("Shutdown").Return(nil)

	err := helper.ValidatePlugin(mockPlugin)

	assert.NoError(t, err)
	mockPlugin.AssertExpectations(t)
}

func TestTestHelper_ValidatePlugin_InvalidMetadata(t *testing.T) {
	helper := NewTestHelper()
	mockPlugin := &MockPlugin{}

	// Test with nil metadata
	mockPlugin.On("GetMetadata").Return((*PluginMetadata)(nil))

	err := helper.ValidatePlugin(mockPlugin)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin metadata is nil")
	mockPlugin.AssertExpectations(t)
}

func TestBenchmarkHelper_BenchmarkConnector(t *testing.T) {
	helper := NewBenchmarkHelper()
	mockConnector := &MockConnectorForSDK{}

	recordsCh := make(chan connector.RecordBatch, 1)
	errCh := make(chan error, 1)

	// Create some test data
	batch := connector.RecordBatch{
		Records: []map[string]any{
			{"id": 1, "name": "test1"},
			{"id": 2, "name": "test2"},
		},
		Object: "test_object",
	}

	// Set up mock expectations
	mockConnector.On("Name").Return("test-connector")
	mockConnector.On("Connect", mock.Anything).Return(nil)
	mockConnector.On("DiscoverMetadata", mock.Anything).Return([]connector.ObjectMetadata{}, nil)
	mockConnector.On("GetFullSnapshot", mock.Anything, "test_object").Return(
		(<-chan connector.RecordBatch)(recordsCh),
		(<-chan error)(errCh),
	)
	mockConnector.On("Close").Return(nil)

	// Send test data and close channels
	go func() {
		recordsCh <- batch
		close(recordsCh)
		close(errCh)
	}()

	result, err := helper.BenchmarkConnector(mockConnector, "test_object", 100)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "test-connector", result.ConnectorName)
	assert.Equal(t, "test_object", result.ObjectName)
	assert.Equal(t, 100, result.RecordCount)
	assert.Equal(t, 2, result.RecordsReceived) // 2 records in the batch
	assert.True(t, result.TotalTime > 0)
	assert.True(t, result.RecordsPerSecond > 0)

	mockConnector.AssertExpectations(t)
}

func TestBenchmarkResult(t *testing.T) {
	result := &BenchmarkResult{
		ConnectorName:    "test-connector",
		ObjectName:       "test_object",
		RecordCount:      1000,
		RecordsReceived:  950,
		StartTime:        time.Now().Add(-1 * time.Minute),
		EndTime:          time.Now(),
		TotalTime:        1 * time.Minute,
		ConnectTime:      5 * time.Second,
		MetadataTime:     2 * time.Second,
		SnapshotTime:     50 * time.Second,
		RecordsPerSecond: 19.0,
	}

	assert.Equal(t, "test-connector", result.ConnectorName)
	assert.Equal(t, "test_object", result.ObjectName)
	assert.Equal(t, 1000, result.RecordCount)
	assert.Equal(t, 950, result.RecordsReceived)
	assert.Equal(t, 1*time.Minute, result.TotalTime)
	assert.Equal(t, 19.0, result.RecordsPerSecond)
}