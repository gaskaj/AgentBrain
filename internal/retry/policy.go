package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// BackoffFunc defines how to calculate the delay for a given attempt.
type BackoffFunc func(attempt int, baseDelay time.Duration) time.Duration

// RetryableFunc determines if an error should trigger a retry.
type RetryableFunc func(error) bool

// OnRetryFunc is called before each retry attempt.
type OnRetryFunc func(attempt int, err error)

// RetryPolicy defines the configuration for retry behavior.
type RetryPolicy struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	BackoffFunc   BackoffFunc
	RetryableFunc RetryableFunc
	OnRetry       OnRetryFunc
	Jitter        bool
}

// DefaultRetryPolicy creates a retry policy with sensible defaults.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   3,
		BaseDelay:     time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFunc:   ExponentialBackoff,
		RetryableFunc: DefaultRetryableFunc,
		Jitter:        true,
	}
}

// NewRetryPolicy creates a new retry policy with the specified parameters.
func NewRetryPolicy(maxAttempts int, baseDelay, maxDelay time.Duration) *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   maxAttempts,
		BaseDelay:     baseDelay,
		MaxDelay:      maxDelay,
		BackoffFunc:   ExponentialBackoff,
		RetryableFunc: DefaultRetryableFunc,
		Jitter:        true,
	}
}

// WithBackoff sets the backoff function for the retry policy.
func (p *RetryPolicy) WithBackoff(backoff BackoffFunc) *RetryPolicy {
	p.BackoffFunc = backoff
	return p
}

// WithRetryableFunc sets the retryable function for the retry policy.
func (p *RetryPolicy) WithRetryableFunc(retryable RetryableFunc) *RetryPolicy {
	p.RetryableFunc = retryable
	return p
}

// WithOnRetry sets the callback function called before each retry.
func (p *RetryPolicy) WithOnRetry(onRetry OnRetryFunc) *RetryPolicy {
	p.OnRetry = onRetry
	return p
}

// WithJitter enables or disables jitter in the backoff calculation.
func (p *RetryPolicy) WithJitter(enabled bool) *RetryPolicy {
	p.Jitter = enabled
	return p
}

// CalculateDelay calculates the delay for the given attempt.
func (p *RetryPolicy) CalculateDelay(attempt int) time.Duration {
	if p.BackoffFunc == nil {
		return p.BaseDelay
	}

	delay := p.BackoffFunc(attempt, p.BaseDelay)
	
	// Apply maximum delay limit
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	// Apply jitter if enabled
	if p.Jitter {
		delay = addJitter(delay)
	}

	return delay
}

// IsRetryable determines if an error should trigger a retry.
func (p *RetryPolicy) IsRetryable(err error) bool {
	if p.RetryableFunc == nil {
		return DefaultRetryableFunc(err)
	}
	return p.RetryableFunc(err)
}

// Backoff Functions

// ExponentialBackoff implements exponential backoff: delay = baseDelay * 2^attempt
func ExponentialBackoff(attempt int, baseDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}
	multiplier := math.Pow(2, float64(attempt-1))
	return time.Duration(float64(baseDelay) * multiplier)
}

// LinearBackoff implements linear backoff: delay = baseDelay * attempt
func LinearBackoff(attempt int, baseDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}
	return time.Duration(int64(baseDelay) * int64(attempt))
}

// FixedBackoff implements fixed delay: delay = baseDelay
func FixedBackoff(attempt int, baseDelay time.Duration) time.Duration {
	return baseDelay
}

// ExponentialBackoffWithJitter implements exponential backoff with full jitter
func ExponentialBackoffWithJitter(attempt int, baseDelay time.Duration) time.Duration {
	delay := ExponentialBackoff(attempt, baseDelay)
	return addJitter(delay)
}

// Custom backoff for API rate limiting scenarios
func APIRateLimitBackoff(attempt int, baseDelay time.Duration) time.Duration {
	// More aggressive backoff for rate limiting
	if attempt <= 0 {
		return baseDelay
	}
	multiplier := math.Pow(3, float64(attempt-1))
	return time.Duration(float64(baseDelay) * multiplier)
}

// Custom backoff for network operations
func NetworkBackoff(attempt int, baseDelay time.Duration) time.Duration {
	// Conservative backoff for network issues
	if attempt <= 0 {
		return baseDelay
	}
	multiplier := math.Pow(1.5, float64(attempt-1))
	return time.Duration(float64(baseDelay) * multiplier)
}

// Retryable Functions

// DefaultRetryableFunc determines if an error is retryable based on common patterns.
func DefaultRetryableFunc(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - not retryable
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Check error message for common retryable patterns
	errStr := err.Error()
	
	// Network errors are typically retryable
	retryablePatterns := []string{
		"connection reset",
		"connection refused",
		"timeout",
		"temporary failure",
		"service unavailable",
		"too many requests",
		"rate limit",
		"throttled",
		"internal server error",
		"bad gateway",
		"service temporarily unavailable",
		"gateway timeout",
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// S3RetryableFunc determines if an S3 error is retryable.
func S3RetryableFunc(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - not retryable
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	errStr := err.Error()
	
	// S3-specific retryable errors
	s3RetryablePatterns := []string{
		"RequestTimeout",
		"ServiceUnavailable", 
		"Throttling",
		"ProvisionedThroughputExceeded",
		"SlowDown",
		"InternalError",
		"RequestTimeTooSkewed", // Can retry after clock sync
	}

	for _, pattern := range s3RetryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	// S3-specific non-retryable errors
	s3NonRetryablePatterns := []string{
		"NoSuchBucket",
		"NoSuchKey", 
		"AccessDenied",
		"InvalidArgument",
		"InvalidBucketName",
		"BucketAlreadyExists",
		"BucketNotEmpty",
	}

	for _, pattern := range s3NonRetryablePatterns {
		if contains(errStr, pattern) {
			return false
		}
	}

	// Fall back to default behavior
	return DefaultRetryableFunc(err)
}

// APIRetryableFunc determines if an API error is retryable.
func APIRetryableFunc(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - not retryable
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	errStr := err.Error()
	
	// HTTP status codes that are retryable
	retryableStatusPatterns := []string{
		"status 429", // Too Many Requests
		"status 500", // Internal Server Error
		"status 502", // Bad Gateway
		"status 503", // Service Unavailable
		"status 504", // Gateway Timeout
	}

	for _, pattern := range retryableStatusPatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	// Non-retryable HTTP status codes
	nonRetryableStatusPatterns := []string{
		"status 400", // Bad Request
		"status 401", // Unauthorized
		"status 403", // Forbidden
		"status 404", // Not Found
		"status 409", // Conflict
		"status 410", // Gone
		"status 422", // Unprocessable Entity
	}

	for _, pattern := range nonRetryableStatusPatterns {
		if contains(errStr, pattern) {
			return false
		}
	}

	// Fall back to default behavior
	return DefaultRetryableFunc(err)
}

// Predefined Policies

// S3Policy returns a retry policy optimized for S3 operations.
func S3Policy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   5,
		BaseDelay:     2 * time.Second,
		MaxDelay:      120 * time.Second,
		BackoffFunc:   ExponentialBackoff,
		RetryableFunc: S3RetryableFunc,
		Jitter:        true,
	}
}

// APIPolicy returns a retry policy optimized for API operations.
func APIPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   3,
		BaseDelay:     time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFunc:   APIRateLimitBackoff,
		RetryableFunc: APIRetryableFunc,
		Jitter:        true,
	}
}

// NetworkPolicy returns a retry policy optimized for network operations.
func NetworkPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   4,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFunc:   NetworkBackoff,
		RetryableFunc: DefaultRetryableFunc,
		Jitter:        true,
	}
}

// DatabasePolicy returns a retry policy optimized for database operations.
func DatabasePolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   3,
		BaseDelay:     time.Second,
		MaxDelay:      15 * time.Second,
		BackoffFunc:   LinearBackoff,
		RetryableFunc: DefaultRetryableFunc,
		Jitter:        false,
	}
}

// Utility functions

func addJitter(delay time.Duration) time.Duration {
	// Add up to 10% jitter
	jitterRange := float64(delay) * 0.1
	jitter := rand.Float64() * jitterRange
	return delay + time.Duration(jitter)
}

func contains(s, substr string) bool {
	// Case-insensitive string contains check
	lowerS := toLower(s)
	lowerSubstr := toLower(substr)
	return stringContains(lowerS, lowerSubstr)
}

func stringContains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			result[i] = s[i] + 32
		} else {
			result[i] = s[i]
		}
	}
	return string(result)
}