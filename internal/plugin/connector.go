package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
)

// PluginManager defines the interface needed by PluginConnector
type PluginManager interface {
	GetConnectorFactory(pluginName string) (connector.Factory, error)
}

// PluginConnector wraps a plugin-based connector and implements the standard connector.Connector interface
type PluginConnector struct {
	name       string
	pluginName string
	manager    PluginManager
	connector  connector.Connector
	config     *config.SourceConfig
	logger     *slog.Logger
	metrics    *ConnectorMetrics
}

// ConnectorMetrics tracks plugin connector performance
type ConnectorMetrics struct {
	ConnectionAttempts int64     `json:"connection_attempts"`
	ConnectionFailures int64     `json:"connection_failures"`
	LastConnection     time.Time `json:"last_connection"`
	OperationCount     int64     `json:"operation_count"`
	ErrorCount         int64     `json:"error_count"`
	LastError          string    `json:"last_error,omitempty"`
	AverageResponseTime time.Duration `json:"average_response_time"`
}

// NewPluginConnector creates a new plugin-based connector wrapper
func NewPluginConnector(name string, pluginName string, manager PluginManager, config *config.SourceConfig, logger *slog.Logger) *PluginConnector {
	return &PluginConnector{
		name:       name,
		pluginName: pluginName,
		manager:    manager,
		config:     config,
		logger:     logger,
		metrics:    &ConnectorMetrics{},
	}
}

// Name returns the connector identifier
func (pc *PluginConnector) Name() string {
	return pc.name
}

// Connect establishes a connection through the plugin connector
func (pc *PluginConnector) Connect(ctx context.Context) error {
	start := time.Now()
	defer func() {
		pc.metrics.ConnectionAttempts++
		pc.metrics.LastConnection = time.Now()
	}()

	pc.logger.Info("Connecting through plugin", "plugin", pc.pluginName, "connector", pc.name)

	// Get the underlying connector from the plugin
	if err := pc.ensureConnector(); err != nil {
		pc.metrics.ConnectionFailures++
		pc.recordError(err)
		return fmt.Errorf("ensure plugin connector: %w", err)
	}

	// Connect through the plugin
	if err := pc.connector.Connect(ctx); err != nil {
		pc.metrics.ConnectionFailures++
		pc.recordError(err)
		return fmt.Errorf("plugin connect: %w", err)
	}

	pc.updateResponseTime(time.Since(start))
	pc.logger.Info("Connected successfully through plugin", "plugin", pc.pluginName)

	return nil
}

// Close releases resources
func (pc *PluginConnector) Close() error {
	if pc.connector == nil {
		return nil
	}

	pc.logger.Info("Closing plugin connector", "plugin", pc.pluginName)

	if err := pc.connector.Close(); err != nil {
		pc.recordError(err)
		return fmt.Errorf("plugin close: %w", err)
	}

	pc.connector = nil
	return nil
}

// DiscoverMetadata discovers available objects through the plugin
func (pc *PluginConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
	start := time.Now()
	defer func() {
		pc.metrics.OperationCount++
		pc.updateResponseTime(time.Since(start))
	}()

	if err := pc.ensureConnector(); err != nil {
		pc.recordError(err)
		return nil, fmt.Errorf("ensure plugin connector: %w", err)
	}

	metadata, err := pc.connector.DiscoverMetadata(ctx)
	if err != nil {
		pc.recordError(err)
		return nil, fmt.Errorf("plugin discover metadata: %w", err)
	}

	pc.logger.Debug("Discovered metadata through plugin", 
		"plugin", pc.pluginName, 
		"objects", len(metadata))

	return metadata, nil
}

// DescribeObject describes a specific object through the plugin
func (pc *PluginConnector) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	start := time.Now()
	defer func() {
		pc.metrics.OperationCount++
		pc.updateResponseTime(time.Since(start))
	}()

	if err := pc.ensureConnector(); err != nil {
		pc.recordError(err)
		return nil, fmt.Errorf("ensure plugin connector: %w", err)
	}

	metadata, err := pc.connector.DescribeObject(ctx, objectName)
	if err != nil {
		pc.recordError(err)
		return nil, fmt.Errorf("plugin describe object: %w", err)
	}

	return metadata, nil
}

// GetIncrementalChanges gets incremental changes through the plugin
func (pc *PluginConnector) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
	pc.metrics.OperationCount++

	if err := pc.ensureConnector(); err != nil {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("ensure plugin connector: %w", err)
		close(errCh)
		pc.recordError(err)
		return nil, errCh
	}

	recordsCh, errCh := pc.connector.GetIncrementalChanges(ctx, objectName, watermarkField, since)

	// Wrap the channels to add metrics tracking
	wrappedRecordsCh := make(chan connector.RecordBatch)
	wrappedErrCh := make(chan error)

	go func() {
		defer close(wrappedRecordsCh)
		defer close(wrappedErrCh)

		for {
			select {
			case batch, ok := <-recordsCh:
				if !ok {
					return
				}
				wrappedRecordsCh <- batch
			case err, ok := <-errCh:
				if !ok {
					return
				}
				if err != nil {
					pc.recordError(err)
				}
				wrappedErrCh <- err
			case <-ctx.Done():
				return
			}
		}
	}()

	return wrappedRecordsCh, wrappedErrCh
}

// GetFullSnapshot gets full snapshot through the plugin
func (pc *PluginConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	pc.metrics.OperationCount++

	if err := pc.ensureConnector(); err != nil {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("ensure plugin connector: %w", err)
		close(errCh)
		pc.recordError(err)
		return nil, errCh
	}

	recordsCh, errCh := pc.connector.GetFullSnapshot(ctx, objectName)

	// Wrap the channels to add metrics tracking
	wrappedRecordsCh := make(chan connector.RecordBatch)
	wrappedErrCh := make(chan error)

	go func() {
		defer close(wrappedRecordsCh)
		defer close(wrappedErrCh)

		for {
			select {
			case batch, ok := <-recordsCh:
				if !ok {
					return
				}
				wrappedRecordsCh <- batch
			case err, ok := <-errCh:
				if !ok {
					return
				}
				if err != nil {
					pc.recordError(err)
				}
				wrappedErrCh <- err
			case <-ctx.Done():
				return
			}
		}
	}()

	return wrappedRecordsCh, wrappedErrCh
}

// ValidateConfig validates the connector configuration through the plugin
func (pc *PluginConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	if err := pc.ensureConnector(); err != nil {
		pc.recordError(err)
		return fmt.Errorf("ensure plugin connector: %w", err)
	}

	if err := pc.connector.ValidateConfig(auth, options); err != nil {
		pc.recordError(err)
		return fmt.Errorf("plugin validate config: %w", err)
	}

	return nil
}

// ConfigSchema returns the configuration schema through the plugin
func (pc *PluginConnector) ConfigSchema() map[string]interface{} {
	if err := pc.ensureConnector(); err != nil {
		pc.logger.Error("Failed to ensure connector for schema", "error", err)
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"error": map[string]interface{}{
					"type": "string",
					"description": fmt.Sprintf("Plugin connector error: %v", err),
				},
			},
		}
	}

	return pc.connector.ConfigSchema()
}

// GetMetrics returns connector performance metrics
func (pc *PluginConnector) GetMetrics() *ConnectorMetrics {
	return pc.metrics
}

// ensureConnector ensures the underlying plugin connector is available
func (pc *PluginConnector) ensureConnector() error {
	if pc.connector != nil {
		return nil
	}

	// Get the connector factory from the plugin manager
	factory, err := pc.manager.GetConnectorFactory(pc.pluginName)
	if err != nil {
		return fmt.Errorf("get connector factory: %w", err)
	}

	// Create the connector instance
	connector, err := factory(pc.config)
	if err != nil {
		return fmt.Errorf("create connector: %w", err)
	}

	pc.connector = connector
	return nil
}

// recordError records an error in metrics
func (pc *PluginConnector) recordError(err error) {
	pc.metrics.ErrorCount++
	pc.metrics.LastError = err.Error()
	pc.logger.Error("Plugin connector error", 
		"plugin", pc.pluginName, 
		"connector", pc.name, 
		"error", err)
}

// updateResponseTime updates the average response time metric
func (pc *PluginConnector) updateResponseTime(duration time.Duration) {
	// Simple moving average calculation
	if pc.metrics.AverageResponseTime == 0 {
		pc.metrics.AverageResponseTime = duration
	} else {
		pc.metrics.AverageResponseTime = (pc.metrics.AverageResponseTime + duration) / 2
	}
}

// PluginConnectorFactory creates plugin-based connectors
type PluginConnectorFactory struct {
	manager    PluginManager
	pluginName string
	logger     *slog.Logger
}

// NewPluginConnectorFactory creates a new plugin connector factory
func NewPluginConnectorFactory(manager PluginManager, pluginName string, logger *slog.Logger) connector.Factory {
	factory := &PluginConnectorFactory{
		manager:    manager,
		pluginName: pluginName,
		logger:     logger,
	}

	return factory.Create
}

// Create creates a new plugin connector instance
func (f *PluginConnectorFactory) Create(cfg *config.SourceConfig) (connector.Connector, error) {
	return NewPluginConnector(
		cfg.Type,
		f.pluginName,
		f.manager,
		cfg,
		f.logger,
	), nil
}