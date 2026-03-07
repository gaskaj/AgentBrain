package connector

import (
	"fmt"
	"sync"

	"github.com/agentbrain/agentbrain/internal/config"
)

// Factory creates a Connector from a source config.
type Factory func(cfg *config.SourceConfig) (Connector, error)

// Registry holds registered connector factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates a new connector registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register adds a connector factory to the registry.
func (r *Registry) Register(connectorType string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[connectorType] = factory
}

// Create instantiates a connector from a source config.
func (r *Registry) Create(cfg *config.SourceConfig) (Connector, error) {
	r.mu.RLock()
	factory, ok := r.factories[cfg.Type]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown connector type: %q", cfg.Type)
	}

	return factory(cfg)
}

// Types returns all registered connector types.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// ValidateSourceConfig validates a source configuration using the appropriate connector.
func (r *Registry) ValidateSourceConfig(sourceConfig *config.SourceConfig) error {
	r.mu.RLock()
	factory, exists := r.factories[sourceConfig.Type]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown connector type: %s", sourceConfig.Type)
	}

	// Create temporary connector instance for validation
	tempConnector, err := factory(sourceConfig)
	if err != nil {
		return fmt.Errorf("failed to create connector for validation: %w", err)
	}

	// Convert string maps to interface{} maps for validation
	authMap := make(map[string]interface{})
	for k, v := range sourceConfig.Auth {
		authMap[k] = v
	}

	optionsMap := make(map[string]interface{})
	for k, v := range sourceConfig.Options {
		optionsMap[k] = v
	}

	return tempConnector.ValidateConfig(authMap, optionsMap)
}

// GetConnectorSchema returns the configuration schema for a connector type.
func (r *Registry) GetConnectorSchema(connectorType string) (map[string]interface{}, error) {
	r.mu.RLock()
	_, exists := r.factories[connectorType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown connector type: %s", connectorType)
	}

	// For now, directly return schema for known connector types
	// This avoids the need to instantiate connectors just for schema retrieval
	switch connectorType {
	case "salesforce":
		// Return hardcoded schema for Salesforce to avoid import cycles
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce OAuth2 client ID",
					"required":    true,
				},
				"client_secret": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce OAuth2 client secret",
					"required":    true,
				},
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce username (must be a valid email)",
					"format":      "email",
					"required":    true,
				},
				"password": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce password",
					"required":    true,
				},
				"security_token": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce security token",
					"required":    true,
				},
				"login_url": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce login URL",
					"format":      "url",
					"default":     "https://login.salesforce.com",
					"required":    false,
				},
				"api_version": map[string]interface{}{
					"type":        "string",
					"description": "Salesforce API version (e.g., v59.0)",
					"pattern":     "^v\\d+\\.\\d+$",
					"default":     "v59.0",
					"required":    false,
				},
			},
			"required": []string{"client_id", "client_secret", "username", "password", "security_token"},
		}, nil
	default:
		return nil, fmt.Errorf("schema not available for connector type: %s", connectorType)
	}
}
