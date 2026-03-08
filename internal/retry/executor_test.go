package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExecuteSuccess(t *testing.T) {
	policy := DefaultRetryPolicy()
	callCount := 0
	
	result, err := Execute(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		return "success", nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, callCount)
}

func TestExecuteRetryableFailure(t *testing.T) {
	policy := NewRetryPolicy(3, 10*time.Millisecond, time.Second)
	callCount := 0
	
	result, err := Execute(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 3 {
			return "", errors.New("connection timeout") // Retryable error
		}
		return "success", nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 3, callCount)
}

func TestExecuteNonRetryableFailure(t *testing.T) {
	policy := DefaultRetryPolicy()
	callCount := 0
	
	result, err := Execute(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		return "", errors.New("not retryable")
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, 1, callCount)
	assert.Contains(t, err.Error(), "non-retryable error")
}

func TestExecuteMaxAttemptsExceeded(t *testing.T) {
	policy := NewRetryPolicy(2, 10*time.Millisecond, time.Second)
	callCount := 0
	
	result, err := Execute(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		return "", errors.New("connection timeout") // Always fail with retryable error
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, 2, callCount)
	assert.Contains(t, err.Error(), "operation failed after 2 attempts")
}

func TestExecuteContextCancellation(t *testing.T) {
	policy := NewRetryPolicy(5, 100*time.Millisecond, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	
	callCount := 0
	result, err := Execute(ctx, policy, func(ctx context.Context) (string, error) {
		callCount++
		return "", errors.New("timeout") // Retryable error
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.Equal(t, 1, callCount) // Only one call before context timeout
}

func TestExecuteVoid(t *testing.T) {
	policy := DefaultRetryPolicy()
	callCount := 0
	
	err := ExecuteVoid(context.Background(), policy, func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return errors.New("connection timeout")
		}
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestExecuteWithCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Minute)
	callCount := 0
	
	result, err := ExecuteWithCircuitBreaker(context.Background(), cb, func(ctx context.Context) (string, error) {
		callCount++
		return "success", nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, callCount)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestExecuteWithCircuitBreakerOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, time.Minute)
	
	// Trigger circuit breaker to open
	cb.Execute(func() error { return errors.New("error") })
	assert.Equal(t, StateOpen, cb.GetState())
	
	result, err := ExecuteWithCircuitBreaker(context.Background(), cb, func(ctx context.Context) (string, error) {
		return "should not execute", nil
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	assert.True(t, IsCircuitBreakerError(err))
}

func TestExecuteVoidWithCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Minute)
	callCount := 0
	
	err := ExecuteVoidWithCircuitBreaker(context.Background(), cb, func(ctx context.Context) error {
		callCount++
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestExecuteWithRetryAndCircuitBreaker(t *testing.T) {
	policy := NewRetryPolicy(3, 10*time.Millisecond, time.Second)
	cb := NewCircuitBreaker("test", 5, time.Minute)
	callCount := 0
	
	result, err := ExecuteWithRetryAndCircuitBreaker(context.Background(), policy, cb, func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 2 {
			return "", errors.New("timeout") // Retryable error
		}
		return "success", nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 2, callCount)
}

func TestExecuteVoidWithRetryAndCircuitBreaker(t *testing.T) {
	policy := NewRetryPolicy(3, 10*time.Millisecond, time.Second)
	cb := NewCircuitBreaker("test", 5, time.Minute)
	callCount := 0
	
	err := ExecuteVoidWithRetryAndCircuitBreaker(context.Background(), policy, cb, func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return errors.New("timeout")
		}
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestExecuteWithStats(t *testing.T) {
	policy := NewRetryPolicy(3, 10*time.Millisecond, time.Second)
	callCount := 0
	
	result := ExecuteWithStats(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 2 {
			return "", errors.New("timeout")
		}
		return "success", nil
	})
	
	assert.NoError(t, result.Error)
	assert.Equal(t, "success", result.Result)
	assert.Equal(t, 2, result.Attempts)
	assert.True(t, result.TotalDuration > 0)
	assert.True(t, result.LastDelay > 0)
}

func TestExecuteWithStatsFailure(t *testing.T) {
	policy := NewRetryPolicy(2, 10*time.Millisecond, time.Second)
	
	result := ExecuteWithStats(context.Background(), policy, func(ctx context.Context) (string, error) {
		return "", errors.New("timeout")
	})
	
	assert.Error(t, result.Error)
	assert.Empty(t, result.Result)
	assert.Equal(t, 2, result.Attempts)
	assert.Contains(t, result.Error.Error(), "operation failed after 2 attempts")
}

func TestRetryableWrapper(t *testing.T) {
	policy := DefaultRetryPolicy()
	callCount := 0
	
	wrapper := NewRetryableWrapper(policy, func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 2 {
			return "", errors.New("timeout")
		}
		return "wrapped success", nil
	})
	
	result, err := wrapper.Execute(context.Background())
	
	assert.NoError(t, err)
	assert.Equal(t, "wrapped success", result)
	assert.Equal(t, 2, callCount)
	assert.Same(t, policy, wrapper.GetPolicy())
}

func TestRetryableWrapperWithStats(t *testing.T) {
	policy := NewRetryPolicy(2, 10*time.Millisecond, time.Second)
	
	wrapper := NewRetryableWrapper(policy, func(ctx context.Context) (string, error) {
		return "", errors.New("timeout")
	})
	
	result := wrapper.ExecuteWithStats(context.Background())
	
	assert.Error(t, result.Error)
	assert.Empty(t, result.Result)
	assert.Equal(t, 2, result.Attempts)
}

func TestRetryableWrapperUpdatePolicy(t *testing.T) {
	originalPolicy := DefaultRetryPolicy()
	wrapper := NewRetryableWrapper(originalPolicy, func(ctx context.Context) (string, error) {
		return "test", nil
	})
	
	newPolicy := NewRetryPolicy(5, 2*time.Second, time.Minute)
	wrapper.UpdatePolicy(newPolicy)
	
	assert.Same(t, newPolicy, wrapper.GetPolicy())
	assert.NotSame(t, originalPolicy, wrapper.GetPolicy())
}

func TestExecuteBatch(t *testing.T) {
	policy := NewRetryPolicy(2, 10*time.Millisecond, time.Second)
	
	operations := []Operation[string]{
		func(ctx context.Context) (string, error) { return "result1", nil },
		func(ctx context.Context) (string, error) { return "", errors.New("timeout") },
		func(ctx context.Context) (string, error) { return "result3", nil },
	}
	
	results := ExecuteBatch(context.Background(), policy, operations, 2)
	
	assert.Len(t, results, 3)
	
	// Check results (order might be different due to concurrency)
	var successResults []string
	var errorCount int
	
	for _, result := range results {
		if result.Error == nil {
			successResults = append(successResults, result.Result)
		} else {
			errorCount++
		}
	}
	
	assert.Len(t, successResults, 2)
	assert.Equal(t, 1, errorCount)
	assert.Contains(t, successResults, "result1")
	assert.Contains(t, successResults, "result3")
}

func TestExecuteBatchEmpty(t *testing.T) {
	policy := DefaultRetryPolicy()
	var operations []Operation[string]
	
	results := ExecuteBatch(context.Background(), policy, operations, 2)
	
	assert.Nil(t, results)
}

func TestWithDeadline(t *testing.T) {
	policy := NewRetryPolicy(3, 50*time.Millisecond, time.Second)
	deadline := time.Now().Add(100 * time.Millisecond)
	callCount := 0
	
	result, err := WithDeadline(context.Background(), deadline, policy, func(ctx context.Context) (string, error) {
		callCount++
		return "", errors.New("timeout") // Always fail
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	// Should be cut short by deadline
	assert.True(t, callCount <= 3)
}

func TestWithTimeout(t *testing.T) {
	policy := NewRetryPolicy(5, 50*time.Millisecond, time.Second)
	timeout := 100 * time.Millisecond
	callCount := 0
	
	result, err := WithTimeout(context.Background(), timeout, policy, func(ctx context.Context) (string, error) {
		callCount++
		return "", errors.New("timeout") // Always fail
	})
	
	assert.Error(t, err)
	assert.Empty(t, result)
	// Should be cut short by timeout
	assert.True(t, callCount <= 5)
}

func TestOnRetryCallback(t *testing.T) {
	var attempts []int
	var errorsSlice []error
	
	policy := NewRetryPolicy(3, 10*time.Millisecond, time.Second).
		WithOnRetry(func(attempt int, err error) {
			attempts = append(attempts, attempt)
			errorsSlice = append(errorsSlice, err)
		})
	
	callCount := 0
	Execute(context.Background(), policy, func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 3 {
			return "", errors.New("timeout")
		}
		return "success", nil
	})
	
	assert.Len(t, attempts, 2) // Called for first 2 failed attempts
	assert.Equal(t, []int{1, 2}, attempts)
	assert.Len(t, errorsSlice, 2)
	for _, err := range errorsSlice {
		assert.Contains(t, err.Error(), "timeout")
	}
}

func TestExecuteWithDifferentTypes(t *testing.T) {
	policy := DefaultRetryPolicy()
	
	// Test with int
	intResult, err := Execute(context.Background(), policy, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 42, intResult)
	
	// Test with struct
	type TestStruct struct {
		Name  string
		Value int
	}
	
	structResult, err := Execute(context.Background(), policy, func(ctx context.Context) (TestStruct, error) {
		return TestStruct{Name: "test", Value: 100}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "test", structResult.Name)
	assert.Equal(t, 100, structResult.Value)
	
	// Test with slice
	sliceResult, err := Execute(context.Background(), policy, func(ctx context.Context) ([]string, error) {
		return []string{"a", "b", "c"}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, sliceResult)
}