package resource

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDegradationController(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("NewDegradationController", func(t *testing.T) {
		thresholds := DegradationThresholds{
			MemoryThreshold:    0.8,
			GoroutineThreshold: 0.9,
		}

		controller := NewDegradationController(thresholds, logger)
		assert.NotNil(t, controller)
		assert.Equal(t, thresholds, controller.thresholds)

		// Should have default strategies registered
		strategies := controller.GetStrategies()
		assert.Contains(t, strategies, ResourceTypeMemory)
		assert.True(t, len(strategies[ResourceTypeMemory]) > 0)
	})

	t.Run("RegisterStrategy", func(t *testing.T) {
		controller := NewDegradationController(DegradationThresholds{}, logger)

		customStrategy := &MockDegradationStrategy{
			name:         "test_strategy",
			resourceType: ResourceTypeMemory,
		}

		controller.RegisterStrategy(ResourceTypeMemory, customStrategy)

		strategies := controller.GetStrategies()
		found := false
		for _, strategy := range strategies[ResourceTypeMemory] {
			if strategy.GetName() == "test_strategy" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("ShouldDegrade", func(t *testing.T) {
		thresholds := DegradationThresholds{
			MemoryThreshold:     0.8,
			GoroutineThreshold:  0.9,
			DiskThreshold:       0.85,
			ConnectionThreshold: 0.75,
		}

		controller := NewDegradationController(thresholds, logger)
		limits := ResourceLimits{
			MaxMemoryMB:    1024,
			MaxGoroutines:  100,
			MaxConnections: 50,
		}

		// Normal usage - should not degrade
		normalUsage := ResourceUsage{
			MemoryPercent:     70.0,
			GoroutinePercent:  60.0,
			DiskPercent:       50.0,
			ConnectionPercent: 40.0,
		}

		shouldDegrade, resourceTypes, err := controller.ShouldDegrade(normalUsage, limits)
		require.NoError(t, err)
		assert.False(t, shouldDegrade)
		assert.Empty(t, resourceTypes)

		// High memory usage - should degrade
		highMemoryUsage := ResourceUsage{
			MemoryPercent:     85.0, // Above 80% threshold
			GoroutinePercent:  60.0,
			DiskPercent:       50.0,
			ConnectionPercent: 40.0,
		}

		shouldDegrade, resourceTypes, err = controller.ShouldDegrade(highMemoryUsage, limits)
		require.NoError(t, err)
		assert.True(t, shouldDegrade)
		assert.Contains(t, resourceTypes, ResourceTypeMemory)

		// Multiple resource pressure
		highUsage := ResourceUsage{
			MemoryPercent:     85.0, // Above threshold
			GoroutinePercent:  95.0, // Above threshold
			DiskPercent:       90.0, // Above threshold
			ConnectionPercent: 80.0, // Above threshold
		}

		shouldDegrade, resourceTypes, err = controller.ShouldDegrade(highUsage, limits)
		require.NoError(t, err)
		assert.True(t, shouldDegrade)
		assert.Contains(t, resourceTypes, ResourceTypeMemory)
		assert.Contains(t, resourceTypes, ResourceTypeGoroutines)
		assert.Contains(t, resourceTypes, ResourceTypeDiskSpace)
		assert.Contains(t, resourceTypes, ResourceTypeConnections)
	})

	t.Run("ApplyDegradation", func(t *testing.T) {
		controller := NewDegradationController(DegradationThresholds{
			MemoryThreshold: 0.8,
		}, logger)

		originalOp := &MockOperation{
			priority: PriorityNormal,
			requirements: ResourceRequirements{
				Memory:     100,
				Goroutines: 4,
			},
		}

		// Usage below threshold - no degradation
		normalUsage := ResourceUsage{
			MemoryPercent: 70.0,
		}

		degradedOp, err := controller.ApplyDegradation(context.Background(), originalOp, normalUsage, ResourceLimits{})
		require.NoError(t, err)
		assert.Equal(t, originalOp, degradedOp) // Should be unchanged

		// Usage above threshold - degradation applied
		highUsage := ResourceUsage{
			MemoryPercent: 85.0,
		}

		degradedOp, err = controller.ApplyDegradation(context.Background(), originalOp, highUsage, ResourceLimits{})
		require.NoError(t, err)
		assert.NotEqual(t, originalOp, degradedOp) // Should be wrapped
		assert.IsType(t, &DegradedOperation{}, degradedOp)
	})
}

func TestDegradationStrategies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("ReduceConcurrencyStrategy", func(t *testing.T) {
		strategy := &ReduceConcurrencyStrategy{
			name:            "reduce_concurrency",
			resourceType:    ResourceTypeMemory,
			reductionFactor: 0.5,
			logger:          logger,
		}

		// Should degrade when memory usage is high
		highUsage := ResourceUsage{MemoryPercent: 85.0}
		assert.True(t, strategy.ShouldDegrade(highUsage, ResourceLimits{}))

		// Should not degrade when memory usage is normal
		normalUsage := ResourceUsage{MemoryPercent: 70.0}
		assert.False(t, strategy.ShouldDegrade(normalUsage, ResourceLimits{}))

		// Apply degradation
		originalOp := &MockOperation{
			requirements: ResourceRequirements{
				Memory:     100,
				Goroutines: 4,
			},
		}

		degradedOp := strategy.ApplyDegradation(context.Background(), originalOp)
		assert.IsType(t, &DegradedOperation{}, degradedOp)

		degraded := degradedOp.(*DegradedOperation)
		assert.Equal(t, 0.5, degraded.concurrencyFactor)
	})

	t.Run("SkipValidationStrategy", func(t *testing.T) {
		strategy := &SkipValidationStrategy{
			name:         "skip_validation",
			resourceType: ResourceTypeMemory,
			logger:       logger,
		}

		// Should degrade when memory usage is very high
		highUsage := ResourceUsage{MemoryPercent: 90.0}
		assert.True(t, strategy.ShouldDegrade(highUsage, ResourceLimits{}))

		// Should not degrade when memory usage is normal
		normalUsage := ResourceUsage{MemoryPercent: 80.0}
		assert.False(t, strategy.ShouldDegrade(normalUsage, ResourceLimits{}))

		// Apply degradation
		originalOp := &MockOperation{}
		degradedOp := strategy.ApplyDegradation(context.Background(), originalOp)
		assert.IsType(t, &DegradedOperation{}, degradedOp)

		degraded := degradedOp.(*DegradedOperation)
		assert.True(t, degraded.skipValidation)
	})

	t.Run("EmergencyStopStrategy", func(t *testing.T) {
		strategy := &EmergencyStopStrategy{
			name:         "emergency_stop",
			resourceType: ResourceTypeMemory,
			threshold:    0.95,
			logger:       logger,
		}

		// Should degrade when usage exceeds threshold
		criticalUsage := ResourceUsage{MemoryPercent: 96.0}
		assert.True(t, strategy.ShouldDegrade(criticalUsage, ResourceLimits{}))

		// Should not degrade when usage is below threshold
		highUsage := ResourceUsage{MemoryPercent: 90.0}
		assert.False(t, strategy.ShouldDegrade(highUsage, ResourceLimits{}))

		// Apply degradation
		originalOp := &MockOperation{}
		degradedOp := strategy.ApplyDegradation(context.Background(), originalOp)
		assert.IsType(t, &DegradedOperation{}, degradedOp)

		degraded := degradedOp.(*DegradedOperation)
		assert.True(t, degraded.emergencyStop)
	})
}

func TestDegradedOperation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("ConcurrencyReduction", func(t *testing.T) {
		originalOp := &MockOperation{
			priority: PriorityNormal,
			requirements: ResourceRequirements{
				Memory:     100,
				Goroutines: 4,
			},
		}

		degraded := &DegradedOperation{
			originalOp:        originalOp,
			concurrencyFactor: 0.5,
			logger:            logger,
		}

		// Priority should be preserved
		assert.Equal(t, PriorityNormal, degraded.GetPriority())

		// Resource requirements should be reduced
		requirements := degraded.GetResourceRequirements()
		assert.Equal(t, int64(50), requirements.Memory)    // 100 * 0.5
		assert.Equal(t, 2, requirements.Goroutines)        // 4 * 0.5
	})

	t.Run("EmergencyStop", func(t *testing.T) {
		originalOp := &MockOperation{}

		degraded := &DegradedOperation{
			originalOp:    originalOp,
			emergencyStop: true,
			strategy:      "emergency_stop",
			logger:        logger,
		}

		err := degraded.Execute(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "emergency resource condition")
	})

	t.Run("NormalExecution", func(t *testing.T) {
		originalOp := &MockOperation{}

		degraded := &DegradedOperation{
			originalOp:     originalOp,
			skipValidation: true,
			strategy:       "skip_validation",
			logger:         logger,
		}

		err := degraded.Execute(context.Background())
		assert.NoError(t, err)
		assert.True(t, originalOp.Executed)
	})
}

// MockDegradationStrategy is a test implementation of DegradationStrategy
type MockDegradationStrategy struct {
	name         string
	resourceType ResourceType
	shouldDegrade bool
}

func (m *MockDegradationStrategy) ShouldDegrade(usage ResourceUsage, limits ResourceLimits) bool {
	return m.shouldDegrade
}

func (m *MockDegradationStrategy) ApplyDegradation(ctx context.Context, operation Operation) Operation {
	return operation
}

func (m *MockDegradationStrategy) GetResourceType() ResourceType {
	return m.resourceType
}

func (m *MockDegradationStrategy) GetName() string {
	return m.name
}

// MockOperation is a test implementation of Operation
type MockOperation struct {
	priority     Priority
	requirements ResourceRequirements
	Executed     bool
}

func (m *MockOperation) GetPriority() Priority {
	return m.priority
}

func (m *MockOperation) GetResourceRequirements() ResourceRequirements {
	return m.requirements
}

func (m *MockOperation) Execute(ctx context.Context) error {
	m.Executed = true
	return nil
}