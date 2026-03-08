package retry

import (
	"context"
	"fmt"
	"time"
)

// Operation represents a retryable operation that returns a value of type T.
type Operation[T any] func(ctx context.Context) (T, error)

// VoidOperation represents a retryable operation that returns only an error.
type VoidOperation func(ctx context.Context) error

// Execute executes an operation with retry logic according to the given policy.
func Execute[T any](ctx context.Context, policy *RetryPolicy, operation Operation[T]) (T, error) {
	var zero T
	var lastErr error

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Execute the operation
		result, err := operation(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if the error is retryable
		if !policy.IsRetryable(err) {
			return zero, fmt.Errorf("non-retryable error on attempt %d: %w", attempt, err)
		}

		// If this was the last attempt, don't wait
		if attempt == policy.MaxAttempts {
			break
		}

		// Call the onRetry callback if set
		if policy.OnRetry != nil {
			policy.OnRetry(attempt, err)
		}

		// Calculate delay for next attempt
		delay := policy.CalculateDelay(attempt)

		// Wait before retrying, respecting context cancellation
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}

	return zero, fmt.Errorf("operation failed after %d attempts: %w", policy.MaxAttempts, lastErr)
}

// ExecuteVoid executes a void operation with retry logic.
func ExecuteVoid(ctx context.Context, policy *RetryPolicy, operation VoidOperation) error {
	_, err := Execute(ctx, policy, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, operation(ctx)
	})
	return err
}

// ExecuteWithCircuitBreaker executes an operation with circuit breaker protection.
func ExecuteWithCircuitBreaker[T any](ctx context.Context, cb *CircuitBreaker, operation Operation[T]) (T, error) {
	var result T
	var opErr error

	err := cb.Execute(func() error {
		var err error
		result, err = operation(ctx)
		opErr = err
		return err
	})

	if err != nil {
		// If circuit breaker error, return it with zero value
		if IsCircuitBreakerError(err) {
			var zero T
			return zero, err
		}
		// Otherwise return the operation error
		var zero T
		return zero, opErr
	}

	return result, nil
}

// ExecuteVoidWithCircuitBreaker executes a void operation with circuit breaker protection.
func ExecuteVoidWithCircuitBreaker(ctx context.Context, cb *CircuitBreaker, operation VoidOperation) error {
	_, err := ExecuteWithCircuitBreaker(ctx, cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, operation(ctx)
	})
	return err
}

// ExecuteWithRetryAndCircuitBreaker executes an operation with both retry logic and circuit breaker protection.
func ExecuteWithRetryAndCircuitBreaker[T any](
	ctx context.Context,
	policy *RetryPolicy,
	cb *CircuitBreaker,
	operation Operation[T],
) (T, error) {
	return Execute(ctx, policy, func(ctx context.Context) (T, error) {
		return ExecuteWithCircuitBreaker(ctx, cb, operation)
	})
}

// ExecuteVoidWithRetryAndCircuitBreaker executes a void operation with both retry and circuit breaker.
func ExecuteVoidWithRetryAndCircuitBreaker(
	ctx context.Context,
	policy *RetryPolicy,
	cb *CircuitBreaker,
	operation VoidOperation,
) error {
	return ExecuteVoid(ctx, policy, func(ctx context.Context) error {
		return ExecuteVoidWithCircuitBreaker(ctx, cb, operation)
	})
}

// ExecutionResult contains the results of a retry execution.
type ExecutionResult[T any] struct {
	Result        T
	Error         error
	Attempts      int
	TotalDuration time.Duration
	LastDelay     time.Duration
}

// ExecuteWithStats executes an operation with retry logic and returns detailed stats.
func ExecuteWithStats[T any](
	ctx context.Context,
	policy *RetryPolicy,
	operation Operation[T],
) ExecutionResult[T] {
	var zero T
	start := time.Now()
	var lastErr error
	var lastDelay time.Duration

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Execute the operation
		result, err := operation(ctx)
		if err == nil {
			return ExecutionResult[T]{
				Result:        result,
				Error:         nil,
				Attempts:      attempt,
				TotalDuration: time.Since(start),
				LastDelay:     lastDelay,
			}
		}

		lastErr = err

		// Check if the error is retryable
		if !policy.IsRetryable(err) {
			return ExecutionResult[T]{
				Result:        zero,
				Error:         fmt.Errorf("non-retryable error on attempt %d: %w", attempt, err),
				Attempts:      attempt,
				TotalDuration: time.Since(start),
				LastDelay:     lastDelay,
			}
		}

		// If this was the last attempt, don't wait
		if attempt == policy.MaxAttempts {
			break
		}

		// Call the onRetry callback if set
		if policy.OnRetry != nil {
			policy.OnRetry(attempt, err)
		}

		// Calculate delay for next attempt
		delay := policy.CalculateDelay(attempt)
		lastDelay = delay

		// Wait before retrying, respecting context cancellation
		select {
		case <-ctx.Done():
			return ExecutionResult[T]{
				Result:        zero,
				Error:         ctx.Err(),
				Attempts:      attempt,
				TotalDuration: time.Since(start),
				LastDelay:     lastDelay,
			}
		case <-time.After(delay):
		}
	}

	return ExecutionResult[T]{
		Result:        zero,
		Error:         fmt.Errorf("operation failed after %d attempts: %w", policy.MaxAttempts, lastErr),
		Attempts:      policy.MaxAttempts,
		TotalDuration: time.Since(start),
		LastDelay:     lastDelay,
	}
}

// RetryableWrapper provides a convenient way to wrap existing functions with retry logic.
type RetryableWrapper[T any] struct {
	policy    *RetryPolicy
	operation Operation[T]
}

// NewRetryableWrapper creates a new retryable wrapper.
func NewRetryableWrapper[T any](policy *RetryPolicy, operation Operation[T]) *RetryableWrapper[T] {
	return &RetryableWrapper[T]{
		policy:    policy,
		operation: operation,
	}
}

// Execute executes the wrapped operation with retry logic.
func (rw *RetryableWrapper[T]) Execute(ctx context.Context) (T, error) {
	return Execute(ctx, rw.policy, rw.operation)
}

// ExecuteWithStats executes the wrapped operation and returns detailed stats.
func (rw *RetryableWrapper[T]) ExecuteWithStats(ctx context.Context) ExecutionResult[T] {
	return ExecuteWithStats(ctx, rw.policy, rw.operation)
}

// UpdatePolicy updates the retry policy.
func (rw *RetryableWrapper[T]) UpdatePolicy(policy *RetryPolicy) {
	rw.policy = policy
}

// GetPolicy returns the current retry policy.
func (rw *RetryableWrapper[T]) GetPolicy() *RetryPolicy {
	return rw.policy
}

// Batch execution utilities

// BatchResult contains the result of a single item in a batch.
type BatchResult[T any] struct {
	Index  int
	Result T
	Error  error
}

// ExecuteBatch executes multiple operations concurrently with retry logic.
func ExecuteBatch[T any](
	ctx context.Context,
	policy *RetryPolicy,
	operations []Operation[T],
	maxConcurrency int,
) []BatchResult[T] {
	if len(operations) == 0 {
		return nil
	}

	if maxConcurrency <= 0 {
		maxConcurrency = len(operations)
	}

	results := make([]BatchResult[T], len(operations))
	sem := make(chan struct{}, maxConcurrency)
	
	// Use a channel to collect results
	resultsChan := make(chan BatchResult[T], len(operations))

	// Start workers
	for i, op := range operations {
		go func(idx int, operation Operation[T]) {
			sem <- struct{}{} // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			result, err := Execute(ctx, policy, operation)
			resultsChan <- BatchResult[T]{
				Index:  idx,
				Result: result,
				Error:  err,
			}
		}(i, op)
	}

	// Collect results
	for i := 0; i < len(operations); i++ {
		result := <-resultsChan
		results[result.Index] = result
	}

	return results
}

// Helper functions for common retry scenarios

// WithDeadline creates a context with deadline and executes the operation.
func WithDeadline[T any](
	ctx context.Context,
	deadline time.Time,
	policy *RetryPolicy,
	operation Operation[T],
) (T, error) {
	deadlineCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return Execute(deadlineCtx, policy, operation)
}

// WithTimeout creates a context with timeout and executes the operation.
func WithTimeout[T any](
	ctx context.Context,
	timeout time.Duration,
	policy *RetryPolicy,
	operation Operation[T],
) (T, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return Execute(timeoutCtx, policy, operation)
}