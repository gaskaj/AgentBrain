package connector

import (
	"context"
	"time"

	"github.com/agentbrain/agentbrain/internal/schema"
)

// Connector is the core abstraction for data source integrations.
type Connector interface {
	// Name returns the connector identifier (e.g., "salesforce").
	Name() string

	// Connect establishes a connection and authenticates.
	Connect(ctx context.Context) error

	// Close releases any resources held by the connector.
	Close() error

	// DiscoverMetadata returns metadata for all available objects.
	DiscoverMetadata(ctx context.Context) ([]ObjectMetadata, error)

	// DescribeObject returns detailed metadata for a specific object.
	DescribeObject(ctx context.Context, objectName string) (*ObjectMetadata, error)

	// GetIncrementalChanges streams records changed since the given watermark.
	GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan RecordBatch, <-chan error)

	// GetFullSnapshot streams all records for a full sync.
	GetFullSnapshot(ctx context.Context, objectName string) (<-chan RecordBatch, <-chan error)
}

// ObjectMetadata describes a data object from the source system.
type ObjectMetadata struct {
	Name           string        `json:"name"`
	Label          string        `json:"label"`
	Queryable      bool          `json:"queryable"`
	Retrievable    bool          `json:"retrievable"`
	Replicateable  bool          `json:"replicateable"`
	RecordCount    int64         `json:"recordCount,omitempty"`
	WatermarkField string        `json:"watermarkField,omitempty"`
	Schema         *schema.Schema `json:"schema,omitempty"`
}

// RecordBatch is a chunk of records from a connector.
type RecordBatch struct {
	Records []map[string]any
	Object  string
}
