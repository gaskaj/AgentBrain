package retry

import (
	"fmt"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed indicates the circuit breaker is closed (normal operation).
	StateClosed State = iota
	// StateOpen indicates the circuit breaker is open (calls are rejected).
	StateOpen
	// StateHalfOpen indicates the circuit breaker is in half-open state (testing).
	StateHalfOpen
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// StateChangeFunc is called when the circuit breaker state changes.
type StateChangeFunc func(name string, from, to State)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name         string
	maxFailures  int
	resetTimeout time.Duration
	
	mu               sync.RWMutex
	state           State
	failureCount    int
	lastFailureTime time.Time
	lastSuccessTime time.Time
	requestCount    int
	successCount    int
	
	onStateChange StateChangeFunc
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:         name,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

// WithStateChangeCallback sets the state change callback.
func (cb *CircuitBreaker) WithStateChangeCallback(callback StateChangeFunc) *CircuitBreaker {
	cb.onStateChange = callback
	return cb
}

// Execute executes a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.canExecute() {
		return &CircuitBreakerError{
			Name:  cb.name,
			State: cb.GetState(),
		}
	}

	err := fn()
	cb.recordResult(err)
	return err
}

// canExecute determines if the circuit breaker allows execution.
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	state := cb.state
	lastFailureTime := cb.lastFailureTime
	cb.mu.RUnlock()

	switch state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if reset timeout has elapsed
		if time.Since(lastFailureTime) > cb.resetTimeout {
			cb.mu.Lock()
			// Double-check the state hasn't changed
			if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.resetTimeout {
				cb.setState(StateHalfOpen)
			}
			cb.mu.Unlock()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// recordResult records the result of an operation.
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.requestCount++

	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()
		
		// In half-open state, any failure immediately opens the circuit
		if cb.state == StateHalfOpen {
			cb.setState(StateOpen)
		} else if cb.state == StateClosed && cb.failureCount >= cb.maxFailures {
			cb.setState(StateOpen)
		}
	} else {
		cb.successCount++
		cb.lastSuccessTime = time.Now()
		
		// In half-open state, success closes the circuit
		if cb.state == StateHalfOpen {
			cb.failureCount = 0
			cb.setState(StateClosed)
		}
	}
}

// setState changes the circuit breaker state and notifies observers.
func (cb *CircuitBreaker) setState(newState State) {
	oldState := cb.state
	cb.state = newState
	
	if cb.onStateChange != nil {
		// Call the callback without holding the lock to avoid deadlocks
		go cb.onStateChange(cb.name, oldState, newState)
	}
}

// GetState returns the current state of the circuit breaker.
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns current circuit breaker metrics.
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	var failureRate float64
	if cb.requestCount > 0 {
		failureRate = float64(cb.failureCount) / float64(cb.requestCount)
	}
	
	var successRate float64
	if cb.requestCount > 0 {
		successRate = float64(cb.successCount) / float64(cb.requestCount)
	}

	return CircuitBreakerMetrics{
		Name:            cb.name,
		State:           cb.state,
		RequestCount:    cb.requestCount,
		FailureCount:    cb.failureCount,
		SuccessCount:    cb.successCount,
		FailureRate:     failureRate,
		SuccessRate:     successRate,
		LastFailureTime: cb.lastFailureTime,
		LastSuccessTime: cb.lastSuccessTime,
	}
}

// Reset resets the circuit breaker to its initial state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	oldState := cb.state
	cb.state = StateClosed
	cb.failureCount = 0
	cb.requestCount = 0
	cb.successCount = 0
	cb.lastFailureTime = time.Time{}
	cb.lastSuccessTime = time.Time{}
	
	if oldState != StateClosed && cb.onStateChange != nil {
		go cb.onStateChange(cb.name, oldState, StateClosed)
	}
}

// CircuitBreakerMetrics contains metrics for a circuit breaker.
type CircuitBreakerMetrics struct {
	Name            string    `json:"name"`
	State           State     `json:"state"`
	RequestCount    int       `json:"request_count"`
	FailureCount    int       `json:"failure_count"`
	SuccessCount    int       `json:"success_count"`
	FailureRate     float64   `json:"failure_rate"`
	SuccessRate     float64   `json:"success_rate"`
	LastFailureTime time.Time `json:"last_failure_time"`
	LastSuccessTime time.Time `json:"last_success_time"`
}

// CircuitBreakerError is returned when a circuit breaker rejects a call.
type CircuitBreakerError struct {
	Name  string
	State State
}

// Error implements the error interface.
func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker '%s' is %s", e.Name, e.State)
}

// IsCircuitBreakerError checks if an error is a circuit breaker error.
func IsCircuitBreakerError(err error) bool {
	_, ok := err.(*CircuitBreakerError)
	return ok
}

// CircuitBreakerRegistry manages multiple circuit breakers.
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
}

// NewCircuitBreakerRegistry creates a new circuit breaker registry.
func NewCircuitBreakerRegistry() *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// GetOrCreate gets an existing circuit breaker or creates a new one.
func (r *CircuitBreakerRegistry) GetOrCreate(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if cb, exists := r.breakers[name]; exists {
		return cb
	}
	
	cb := NewCircuitBreaker(name, maxFailures, resetTimeout)
	r.breakers[name] = cb
	return cb
}

// Get retrieves a circuit breaker by name.
func (r *CircuitBreakerRegistry) Get(name string) (*CircuitBreaker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	cb, exists := r.breakers[name]
	return cb, exists
}

// List returns all circuit breakers.
func (r *CircuitBreakerRegistry) List() []*CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	breakers := make([]*CircuitBreaker, 0, len(r.breakers))
	for _, cb := range r.breakers {
		breakers = append(breakers, cb)
	}
	return breakers
}

// GetMetrics returns metrics for all circuit breakers.
func (r *CircuitBreakerRegistry) GetMetrics() map[string]CircuitBreakerMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	metrics := make(map[string]CircuitBreakerMetrics, len(r.breakers))
	for name, cb := range r.breakers {
		metrics[name] = cb.GetMetrics()
	}
	return metrics
}

// Reset resets all circuit breakers.
func (r *CircuitBreakerRegistry) Reset() {
	r.mu.RLock()
	breakers := make([]*CircuitBreaker, 0, len(r.breakers))
	for _, cb := range r.breakers {
		breakers = append(breakers, cb)
	}
	r.mu.RUnlock()
	
	for _, cb := range breakers {
		cb.Reset()
	}
}

// Remove removes a circuit breaker from the registry.
func (r *CircuitBreakerRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, name)
}