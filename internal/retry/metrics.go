package retry

import (
	"context"
	"sync"
	"time"
)

// RetryMetrics contains metrics for retry operations.
type RetryMetrics struct {
	Operation        string        `json:"operation"`
	TotalAttempts    int64         `json:"total_attempts"`
	SuccessfulRetries int64        `json:"successful_retries"`
	FailedRetries    int64         `json:"failed_retries"`
	AverageDelay     time.Duration `json:"average_delay"`
	MaxDelay         time.Duration `json:"max_delay"`
	MinDelay         time.Duration `json:"min_delay"`
	TotalDelay       time.Duration `json:"total_delay"`
	SuccessRate      float64       `json:"success_rate"`
	LastAttempt      time.Time     `json:"last_attempt"`
}

// DetailedCircuitBreakerMetrics contains detailed metrics for circuit breaker operations.
type DetailedCircuitBreakerMetrics struct {
	Name                string        `json:"name"`
	State               State         `json:"state"`
	RequestCount        int64         `json:"request_count"`
	SuccessCount        int64         `json:"success_count"`
	FailureCount        int64         `json:"failure_count"`
	SuccessRate         float64       `json:"success_rate"`
	FailureRate         float64       `json:"failure_rate"`
	StateTransitions    int64         `json:"state_transitions"`
	LastFailureTime     time.Time     `json:"last_failure_time"`
	LastSuccessTime     time.Time     `json:"last_success_time"`
	LastStateChange     time.Time     `json:"last_state_change"`
	TotalOpenDuration   time.Duration `json:"total_open_duration"`
	OpenCount           int64         `json:"open_count"`
	HalfOpenCount       int64         `json:"half_open_count"`
}

// AggregatedMetrics contains system-wide retry and circuit breaker metrics.
type AggregatedMetrics struct {
	RetryMetrics          map[string]RetryMetrics                    `json:"retry_metrics"`
	CircuitBreakerMetrics map[string]DetailedCircuitBreakerMetrics  `json:"circuit_breaker_metrics"`
	GeneratedAt           time.Time                                  `json:"generated_at"`
	TotalRetryAttempts    int64                                     `json:"total_retry_attempts"`
	TotalSuccessfulOps    int64                                     `json:"total_successful_operations"`
	TotalFailedOps        int64                                     `json:"total_failed_operations"`
	OverallSuccessRate    float64                                   `json:"overall_success_rate"`
}

// MetricsCollector collects and aggregates retry and circuit breaker metrics.
type MetricsCollector struct {
	mu                    sync.RWMutex
	retryMetrics          map[string]*retryMetricsInternal
	circuitBreakerMetrics map[string]*circuitBreakerMetricsInternal
}

// retryMetricsInternal holds internal state for retry metrics calculation.
type retryMetricsInternal struct {
	Operation         string
	TotalAttempts     int64
	SuccessfulRetries int64
	FailedRetries     int64
	TotalDelay        time.Duration
	MaxDelay          time.Duration
	MinDelay          time.Duration
	LastAttempt       time.Time
}

// circuitBreakerMetricsInternal holds internal state for circuit breaker metrics.
type circuitBreakerMetricsInternal struct {
	Name                string
	State               State
	RequestCount        int64
	SuccessCount        int64
	FailureCount        int64
	StateTransitions    int64
	LastFailureTime     time.Time
	LastSuccessTime     time.Time
	LastStateChange     time.Time
	TotalOpenDuration   time.Duration
	OpenCount           int64
	HalfOpenCount       int64
	openStartTime       time.Time
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		retryMetrics:          make(map[string]*retryMetricsInternal),
		circuitBreakerMetrics: make(map[string]*circuitBreakerMetricsInternal),
	}
}

// RecordRetryAttempt records a retry attempt for an operation.
func (mc *MetricsCollector) RecordRetryAttempt(operation string, attempt int, delay time.Duration, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics, exists := mc.retryMetrics[operation]
	if !exists {
		metrics = &retryMetricsInternal{
			Operation: operation,
			MinDelay:  delay,
			MaxDelay:  delay,
		}
		mc.retryMetrics[operation] = metrics
	}

	metrics.TotalAttempts++
	metrics.TotalDelay += delay
	metrics.LastAttempt = time.Now()

	if delay > metrics.MaxDelay {
		metrics.MaxDelay = delay
	}
	if delay < metrics.MinDelay || metrics.MinDelay == 0 {
		metrics.MinDelay = delay
	}

	if success {
		metrics.SuccessfulRetries++
	} else {
		metrics.FailedRetries++
	}
}

// RecordCircuitBreakerEvent records a circuit breaker event.
func (mc *MetricsCollector) RecordCircuitBreakerEvent(name string, state State, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics, exists := mc.circuitBreakerMetrics[name]
	if !exists {
		metrics = &circuitBreakerMetricsInternal{
			Name:  name,
			State: state,
		}
		mc.circuitBreakerMetrics[name] = metrics
	}

	// Record state change if different
	if metrics.State != state {
		metrics.StateTransitions++
		metrics.LastStateChange = time.Now()
		
		// Handle state-specific metrics
		if metrics.State == StateOpen && metrics.openStartTime.IsZero() == false {
			metrics.TotalOpenDuration += time.Since(metrics.openStartTime)
		}
		
		switch state {
		case StateOpen:
			metrics.OpenCount++
			metrics.openStartTime = time.Now()
		case StateHalfOpen:
			metrics.HalfOpenCount++
		}
		
		metrics.State = state
	}

	metrics.RequestCount++
	if success {
		metrics.SuccessCount++
		metrics.LastSuccessTime = time.Now()
	} else {
		metrics.FailureCount++
		metrics.LastFailureTime = time.Now()
	}
}

// GetRetryMetrics returns retry metrics for all operations.
func (mc *MetricsCollector) GetRetryMetrics() map[string]RetryMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]RetryMetrics, len(mc.retryMetrics))
	for operation, internal := range mc.retryMetrics {
		var avgDelay time.Duration
		if internal.TotalAttempts > 0 {
			avgDelay = time.Duration(int64(internal.TotalDelay) / internal.TotalAttempts)
		}

		var successRate float64
		if internal.TotalAttempts > 0 {
			successRate = float64(internal.SuccessfulRetries) / float64(internal.TotalAttempts)
		}

		result[operation] = RetryMetrics{
			Operation:         internal.Operation,
			TotalAttempts:     internal.TotalAttempts,
			SuccessfulRetries: internal.SuccessfulRetries,
			FailedRetries:     internal.FailedRetries,
			AverageDelay:      avgDelay,
			MaxDelay:          internal.MaxDelay,
			MinDelay:          internal.MinDelay,
			TotalDelay:        internal.TotalDelay,
			SuccessRate:       successRate,
			LastAttempt:       internal.LastAttempt,
		}
	}

	return result
}

// GetCircuitBreakerMetrics returns circuit breaker metrics for all breakers.
func (mc *MetricsCollector) GetCircuitBreakerMetrics() map[string]DetailedCircuitBreakerMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]DetailedCircuitBreakerMetrics, len(mc.circuitBreakerMetrics))
	for name, internal := range mc.circuitBreakerMetrics {
		var successRate, failureRate float64
		if internal.RequestCount > 0 {
			successRate = float64(internal.SuccessCount) / float64(internal.RequestCount)
			failureRate = float64(internal.FailureCount) / float64(internal.RequestCount)
		}

		// Calculate total open duration including current open period
		totalOpenDuration := internal.TotalOpenDuration
		if internal.State == StateOpen && !internal.openStartTime.IsZero() {
			totalOpenDuration += time.Since(internal.openStartTime)
		}

		result[name] = DetailedCircuitBreakerMetrics{
			Name:                internal.Name,
			State:               internal.State,
			RequestCount:        internal.RequestCount,
			SuccessCount:        internal.SuccessCount,
			FailureCount:        internal.FailureCount,
			SuccessRate:         successRate,
			FailureRate:         failureRate,
			StateTransitions:    internal.StateTransitions,
			LastFailureTime:     internal.LastFailureTime,
			LastSuccessTime:     internal.LastSuccessTime,
			LastStateChange:     internal.LastStateChange,
			TotalOpenDuration:   totalOpenDuration,
			OpenCount:           internal.OpenCount,
			HalfOpenCount:       internal.HalfOpenCount,
		}
	}

	return result
}

// GetAggregatedMetrics returns system-wide aggregated metrics.
func (mc *MetricsCollector) GetAggregatedMetrics() AggregatedMetrics {
	retryMetrics := mc.GetRetryMetrics()
	circuitBreakerMetrics := mc.GetCircuitBreakerMetrics()

	var totalRetryAttempts, totalSuccessful, totalFailed int64
	for _, metrics := range retryMetrics {
		totalRetryAttempts += metrics.TotalAttempts
		totalSuccessful += metrics.SuccessfulRetries
		totalFailed += metrics.FailedRetries
	}

	var overallSuccessRate float64
	if totalRetryAttempts > 0 {
		overallSuccessRate = float64(totalSuccessful) / float64(totalRetryAttempts)
	}

	return AggregatedMetrics{
		RetryMetrics:          retryMetrics,
		CircuitBreakerMetrics: circuitBreakerMetrics,
		GeneratedAt:           time.Now(),
		TotalRetryAttempts:    totalRetryAttempts,
		TotalSuccessfulOps:    totalSuccessful,
		TotalFailedOps:        totalFailed,
		OverallSuccessRate:    overallSuccessRate,
	}
}

// Reset clears all metrics (useful for testing).
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.retryMetrics = make(map[string]*retryMetricsInternal)
	mc.circuitBreakerMetrics = make(map[string]*circuitBreakerMetricsInternal)
}

// GetOperationMetrics returns metrics for a specific operation.
func (mc *MetricsCollector) GetOperationMetrics(operation string) (RetryMetrics, bool) {
	allMetrics := mc.GetRetryMetrics()
	metrics, exists := allMetrics[operation]
	return metrics, exists
}

// GetCircuitBreakerMetricsByName returns metrics for a specific circuit breaker.
func (mc *MetricsCollector) GetCircuitBreakerMetricsByName(name string) (DetailedCircuitBreakerMetrics, bool) {
	allMetrics := mc.GetCircuitBreakerMetrics()
	metrics, exists := allMetrics[name]
	return metrics, exists
}

// HealthStatus represents the health status of the retry system.
type HealthStatus struct {
	Healthy                 bool      `json:"healthy"`
	OverallSuccessRate      float64   `json:"overall_success_rate"`
	CircuitBreakersOpen     int       `json:"circuit_breakers_open"`
	TotalCircuitBreakers    int       `json:"total_circuit_breakers"`
	HighFailureRateOps      []string  `json:"high_failure_rate_operations"`
	LastMetricsUpdate       time.Time `json:"last_metrics_update"`
	RecommendedActions      []string  `json:"recommended_actions"`
}

// GetHealthStatus returns the current health status of the retry system.
func (mc *MetricsCollector) GetHealthStatus() HealthStatus {
	aggregated := mc.GetAggregatedMetrics()
	
	var circuitBreakersOpen, totalCircuitBreakers int
	var highFailureRateOps []string
	var recommendedActions []string
	
	// Check circuit breaker states
	for _, cbMetrics := range aggregated.CircuitBreakerMetrics {
		totalCircuitBreakers++
		if cbMetrics.State == StateOpen {
			circuitBreakersOpen++
		}
	}
	
	// Check for operations with high failure rates
	for operation, metrics := range aggregated.RetryMetrics {
		if metrics.SuccessRate < 0.8 && metrics.TotalAttempts > 10 { // Less than 80% success rate with significant attempts
			highFailureRateOps = append(highFailureRateOps, operation)
		}
	}
	
	// Generate recommended actions
	if circuitBreakersOpen > 0 {
		recommendedActions = append(recommendedActions, "Investigate services with open circuit breakers")
	}
	if len(highFailureRateOps) > 0 {
		recommendedActions = append(recommendedActions, "Review retry policies for high failure rate operations")
	}
	if aggregated.OverallSuccessRate < 0.9 {
		recommendedActions = append(recommendedActions, "Overall success rate is below 90%, consider system health review")
	}
	
	// Determine overall health
	healthy := aggregated.OverallSuccessRate >= 0.9 && 
			   circuitBreakersOpen == 0 && 
			   len(highFailureRateOps) == 0
	
	return HealthStatus{
		Healthy:               healthy,
		OverallSuccessRate:    aggregated.OverallSuccessRate,
		CircuitBreakersOpen:   circuitBreakersOpen,
		TotalCircuitBreakers:  totalCircuitBreakers,
		HighFailureRateOps:    highFailureRateOps,
		LastMetricsUpdate:     aggregated.GeneratedAt,
		RecommendedActions:    recommendedActions,
	}
}

// InstrumentedRetryPolicy wraps a RetryPolicy with metrics collection.
type InstrumentedRetryPolicy struct {
	*RetryPolicy
	collector *MetricsCollector
	operation string
}

// NewInstrumentedRetryPolicy creates a retry policy that collects metrics.
func NewInstrumentedRetryPolicy(policy *RetryPolicy, collector *MetricsCollector, operation string) *InstrumentedRetryPolicy {
	instrumented := &InstrumentedRetryPolicy{
		RetryPolicy: policy,
		collector:   collector,
		operation:   operation,
	}
	
	// Wrap the existing OnRetry callback to collect metrics
	originalOnRetry := policy.OnRetry
	instrumented.OnRetry = func(attempt int, err error) {
		// Calculate delay for this attempt
		delay := policy.CalculateDelay(attempt)
		instrumented.collector.RecordRetryAttempt(operation, attempt, delay, false)
		
		// Call original callback if it exists
		if originalOnRetry != nil {
			originalOnRetry(attempt, err)
		}
	}
	
	return instrumented
}

// ExecuteWithMetrics executes an operation and records success metrics.
func (irp *InstrumentedRetryPolicy) ExecuteWithMetrics(ctx context.Context, operation Operation[any]) (any, error) {
	start := time.Now()
	result, err := Execute(ctx, irp.RetryPolicy, operation)
	duration := time.Since(start)
	
	// Record final result
	irp.collector.RecordRetryAttempt(irp.operation, 1, duration, err == nil)
	
	return result, err
}