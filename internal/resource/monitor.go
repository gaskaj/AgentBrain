package resource

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"syscall"
	"time"
)

// SystemResourceMonitor monitors system-level resource usage
type SystemResourceMonitor struct {
	logger     *slog.Logger
	thresholds map[ResourceType]float64
}

// NewSystemResourceMonitor creates a new system resource monitor
func NewSystemResourceMonitor(logger *slog.Logger) *SystemResourceMonitor {
	return &SystemResourceMonitor{
		logger:     logger,
		thresholds: make(map[ResourceType]float64),
	}
}

// Monitor collects current system resource usage
func (m *SystemResourceMonitor) Monitor(ctx context.Context) (ResourceUsage, error) {
	usage := ResourceUsage{
		Timestamp: time.Now(),
	}

	// Memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	usage.MemoryUsedMB = int64(memStats.Alloc / 1024 / 1024)

	// Goroutine count
	usage.GoroutineCount = runtime.NumGoroutine()

	// File handle count (Unix-like systems)
	fileHandles, err := m.getFileHandleCount()
	if err != nil {
		m.logger.Warn("failed to get file handle count", "error", err)
	} else {
		usage.FileHandleCount = fileHandles
	}

	// Disk usage (simplified - would need more sophisticated implementation)
	diskUsage, err := m.getDiskUsage()
	if err != nil {
		m.logger.Warn("failed to get disk usage", "error", err)
	} else {
		usage.DiskUsedMB = diskUsage
	}

	return usage, nil
}

// SetThresholds sets alert thresholds for resource types
func (m *SystemResourceMonitor) SetThresholds(thresholds map[ResourceType]float64) error {
	m.thresholds = make(map[ResourceType]float64)
	for resourceType, threshold := range thresholds {
		if threshold < 0 || threshold > 1 {
			return fmt.Errorf("threshold for %s must be between 0 and 1, got %f", resourceType, threshold)
		}
		m.thresholds[resourceType] = threshold
	}
	return nil
}

func (m *SystemResourceMonitor) getFileHandleCount() (int, error) {
	// This is a Unix-specific implementation
	var rlimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit)
	if err != nil {
		return 0, fmt.Errorf("get file handle limit: %w", err)
	}

	// On Linux, we could read /proc/self/fd/ to get actual count
	// For now, return 0 to indicate we don't have the actual count
	return 0, nil
}

func (m *SystemResourceMonitor) getDiskUsage() (int64, error) {
	// Get disk usage for current working directory
	pwd, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("get working directory: %w", err)
	}

	var stat syscall.Statfs_t
	err = syscall.Statfs(pwd, &stat)
	if err != nil {
		return 0, fmt.Errorf("get filesystem stats: %w", err)
	}

	// Calculate used space in MB
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes
	usedMB := int64(usedBytes / 1024 / 1024)

	return usedMB, nil
}

// ConnectionMonitor tracks active network connections
type ConnectionMonitor struct {
	logger      *slog.Logger
	connections map[string]int // keyed by connection type
	thresholds  map[ResourceType]float64
}

// NewConnectionMonitor creates a new connection monitor
func NewConnectionMonitor(logger *slog.Logger) *ConnectionMonitor {
	return &ConnectionMonitor{
		logger:      logger,
		connections: make(map[string]int),
		thresholds:  make(map[ResourceType]float64),
	}
}

// RecordConnection records a new active connection
func (m *ConnectionMonitor) RecordConnection(connType string) {
	m.connections[connType]++
}

// RecordDisconnection records a closed connection
func (m *ConnectionMonitor) RecordDisconnection(connType string) {
	if m.connections[connType] > 0 {
		m.connections[connType]--
	}
}

// Monitor returns current connection usage
func (m *ConnectionMonitor) Monitor(ctx context.Context) (ResourceUsage, error) {
	usage := ResourceUsage{
		Timestamp: time.Now(),
	}

	// Sum all connection types
	total := 0
	for _, count := range m.connections {
		total += count
	}
	usage.ConnectionCount = total

	return usage, nil
}

// SetThresholds sets alert thresholds for connection monitoring
func (m *ConnectionMonitor) SetThresholds(thresholds map[ResourceType]float64) error {
	m.thresholds = make(map[ResourceType]float64)
	for resourceType, threshold := range thresholds {
		if threshold < 0 || threshold > 1 {
			return fmt.Errorf("threshold for %s must be between 0 and 1, got %f", resourceType, threshold)
		}
		m.thresholds[resourceType] = threshold
	}
	return nil
}

// GetConnectionBreakdown returns connection counts by type
func (m *ConnectionMonitor) GetConnectionBreakdown() map[string]int {
	breakdown := make(map[string]int)
	for connType, count := range m.connections {
		breakdown[connType] = count
	}
	return breakdown
}

// CompositeResourceMonitor combines multiple monitors
type CompositeResourceMonitor struct {
	monitors   []ResourceMonitor
	logger     *slog.Logger
	thresholds map[ResourceType]float64
}

// NewCompositeResourceMonitor creates a composite monitor
func NewCompositeResourceMonitor(logger *slog.Logger, monitors ...ResourceMonitor) *CompositeResourceMonitor {
	return &CompositeResourceMonitor{
		monitors:   monitors,
		logger:     logger,
		thresholds: make(map[ResourceType]float64),
	}
}

// AddMonitor adds a monitor to the composite
func (m *CompositeResourceMonitor) AddMonitor(monitor ResourceMonitor) {
	m.monitors = append(m.monitors, monitor)
}

// Monitor aggregates usage from all child monitors
func (m *CompositeResourceMonitor) Monitor(ctx context.Context) (ResourceUsage, error) {
	aggregated := ResourceUsage{
		Timestamp: time.Now(),
	}

	for _, monitor := range m.monitors {
		usage, err := monitor.Monitor(ctx)
		if err != nil {
			m.logger.Warn("monitor failed", "error", err)
			continue
		}

		// Aggregate usage (take maximum values or sum as appropriate)
		if usage.MemoryUsedMB > aggregated.MemoryUsedMB {
			aggregated.MemoryUsedMB = usage.MemoryUsedMB
			aggregated.MemoryPercent = usage.MemoryPercent
		}
		if usage.GoroutineCount > aggregated.GoroutineCount {
			aggregated.GoroutineCount = usage.GoroutineCount
			aggregated.GoroutinePercent = usage.GoroutinePercent
		}
		if usage.FileHandleCount > aggregated.FileHandleCount {
			aggregated.FileHandleCount = usage.FileHandleCount
			aggregated.FileHandlePercent = usage.FileHandlePercent
		}
		if usage.ConnectionCount > aggregated.ConnectionCount {
			aggregated.ConnectionCount = usage.ConnectionCount
			aggregated.ConnectionPercent = usage.ConnectionPercent
		}
		if usage.DiskUsedMB > aggregated.DiskUsedMB {
			aggregated.DiskUsedMB = usage.DiskUsedMB
			aggregated.DiskPercent = usage.DiskPercent
		}
	}

	return aggregated, nil
}

// SetThresholds sets thresholds for the composite monitor
func (m *CompositeResourceMonitor) SetThresholds(thresholds map[ResourceType]float64) error {
	m.thresholds = make(map[ResourceType]float64)
	for resourceType, threshold := range thresholds {
		if threshold < 0 || threshold > 1 {
			return fmt.Errorf("threshold for %s must be between 0 and 1, got %f", resourceType, threshold)
		}
		m.thresholds[resourceType] = threshold
	}

	// Propagate thresholds to child monitors
	for _, monitor := range m.monitors {
		if err := monitor.SetThresholds(thresholds); err != nil {
			m.logger.Warn("failed to set thresholds for monitor", "error", err)
		}
	}

	return nil
}