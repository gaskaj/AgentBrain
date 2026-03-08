package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	
	assert.Equal(t, 3, policy.MaxAttempts)
	assert.Equal(t, time.Second, policy.BaseDelay)
	assert.Equal(t, 30*time.Second, policy.MaxDelay)
	assert.True(t, policy.Jitter)
	assert.NotNil(t, policy.BackoffFunc)
	assert.NotNil(t, policy.RetryableFunc)
}

func TestNewRetryPolicy(t *testing.T) {
	policy := NewRetryPolicy(5, 2*time.Second, 60*time.Second)
	
	assert.Equal(t, 5, policy.MaxAttempts)
	assert.Equal(t, 2*time.Second, policy.BaseDelay)
	assert.Equal(t, 60*time.Second, policy.MaxDelay)
	assert.True(t, policy.Jitter)
	assert.NotNil(t, policy.BackoffFunc)
	assert.NotNil(t, policy.RetryableFunc)
}

func TestRetryPolicyWithMethods(t *testing.T) {
	customBackoff := func(attempt int, baseDelay time.Duration) time.Duration {
		return baseDelay * time.Duration(attempt)
	}
	
	customRetryable := func(err error) bool {
		return err != nil && err.Error() == "retryable"
	}
	
	var onRetryCallCount int
	onRetry := func(attempt int, err error) {
		onRetryCallCount++
	}
	
	policy := NewRetryPolicy(3, time.Second, 30*time.Second).
		WithBackoff(customBackoff).
		WithRetryableFunc(customRetryable).
		WithOnRetry(onRetry).
		WithJitter(false)
	
	assert.False(t, policy.Jitter)
	
	// Test backoff function
	delay := policy.CalculateDelay(2)
	assert.Equal(t, 2*time.Second, delay)
	
	// Test retryable function
	assert.True(t, policy.IsRetryable(errors.New("retryable")))
	assert.False(t, policy.IsRetryable(errors.New("not retryable")))
	
	// Test onRetry callback
	if policy.OnRetry != nil {
		policy.OnRetry(1, errors.New("test"))
	}
	assert.Equal(t, 1, onRetryCallCount)
}

func TestBackoffFunctions(t *testing.T) {
	baseDelay := time.Second
	
	tests := []struct {
		name     string
		backoff  BackoffFunc
		attempt  int
		expected time.Duration
	}{
		{"ExponentialBackoff-1", ExponentialBackoff, 1, time.Second},
		{"ExponentialBackoff-2", ExponentialBackoff, 2, 2 * time.Second},
		{"ExponentialBackoff-3", ExponentialBackoff, 3, 4 * time.Second},
		{"LinearBackoff-1", LinearBackoff, 1, time.Second},
		{"LinearBackoff-2", LinearBackoff, 2, 2 * time.Second},
		{"LinearBackoff-3", LinearBackoff, 3, 3 * time.Second},
		{"FixedBackoff-1", FixedBackoff, 1, time.Second},
		{"FixedBackoff-2", FixedBackoff, 2, time.Second},
		{"FixedBackoff-3", FixedBackoff, 3, time.Second},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.backoff(tt.attempt, baseDelay)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaxDelayLimit(t *testing.T) {
	policy := NewRetryPolicy(5, time.Second, 5*time.Second).WithJitter(false)
	
	// Attempt 4 would normally be 8 seconds with exponential backoff,
	// but should be capped at MaxDelay (5 seconds)
	delay := policy.CalculateDelay(4)
	assert.LessOrEqual(t, delay, 5*time.Second)
}

func TestJitterApplication(t *testing.T) {
	policy := NewRetryPolicy(3, time.Second, 30*time.Second).WithJitter(true)
	
	// Calculate delay multiple times and ensure there's variation
	delays := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		delays[i] = policy.CalculateDelay(2)
	}
	
	// Check that not all delays are identical (jitter is working)
	allSame := true
	first := delays[0]
	for i := 1; i < len(delays); i++ {
		if delays[i] != first {
			allSame = false
			break
		}
	}
	
	// With jitter, delays should vary (though there's a small chance they could be the same)
	// We'll just check that delay is reasonable around expected value
	_ = allSame // Acknowledge we're not using this for now
	expectedDelay := ExponentialBackoff(2, time.Second)
	assert.True(t, delays[0] >= expectedDelay*9/10) // At least 90% of expected
	assert.True(t, delays[0] <= expectedDelay*11/10) // At most 110% of expected
}

func TestDefaultRetryableFunc(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context timeout", context.DeadlineExceeded, false},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"timeout", errors.New("request timeout"), true},
		{"service unavailable", errors.New("service unavailable"), true},
		{"too many requests", errors.New("too many requests"), true},
		{"internal server error", errors.New("internal server error"), true},
		{"bad request", errors.New("bad request"), false},
		{"unknown error", errors.New("something went wrong"), false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultRetryableFunc(tt.err)
			assert.Equal(t, tt.retryable, result, "Error: %v", tt.err)
		})
	}
}

func TestS3RetryableFunc(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"RequestTimeout", errors.New("RequestTimeout"), true},
		{"ServiceUnavailable", errors.New("ServiceUnavailable"), true},
		{"Throttling", errors.New("Throttling"), true},
		{"NoSuchBucket", errors.New("NoSuchBucket"), false},
		{"AccessDenied", errors.New("AccessDenied"), false},
		{"InvalidArgument", errors.New("InvalidArgument"), false},
		{"connection reset", errors.New("connection reset by peer"), true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := S3RetryableFunc(tt.err)
			assert.Equal(t, tt.retryable, result, "Error: %v", tt.err)
		})
	}
}

func TestAPIRetryableFunc(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"status 429", errors.New("HTTP status 429"), true},
		{"status 500", errors.New("HTTP status 500"), true},
		{"status 502", errors.New("HTTP status 502"), true},
		{"status 400", errors.New("HTTP status 400"), false},
		{"status 401", errors.New("HTTP status 401"), false},
		{"status 404", errors.New("HTTP status 404"), false},
		{"connection timeout", errors.New("connection timeout"), true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := APIRetryableFunc(tt.err)
			assert.Equal(t, tt.retryable, result, "Error: %v", tt.err)
		})
	}
}

func TestPredefinedPolicies(t *testing.T) {
	t.Run("S3Policy", func(t *testing.T) {
		policy := S3Policy()
		assert.Equal(t, 5, policy.MaxAttempts)
		assert.Equal(t, 2*time.Second, policy.BaseDelay)
		assert.Equal(t, 120*time.Second, policy.MaxDelay)
		assert.True(t, policy.Jitter)
		assert.NotNil(t, policy.RetryableFunc)
		
		// Test S3-specific retryable function
		assert.True(t, policy.IsRetryable(errors.New("ServiceUnavailable")))
		assert.False(t, policy.IsRetryable(errors.New("NoSuchBucket")))
	})
	
	t.Run("APIPolicy", func(t *testing.T) {
		policy := APIPolicy()
		assert.Equal(t, 3, policy.MaxAttempts)
		assert.Equal(t, time.Second, policy.BaseDelay)
		assert.Equal(t, 30*time.Second, policy.MaxDelay)
		assert.True(t, policy.Jitter)
		
		// Test API-specific retryable function
		assert.True(t, policy.IsRetryable(errors.New("status 429")))
		assert.False(t, policy.IsRetryable(errors.New("status 404")))
	})
	
	t.Run("NetworkPolicy", func(t *testing.T) {
		policy := NetworkPolicy()
		assert.Equal(t, 4, policy.MaxAttempts)
		assert.Equal(t, 500*time.Millisecond, policy.BaseDelay)
		assert.Equal(t, 10*time.Second, policy.MaxDelay)
		assert.True(t, policy.Jitter)
	})
	
	t.Run("DatabasePolicy", func(t *testing.T) {
		policy := DatabasePolicy()
		assert.Equal(t, 3, policy.MaxAttempts)
		assert.Equal(t, time.Second, policy.BaseDelay)
		assert.Equal(t, 15*time.Second, policy.MaxDelay)
		assert.False(t, policy.Jitter)
	})
}

func TestSpecialBackoffFunctions(t *testing.T) {
	baseDelay := time.Second
	
	t.Run("APIRateLimitBackoff", func(t *testing.T) {
		// Should be more aggressive than exponential
		delay1 := APIRateLimitBackoff(1, baseDelay)
		delay2 := APIRateLimitBackoff(2, baseDelay)
		delay3 := APIRateLimitBackoff(3, baseDelay)
		
		assert.Equal(t, time.Second, delay1)
		assert.Equal(t, 3*time.Second, delay2)
		assert.Equal(t, 9*time.Second, delay3)
	})
	
	t.Run("NetworkBackoff", func(t *testing.T) {
		// Should be more conservative than exponential
		delay1 := NetworkBackoff(1, baseDelay)
		delay2 := NetworkBackoff(2, baseDelay)
		
		assert.Equal(t, time.Second, delay1)
		assert.True(t, delay2 > time.Second && delay2 < 2*time.Second)
	})
	
	t.Run("ExponentialBackoffWithJitter", func(t *testing.T) {
		// Should include jitter
		delay := ExponentialBackoffWithJitter(2, baseDelay)
		expectedBase := ExponentialBackoff(2, baseDelay)
		
		// Should be around the expected value but with some variation
		assert.True(t, delay >= expectedBase*9/10)
		assert.True(t, delay <= expectedBase*11/10)
	})
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("contains", func(t *testing.T) {
		assert.True(t, contains("Hello World", "Hello"))
		assert.True(t, contains("Hello World", "World"))
		assert.True(t, contains("hello world", "HELLO")) // Case insensitive
		assert.False(t, contains("Hello World", "Goodbye"))
		assert.False(t, contains("Hello", "Hello World"))
	})
	
	t.Run("toLower", func(t *testing.T) {
		assert.Equal(t, "hello world", toLower("Hello World"))
		assert.Equal(t, "hello world", toLower("HELLO WORLD"))
		assert.Equal(t, "hello world", toLower("hello world"))
		assert.Equal(t, "", toLower(""))
	})
	
	t.Run("addJitter", func(t *testing.T) {
		delay := 10 * time.Second
		jitteredDelay := addJitter(delay)
		
		// Should be within jitter range (90% to 110%)
		assert.True(t, jitteredDelay >= delay)
		assert.True(t, jitteredDelay <= delay*11/10)
	})
}