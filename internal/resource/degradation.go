package resource

import (
	"context"
	"fmt"
	"log/slog"
)

// DegradationController manages graceful degradation strategies
type DegradationController struct {
	strategies map[ResourceType][]DegradationStrategy
	thresholds DegradationThresholds
	logger     *slog.Logger
}

// NewDegradationController creates a new degradation controller
func NewDegradationController(thresholds DegradationThresholds, logger *slog.Logger) *DegradationController {
	controller := &DegradationController{
		strategies: make(map[ResourceType][]DegradationStrategy),
		thresholds: thresholds,
		logger:     logger,
	}

	// Register default strategies
	controller.registerDefaultStrategies()
	
	return controller
}

// RegisterStrategy adds a degradation strategy for a resource type
func (dc *DegradationController) RegisterStrategy(resourceType ResourceType, strategy DegradationStrategy) {
	if dc.strategies[resourceType] == nil {
		dc.strategies[resourceType] = []DegradationStrategy{}
	}
	dc.strategies[resourceType] = append(dc.strategies[resourceType], strategy)
	
	dc.logger.Info("registered degradation strategy",
		"resource_type", resourceType,
		"strategy", strategy.GetName())
}

// ShouldDegrade checks if degradation should be applied based on resource usage
func (dc *DegradationController) ShouldDegrade(usage ResourceUsage, limits ResourceLimits) (bool, []ResourceType, error) {
	var resourceTypes []ResourceType

	// Check memory threshold
	if usage.MemoryPercent >= dc.thresholds.MemoryThreshold*100 {
		resourceTypes = append(resourceTypes, ResourceTypeMemory)
	}

	// Check goroutine threshold
	if usage.GoroutinePercent >= dc.thresholds.GoroutineThreshold*100 {
		resourceTypes = append(resourceTypes, ResourceTypeGoroutines)
	}

	// Check disk threshold
	if usage.DiskPercent >= dc.thresholds.DiskThreshold*100 {
		resourceTypes = append(resourceTypes, ResourceTypeDiskSpace)
	}

	// Check connection threshold
	if usage.ConnectionPercent >= dc.thresholds.ConnectionThreshold*100 {
		resourceTypes = append(resourceTypes, ResourceTypeConnections)
	}

	shouldDegrade := len(resourceTypes) > 0
	return shouldDegrade, resourceTypes, nil
}

// ApplyDegradation applies appropriate degradation strategies to an operation
func (dc *DegradationController) ApplyDegradation(ctx context.Context, operation Operation, usage ResourceUsage, limits ResourceLimits) (Operation, error) {
	shouldDegrade, resourceTypes, err := dc.ShouldDegrade(usage, limits)
	if err != nil {
		return operation, fmt.Errorf("check degradation: %w", err)
	}

	if !shouldDegrade {
		return operation, nil
	}

	dc.logger.Warn("applying degradation strategies", 
		"resource_types", resourceTypes,
		"memory_percent", usage.MemoryPercent,
		"goroutine_percent", usage.GoroutinePercent)

	degradedOperation := operation
	
	// Apply strategies for each resource type that needs degradation
	for _, resourceType := range resourceTypes {
		strategies := dc.strategies[resourceType]
		for _, strategy := range strategies {
			if strategy.ShouldDegrade(usage, limits) {
				degradedOperation = strategy.ApplyDegradation(ctx, degradedOperation)
				dc.logger.Info("applied degradation strategy",
					"resource_type", resourceType,
					"strategy", strategy.GetName())
			}
		}
	}

	return degradedOperation, nil
}

// GetStrategies returns all registered strategies
func (dc *DegradationController) GetStrategies() map[ResourceType][]DegradationStrategy {
	return dc.strategies
}

func (dc *DegradationController) registerDefaultStrategies() {
	// Memory degradation strategies
	dc.RegisterStrategy(ResourceTypeMemory, &ReduceConcurrencyStrategy{
		name:         "reduce_concurrency",
		resourceType: ResourceTypeMemory,
		reductionFactor: 0.5,
		logger:       dc.logger,
	})
	
	dc.RegisterStrategy(ResourceTypeMemory, &SkipValidationStrategy{
		name:         "skip_validation",
		resourceType: ResourceTypeMemory,
		logger:       dc.logger,
	})

	// Goroutine degradation strategies
	dc.RegisterStrategy(ResourceTypeGoroutines, &ReduceConcurrencyStrategy{
		name:         "reduce_concurrency_goroutines",
		resourceType: ResourceTypeGoroutines,
		reductionFactor: 0.3,
		logger:       dc.logger,
	})

	// Emergency stop strategy for critical resource exhaustion
	emergencyStop := &EmergencyStopStrategy{
		name:         "emergency_stop",
		resourceType: ResourceTypeMemory,
		threshold:    0.95,
		logger:       dc.logger,
	}
	dc.RegisterStrategy(ResourceTypeMemory, emergencyStop)
	dc.RegisterStrategy(ResourceTypeGoroutines, emergencyStop)
}

// ReduceConcurrencyStrategy reduces operation concurrency when resources are constrained
type ReduceConcurrencyStrategy struct {
	name            string
	resourceType    ResourceType
	reductionFactor float64
	logger          *slog.Logger
}

func (s *ReduceConcurrencyStrategy) ShouldDegrade(usage ResourceUsage, limits ResourceLimits) bool {
	switch s.resourceType {
	case ResourceTypeMemory:
		return usage.MemoryPercent > 80
	case ResourceTypeGoroutines:
		return usage.GoroutinePercent > 80
	default:
		return false
	}
}

func (s *ReduceConcurrencyStrategy) ApplyDegradation(ctx context.Context, operation Operation) Operation {
	// Wrap the operation to reduce concurrency
	return &DegradedOperation{
		originalOp:      operation,
		strategy:        s.name,
		concurrencyFactor: s.reductionFactor,
		logger:          s.logger,
	}
}

func (s *ReduceConcurrencyStrategy) GetResourceType() ResourceType {
	return s.resourceType
}

func (s *ReduceConcurrencyStrategy) GetName() string {
	return s.name
}

// SkipValidationStrategy skips non-critical validation when memory is constrained
type SkipValidationStrategy struct {
	name         string
	resourceType ResourceType
	logger       *slog.Logger
}

func (s *SkipValidationStrategy) ShouldDegrade(usage ResourceUsage, limits ResourceLimits) bool {
	return usage.MemoryPercent > 85
}

func (s *SkipValidationStrategy) ApplyDegradation(ctx context.Context, operation Operation) Operation {
	return &DegradedOperation{
		originalOp:     operation,
		strategy:       s.name,
		skipValidation: true,
		logger:         s.logger,
	}
}

func (s *SkipValidationStrategy) GetResourceType() ResourceType {
	return s.resourceType
}

func (s *SkipValidationStrategy) GetName() string {
	return s.name
}

// EmergencyStopStrategy stops operations when resource usage becomes critical
type EmergencyStopStrategy struct {
	name         string
	resourceType ResourceType
	threshold    float64
	logger       *slog.Logger
}

func (s *EmergencyStopStrategy) ShouldDegrade(usage ResourceUsage, limits ResourceLimits) bool {
	switch s.resourceType {
	case ResourceTypeMemory:
		return usage.MemoryPercent > s.threshold*100
	case ResourceTypeGoroutines:
		return usage.GoroutinePercent > s.threshold*100
	default:
		return false
	}
}

func (s *EmergencyStopStrategy) ApplyDegradation(ctx context.Context, operation Operation) Operation {
	return &DegradedOperation{
		originalOp:    operation,
		strategy:      s.name,
		emergencyStop: true,
		logger:        s.logger,
	}
}

func (s *EmergencyStopStrategy) GetResourceType() ResourceType {
	return s.resourceType
}

func (s *EmergencyStopStrategy) GetName() string {
	return s.name
}

// DegradedOperation wraps an original operation with degradation behavior
type DegradedOperation struct {
	originalOp        Operation
	strategy          string
	concurrencyFactor float64
	skipValidation    bool
	emergencyStop     bool
	logger            *slog.Logger
}

func (d *DegradedOperation) GetPriority() Priority {
	return d.originalOp.GetPriority()
}

func (d *DegradedOperation) GetResourceRequirements() ResourceRequirements {
	requirements := d.originalOp.GetResourceRequirements()
	
	// Apply degradation to requirements
	if d.concurrencyFactor > 0 && d.concurrencyFactor < 1.0 {
		requirements.Goroutines = int(float64(requirements.Goroutines) * d.concurrencyFactor)
		requirements.Memory = int64(float64(requirements.Memory) * d.concurrencyFactor)
		if requirements.Goroutines < 1 {
			requirements.Goroutines = 1
		}
	}
	
	return requirements
}

func (d *DegradedOperation) Execute(ctx context.Context) error {
	if d.emergencyStop {
		d.logger.Error("operation stopped due to emergency resource condition", "strategy", d.strategy)
		return fmt.Errorf("operation stopped due to emergency resource condition")
	}
	
	d.logger.Info("executing degraded operation", 
		"strategy", d.strategy,
		"skip_validation", d.skipValidation,
		"concurrency_factor", d.concurrencyFactor)
	
	// Execute the original operation (potentially with modified behavior)
	return d.originalOp.Execute(ctx)
}