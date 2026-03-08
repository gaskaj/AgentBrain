package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	
	assert.Equal(t, "test", cb.name)
	assert.Equal(t, 3, cb.maxFailures)
	assert.Equal(t, time.Minute, cb.resetTimeout)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreakerExecuteSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	
	err := cb.Execute(func() error {
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
	
	metrics := cb.GetMetrics()
	assert.Equal(t, 1, metrics.RequestCount)
	assert.Equal(t, 1, metrics.SuccessCount)
	assert.Equal(t, 0, metrics.FailureCount)
	assert.Equal(t, 1.0, metrics.SuccessRate)
	assert.Equal(t, 0.0, metrics.FailureRate)
}

func TestCircuitBreakerExecuteFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	testErr := errors.New("test error")
	
	err := cb.Execute(func() error {
		return testErr
	})
	
	assert.Equal(t, testErr, err)
	assert.Equal(t, StateClosed, cb.GetState()) // Still closed, not enough failures
	
	metrics := cb.GetMetrics()
	assert.Equal(t, 1, metrics.RequestCount)
	assert.Equal(t, 0, metrics.SuccessCount)
	assert.Equal(t, 1, metrics.FailureCount)
	assert.Equal(t, 0.0, metrics.SuccessRate)
	assert.Equal(t, 1.0, metrics.FailureRate)
}

func TestCircuitBreakerOpensAfterMaxFailures(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	testErr := errors.New("test error")
	
	// Execute 3 failures to reach the threshold
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			return testErr
		})
		assert.Equal(t, testErr, err)
	}
	
	assert.Equal(t, StateOpen, cb.GetState())
	
	// Next execution should fail with circuit breaker error
	err := cb.Execute(func() error {
		return nil // This won't be executed
	})
	
	assert.Error(t, err)
	assert.True(t, IsCircuitBreakerError(err))
	
	cbErr, ok := err.(*CircuitBreakerError)
	assert.True(t, ok)
	assert.Equal(t, "test", cbErr.Name)
	assert.Equal(t, StateOpen, cbErr.State)
}

func TestCircuitBreakerHalfOpenTransition(t *testing.T) {
	resetTimeout := 10 * time.Millisecond
	cb := NewCircuitBreaker("test", 2, resetTimeout)
	testErr := errors.New("test error")
	
	// Trigger circuit breaker to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateOpen, cb.GetState())
	
	// Wait for reset timeout
	time.Sleep(resetTimeout + time.Millisecond)
	
	// Next execution should transition to half-open
	err := cb.Execute(func() error {
		return nil // Success
	})
	
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState()) // Success in half-open closes the circuit
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	resetTimeout := 10 * time.Millisecond
	cb := NewCircuitBreaker("test", 2, resetTimeout)
	testErr := errors.New("test error")
	
	// Trigger circuit breaker to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateOpen, cb.GetState())
	
	// Wait for reset timeout
	time.Sleep(resetTimeout + time.Millisecond)
	
	// Next execution should transition to half-open then immediately back to open
	err := cb.Execute(func() error {
		return testErr // Failure
	})
	
	assert.Equal(t, testErr, err)
	assert.Equal(t, StateOpen, cb.GetState()) // Failure in half-open reopens the circuit
}

func TestCircuitBreakerStateChangeCallback(t *testing.T) {
	var stateChanges []string
	cb := NewCircuitBreaker("test", 2, time.Minute).
		WithStateChangeCallback(func(name string, from, to State) {
			stateChanges = append(stateChanges, name+":"+from.String()+"->"+to.String())
		})
	
	testErr := errors.New("test error")
	
	// Trigger state changes
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	
	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)
	
	assert.Contains(t, stateChanges, "test:CLOSED->OPEN")
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Minute)
	testErr := errors.New("test error")
	
	// Trigger failures and open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateOpen, cb.GetState())
	
	// Reset the circuit breaker
	cb.Reset()
	
	assert.Equal(t, StateClosed, cb.GetState())
	
	metrics := cb.GetMetrics()
	assert.Equal(t, 0, metrics.RequestCount)
	assert.Equal(t, 0, metrics.SuccessCount)
	assert.Equal(t, 0, metrics.FailureCount)
	assert.Equal(t, 0.0, metrics.SuccessRate)
	assert.Equal(t, 0.0, metrics.FailureRate)
}

func TestCircuitBreakerMetrics(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	testErr := errors.New("test error")
	
	// Execute some operations
	cb.Execute(func() error { return nil })       // Success
	cb.Execute(func() error { return testErr })   // Failure
	cb.Execute(func() error { return nil })       // Success
	
	metrics := cb.GetMetrics()
	
	assert.Equal(t, "test", metrics.Name)
	assert.Equal(t, StateClosed, metrics.State)
	assert.Equal(t, 3, metrics.RequestCount)
	assert.Equal(t, 2, metrics.SuccessCount)
	assert.Equal(t, 1, metrics.FailureCount)
	assert.InDelta(t, 2.0/3.0, metrics.SuccessRate, 0.01)
	assert.InDelta(t, 1.0/3.0, metrics.FailureRate, 0.01)
	assert.False(t, metrics.LastSuccessTime.IsZero())
	assert.False(t, metrics.LastFailureTime.IsZero())
}

func TestCircuitBreakerRegistry(t *testing.T) {
	registry := NewCircuitBreakerRegistry()
	
	t.Run("GetOrCreate", func(t *testing.T) {
		cb1 := registry.GetOrCreate("test1", 3, time.Minute)
		cb2 := registry.GetOrCreate("test1", 5, 2*time.Minute) // Should return existing
		
		assert.Same(t, cb1, cb2) // Same instance
		assert.Equal(t, 3, cb1.maxFailures) // Original config preserved
	})
	
	t.Run("Get", func(t *testing.T) {
		cb, exists := registry.Get("test1")
		assert.True(t, exists)
		assert.NotNil(t, cb)
		
		cb, exists = registry.Get("nonexistent")
		assert.False(t, exists)
		assert.Nil(t, cb)
	})
	
	t.Run("List", func(t *testing.T) {
		registry.GetOrCreate("test2", 2, time.Minute)
		registry.GetOrCreate("test3", 4, 2*time.Minute)
		
		breakers := registry.List()
		assert.Len(t, breakers, 3) // test1, test2, test3
	})
	
	t.Run("GetMetrics", func(t *testing.T) {
		// Execute some operations on different breakers
		cb1, _ := registry.Get("test1")
		cb2, _ := registry.Get("test2")
		
		cb1.Execute(func() error { return nil })
		cb2.Execute(func() error { return errors.New("error") })
		
		allMetrics := registry.GetMetrics()
		assert.Len(t, allMetrics, 3)
		assert.Contains(t, allMetrics, "test1")
		assert.Contains(t, allMetrics, "test2")
		assert.Contains(t, allMetrics, "test3")
	})
	
	t.Run("Reset", func(t *testing.T) {
		registry.Reset()
		
		// All breakers should be reset
		allMetrics := registry.GetMetrics()
		for _, metrics := range allMetrics {
			assert.Equal(t, StateClosed, metrics.State)
			assert.Equal(t, 0, metrics.RequestCount)
		}
	})
	
	t.Run("Remove", func(t *testing.T) {
		registry.Remove("test1")
		
		cb, exists := registry.Get("test1")
		assert.False(t, exists)
		assert.Nil(t, cb)
		
		breakers := registry.List()
		assert.Len(t, breakers, 2) // test2, test3 remain
	})
}

func TestStateString(t *testing.T) {
	assert.Equal(t, "CLOSED", StateClosed.String())
	assert.Equal(t, "OPEN", StateOpen.String())
	assert.Equal(t, "HALF_OPEN", StateHalfOpen.String())
	assert.Equal(t, "UNKNOWN", State(999).String())
}

func TestCircuitBreakerError(t *testing.T) {
	err := &CircuitBreakerError{
		Name:  "test-service",
		State: StateOpen,
	}
	
	assert.Equal(t, "circuit breaker 'test-service' is OPEN", err.Error())
	assert.True(t, IsCircuitBreakerError(err))
	assert.False(t, IsCircuitBreakerError(errors.New("other error")))
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	cb := NewCircuitBreaker("test", 10, time.Minute)
	
	// Run concurrent executions
	concurrency := 100
	successChan := make(chan bool, concurrency)
	
	for i := 0; i < concurrency; i++ {
		go func() {
			err := cb.Execute(func() error {
				return nil
			})
			successChan <- (err == nil)
		}()
	}
	
	// Collect results
	successCount := 0
	for i := 0; i < concurrency; i++ {
		if <-successChan {
			successCount++
		}
	}
	
	assert.Equal(t, concurrency, successCount)
	
	metrics := cb.GetMetrics()
	assert.Equal(t, concurrency, metrics.RequestCount)
	assert.Equal(t, concurrency, metrics.SuccessCount)
	assert.Equal(t, 0, metrics.FailureCount)
}