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
