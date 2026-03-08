package migration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// VersionedState wraps any state data with version information.
type VersionedState struct {
	Version   int         `json:"version"`
	Data      interface{} `json:"data"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// SchemaRegistry manages schema versions and validation.
type SchemaRegistry interface {
	RegisterSchema(version int, schema interface{}) error
	GetLatestVersion() int
	GetSchema(version int) (interface{}, error)
	ValidateSchema(version int, data interface{}) error
	GetVersionedState(data interface{}) *VersionedState
	ExtractData(versionedState *VersionedState, target interface{}) error
}

// DefaultSchemaRegistry provides an in-memory implementation.
type DefaultSchemaRegistry struct {
	mu        sync.RWMutex
	schemas   map[int]reflect.Type
	instances map[int]interface{}
}

// NewSchemaRegistry creates a new schema registry.
func NewSchemaRegistry() *DefaultSchemaRegistry {
	return &DefaultSchemaRegistry{
		schemas:   make(map[int]reflect.Type),
		instances: make(map[int]interface{}),
	}
}

// RegisterSchema registers a schema version with the registry.
func (r *DefaultSchemaRegistry) RegisterSchema(version int, schema interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if version <= 0 {
		return fmt.Errorf("schema version must be positive, got %d", version)
	}

	schemaType := reflect.TypeOf(schema)
	if schemaType == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	// Store both the type and a zero instance for validation
	r.schemas[version] = schemaType
	r.instances[version] = schema

	return nil
}

// GetLatestVersion returns the highest registered version.
func (r *DefaultSchemaRegistry) GetLatestVersion() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	latest := 0
	for version := range r.schemas {
		if version > latest {
			latest = version
		}
	}
	return latest
}

// GetSchema returns a zero instance of the schema for the given version.
func (r *DefaultSchemaRegistry) GetSchema(version int) (interface{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemaType, exists := r.schemas[version]
	if !exists {
		return nil, fmt.Errorf("schema version %d not found", version)
	}

	// Return a new zero instance of the schema type
	return reflect.New(schemaType.Elem()).Interface(), nil
}

// ValidateSchema validates that data conforms to the registered schema.
func (r *DefaultSchemaRegistry) ValidateSchema(version int, data interface{}) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	expectedType, exists := r.schemas[version]
	if !exists {
		return fmt.Errorf("schema version %d not found", version)
	}

	dataType := reflect.TypeOf(data)
	if dataType != expectedType {
		return fmt.Errorf("data type %s does not match schema type %s for version %d",
			dataType, expectedType, version)
	}

	return nil
}

// GetVersionedState wraps data in a versioned container using the latest schema version.
func (r *DefaultSchemaRegistry) GetVersionedState(data interface{}) *VersionedState {
	now := time.Now()
	return &VersionedState{
		Version:   r.GetLatestVersion(),
		Data:      data,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ExtractData extracts the data from a versioned state into the target.
func (r *DefaultSchemaRegistry) ExtractData(versionedState *VersionedState, target interface{}) error {
	if versionedState == nil {
		return fmt.Errorf("versioned state cannot be nil")
	}

	// Convert data through JSON to handle interface{} conversion
	jsonBytes, err := json.Marshal(versionedState.Data)
	if err != nil {
		return fmt.Errorf("marshal versioned data: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("unmarshal versioned data: %w", err)
	}

	return nil
}

// IsVersioned checks if the given data appears to be version-wrapped.
func IsVersioned(data []byte) bool {
	var test struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(data, &test) == nil && test.Version > 0
}

// WrapLegacyData wraps unversioned data in a version 1 container.
func WrapLegacyData(data interface{}) *VersionedState {
	now := time.Now()
	return &VersionedState{
		Version:   1,
		Data:      data,
		CreatedAt: now,
		UpdatedAt: now,
	}
}