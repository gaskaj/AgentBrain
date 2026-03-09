package connector

import (
	"fmt"
	"strings"
	"sync"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/resource"
)

// Factory creates a Connector from a source config.
type Factory func(cfg *config.SourceConfig) (Connector, error)

// Registry holds registered connector factories.
type Registry struct {
	mu             sync.RWMutex
	factories      map[string]Factory
	pluginManager  PluginManager // Interface to plugin manager
	resourceManager *resource.Manager
	httpClientPool resource.HTTPClientPool
}

// PluginManager interface for plugin integration
type PluginManager interface {
	GetConnectorFactory(pluginName string) (Factory, error)
}

// NewRegistry creates a new connector registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// SetPluginManager sets the plugin manager for the registry
func (r *Registry) SetPluginManager(pm PluginManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pluginManager = pm
}

// SetResourceManager sets the resource manager for connection pooling
func (r *Registry) SetResourceManager(rm *resource.Manager) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resourceManager = rm
	
	if rm != nil {
		// Get HTTP client pool from resource manager
		if pool, err := rm.GetPool("http_clients"); err == nil {
			// Use type assertion to check for the specific type we registered
			if httpPool, ok := pool.(*resource.HTTPClientPoolImpl); ok {
				r.httpClientPool = httpPool
			}
		}
	}
	return nil
}

// Register adds a connector factory to the registry.
func (r *Registry) Register(connectorType string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[connectorType] = factory
}

// Create instantiates a connector from a source config.
func (r *Registry) Create(cfg *config.SourceConfig) (Connector, error) {
	// Check if this is a plugin-based connector
	if strings.HasPrefix(cfg.Type, "plugin:") {
		return r.createPluginConnector(cfg)
	}

	r.mu.RLock()
	factory, ok := r.factories[cfg.Type]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown connector type: %q", cfg.Type)
	}

	return factory(cfg)
}

// createPluginConnector creates a connector from a plugin
func (r *Registry) createPluginConnector(cfg *config.SourceConfig) (Connector, error) {
	r.mu.RLock()
	pluginManager := r.pluginManager
	r.mu.RUnlock()

	if pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not available for plugin connector: %s", cfg.Type)
	}

	// Extract plugin name from type (format: "plugin:pluginname")
	pluginName := strings.TrimPrefix(cfg.Type, "plugin:")
	if pluginName == "" {
		return nil, fmt.Errorf("invalid plugin connector type: %s", cfg.Type)
	}

	// Get the factory from the plugin
	factory, err := pluginManager.GetConnectorFactory(pluginName)
	if err != nil {
		return nil, fmt.Errorf("get plugin factory for %s: %w", pluginName, err)
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
	// Check if this is a plugin-based connector
	if strings.HasPrefix(sourceConfig.Type, "plugin:") {
		return r.validatePluginSourceConfig(sourceConfig)
	}

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

// validatePluginSourceConfig validates a plugin-based source configuration
func (r *Registry) validatePluginSourceConfig(sourceConfig *config.SourceConfig) error {
	r.mu.RLock()
	pluginManager := r.pluginManager
	r.mu.RUnlock()

	if pluginManager == nil {
		return fmt.Errorf("plugin manager not available for validation")
	}

	// Extract plugin name
	pluginName := strings.TrimPrefix(sourceConfig.Type, "plugin:")
	if pluginName == "" {
		return fmt.Errorf("invalid plugin connector type: %s", sourceConfig.Type)
	}

	// Get the factory from the plugin
	factory, err := pluginManager.GetConnectorFactory(pluginName)
	if err != nil {
		return fmt.Errorf("get plugin factory for validation: %w", err)
	}

	// Create temporary connector for validation
	tempConnector, err := factory(sourceConfig)
	if err != nil {
		return fmt.Errorf("failed to create plugin connector for validation: %w", err)
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
	// Check if this is a plugin-based connector
	if strings.HasPrefix(connectorType, "plugin:") {
		return r.getPluginConnectorSchema(connectorType)
	}

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

// getPluginConnectorSchema returns the schema for a plugin-based connector
func (r *Registry) getPluginConnectorSchema(connectorType string) (map[string]interface{}, error) {
	r.mu.RLock()
	pluginManager := r.pluginManager
	r.mu.RUnlock()

	if pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not available")
	}

	// Extract plugin name
	pluginName := strings.TrimPrefix(connectorType, "plugin:")
	if pluginName == "" {
		return nil, fmt.Errorf("invalid plugin connector type: %s", connectorType)
	}

	// Get the factory from the plugin
	factory, err := pluginManager.GetConnectorFactory(pluginName)
	if err != nil {
		return nil, fmt.Errorf("get plugin factory for schema: %w", err)
	}

	// Create a temporary connector to get the schema
	// Note: This requires a minimal config since we're just getting the schema
	tempConfig := &config.SourceConfig{
		Type:    connectorType,
		Enabled: false, // Not enabled since this is just for schema
		Auth:    make(map[string]string),
		Options: make(map[string]string),
	}

	tempConnector, err := factory(tempConfig)
	if err != nil {
		return nil, fmt.Errorf("create temporary plugin connector for schema: %w", err)
	}

	return tempConnector.ConfigSchema(), nil
}
