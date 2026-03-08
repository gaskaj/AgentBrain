package sync

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

// SyncPhase represents different phases of the sync process
type SyncPhase string

const (
	SyncPhaseConnect   SyncPhase = "connect"
	SyncPhaseDiscover  SyncPhase = "discover"
	SyncPhaseExtract   SyncPhase = "extract"
	SyncPhaseTransform SyncPhase = "transform"
	SyncPhaseStore     SyncPhase = "store"
	SyncPhaseCommit    SyncPhase = "commit"
	SyncPhaseValidate  SyncPhase = "validate"
)

// SyncError provides rich context for sync operation failures
type SyncError struct {
	Phase       SyncPhase    `json:"phase"`
	Object      string       `json:"object,omitempty"`
	BatchID     string       `json:"batch_id,omitempty"`
	RecordCount int          `json:"record_count"`
	Cause       error        `json:"-"`
	CauseMsg    string       `json:"cause"`
	Context     ErrorContext `json:"context"`
	Retryable   bool         `json:"retryable"`
	Suggestion  string       `json:"suggestion"`
	StackTrace  string       `json:"stack_trace,omitempty"`
	Timestamp   time.Time    `json:"timestamp"`
	CorrelationID string     `json:"correlation_id,omitempty"`
}

// ErrorContext captures system state at the time of failure
type ErrorContext struct {
	ConnectorState map[string]interface{} `json:"connector_state,omitempty"`
	StorageState   StorageMetrics         `json:"storage_state"`
	MemoryUsage    int64                  `json:"memory_usage_bytes"`
	GoroutineCount int                    `json:"goroutine_count"`
	Timestamp      time.Time              `json:"timestamp"`
	Source         string                 `json:"source"`
}

// StorageMetrics contains storage-related metrics at error time
type StorageMetrics struct {
	S3Connected    bool   `json:"s3_connected"`
	DiskSpaceMB    int64  `json:"disk_space_mb"`
	PendingUploads int    `json:"pending_uploads"`
	LastWriteTime  *time.Time `json:"last_write_time,omitempty"`
}

// Error implements the error interface
func (e *SyncError) Error() string {
	return fmt.Sprintf("sync error in %s phase for object %s: %s", 
		e.Phase, e.Object, e.CauseMsg)
}

// Unwrap allows error unwrapping for Go 1.13+ error handling
func (e *SyncError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns whether this error can be retried
func (e *SyncError) IsRetryable() bool {
	return e.Retryable
}

// JSON serializes the error to JSON for logging and storage
func (e *SyncError) JSON() ([]byte, error) {
	// Ensure CauseMsg is set for JSON serialization
	if e.Cause != nil && e.CauseMsg == "" {
		e.CauseMsg = e.Cause.Error()
	}
	return json.Marshal(e)
}

// NewSyncError creates a new sync error with context
func NewSyncError(phase SyncPhase, object string, err error, retryable bool) *SyncError {
	ctx := captureErrorContext()
	
	syncErr := &SyncError{
		Phase:         phase,
		Object:        object,
		Cause:         err,
		CauseMsg:      err.Error(),
		Context:       ctx,
		Retryable:     retryable,
		Timestamp:     time.Now(),
		StackTrace:    captureStackTrace(),
	}
	
	// Add phase-specific suggestions
	syncErr.Suggestion = generateSuggestion(phase, err)
	
	return syncErr
}

// NewSyncErrorWithBatch creates a sync error with batch context
func NewSyncErrorWithBatch(phase SyncPhase, object, batchID string, recordCount int, err error, retryable bool) *SyncError {
	syncErr := NewSyncError(phase, object, err, retryable)
	syncErr.BatchID = batchID
	syncErr.RecordCount = recordCount
	return syncErr
}

// WithCorrelationID adds a correlation ID for tracing
func (e *SyncError) WithCorrelationID(id string) *SyncError {
	e.CorrelationID = id
	return e
}

// WithConnectorState adds connector state to the error context
func (e *SyncError) WithConnectorState(state map[string]interface{}) *SyncError {
	e.Context.ConnectorState = state
	return e
}

// captureErrorContext gathers system state at error time
func captureErrorContext() ErrorContext {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	return ErrorContext{
		MemoryUsage:    int64(m.Alloc),
		GoroutineCount: runtime.NumGoroutine(),
		Timestamp:      time.Now(),
		StorageState: StorageMetrics{
			S3Connected: true, // TODO: Add actual S3 health check
			DiskSpaceMB: 1024, // TODO: Add actual disk space check
		},
	}
}

// captureStackTrace captures the current stack trace
func captureStackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// generateSuggestion provides actionable recovery suggestions based on phase and error
func generateSuggestion(phase SyncPhase, err error) string {
	switch phase {
	case SyncPhaseConnect:
		return "Check connector credentials and API endpoint availability. Verify network connectivity."
	case SyncPhaseDiscover:
		return "Verify object permissions and API access. Check if objects exist in the source system."
	case SyncPhaseExtract:
		return "Check API rate limits and query timeouts. Consider reducing batch size or adding retry delays."
	case SyncPhaseTransform:
		return "Validate data schema and transformation rules. Check for data type mismatches."
	case SyncPhaseStore:
		return "Verify S3 bucket permissions and network connectivity. Check available disk space."
	case SyncPhaseCommit:
		return "Check Delta Lake transaction conflicts. Verify log file integrity and retry the commit."
	case SyncPhaseValidate:
		return "Review validation rules and data quality. Consider adjusting validation thresholds."
	default:
		return "Check logs for detailed error information and contact support if issue persists."
	}
}

// ErrorAggregator collects and aggregates errors from parallel operations
type ErrorAggregator struct {
	errors []error
	mu     sync.RWMutex
}

// NewErrorAggregator creates a new error aggregator
func NewErrorAggregator() *ErrorAggregator {
	return &ErrorAggregator{}
}

// Add adds an error to the aggregator
func (ea *ErrorAggregator) Add(err error) {
	if err == nil {
		return
	}
	ea.mu.Lock()
	defer ea.mu.Unlock()
	ea.errors = append(ea.errors, err)
}

// HasErrors returns true if any errors have been collected
func (ea *ErrorAggregator) HasErrors() bool {
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	return len(ea.errors) > 0
}

// Errors returns all collected errors
func (ea *ErrorAggregator) Errors() []error {
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	result := make([]error, len(ea.errors))
	copy(result, ea.errors)
	return result
}

// Error returns a formatted error message containing all errors
func (ea *ErrorAggregator) Error() error {
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	
	if len(ea.errors) == 0 {
		return nil
	}
	
	if len(ea.errors) == 1 {
		return ea.errors[0]
	}
	
	return fmt.Errorf("multiple sync errors (%d total): %v", len(ea.errors), ea.errors)
}

// RecoveryState represents the state needed to resume a failed sync
type RecoveryState struct {
	Source        string            `json:"source"`
	Object        string            `json:"object"`
	Phase         SyncPhase         `json:"phase"`
	LastBatchID   string            `json:"last_batch_id,omitempty"`
	WatermarkValue interface{}      `json:"watermark_value,omitempty"`
	ErrorContext  ErrorContext      `json:"error_context"`
	Attempts      int               `json:"attempts"`
	LastAttempt   time.Time         `json:"last_attempt"`
	NextRetry     time.Time         `json:"next_retry"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// CanRetry returns whether this recovery state allows for another retry
func (rs *RecoveryState) CanRetry(maxAttempts int) bool {
	return rs.Attempts < maxAttempts && time.Now().After(rs.NextRetry)
}

// IncrementAttempt increments the attempt counter and calculates next retry time
func (rs *RecoveryState) IncrementAttempt(baseDelay, maxDelay time.Duration) {
	rs.Attempts++
	rs.LastAttempt = time.Now()
	
	// Calculate exponential backoff with jitter
	delay := time.Duration(rs.Attempts-1) * baseDelay
	if delay > maxDelay {
		delay = maxDelay
	}
	
	// Add jitter (±10% random variation)
	jitter := time.Duration(float64(delay) * 0.1 * (2.0*rand.Float64() - 1.0))
	rs.NextRetry = rs.LastAttempt.Add(delay + jitter)
}