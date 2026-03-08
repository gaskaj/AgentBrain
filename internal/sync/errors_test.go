package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSyncError(t *testing.T) {
	originalErr := errors.New("connection timeout")
	syncErr := NewSyncError(SyncPhaseConnect, "Account", originalErr, true)

	assert.Equal(t, SyncPhaseConnect, syncErr.Phase)
	assert.Equal(t, "Account", syncErr.Object)
	assert.Equal(t, originalErr, syncErr.Cause)
	assert.Equal(t, "connection timeout", syncErr.CauseMsg)
	assert.True(t, syncErr.Retryable)
	// CorrelationID is not set by default constructor
	assert.Empty(t, syncErr.CorrelationID)
	assert.NotEmpty(t, syncErr.Suggestion)
	assert.NotZero(t, syncErr.Timestamp)
	assert.NotZero(t, syncErr.Context.MemoryUsage)
	assert.NotZero(t, syncErr.Context.GoroutineCount)
}

func TestNewSyncErrorWithBatch(t *testing.T) {
	originalErr := errors.New("batch processing failed")
	syncErr := NewSyncErrorWithBatch(SyncPhaseExtract, "Contact", "batch-123", 500, originalErr, true)

	assert.Equal(t, SyncPhaseExtract, syncErr.Phase)
	assert.Equal(t, "Contact", syncErr.Object)
	assert.Equal(t, "batch-123", syncErr.BatchID)
	assert.Equal(t, 500, syncErr.RecordCount)
	assert.Equal(t, originalErr, syncErr.Cause)
	assert.True(t, syncErr.Retryable)
}

func TestSyncErrorJSON(t *testing.T) {
	originalErr := errors.New("test error")
	syncErr := NewSyncError(SyncPhaseStore, "Opportunity", originalErr, false)
	syncErr.CorrelationID = "test-correlation-id"

	jsonBytes, err := syncErr.JSON()
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonBytes)

	// Verify JSON contains expected fields
	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, "store")
	assert.Contains(t, jsonStr, "Opportunity")
	assert.Contains(t, jsonStr, "test error")
	assert.Contains(t, jsonStr, "test-correlation-id")
	assert.Contains(t, jsonStr, "false") // retryable
}

func TestSyncErrorWithCorrelationID(t *testing.T) {
	originalErr := errors.New("test error")
	syncErr := NewSyncError(SyncPhaseTransform, "Lead", originalErr, true)
	
	correlationID := "test-id-12345"
	result := syncErr.WithCorrelationID(correlationID)
	
	assert.Equal(t, correlationID, result.CorrelationID)
	assert.Equal(t, syncErr, result) // Should return same instance
}

func TestSyncErrorWithConnectorState(t *testing.T) {
	originalErr := errors.New("test error")
	syncErr := NewSyncError(SyncPhaseConnect, "User", originalErr, true)
	
	connectorState := map[string]interface{}{
		"authenticated": true,
		"rate_limited": false,
		"last_request": time.Now().Unix(),
	}
	
	result := syncErr.WithConnectorState(connectorState)
	
	assert.Equal(t, connectorState, result.Context.ConnectorState)
	assert.Equal(t, syncErr, result) // Should return same instance
}

func TestSyncErrorUnwrap(t *testing.T) {
	originalErr := errors.New("original error")
	syncErr := NewSyncError(SyncPhaseValidate, "Case", originalErr, true)

	unwrapped := syncErr.Unwrap()
	assert.Equal(t, originalErr, unwrapped)
}

func TestGenerateSuggestion(t *testing.T) {
	testCases := []struct {
		phase    SyncPhase
		expected string
	}{
		{
			phase:    SyncPhaseConnect,
			expected: "Check connector credentials and API endpoint availability. Verify network connectivity.",
		},
		{
			phase:    SyncPhaseDiscover,
			expected: "Verify object permissions and API access. Check if objects exist in the source system.",
		},
		{
			phase:    SyncPhaseExtract,
			expected: "Check API rate limits and query timeouts. Consider reducing batch size or adding retry delays.",
		},
		{
			phase:    SyncPhaseStore,
			expected: "Verify S3 bucket permissions and network connectivity. Check available disk space.",
		},
		{
			phase:    SyncPhaseCommit,
			expected: "Check Delta Lake transaction conflicts. Verify log file integrity and retry the commit.",
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.phase), func(t *testing.T) {
			err := errors.New("test error")
			suggestion := generateSuggestion(tc.phase, err)
			assert.Equal(t, tc.expected, suggestion)
		})
	}
}

func TestErrorAggregator(t *testing.T) {
	aggregator := NewErrorAggregator()

	// Initially no errors
	assert.False(t, aggregator.HasErrors())
	assert.Empty(t, aggregator.Errors())
	assert.NoError(t, aggregator.Error())

	// Add first error
	err1 := errors.New("first error")
	aggregator.Add(err1)
	assert.True(t, aggregator.HasErrors())
	assert.Len(t, aggregator.Errors(), 1)
	assert.Equal(t, err1, aggregator.Error())

	// Add second error
	err2 := errors.New("second error")
	aggregator.Add(err2)
	assert.True(t, aggregator.HasErrors())
	assert.Len(t, aggregator.Errors(), 2)
	
	combinedErr := aggregator.Error()
	assert.Error(t, combinedErr)
	assert.Contains(t, combinedErr.Error(), "multiple sync errors")
	assert.Contains(t, combinedErr.Error(), "2 total")

	// Add nil error (should be ignored)
	aggregator.Add(nil)
	assert.Len(t, aggregator.Errors(), 2) // Still 2 errors
}

func TestRecoveryStateCanRetry(t *testing.T) {
	state := &RecoveryState{
		Source:    "test-source",
		Object:    "test-object",
		Phase:     SyncPhaseExtract,
		Attempts:  2,
		NextRetry: time.Now().Add(-1 * time.Minute), // Past time
	}

	// Can retry if under max attempts and past next retry time
	assert.True(t, state.CanRetry(3))
	
	// Cannot retry if at max attempts
	assert.False(t, state.CanRetry(2))
	
	// Cannot retry if before next retry time
	state.NextRetry = time.Now().Add(1 * time.Minute) // Future time
	assert.False(t, state.CanRetry(3))
}

func TestRecoveryStateIncrementAttempt(t *testing.T) {
	state := &RecoveryState{
		Source:    "test-source",
		Object:    "test-object",
		Phase:     SyncPhaseExtract,
		Attempts:  0,
		NextRetry: time.Time{},
	}

	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second
	beforeTime := time.Now()

	// First attempt
	state.IncrementAttempt(baseDelay, maxDelay)
	assert.Equal(t, 1, state.Attempts)
	assert.True(t, state.LastAttempt.After(beforeTime))
	// NextRetry should be at or after LastAttempt (allowing for same time due to jitter)
	assert.True(t, state.NextRetry.After(state.LastAttempt) || state.NextRetry.Equal(state.LastAttempt))

	// Second attempt
	state.IncrementAttempt(baseDelay, maxDelay)
	assert.Equal(t, 2, state.Attempts)
	assert.True(t, state.NextRetry.After(state.LastAttempt) || state.NextRetry.Equal(state.LastAttempt))

	// Check that attempts increment properly
	initialAttempts := state.Attempts
	state.IncrementAttempt(baseDelay, maxDelay)
	assert.Equal(t, initialAttempts+1, state.Attempts)
}

func TestSyncErrorIsRetryable(t *testing.T) {
	// Create retryable error
	retryableErr := NewSyncError(SyncPhaseConnect, "Account", errors.New("timeout"), true)
	assert.True(t, retryableErr.IsRetryable())

	// Create non-retryable error  
	nonRetryableErr := NewSyncError(SyncPhaseValidate, "Account", errors.New("validation failed"), false)
	assert.False(t, nonRetryableErr.IsRetryable())
}

func TestErrorContextCapture(t *testing.T) {
	ctx := captureErrorContext()
	
	assert.NotZero(t, ctx.Timestamp)
	assert.Greater(t, ctx.MemoryUsage, int64(0))
	assert.Greater(t, ctx.GoroutineCount, 0)
	assert.NotZero(t, ctx.StorageState)
}

func TestSyncErrorError(t *testing.T) {
	originalErr := errors.New("connection failed")
	syncErr := NewSyncError(SyncPhaseConnect, "Account", originalErr, true)
	
	errMsg := syncErr.Error()
	assert.Contains(t, errMsg, "sync error in connect phase")
	assert.Contains(t, errMsg, "Account")
	assert.Contains(t, errMsg, "connection failed")
}