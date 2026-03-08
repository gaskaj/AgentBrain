package sync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRecoveryStateStore is a mock implementation for testing
type MockRecoveryStateStore struct {
	mock.Mock
}

func (m *MockRecoveryStateStore) SaveRecoveryState(ctx context.Context, state *RecoveryState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockRecoveryStateStore) LoadRecoveryState(ctx context.Context, source, object string) (*RecoveryState, error) {
	args := m.Called(ctx, source, object)
	return args.Get(0).(*RecoveryState), args.Error(1)
}

func (m *MockRecoveryStateStore) DeleteRecoveryState(ctx context.Context, source, object string) error {
	args := m.Called(ctx, source, object)
	return args.Error(0)
}

func (m *MockRecoveryStateStore) ListRecoveryStates(ctx context.Context, source string) ([]*RecoveryState, error) {
	args := m.Called(ctx, source)
	return args.Get(0).([]*RecoveryState), args.Error(1)
}

func TestCircuitBreakerClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cb := NewCircuitBreaker("test", 3, 1*time.Minute, logger)

	// Circuit should be closed initially
	assert.Equal(t, CircuitBreakerClosed, cb.GetState())

	// Should allow execution
	successOp := func(ctx context.Context) (*Result, error) {
		return &Result{RecordsProcessed: 10}, nil
	}
	
	result, err := cb.Execute(context.Background(), successOp)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), result.RecordsProcessed)
	assert.Equal(t, CircuitBreakerClosed, cb.GetState())
}

func TestCircuitBreakerOpens(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cb := NewCircuitBreaker("test", 2, 1*time.Minute, logger) // Low threshold for testing

	failingOp := func(ctx context.Context) (*Result, error) {
		return nil, errors.New("operation failed")
	}

	// First failure
	_, err := cb.Execute(context.Background(), failingOp)
	assert.Error(t, err)
	assert.Equal(t, CircuitBreakerClosed, cb.GetState())

	// Second failure - should open circuit
	_, err = cb.Execute(context.Background(), failingOp)
	assert.Error(t, err)
	assert.Equal(t, CircuitBreakerOpen, cb.GetState())

	// Third attempt should fail immediately
	_, err = cb.Execute(context.Background(), failingOp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker test is open")
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cb := NewCircuitBreaker("test", 1, 10*time.Millisecond, logger) // Very short timeout

	failingOp := func(ctx context.Context) (*Result, error) {
		return nil, errors.New("operation failed")
	}
	successOp := func(ctx context.Context) (*Result, error) {
		return &Result{RecordsProcessed: 10}, nil
	}

	// Fail once to open circuit
	_, err := cb.Execute(context.Background(), failingOp)
	assert.Error(t, err)
	assert.Equal(t, CircuitBreakerOpen, cb.GetState())

	// Wait for timeout to transition to half-open
	time.Sleep(15 * time.Millisecond)

	// Next successful execution should close circuit
	result, err := cb.Execute(context.Background(), successOp)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), result.RecordsProcessed)
	assert.Equal(t, CircuitBreakerClosed, cb.GetState())
}

func TestDefaultRecoveryConfig(t *testing.T) {
	config := DefaultRecoveryConfig()
	
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.BaseDelay)
	assert.Equal(t, 60*time.Second, config.MaxDelay)
	assert.Equal(t, 5, config.CircuitBreakerThreshold)
	assert.Equal(t, 2*time.Minute, config.CircuitBreakerTimeout)
	assert.True(t, config.PartialRecovery)
	assert.False(t, config.SkipFailedObjects)
}

func TestRecoveryManagerExecuteWithRetrySuccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{
		MaxRetries:  2,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock no existing recovery state
	mockStore.On("LoadRecoveryState", mock.Anything, "test-source", "test-object").Return(
		(*RecoveryState)(nil), errors.New("not found"))
	
	// Mock successful deletion after success
	mockStore.On("DeleteRecoveryState", mock.Anything, "test-source", "test-object").Return(nil)

	successOp := func(ctx context.Context) (*Result, error) {
		return &Result{RecordsProcessed: 100}, nil
	}

	result, err := rm.ExecuteWithRetry(context.Background(), "test-source", "test-object", SyncPhaseExtract, successOp)
	
	assert.NoError(t, err)
	assert.Equal(t, int64(100), result.RecordsProcessed)
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerExecuteWithRetryEventualSuccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{
		MaxRetries:              3,
		BaseDelay:               10 * time.Millisecond,
		MaxDelay:                100 * time.Millisecond,
		CircuitBreakerThreshold: 5, // Set high to avoid circuit breaker interference
		CircuitBreakerTimeout:   1 * time.Minute,
	}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock no existing recovery state
	mockStore.On("LoadRecoveryState", mock.Anything, "test-source", "test-object").Return(
		(*RecoveryState)(nil), errors.New("not found"))
	
	// Mock recovery state saves for failures
	mockStore.On("SaveRecoveryState", mock.Anything, mock.AnythingOfType("*sync.RecoveryState")).Return(nil)
	
	// Mock successful deletion after eventual success
	mockStore.On("DeleteRecoveryState", mock.Anything, "test-source", "test-object").Return(nil)

	attemptCount := 0
	retryableOp := func(ctx context.Context) (*Result, error) {
		attemptCount++
		if attemptCount == 2 { // Succeed on 2nd attempt (with MaxRetries=3, we get up to 3 total attempts)
			return &Result{RecordsProcessed: 50}, nil
		}
		return nil, NewSyncError(SyncPhaseExtract, "test-object", errors.New("transient failure"), true)
	}

	result, err := rm.ExecuteWithRetry(context.Background(), "test-source", "test-object", SyncPhaseExtract, retryableOp)
	
	assert.NoError(t, err)
	assert.Equal(t, int64(50), result.RecordsProcessed)
	assert.Equal(t, 2, attemptCount)
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerExecuteWithRetryMaxRetriesExceeded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{
		MaxRetries:  2,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock no existing recovery state
	mockStore.On("LoadRecoveryState", mock.Anything, "test-source", "test-object").Return(
		(*RecoveryState)(nil), errors.New("not found"))
	
	// Mock recovery state saves
	mockStore.On("SaveRecoveryState", mock.Anything, mock.AnythingOfType("*sync.RecoveryState")).Return(nil)

	failingOp := func(ctx context.Context) (*Result, error) {
		return nil, NewSyncError(SyncPhaseExtract, "test-object", errors.New("persistent failure"), true)
	}

	result, err := rm.ExecuteWithRetry(context.Background(), "test-source", "test-object", SyncPhaseExtract, failingOp)
	
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "sync error in extract phase")
	
	var syncErr *SyncError
	assert.True(t, errors.As(err, &syncErr))
	assert.Equal(t, "test-object", syncErr.Object)
	assert.Equal(t, SyncPhaseExtract, syncErr.Phase)
	
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerSkipFailedObjects(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{
		MaxRetries:        2,
		BaseDelay:         10 * time.Millisecond,
		MaxDelay:          100 * time.Millisecond,
		SkipFailedObjects: true,
	}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock existing recovery state that has exceeded max retries
	existingState := &RecoveryState{
		Source:    "test-source",
		Object:    "test-object",
		Phase:     SyncPhaseExtract,
		Attempts:  3, // Exceeds MaxRetries
		NextRetry: time.Now().Add(-1 * time.Minute),
	}
	mockStore.On("LoadRecoveryState", mock.Anything, "test-source", "test-object").Return(existingState, nil)

	failingOp := func(ctx context.Context) (*Result, error) {
		return nil, errors.New("should not be called")
	}

	result, err := rm.ExecuteWithRetry(context.Background(), "test-source", "test-object", SyncPhaseExtract, failingOp)
	
	// Should succeed by skipping the failed object
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.RecordsProcessed)
	
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerNonRetryableError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock no existing recovery state
	mockStore.On("LoadRecoveryState", mock.Anything, "test-source", "test-object").Return(
		(*RecoveryState)(nil), errors.New("not found"))

	nonRetryableOp := func(ctx context.Context) (*Result, error) {
		return nil, NewSyncError(SyncPhaseValidate, "test-object", errors.New("validation failed"), false)
	}

	result, err := rm.ExecuteWithRetry(context.Background(), "test-source", "test-object", SyncPhaseValidate, nonRetryableOp)
	
	assert.Error(t, err)
	assert.Nil(t, result)
	
	var syncErr *SyncError
	assert.True(t, errors.As(err, &syncErr))
	assert.False(t, syncErr.IsRetryable())
	
	// Should not have tried to save recovery state for non-retryable error
	mockStore.AssertNotCalled(t, "SaveRecoveryState")
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerResumeFailedSyncs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := RecoveryConfig{MaxRetries: 3}

	rm := NewRecoveryManager(config, mockStore, logger)

	// Mock recovery states
	states := []*RecoveryState{
		{
			Source:    "test-source",
			Object:    "object1",
			Phase:     SyncPhaseExtract,
			Attempts:  1,
			NextRetry: time.Now().Add(-1 * time.Minute),
		},
		{
			Source:    "test-source",
			Object:    "object2", 
			Phase:     SyncPhaseStore,
			Attempts:  2,
			NextRetry: time.Now().Add(-1 * time.Minute),
		},
	}

	mockStore.On("ListRecoveryStates", mock.Anything, "test-source").Return(states, nil)
	mockStore.On("DeleteRecoveryState", mock.Anything, "test-source", "object1").Return(nil)
	mockStore.On("DeleteRecoveryState", mock.Anything, "test-source", "object2").Return(nil)

	err := rm.ResumeFailedSyncs(context.Background(), "test-source")
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestRecoveryManagerGetCircuitBreakerStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockStore := new(MockRecoveryStateStore)
	config := DefaultRecoveryConfig()

	rm := NewRecoveryManager(config, mockStore, logger)
	
	status := rm.GetCircuitBreakerStatus()
	assert.Equal(t, CircuitBreakerClosed, status)
}