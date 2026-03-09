package resource

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// Manager coordinates resource allocation, monitoring, and degradation across the system
type Manager struct {
	limits         ResourceLimits
	pools          map[string]ResourcePool
	monitors       []ResourceMonitor
	degrader       *DegradationController
	logger         *slog.Logger
	mu             sync.RWMutex
	lastUsage      ResourceUsage
	enabled        bool
	stopCh         chan struct{}
	doneCh         chan struct{}
	alertCallbacks []func(ResourceAlert)
}

// NewManager creates a new resource manager
func NewManager(limits ResourceLimits, logger *slog.Logger) *Manager {
	return &Manager{
		limits:         limits,
		pools:          make(map[string]ResourcePool),
		monitors:       []ResourceMonitor{},
		logger:         logger,
		enabled:        true,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		alertCallbacks: []func(ResourceAlert){},
	}
}

// SetDegradationController sets the degradation controller
func (m *Manager) SetDegradationController(degrader *DegradationController) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.degrader = degrader
}

// RegisterPool registers a resource pool with the manager
func (m *Manager) RegisterPool(name string, pool ResourcePool) error {
	if !m.enabled {
		return fmt.Errorf("resource manager is disabled")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.pools[name]; exists {
		return fmt.Errorf("pool %s already registered", name)
	}

	m.pools[name] = pool
	m.logger.Info("registered resource pool", "name", name)
	return nil
}

// GetPool returns a registered resource pool
func (m *Manager) GetPool(name string) (ResourcePool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pool, exists := m.pools[name]
	if !exists {
		return nil, fmt.Errorf("pool %s not found", name)
	}
	return pool, nil
}

// RegisterMonitor adds a resource monitor
func (m *Manager) RegisterMonitor(monitor ResourceMonitor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monitors = append(m.monitors, monitor)
}

// RegisterAlertCallback adds a callback for resource alerts
func (m *Manager) RegisterAlertCallback(callback func(ResourceAlert)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertCallbacks = append(m.alertCallbacks, callback)
}

// Start begins resource monitoring
func (m *Manager) Start(ctx context.Context) error {
	if !m.enabled {
		m.logger.Info("resource manager is disabled")
		return nil
	}

	m.logger.Info("starting resource manager", 
		"memory_limit_mb", m.limits.MaxMemoryMB,
		"goroutine_limit", m.limits.MaxGoroutines)

	go m.monitoringLoop(ctx)
	return nil
}

// Stop gracefully shuts down the resource manager
func (m *Manager) Stop(ctx context.Context) error {
	if !m.enabled {
		return nil
	}

	m.logger.Info("stopping resource manager")
	close(m.stopCh)
	
	// Wait for monitoring loop to finish or context timeout
	select {
	case <-m.doneCh:
		m.logger.Info("resource manager stopped")
	case <-ctx.Done():
		m.logger.Warn("resource manager stop timed out")
	}

	// Close all pools
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for name, pool := range m.pools {
		if err := pool.Close(); err != nil {
			m.logger.Warn("failed to close pool", "name", name, "error", err)
		}
	}

	return nil
}

// GetCurrentUsage returns the current resource usage snapshot
func (m *Manager) GetCurrentUsage(ctx context.Context) (ResourceUsage, error) {
	if !m.enabled {
		return ResourceUsage{}, fmt.Errorf("resource manager is disabled")
	}

	return m.collectResourceUsage(ctx)
}

// GetLastUsage returns the last collected resource usage
func (m *Manager) GetLastUsage() ResourceUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUsage
}

// CheckResourceAvailability checks if resources are available for an operation
func (m *Manager) CheckResourceAvailability(ctx context.Context, requirements ResourceRequirements) error {
	if !m.enabled {
		return nil
	}

	usage, err := m.collectResourceUsage(ctx)
	if err != nil {
		return fmt.Errorf("collect resource usage: %w", err)
	}

	// Check memory
	if requirements.Memory > 0 {
		availableMemory := m.limits.MaxMemoryMB - usage.MemoryUsedMB
		if requirements.Memory > availableMemory {
			return fmt.Errorf("insufficient memory: need %d MB, available %d MB", 
				requirements.Memory, availableMemory)
		}
	}

	// Check goroutines
	if requirements.Goroutines > 0 {
		availableGoroutines := m.limits.MaxGoroutines - usage.GoroutineCount
		if requirements.Goroutines > availableGoroutines {
			return fmt.Errorf("insufficient goroutines: need %d, available %d", 
				requirements.Goroutines, availableGoroutines)
		}
	}

	// Check disk space
	if requirements.DiskSpace > 0 {
		availableDisk := m.limits.MaxDiskUsageMB - usage.DiskUsedMB
		if requirements.DiskSpace > availableDisk {
			return fmt.Errorf("insufficient disk space: need %d MB, available %d MB", 
				requirements.DiskSpace, availableDisk)
		}
	}

	return nil
}

// ShouldDegrade checks if degradation should be applied based on current resource usage
func (m *Manager) ShouldDegrade(ctx context.Context) (bool, []ResourceType, error) {
	if !m.enabled || m.degrader == nil {
		return false, nil, nil
	}

	usage, err := m.collectResourceUsage(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("collect resource usage: %w", err)
	}

	return m.degrader.ShouldDegrade(usage, m.limits)
}

// ApplyDegradation applies degradation strategies to an operation
func (m *Manager) ApplyDegradation(ctx context.Context, operation Operation) (Operation, error) {
	if !m.enabled || m.degrader == nil {
		return operation, nil
	}

	usage, err := m.collectResourceUsage(ctx)
	if err != nil {
		return operation, fmt.Errorf("collect resource usage: %w", err)
	}

	return m.degrader.ApplyDegradation(ctx, operation, usage, m.limits)
}

// GetPoolHealth returns health information for all registered pools
func (m *Manager) GetPoolHealth() map[string]PoolHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]PoolHealth)
	for name, pool := range m.pools {
		health[name] = pool.Health()
	}
	return health
}

// IsEnabled returns whether the resource manager is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// SetEnabled enables or disables resource management
func (m *Manager) SetEnabled(enabled bool) {
	m.enabled = enabled
	if enabled {
		m.logger.Info("resource management enabled")
	} else {
		m.logger.Info("resource management disabled")
	}
}

func (m *Manager) monitoringLoop(ctx context.Context) {
	defer close(m.doneCh)

	// Monitor every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("resource monitoring stopped due to context cancellation")
			return
		case <-m.stopCh:
			m.logger.Info("resource monitoring stopped")
			return
		case <-ticker.C:
			if err := m.performResourceCheck(ctx); err != nil {
				m.logger.Error("resource check failed", "error", err)
			}
		}
	}
}

func (m *Manager) performResourceCheck(ctx context.Context) error {
	usage, err := m.collectResourceUsage(ctx)
	if err != nil {
		return fmt.Errorf("collect resource usage: %w", err)
	}

	// Update last usage
	m.mu.Lock()
	m.lastUsage = usage
	m.mu.Unlock()

	// Check for alerts
	alerts := m.checkResourceAlerts(usage)
	for _, alert := range alerts {
		m.dispatchAlert(alert)
	}

	// Log resource usage periodically (every 5 minutes)
	if time.Now().Minute()%5 == 0 && time.Now().Second() < 30 {
		m.logger.Info("resource usage",
			"memory_mb", usage.MemoryUsedMB,
			"memory_percent", fmt.Sprintf("%.1f%%", usage.MemoryPercent),
			"goroutines", usage.GoroutineCount,
			"goroutine_percent", fmt.Sprintf("%.1f%%", usage.GoroutinePercent),
		)
	}

	return nil
}

func (m *Manager) collectResourceUsage(ctx context.Context) (ResourceUsage, error) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	usage := ResourceUsage{
		MemoryUsedMB:     int64(memStats.Alloc / 1024 / 1024),
		GoroutineCount:   runtime.NumGoroutine(),
		Timestamp:        time.Now(),
	}

	// Calculate percentages
	if m.limits.MaxMemoryMB > 0 {
		usage.MemoryPercent = float64(usage.MemoryUsedMB) / float64(m.limits.MaxMemoryMB) * 100
	}
	if m.limits.MaxGoroutines > 0 {
		usage.GoroutinePercent = float64(usage.GoroutineCount) / float64(m.limits.MaxGoroutines) * 100
	}

	// Run additional monitors
	for _, monitor := range m.monitors {
		monitorUsage, err := monitor.Monitor(ctx)
		if err != nil {
			m.logger.Warn("monitor failed", "error", err)
			continue
		}

		// Merge usage data (simplified - could be more sophisticated)
		if monitorUsage.FileHandleCount > 0 {
			usage.FileHandleCount = monitorUsage.FileHandleCount
			if m.limits.MaxFileHandles > 0 {
				usage.FileHandlePercent = float64(usage.FileHandleCount) / float64(m.limits.MaxFileHandles) * 100
			}
		}
		if monitorUsage.ConnectionCount > 0 {
			usage.ConnectionCount = monitorUsage.ConnectionCount
			if m.limits.MaxConnections > 0 {
				usage.ConnectionPercent = float64(usage.ConnectionCount) / float64(m.limits.MaxConnections) * 100
			}
		}
		if monitorUsage.DiskUsedMB > 0 {
			usage.DiskUsedMB = monitorUsage.DiskUsedMB
			if m.limits.MaxDiskUsageMB > 0 {
				usage.DiskPercent = float64(usage.DiskUsedMB) / float64(m.limits.MaxDiskUsageMB) * 100
			}
		}
	}

	return usage, nil
}

func (m *Manager) checkResourceAlerts(usage ResourceUsage) []ResourceAlert {
	var alerts []ResourceAlert
	now := time.Now()

	// Memory alerts
	if usage.MemoryPercent > 90 {
		alerts = append(alerts, ResourceAlert{
			Type:         ResourceTypeMemory,
			Severity:     "critical",
			Message:      fmt.Sprintf("Memory usage critical: %.1f%%", usage.MemoryPercent),
			Threshold:    90.0,
			CurrentValue: usage.MemoryPercent,
			Timestamp:    now,
		})
	} else if usage.MemoryPercent > 80 {
		alerts = append(alerts, ResourceAlert{
			Type:         ResourceTypeMemory,
			Severity:     "warning",
			Message:      fmt.Sprintf("Memory usage high: %.1f%%", usage.MemoryPercent),
			Threshold:    80.0,
			CurrentValue: usage.MemoryPercent,
			Timestamp:    now,
		})
	}

	// Goroutine alerts
	if usage.GoroutinePercent > 90 {
		alerts = append(alerts, ResourceAlert{
			Type:         ResourceTypeGoroutines,
			Severity:     "critical",
			Message:      fmt.Sprintf("Goroutine usage critical: %.1f%% (%d)", usage.GoroutinePercent, usage.GoroutineCount),
			Threshold:    90.0,
			CurrentValue: usage.GoroutinePercent,
			Timestamp:    now,
		})
	} else if usage.GoroutinePercent > 80 {
		alerts = append(alerts, ResourceAlert{
			Type:         ResourceTypeGoroutines,
			Severity:     "warning",
			Message:      fmt.Sprintf("Goroutine usage high: %.1f%% (%d)", usage.GoroutinePercent, usage.GoroutineCount),
			Threshold:    80.0,
			CurrentValue: usage.GoroutinePercent,
			Timestamp:    now,
		})
	}

	return alerts
}

func (m *Manager) dispatchAlert(alert ResourceAlert) {
	m.logger.Warn("resource alert", 
		"type", alert.Type,
		"severity", alert.Severity,
		"message", alert.Message,
		"current_value", alert.CurrentValue,
		"threshold", alert.Threshold)

	m.mu.RLock()
	callbacks := m.alertCallbacks
	m.mu.RUnlock()

	for _, callback := range callbacks {
		go func(cb func(ResourceAlert)) {
			defer func() {
				if r := recover(); r != nil {
					m.logger.Error("alert callback panicked", "error", r)
				}
			}()
			cb(alert)
		}(callback)
	}
}