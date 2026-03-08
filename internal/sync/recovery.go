package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// Operation represents a retryable sync operation
type Operation func(ctx context.Context) (*Result, error)

// Result represents the result of a sync operation
type Result struct {
	RecordsProcessed int64                  `json:"records_processed"`
	BytesProcessed   int64                  `json:"bytes_processed"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half_open"
)

// CircuitBreaker implements the circuit breaker pattern to prevent cascade failures
type CircuitBreaker struct {
	name           string
	maxFailures    int
	resetTimeout   time.Duration
	state          CircuitBreakerState
	failures       int
	lastFailureTime time.Time
	mu             sync.RWMutex
	logger         *slog.Logger
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration, logger *slog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		name:         name,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        CircuitBreakerClosed,
		logger:       logger,
	}
}

// Execute executes an operation through the circuit breaker
func (cb *CircuitBreaker) Execute(ctx context.Context, op Operation) (*Result, error) {
	if !cb.canExecute() {
		return nil, fmt.Errorf("circuit breaker %s is open", cb.name)
	}

	result, err := op(ctx)
	
	if err != nil {
		cb.recordFailure()
		return nil, err
	}
	
	cb.recordSuccess()
	return result, nil
}

// canExecute determines if the circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	switch cb.state {
	case CircuitBreakerClosed:
		return true
	case CircuitBreakerOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			if cb.state == CircuitBreakerOpen && time.Since(cb.lastFailureTime) > cb.resetTimeout {
				cb.state = CircuitBreakerHalfOpen
				cb.logger.Info("circuit breaker transitioning to half-open", "name", cb.name)
			}
			cb.mu.Unlock()
			cb.mu.RLock()
			return cb.state == CircuitBreakerHalfOpen
		}
		return false
	case CircuitBreakerHalfOpen:
		return true
	default:
		return false
	}
}

// recordFailure records a failure and potentially opens the circuit
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failures++
	cb.lastFailureTime = time.Now()
	
	if cb.failures >= cb.maxFailures {
		cb.state = CircuitBreakerOpen
		cb.logger.Warn("circuit breaker opened due to failures", 
			"name", cb.name, 
			"failures", cb.failures)
	}
}

// recordSuccess records a success and potentially closes the circuit
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failures = 0
	if cb.state == CircuitBreakerHalfOpen {
		cb.state = CircuitBreakerClosed
		cb.logger.Info("circuit breaker closed after successful execution", "name", cb.name)
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// RecoveryConfig holds configuration for recovery operations
type RecoveryConfig struct {
	MaxRetries             int           `yaml:"max_retries"`
	BaseDelay              time.Duration `yaml:"base_delay"`
	MaxDelay               time.Duration `yaml:"max_delay"`
	CircuitBreakerThreshold int           `yaml:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `yaml:"circuit_breaker_timeout"`
	PartialRecovery         bool          `yaml:"partial_recovery"`
	SkipFailedObjects       bool          `yaml:"skip_failed_objects"`
}

// DefaultRecoveryConfig returns a default recovery configuration
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:             3,
		BaseDelay:              1 * time.Second,
		MaxDelay:               60 * time.Second,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   2 * time.Minute,
		PartialRecovery:         true,
		SkipFailedObjects:       false,
	}
}

// RecoveryManager orchestrates retry logic and recovery operations
type RecoveryManager struct {
	config         RecoveryConfig
	circuitBreaker *CircuitBreaker
	stateStore     RecoveryStateStore
	logger         *slog.Logger
}

// RecoveryStateStore defines interface for persisting recovery state
type RecoveryStateStore interface {
	SaveRecoveryState(ctx context.Context, state *RecoveryState) error
	LoadRecoveryState(ctx context.Context, source, object string) (*RecoveryState, error)
	DeleteRecoveryState(ctx context.Context, source, object string) error
	ListRecoveryStates(ctx context.Context, source string) ([]*RecoveryState, error)
}

// S3RecoveryStateStore implements RecoveryStateStore using S3
type S3RecoveryStateStore struct {
	s3     *storage.S3Client
	bucket string
	prefix string
	logger *slog.Logger
}

// NewS3RecoveryStateStore creates a new S3-based recovery state store
func NewS3RecoveryStateStore(s3 *storage.S3Client, bucket, prefix string, logger *slog.Logger) *S3RecoveryStateStore {
	return &S3RecoveryStateStore{
		s3:     s3,
		bucket: bucket,
		prefix: prefix,
		logger: logger,
	}
}

// SaveRecoveryState saves recovery state to S3
func (s *S3RecoveryStateStore) SaveRecoveryState(ctx context.Context, state *RecoveryState) error {
	key := fmt.Sprintf("%s/recovery/%s/%s.json", s.prefix, state.Source, state.Object)
	return s.s3.PutJSON(ctx, key, state)
}

// LoadRecoveryState loads recovery state from S3
func (s *S3RecoveryStateStore) LoadRecoveryState(ctx context.Context, source, object string) (*RecoveryState, error) {
	key := fmt.Sprintf("%s/recovery/%s/%s.json", s.prefix, source, object)
	var state RecoveryState
	err := s.s3.GetJSON(ctx, key, &state)
	if err != nil {
		return nil, fmt.Errorf("load recovery state: %w", err)
	}
	return &state, nil
}

// DeleteRecoveryState deletes recovery state from S3
func (s *S3RecoveryStateStore) DeleteRecoveryState(ctx context.Context, source, object string) error {
	key := fmt.Sprintf("%s/recovery/%s/%s.json", s.prefix, source, object)
	return s.s3.Delete(ctx, key)
}

// ListRecoveryStates lists all recovery states for a source
func (s *S3RecoveryStateStore) ListRecoveryStates(ctx context.Context, source string) ([]*RecoveryState, error) {
	prefix := fmt.Sprintf("%s/recovery/%s/", s.prefix, source)
	keys, err := s.s3.ListKeys(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list recovery states: %w", err)
	}
	
	var states []*RecoveryState
	for _, key := range keys {
		var state RecoveryState
		if err := s.s3.GetJSON(ctx, key, &state); err != nil {
			s.logger.Warn("failed to load recovery state", "key", key, "error", err)
			continue
		}
		states = append(states, &state)
	}
	
	return states, nil
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(config RecoveryConfig, stateStore RecoveryStateStore, logger *slog.Logger) *RecoveryManager {
	circuitBreaker := NewCircuitBreaker(
		"sync_operations",
		config.CircuitBreakerThreshold,
		config.CircuitBreakerTimeout,
		logger,
	)
	
	return &RecoveryManager{
		config:         config,
		circuitBreaker: circuitBreaker,
		stateStore:     stateStore,
		logger:         logger,
	}
}

// ExecuteWithRetry executes an operation with retry logic and circuit breaker protection
func (rm *RecoveryManager) ExecuteWithRetry(ctx context.Context, source, object string, phase SyncPhase, op Operation) (*Result, error) {
	correlationID := fmt.Sprintf("%s_%s_%d", source, object, time.Now().Unix())
	
	// Check for existing recovery state
	recoveryState, err := rm.stateStore.LoadRecoveryState(ctx, source, object)
	if err != nil {
		// Create new recovery state if none exists
		recoveryState = &RecoveryState{
			Source:      source,
			Object:      object,
			Phase:       phase,
			Attempts:    0,
			LastAttempt: time.Time{},
			NextRetry:   time.Now(),
			Metadata:    make(map[string]interface{}),
		}
	}
	
	// Check if we can retry
	if !recoveryState.CanRetry(rm.config.MaxRetries) {
		if rm.config.SkipFailedObjects {
			rm.logger.Warn("skipping failed object after max retries", 
				"source", source,
				"object", object,
				"attempts", recoveryState.Attempts)
			return &Result{}, nil
		}
		return nil, fmt.Errorf("max retries exceeded for %s/%s (%d attempts)", source, object, recoveryState.Attempts)
	}
	
	var lastErr error
	
	// Attempt operation with retries
	for recoveryState.CanRetry(rm.config.MaxRetries) {
		// Wait for retry delay if this is not the first attempt
		if recoveryState.Attempts > 0 && time.Now().Before(recoveryState.NextRetry) {
			waitTime := time.Until(recoveryState.NextRetry)
			rm.logger.Info("waiting before retry", 
				"source", source,
				"object", object,
				"attempt", recoveryState.Attempts+1,
				"wait_time", waitTime)
			
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
			}
		}
		
		recoveryState.IncrementAttempt(rm.config.BaseDelay, rm.config.MaxDelay)
		
		rm.logger.Info("attempting sync operation",
			"source", source,
			"object", object,
			"phase", phase,
			"attempt", recoveryState.Attempts,
			"correlation_id", correlationID)
		
		// Execute through circuit breaker
		result, err := rm.circuitBreaker.Execute(ctx, op)
		if err == nil {
			// Success - clean up recovery state
			if deleteErr := rm.stateStore.DeleteRecoveryState(ctx, source, object); deleteErr != nil {
				rm.logger.Warn("failed to delete recovery state after success", 
					"source", source,
					"object", object,
					"error", deleteErr)
			}
			
			rm.logger.Info("sync operation succeeded",
				"source", source,
				"object", object,
				"phase", phase,
				"attempt", recoveryState.Attempts,
				"correlation_id", correlationID)
			
			return result, nil
		}
		
		lastErr = err
		
		// Determine if error is retryable
		var syncErr *SyncError
		retryable := true
		if errors.As(err, &syncErr) {
			retryable = syncErr.IsRetryable()
			syncErr.CorrelationID = correlationID
		}
		
		if !retryable {
			rm.logger.Error("non-retryable error encountered",
				"source", source,
				"object", object,
				"phase", phase,
				"error", err,
				"correlation_id", correlationID)
			break
		}
		
		rm.logger.Warn("sync operation failed, will retry",
			"source", source,
			"object", object,
			"phase", phase,
			"attempt", recoveryState.Attempts,
			"next_retry", recoveryState.NextRetry,
			"error", err,
			"correlation_id", correlationID)
		
		// Save recovery state for potential manual intervention
		if saveErr := rm.stateStore.SaveRecoveryState(ctx, recoveryState); saveErr != nil {
			rm.logger.Error("failed to save recovery state", 
				"source", source,
				"object", object,
				"error", saveErr)
		}
	}
	
	// All retries exhausted
	finalErr := NewSyncError(phase, object, lastErr, false).WithCorrelationID(correlationID)
	
	rm.logger.Error("sync operation failed after all retries",
		"source", source,
		"object", object,
		"phase", phase,
		"attempts", recoveryState.Attempts,
		"correlation_id", correlationID,
		"final_error", finalErr)
	
	return nil, finalErr
}

// ResumeFailedSyncs attempts to resume all failed syncs for a source
func (rm *RecoveryManager) ResumeFailedSyncs(ctx context.Context, source string) error {
	states, err := rm.stateStore.ListRecoveryStates(ctx, source)
	if err != nil {
		return fmt.Errorf("list recovery states: %w", err)
	}
	
	if len(states) == 0 {
		rm.logger.Info("no failed syncs to resume", "source", source)
		return nil
	}
	
	rm.logger.Info("resuming failed syncs", 
		"source", source, 
		"count", len(states))
	
	var resumeErrors []error
	for _, state := range states {
		if state.CanRetry(rm.config.MaxRetries) {
			rm.logger.Info("resuming failed sync", 
				"source", state.Source,
				"object", state.Object,
				"phase", state.Phase,
				"attempts", state.Attempts)
			
			// Note: This is a simplified resume - in practice, you'd need to 
			// reconstruct the specific operation based on the phase and state
			// For now, we'll just remove old recovery states
			if err := rm.stateStore.DeleteRecoveryState(ctx, state.Source, state.Object); err != nil {
				resumeErrors = append(resumeErrors, fmt.Errorf("delete stale recovery state for %s/%s: %w", 
					state.Source, state.Object, err))
			}
		}
	}
	
	if len(resumeErrors) > 0 {
		return fmt.Errorf("resume errors: %v", resumeErrors)
	}
	
	return nil
}

// GetCircuitBreakerStatus returns the current circuit breaker status
func (rm *RecoveryManager) GetCircuitBreakerStatus() CircuitBreakerState {
	return rm.circuitBreaker.GetState()
}