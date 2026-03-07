package salesforce

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
)

const (
	connectorName = "salesforce"
	// Use Bulk API for queries expected to return more than this many records.
	bulkThreshold = 2000
)

// SalesforceConnector implements the connector.Connector interface.
type SalesforceConnector struct {
	client    *Client
	cfg       *config.SourceConfig
	logger    *slog.Logger
	batchSize int
}

// NewConnector creates a new Salesforce connector from config.
func NewConnector(cfg *config.SourceConfig) (connector.Connector, error) {
	// Convert string maps to interface{} maps for validation
	authMap := make(map[string]interface{})
	for k, v := range cfg.Auth {
		authMap[k] = v
	}
	optionsMap := make(map[string]interface{})
	for k, v := range cfg.Options {
		optionsMap[k] = v
	}

	// Parse and validate configuration using structured config
	structuredConfig, err := FromMap(authMap, optionsMap)
	if err != nil {
		return nil, fmt.Errorf("salesforce connector configuration error: %w", err)
	}

	// Convert to legacy AuthConfig format for backward compatibility
	auth := structuredConfig.ToAuthConfig()

	logger := slog.Default().With("connector", connectorName)
	client := NewClient(auth, logger)

	return &SalesforceConnector{
		client:    client,
		cfg:       cfg,
		logger:    logger,
		batchSize: cfg.BatchSize,
	}, nil
}

func (c *SalesforceConnector) Name() string {
	return connectorName
}

func (c *SalesforceConnector) Connect(ctx context.Context) error {
	return c.client.Authenticate(ctx)
}

func (c *SalesforceConnector) Close() error {
	return nil
}

func (c *SalesforceConnector) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
	return c.client.DescribeGlobal(ctx)
}

func (c *SalesforceConnector) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	return c.client.DescribeObject(ctx, objectName)
}

func (c *SalesforceConnector) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
	meta, err := c.client.DescribeObject(ctx, objectName)
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("describe %s for incremental: %w", objectName, err)
		close(errCh)
		recCh := make(chan connector.RecordBatch)
		close(recCh)
		return recCh, errCh
	}

	fields := meta.Schema.FieldNames()
	soql := BuildIncrementalSOQL(objectName, fields, watermarkField, since)
	c.logger.Info("incremental query", "object", objectName, "soql_prefix", truncate(soql, 200))

	return c.client.BulkQuery(ctx, soql, objectName, c.batchSize, c.logger)
}

func (c *SalesforceConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	meta, err := c.client.DescribeObject(ctx, objectName)
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("describe %s for full: %w", objectName, err)
		close(errCh)
		recCh := make(chan connector.RecordBatch)
		close(recCh)
		return recCh, errCh
	}

	fields := meta.Schema.FieldNames()
	soql := BuildFullSOQL(objectName, fields)
	c.logger.Info("full snapshot query", "object", objectName, "soql_prefix", truncate(soql, 200))

	return c.client.BulkQuery(ctx, soql, objectName, c.batchSize, c.logger)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ValidateConfig validates the Salesforce connector configuration.
func (c *SalesforceConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	_, err := FromMap(auth, options)
	return err
}

// ConfigSchema returns the configuration schema for the Salesforce connector.
func (c *SalesforceConnector) ConfigSchema() map[string]interface{} {
	config := &Config{}
	return config.Schema()
}

// Register adds the Salesforce connector factory to a registry.
func Register(registry *connector.Registry) {
	registry.Register(connectorName, func(cfg *config.SourceConfig) (connector.Connector, error) {
		return NewConnector(cfg)
	})
}
