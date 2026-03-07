package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
)

// Mock connector that always returns validation errors for testing
type mockConnector struct {
	validateError error
	schema        map[string]interface{}
}

func (m *mockConnector) Name() string                     { return "mock" }
func (m *mockConnector) Connect(ctx context.Context) error    { return nil }
func (m *mockConnector) Close() error                    { return nil }
func (m *mockConnector) DiscoverMetadata(ctx context.Context) ([]ObjectMetadata, error) { return nil, nil }
func (m *mockConnector) DescribeObject(ctx context.Context, objectName string) (*ObjectMetadata, error) { return nil, nil }
func (m *mockConnector) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan RecordBatch, <-chan error) { return nil, nil }
func (m *mockConnector) GetFullSnapshot(ctx context.Context, objectName string) (<-chan RecordBatch, <-chan error) { return nil, nil }

func (m *mockConnector) ValidateConfig(auth map[string]interface{}, options map[string]interface{}) error {
	return m.validateError
}

func (m *mockConnector) ConfigSchema() map[string]interface{} {
	return m.schema
}

func TestRegistry_ValidateSourceConfig(t *testing.T) {
	tests := []struct {
		name          string
		connectorType string
		sourceConfig  *config.SourceConfig
		validateError error
		wantErr       bool
		errContains   string
	}{
		{
			name:          "unknown connector type",
			connectorType: "unknown",
			sourceConfig: &config.SourceConfig{
				Type: "unknown",
				Auth: map[string]string{},
				Options: map[string]string{},
			},
			wantErr:     true,
			errContains: "unknown connector type",
		},
		{
			name:          "validation passes",
			connectorType: "mock",
			sourceConfig: &config.SourceConfig{
				Type: "mock",
				Auth: map[string]string{
					"valid": "config",
				},
				Options: map[string]string{},
			},
			validateError: nil,
			wantErr:       false,
		},
		{
			name:          "validation fails",
			connectorType: "mock",
			sourceConfig: &config.SourceConfig{
				Type: "mock",
				Auth: map[string]string{},
				Options: map[string]string{},
			},
			validateError: errors.New("validation failed"),
			wantErr:       true,
			errContains:   "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()

			if tt.connectorType == "mock" {
				// Register mock connector
				registry.Register(tt.connectorType, func(cfg *config.SourceConfig) (Connector, error) {
					return &mockConnector{
						validateError: tt.validateError,
					}, nil
				})
			}

			err := registry.ValidateSourceConfig(tt.sourceConfig)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSourceConfig() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateSourceConfig() error = %v, should contain %v", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSourceConfig() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestRegistry_GetConnectorSchema(t *testing.T) {
	tests := []struct {
		name          string
		connectorType string
		wantErr       bool
		errContains   string
	}{
		{
			name:          "salesforce schema",
			connectorType: "salesforce", 
			wantErr:       false,
		},
		{
			name:          "unknown connector",
			connectorType: "unknown",
			wantErr:       true,
			errContains:   "unknown connector type",
		},
		{
			name:          "unsupported schema",
			connectorType: "unsupported",
			wantErr:       true,
			errContains:   "schema not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()

			// Register salesforce connector for the test
			if tt.connectorType == "salesforce" {
				registry.Register(tt.connectorType, func(cfg *config.SourceConfig) (Connector, error) {
					return &mockConnector{}, nil
				})
			}

			// Register a connector that doesn't have schema support
			if tt.connectorType == "unsupported" {
				registry.Register(tt.connectorType, func(cfg *config.SourceConfig) (Connector, error) {
					return &mockConnector{}, nil
				})
			}

			schema, err := registry.GetConnectorSchema(tt.connectorType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetConnectorSchema() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("GetConnectorSchema() error = %v, should contain %v", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("GetConnectorSchema() unexpected error = %v", err)
					return
				}
				if schema == nil {
					t.Errorf("GetConnectorSchema() expected schema but got nil")
					return
				}

				// For salesforce, verify basic schema structure
				if tt.connectorType == "salesforce" {
					if schema["type"] != "object" {
						t.Errorf("GetConnectorSchema() schema type = %v, want object", schema["type"])
					}
					if _, ok := schema["properties"]; !ok {
						t.Errorf("GetConnectorSchema() schema missing properties")
					}
				}
			}
		})
	}
}