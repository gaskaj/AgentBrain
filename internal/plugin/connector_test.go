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

// MockConnector is a mock implementation of connector.Connector
type MockConnector struct {
	mock.Mock
}

func (m *MockConnector) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConnector) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockConnector) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
	args := m.Called(ctx)
	return args.Get(0).([]connector.ObjectMetadata), args.Error(1)
}

func (m *MockConnector) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	args := m.Called(ctx, objectName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*connector.ObjectMetadata), args.Error(1)
}

func (m *MockConnector) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
	args := m.Called(ctx, objectName, watermarkField, since)
	return args.Get(0).(<-chan connector.RecordBatch), args.Get(1).(<-chan error)
}

func (m *MockConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	args := m.Called(ctx, objectName)
	return args.Get(0).(<-chan connector.RecordBatch), args.Get(1).(<-chan error)
}

func (m *MockConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	args := m.Called(auth, options)
	return args.Error(0)
}

func (m *MockConnector) ConfigSchema() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

// MockPluginManager is a mock implementation of the plugin manager
type MockPluginManager struct {
	mock.Mock
}

func (m *MockPluginManager) GetConnectorFactory(pluginName string) (connector.Factory, error) {
	args := m.Called(pluginName)
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(connector.Factory), args.Error(1)
}

func TestPluginConnector_Name(t *testing.T) {
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)
	
	assert.Equal(t, "test-connector", pc.Name())
}

func TestPluginConnector_Connect(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	// Mock the factory to return our mock connector
	var factory connector.Factory = func(cfg *config.SourceConfig) (connector.Connector, error) {
		return mockConnector, nil
	}
	manager.On("GetConnectorFactory", "test-plugin").Return(factory, nil)

	mockConnector.On("Connect", mock.Anything).Return(nil)

	ctx := context.Background()
	err := pc.Connect(ctx)

	assert.NoError(t, err)
	assert.Equal(t, int64(1), pc.metrics.ConnectionAttempts)
	manager.AssertExpectations(t)
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_ConnectError(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	// Mock the factory to return our mock connector
	var factory connector.Factory = func(cfg *config.SourceConfig) (connector.Connector, error) {
		return mockConnector, nil
	}
	manager.On("GetConnectorFactory", "test-plugin").Return(factory, nil)

	mockConnector.On("Connect", mock.Anything).Return(assert.AnError)

	ctx := context.Background()
	err := pc.Connect(ctx)

	assert.Error(t, err)
	assert.Equal(t, int64(1), pc.metrics.ConnectionAttempts)
	assert.Equal(t, int64(1), pc.metrics.ConnectionFailures)
	assert.Equal(t, int64(1), pc.metrics.ErrorCount)
	manager.AssertExpectations(t)
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_Close(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)
	pc.connector = mockConnector // Set connector directly for this test

	mockConnector.On("Close").Return(nil)

	err := pc.Close()

	assert.NoError(t, err)
	assert.Nil(t, pc.connector) // Should be nil after close
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_DiscoverMetadata(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	expectedMetadata := []connector.ObjectMetadata{
		{Name: "test_object", Label: "Test Object", Queryable: true},
	}

	// Mock the factory to return our mock connector
	var factory connector.Factory = func(cfg *config.SourceConfig) (connector.Connector, error) {
		return mockConnector, nil
	}
	manager.On("GetConnectorFactory", "test-plugin").Return(factory, nil)

	mockConnector.On("DiscoverMetadata", mock.Anything).Return(expectedMetadata, nil)

	ctx := context.Background()
	metadata, err := pc.DiscoverMetadata(ctx)

	assert.NoError(t, err)
	assert.Equal(t, expectedMetadata, metadata)
	assert.Equal(t, int64(1), pc.metrics.OperationCount)
	manager.AssertExpectations(t)
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_ValidateConfig(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	auth := map[string]interface{}{"key": "value"}
	options := map[string]interface{}{"option": "value"}

	// Mock the factory to return our mock connector
	var factory connector.Factory = func(cfg *config.SourceConfig) (connector.Connector, error) {
		return mockConnector, nil
	}
	manager.On("GetConnectorFactory", "test-plugin").Return(factory, nil)

	mockConnector.On("ValidateConfig", auth, options).Return(nil)

	err := pc.ValidateConfig(auth, options)

	assert.NoError(t, err)
	manager.AssertExpectations(t)
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_ConfigSchema(t *testing.T) {
	mockConnector := &MockConnector{}
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	expectedSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{"type": "string"},
		},
	}

	// Mock the factory to return our mock connector
	var factory connector.Factory = func(cfg *config.SourceConfig) (connector.Connector, error) {
		return mockConnector, nil
	}
	manager.On("GetConnectorFactory", "test-plugin").Return(factory, nil)

	mockConnector.On("ConfigSchema").Return(expectedSchema)

	schema := pc.ConfigSchema()

	assert.Equal(t, expectedSchema, schema)
	manager.AssertExpectations(t)
	mockConnector.AssertExpectations(t)
}

func TestPluginConnector_GetMetrics(t *testing.T) {
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.SourceConfig{Type: "test"}

	pc := NewPluginConnector("test-connector", "test-plugin", manager, cfg, logger)

	metrics := pc.GetMetrics()

	require.NotNil(t, metrics)
	assert.Equal(t, int64(0), metrics.ConnectionAttempts)
	assert.Equal(t, int64(0), metrics.ConnectionFailures)
	assert.Equal(t, int64(0), metrics.OperationCount)
	assert.Equal(t, int64(0), metrics.ErrorCount)
}

func TestPluginConnectorFactory_Create(t *testing.T) {
	manager := &MockPluginManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	factoryFunc := NewPluginConnectorFactory(manager, "test-plugin", logger)

	cfg := &config.SourceConfig{Type: "test"}
	conn, err := factoryFunc(cfg)

	assert.NoError(t, err)
	require.NotNil(t, conn)

	pluginConnector, ok := conn.(*PluginConnector)
	require.True(t, ok)
	assert.Equal(t, "test", pluginConnector.name)
	assert.Equal(t, "test-plugin", pluginConnector.pluginName)
}

func TestConnectorMetrics(t *testing.T) {
	metrics := &ConnectorMetrics{
		ConnectionAttempts: 5,
		ConnectionFailures: 1,
		OperationCount:     10,
		ErrorCount:         2,
		LastError:          "test error",
	}

	assert.Equal(t, int64(5), metrics.ConnectionAttempts)
	assert.Equal(t, int64(1), metrics.ConnectionFailures)
	assert.Equal(t, int64(10), metrics.OperationCount)
	assert.Equal(t, int64(2), metrics.ErrorCount)
	assert.Equal(t, "test error", metrics.LastError)
}