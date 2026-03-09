package resource

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	
	t.Run("NewManager", func(t *testing.T) {
		limits := ResourceLimits{
			MaxMemoryMB:    1024,
			MaxGoroutines:  100,
			MaxConnections: 50,
		}
		
		manager := NewManager(limits, logger)
		assert.NotNil(t, manager)
		assert.True(t, manager.IsEnabled())
		assert.Equal(t, limits, manager.limits)
	})
	
	t.Run("RegisterAndGetPool", func(t *testing.T) {
		manager := NewManager(ResourceLimits{}, logger)
		
		mockPool := &MockResourcePool{}
		err := manager.RegisterPool("test", mockPool)
		require.NoError(t, err)
		
		retrieved, err := manager.GetPool("test")
		require.NoError(t, err)
		assert.Equal(t, mockPool, retrieved)
		
		// Test duplicate registration
		err = manager.RegisterPool("test", mockPool)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
		
		// Test non-existent pool
		_, err = manager.GetPool("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
	
	t.Run("ResourceUsageCollection", func(t *testing.T) {
		manager := NewManager(ResourceLimits{
			MaxMemoryMB:   1024,
			MaxGoroutines: 100,
		}, logger)
		
		ctx := context.Background()
		usage, err := manager.GetCurrentUsage(ctx)
		require.NoError(t, err)
		
		assert.True(t, usage.MemoryUsedMB >= 0) // Could be 0 in tests
		assert.True(t, usage.GoroutineCount > 0)
		assert.NotZero(t, usage.Timestamp)
		assert.True(t, usage.MemoryPercent >= 0)
		assert.True(t, usage.GoroutinePercent >= 0)
	})
	
	t.Run("CheckResourceAvailability", func(t *testing.T) {
		manager := NewManager(ResourceLimits{
			MaxMemoryMB:   100,
			MaxGoroutines: 10,
		}, logger)
		
		ctx := context.Background()
		
		// Should succeed with reasonable requirements
		err := manager.CheckResourceAvailability(ctx, ResourceRequirements{
			Memory:     10,
			Goroutines: 1,
		})
		assert.NoError(t, err)
		
		// Should fail with excessive requirements
		err = manager.CheckResourceAvailability(ctx, ResourceRequirements{
			Memory:     1000, // More than MaxMemoryMB
			Goroutines: 1,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient memory")
	})
	
	t.Run("DisabledManager", func(t *testing.T) {
		manager := NewManager(ResourceLimits{}, logger)
		manager.SetEnabled(false)
		
		assert.False(t, manager.IsEnabled())
		
		// Operations should not fail when disabled
		ctx := context.Background()
		_, err := manager.GetCurrentUsage(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disabled")
		
		err = manager.CheckResourceAvailability(ctx, ResourceRequirements{
			Memory: 1000,
		})
		assert.NoError(t, err) // Should not check when disabled
	})
	
	t.Run("StartAndStop", func(t *testing.T) {
		manager := NewManager(ResourceLimits{}, logger)
		
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		
		err := manager.Start(ctx)
		assert.NoError(t, err)
		
		// Wait a bit for monitoring to start
		time.Sleep(100 * time.Millisecond)
		
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		
		err = manager.Stop(stopCtx)
		assert.NoError(t, err)
	})
	
	t.Run("AlertCallbacks", func(t *testing.T) {
		manager := NewManager(ResourceLimits{}, logger)
		
		var receivedAlert ResourceAlert
		manager.RegisterAlertCallback(func(alert ResourceAlert) {
			receivedAlert = alert
		})
		
		// Simulate alert
		testAlert := ResourceAlert{
			Type:         ResourceTypeMemory,
			Severity:     "warning",
			Message:      "Test alert",
			Threshold:    80.0,
			CurrentValue: 85.0,
			Timestamp:    time.Now(),
		}
		
		manager.dispatchAlert(testAlert)
		
		// Give callback time to execute
		time.Sleep(100 * time.Millisecond)
		
		assert.Equal(t, testAlert.Type, receivedAlert.Type)
		assert.Equal(t, testAlert.Severity, receivedAlert.Severity)
		assert.Equal(t, testAlert.Message, receivedAlert.Message)
	})
}

func TestResourceManagerIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	
	t.Run("WithDegradationController", func(t *testing.T) {
		manager := NewManager(ResourceLimits{
			MaxMemoryMB:   1024,
			MaxGoroutines: 100,
		}, logger)
		
		degrader := NewDegradationController(DegradationThresholds{
			MemoryThreshold:    0.8,
			GoroutineThreshold: 0.9,
		}, logger)
		
		manager.SetDegradationController(degrader)
		
		ctx := context.Background()
		shouldDegrade, resourceTypes, err := manager.ShouldDegrade(ctx)
		require.NoError(t, err)
		
		// Note: degradation depends on actual resource usage, so we can't guarantee
		// specific behavior without mocking. Just verify the call doesn't error.
		if shouldDegrade {
			t.Logf("Degradation triggered for resource types: %v", resourceTypes)
		}
	})
	
	t.Run("WithMonitors", func(t *testing.T) {
		manager := NewManager(ResourceLimits{}, logger)
		
		mockMonitor := &MockResourceMonitor{}
		manager.RegisterMonitor(mockMonitor)
		
		ctx := context.Background()
		usage, err := manager.GetCurrentUsage(ctx)
		require.NoError(t, err)
		
		assert.True(t, mockMonitor.WasCalled)
		assert.NotZero(t, usage.Timestamp)
	})
}

// MockResourcePool is a test implementation of ResourcePool
type MockResourcePool struct {
	acquired []interface{}
	health   PoolHealth
}

func (m *MockResourcePool) Acquire(ctx context.Context, priority Priority) (interface{}, error) {
	resource := "mock-resource"
	m.acquired = append(m.acquired, resource)
	m.health.Active++
	return resource, nil
}

func (m *MockResourcePool) Release(resource interface{}) {
	for i, r := range m.acquired {
		if r == resource {
			m.acquired = append(m.acquired[:i], m.acquired[i+1:]...)
			m.health.Active--
			break
		}
	}
}

func (m *MockResourcePool) Health() PoolHealth {
	return m.health
}

func (m *MockResourcePool) Close() error {
	m.acquired = nil
	m.health = PoolHealth{}
	return nil
}

// MockResourceMonitor is a test implementation of ResourceMonitor
type MockResourceMonitor struct {
	WasCalled bool
}

func (m *MockResourceMonitor) Monitor(ctx context.Context) (ResourceUsage, error) {
	m.WasCalled = true
	return ResourceUsage{
		ConnectionCount: 5,
		DiskUsedMB:      100,
		Timestamp:       time.Now(),
	}, nil
}

func (m *MockResourceMonitor) SetThresholds(thresholds map[ResourceType]float64) error {
	return nil
}