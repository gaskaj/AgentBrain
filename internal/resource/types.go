package resource

import (
	"context"
	"net/http"
	"time"

	"github.com/parquet-go/parquet-go"
)

// Priority defines resource allocation priority levels
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// String returns string representation of priority
func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ResourceType defines types of resources being managed
type ResourceType string

const (
	ResourceTypeMemory      ResourceType = "memory"
	ResourceTypeGoroutines  ResourceType = "goroutines"
	ResourceTypeFileHandles ResourceType = "file_handles"
	ResourceTypeConnections ResourceType = "connections"
	ResourceTypeDiskSpace   ResourceType = "disk_space"
	ResourceTypeHTTPClients ResourceType = "http_clients"
	ResourceTypeParquetWriters ResourceType = "parquet_writers"
)

// ResourceLimits defines system resource limits
type ResourceLimits struct {
	MaxMemoryMB     int64 `yaml:"max_memory_mb"`
	MaxGoroutines   int   `yaml:"max_goroutines"`
	MaxFileHandles  int   `yaml:"max_file_handles"`
	MaxConnections  int   `yaml:"max_connections"`
	MaxDiskUsageMB  int64 `yaml:"max_disk_usage_mb"`
}

// ResourceUsage represents current resource utilization
type ResourceUsage struct {
	MemoryUsedMB    int64   `json:"memory_used_mb"`
	MemoryPercent   float64 `json:"memory_percent"`
	GoroutineCount  int     `json:"goroutine_count"`
	GoroutinePercent float64 `json:"goroutine_percent"`
	FileHandleCount int     `json:"file_handle_count"`
	FileHandlePercent float64 `json:"file_handle_percent"`
	ConnectionCount int     `json:"connection_count"`
	ConnectionPercent float64 `json:"connection_percent"`
	DiskUsedMB      int64   `json:"disk_used_mb"`
	DiskPercent     float64 `json:"disk_percent"`
	Timestamp       time.Time `json:"timestamp"`
}

// PoolHealth represents the health status of a resource pool
type PoolHealth struct {
	Active    int       `json:"active"`
	Idle      int       `json:"idle"`
	Total     int       `json:"total"`
	MaxSize   int       `json:"max_size"`
	Hits      int64     `json:"hits"`
	Misses    int64     `json:"misses"`
	Timeouts  int64     `json:"timeouts"`
	Errors    int64     `json:"errors"`
	LastReset time.Time `json:"last_reset"`
}

// DegradationThresholds defines when degradation strategies should trigger
type DegradationThresholds struct {
	MemoryThreshold    float64 `yaml:"memory_threshold"`
	GoroutineThreshold float64 `yaml:"goroutine_threshold"`
	DiskThreshold      float64 `yaml:"disk_threshold"`
	ConnectionThreshold float64 `yaml:"connection_threshold"`
}

// Operation represents a resource-consuming operation that can be degraded
type Operation interface {
	GetPriority() Priority
	GetResourceRequirements() ResourceRequirements
	Execute(ctx context.Context) error
}

// ResourceRequirements specifies what resources an operation needs
type ResourceRequirements struct {
	Memory      int64 `json:"memory"`
	Goroutines  int   `json:"goroutines"`
	FileHandles int   `json:"file_handles"`
	Connections int   `json:"connections"`
	DiskSpace   int64 `json:"disk_space"`
}

// ResourcePool defines a generic interface for resource pooling
type ResourcePool interface {
	Acquire(ctx context.Context, priority Priority) (interface{}, error)
	Release(resource interface{})
	Health() PoolHealth
	Close() error
}

// HTTPClientPool manages a pool of HTTP clients
type HTTPClientPool interface {
	ResourcePool
	AcquireHTTPClient(ctx context.Context, priority Priority) (*http.Client, error)
	ReleaseHTTPClient(*http.Client)
}

// ParquetWriterPool manages a pool of Parquet writers
type ParquetWriterPool interface {
	Acquire(ctx context.Context, schema *parquet.Schema) (*parquet.GenericWriter[any], error)
	Release(*parquet.GenericWriter[any])
	Health() PoolHealth
	Close() error
}

// ResourceMonitor tracks resource usage for a specific component
type ResourceMonitor interface {
	Monitor(ctx context.Context) (ResourceUsage, error)
	SetThresholds(thresholds map[ResourceType]float64) error
}

// DegradationStrategy defines how to handle resource pressure for specific resource types
type DegradationStrategy interface {
	ShouldDegrade(usage ResourceUsage, limits ResourceLimits) bool
	ApplyDegradation(ctx context.Context, operation Operation) Operation
	GetResourceType() ResourceType
	GetName() string
}

// ResourceAlert represents a resource-related alert condition
type ResourceAlert struct {
	Type       ResourceType `json:"type"`
	Severity   string       `json:"severity"`
	Message    string       `json:"message"`
	Threshold  float64      `json:"threshold"`
	CurrentValue float64     `json:"current_value"`
	Timestamp  time.Time    `json:"timestamp"`
}

// PoolConfig defines configuration for resource pools
type PoolConfig struct {
	MaxSize        int           `yaml:"max_size"`
	InitialSize    int           `yaml:"initial_size"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
	MaxLifetime    time.Duration `yaml:"max_lifetime"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
}