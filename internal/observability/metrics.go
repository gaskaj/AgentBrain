package observability

import (
	"sync"
	"time"
)

// CheckpointMetrics contains checkpoint-related performance metrics.
type CheckpointMetrics struct {
	TotalCheckpoints      int64         `json:"total_checkpoints"`
	LastCheckpointTime    time.Time     `json:"last_checkpoint_time"`
	CheckpointDuration    time.Duration `json:"checkpoint_duration"`
	ValidationSuccesses   int64         `json:"validation_successes"`
	ValidationFailures    int64         `json:"validation_failures"`
	StorageSpaceSaved     int64         `json:"storage_space_saved_bytes"`
	LogReplayTime         time.Duration `json:"log_replay_time"`
	CompactionRuns        int64         `json:"compaction_runs"`
	CheckpointsDeleted    int64         `json:"checkpoints_deleted"`
}

// DeltaMetrics contains Delta Lake related metrics.
type DeltaMetrics struct {
	TablesManaged         int64                      `json:"tables_managed"`
	TotalCommits          int64                      `json:"total_commits"`
	ActiveFiles           int64                      `json:"active_files"`
	TotalDataSize         int64                      `json:"total_data_size_bytes"`
	CheckpointMetrics     CheckpointMetrics          `json:"checkpoint_metrics"`
	TableMetrics          map[string]TableMetrics    `json:"table_metrics"`
}

// RetrySystemMetrics contains retry and circuit breaker metrics.
type RetrySystemMetrics struct {
	TotalRetryAttempts     int64                          `json:"total_retry_attempts"`
	SuccessfulRetries      int64                          `json:"successful_retries"`
	FailedRetries          int64                          `json:"failed_retries"`
	OverallSuccessRate     float64                        `json:"overall_success_rate"`
	CircuitBreakersOpen    int                            `json:"circuit_breakers_open"`
	TotalCircuitBreakers   int                            `json:"total_circuit_breakers"`
	OperationMetrics       map[string]RetryOperationMetrics `json:"operation_metrics"`
	CircuitBreakerMetrics  map[string]CircuitBreakerStatus  `json:"circuit_breaker_metrics"`
	LastMetricsUpdate      time.Time                      `json:"last_metrics_update"`
}

// RetryOperationMetrics contains metrics for a specific retry operation.
type RetryOperationMetrics struct {
	Operation         string        `json:"operation"`
	TotalAttempts     int64         `json:"total_attempts"`
	SuccessfulRetries int64         `json:"successful_retries"`
	FailedRetries     int64         `json:"failed_retries"`
	AverageDelay      time.Duration `json:"average_delay"`
	SuccessRate       float64       `json:"success_rate"`
	LastAttempt       time.Time     `json:"last_attempt"`
}

// CircuitBreakerStatus contains the status of a circuit breaker.
type CircuitBreakerStatus struct {
	Name            string    `json:"name"`
	State           string    `json:"state"`
	RequestCount    int64     `json:"request_count"`
	FailureRate     float64   `json:"failure_rate"`
	LastFailure     time.Time `json:"last_failure"`
	StateTransitions int64    `json:"state_transitions"`
}

// TableMetrics contains metrics for individual Delta tables.
type TableMetrics struct {
	Source               string        `json:"source"`
	Object               string        `json:"object"`
	CurrentVersion       int64         `json:"current_version"`
	FileCount            int64         `json:"file_count"`
	DataSize             int64         `json:"data_size_bytes"`
	LastCommitTime       time.Time     `json:"last_commit_time"`
	LastCheckpointTime   time.Time     `json:"last_checkpoint_time"`
	CommitsPerHour       float64       `json:"commits_per_hour"`
}

// MetricsCollector collects and aggregates Delta Lake metrics.
type MetricsCollector struct {
	mu            sync.RWMutex
	deltaMetrics      DeltaMetrics
	retryMetrics      RetrySystemMetrics
	retryEnabled      bool
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		deltaMetrics: DeltaMetrics{
			TableMetrics: make(map[string]TableMetrics),
		},
		retryMetrics: RetrySystemMetrics{
			OperationMetrics:      make(map[string]RetryOperationMetrics),
			CircuitBreakerMetrics: make(map[string]CircuitBreakerStatus),
		},
	}
}

// RecordCheckpointCreated records a checkpoint creation event.
func (mc *MetricsCollector) RecordCheckpointCreated(tableName string, version int64, duration time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.TotalCheckpoints++
	mc.deltaMetrics.CheckpointMetrics.LastCheckpointTime = time.Now()
	mc.deltaMetrics.CheckpointMetrics.CheckpointDuration = duration
	
	// Update table-specific metrics
	if table, exists := mc.deltaMetrics.TableMetrics[tableName]; exists {
		table.LastCheckpointTime = time.Now()
		mc.deltaMetrics.TableMetrics[tableName] = table
	}
}

// RecordCheckpointValidation records a checkpoint validation result.
func (mc *MetricsCollector) RecordCheckpointValidation(tableName string, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	if success {
		mc.deltaMetrics.CheckpointMetrics.ValidationSuccesses++
	} else {
		mc.deltaMetrics.CheckpointMetrics.ValidationFailures++
	}
}

// RecordLogReplay records the time taken for log replay.
func (mc *MetricsCollector) RecordLogReplay(tableName string, duration time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.LogReplayTime = duration
}

// RecordStorageSaved records storage space saved by cleanup operations.
func (mc *MetricsCollector) RecordStorageSaved(bytes int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.StorageSpaceSaved += bytes
}

// RecordCompactionRun records a file compaction operation.
func (mc *MetricsCollector) RecordCompactionRun(tableName string, filesBefore, filesAfter int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.CompactionRuns++
}

// RecordCheckpointDeleted records a checkpoint deletion.
func (mc *MetricsCollector) RecordCheckpointDeleted(tableName string, version int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.CheckpointsDeleted++
}

// RecordTableCommit records a commit to a Delta table.
func (mc *MetricsCollector) RecordTableCommit(source, object string, version int64, fileCount, dataSize int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	tableName := source + "_" + object
	now := time.Now()
	
	mc.deltaMetrics.TotalCommits++
	
	table, exists := mc.deltaMetrics.TableMetrics[tableName]
	if !exists {
		table = TableMetrics{
			Source: source,
			Object: object,
		}
		mc.deltaMetrics.TablesManaged++
	}
	
	// Calculate commits per hour
	if !table.LastCommitTime.IsZero() {
		hoursSinceLastCommit := now.Sub(table.LastCommitTime).Hours()
		if hoursSinceLastCommit > 0 {
			table.CommitsPerHour = 1.0 / hoursSinceLastCommit
		}
	}
	
	table.CurrentVersion = version
	table.FileCount = fileCount
	table.DataSize = dataSize
	table.LastCommitTime = now
	
	mc.deltaMetrics.TableMetrics[tableName] = table
	
	// Update aggregate metrics
	mc.deltaMetrics.ActiveFiles += fileCount
	mc.deltaMetrics.TotalDataSize += dataSize
}

// GetDeltaMetrics returns a snapshot of current Delta Lake metrics.
func (mc *MetricsCollector) GetDeltaMetrics() DeltaMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	// Return a deep copy to avoid race conditions
	metrics := mc.deltaMetrics
	
	// Copy table metrics map
	metrics.TableMetrics = make(map[string]TableMetrics)
	for k, v := range mc.deltaMetrics.TableMetrics {
		metrics.TableMetrics[k] = v
	}
	
	return metrics
}

// GetTableMetrics returns metrics for a specific table.
func (mc *MetricsCollector) GetTableMetrics(source, object string) (TableMetrics, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	tableName := source + "_" + object
	metrics, exists := mc.deltaMetrics.TableMetrics[tableName]
	return metrics, exists
}

// GetCheckpointHealthScore calculates a health score for checkpointing (0-100).
func (mc *MetricsCollector) GetCheckpointHealthScore() float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	metrics := mc.deltaMetrics.CheckpointMetrics
	
	if metrics.TotalCheckpoints == 0 {
		return 50.0 // Neutral score if no checkpoints yet
	}
	
	score := 100.0
	
	// Reduce score for validation failures
	if totalValidations := metrics.ValidationSuccesses + metrics.ValidationFailures; totalValidations > 0 {
		failureRate := float64(metrics.ValidationFailures) / float64(totalValidations)
		score -= failureRate * 30.0 // Max 30 point penalty for failures
	}
	
	// Reduce score if checkpoints are stale
	if !metrics.LastCheckpointTime.IsZero() {
		hoursSinceLastCheckpoint := time.Since(metrics.LastCheckpointTime).Hours()
		if hoursSinceLastCheckpoint > 24 {
			stalePenalty := (hoursSinceLastCheckpoint - 24) * 2 // 2 points per hour after 24h
			if stalePenalty > 20 {
				stalePenalty = 20 // Max 20 point penalty
			}
			score -= stalePenalty
		}
	}
	
	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	
	return score
}

// Reset clears all metrics (useful for testing).
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics = DeltaMetrics{
		TableMetrics: make(map[string]TableMetrics),
	}
	mc.retryMetrics = RetrySystemMetrics{
		OperationMetrics:      make(map[string]RetryOperationMetrics),
		CircuitBreakerMetrics: make(map[string]CircuitBreakerStatus),
	}
}

// EnableRetryMetrics enables retry metrics collection.
func (mc *MetricsCollector) EnableRetryMetrics() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.retryEnabled = true
}

// RecordRetryAttempt records a retry attempt for an operation.
func (mc *MetricsCollector) RecordRetryAttempt(operation string, success bool, delay time.Duration) {
	if !mc.retryEnabled {
		return
	}
	
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	metrics, exists := mc.retryMetrics.OperationMetrics[operation]
	if !exists {
		metrics = RetryOperationMetrics{
			Operation: operation,
		}
	}
	
	metrics.TotalAttempts++
	metrics.LastAttempt = time.Now()
	
	if success {
		metrics.SuccessfulRetries++
		mc.retryMetrics.SuccessfulRetries++
	} else {
		metrics.FailedRetries++
		mc.retryMetrics.FailedRetries++
	}
	
	mc.retryMetrics.TotalRetryAttempts++
	
	// Calculate success rate
	if metrics.TotalAttempts > 0 {
		metrics.SuccessRate = float64(metrics.SuccessfulRetries) / float64(metrics.TotalAttempts)
	}
	
	// Update average delay (simple moving average approximation)
	if metrics.AverageDelay == 0 {
		metrics.AverageDelay = delay
	} else {
		metrics.AverageDelay = (metrics.AverageDelay + delay) / 2
	}
	
	mc.retryMetrics.OperationMetrics[operation] = metrics
	
	// Update overall success rate
	if mc.retryMetrics.TotalRetryAttempts > 0 {
		mc.retryMetrics.OverallSuccessRate = float64(mc.retryMetrics.SuccessfulRetries) / float64(mc.retryMetrics.TotalRetryAttempts)
	}
	
	mc.retryMetrics.LastMetricsUpdate = time.Now()
}

// RecordCircuitBreakerState records the state of a circuit breaker.
func (mc *MetricsCollector) RecordCircuitBreakerState(name, state string, requestCount, failureCount int64, lastFailure time.Time) {
	if !mc.retryEnabled {
		return
	}
	
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	status, exists := mc.retryMetrics.CircuitBreakerMetrics[name]
	if !exists {
		status = CircuitBreakerStatus{Name: name}
		mc.retryMetrics.TotalCircuitBreakers++
	}
	
	// Track state transitions
	if status.State != "" && status.State != state {
		status.StateTransitions++
	}
	
	// Update open circuit breaker count
	if status.State != "OPEN" && state == "OPEN" {
		mc.retryMetrics.CircuitBreakersOpen++
	} else if status.State == "OPEN" && state != "OPEN" {
		mc.retryMetrics.CircuitBreakersOpen--
	}
	
	status.State = state
	status.RequestCount = requestCount
	status.LastFailure = lastFailure
	
	// Calculate failure rate
	if requestCount > 0 {
		status.FailureRate = float64(failureCount) / float64(requestCount)
	}
	
	mc.retryMetrics.CircuitBreakerMetrics[name] = status
	mc.retryMetrics.LastMetricsUpdate = time.Now()
}

// GetRetryMetrics returns current retry system metrics.
func (mc *MetricsCollector) GetRetryMetrics() RetrySystemMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	// Return a deep copy to avoid race conditions
	metrics := mc.retryMetrics
	
	// Copy operation metrics map
	metrics.OperationMetrics = make(map[string]RetryOperationMetrics)
	for k, v := range mc.retryMetrics.OperationMetrics {
		metrics.OperationMetrics[k] = v
	}
	
	// Copy circuit breaker metrics map
	metrics.CircuitBreakerMetrics = make(map[string]CircuitBreakerStatus)
	for k, v := range mc.retryMetrics.CircuitBreakerMetrics {
		metrics.CircuitBreakerMetrics[k] = v
	}
	
	return metrics
}

// GetRetryHealthScore calculates a health score for the retry system (0-100).
func (mc *MetricsCollector) GetRetryHealthScore() float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	if !mc.retryEnabled {
		return 100.0 // If retry system not used, consider it healthy
	}
	
	score := 100.0
	
	// Penalize low overall success rate
	if mc.retryMetrics.TotalRetryAttempts > 10 { // Only if significant sample size
		successRatePenalty := (1.0 - mc.retryMetrics.OverallSuccessRate) * 40.0 // Max 40 point penalty
		score -= successRatePenalty
	}
	
	// Penalize open circuit breakers
	if mc.retryMetrics.TotalCircuitBreakers > 0 {
		openRatio := float64(mc.retryMetrics.CircuitBreakersOpen) / float64(mc.retryMetrics.TotalCircuitBreakers)
		openPenalty := openRatio * 30.0 // Max 30 point penalty for all breakers open
		score -= openPenalty
	}
	
	// Penalize high failure rates on individual operations
	highFailureOps := 0
	for _, opMetrics := range mc.retryMetrics.OperationMetrics {
		if opMetrics.TotalAttempts > 5 && opMetrics.SuccessRate < 0.8 {
			highFailureOps++
		}
	}
	
	if len(mc.retryMetrics.OperationMetrics) > 0 {
		highFailureRatio := float64(highFailureOps) / float64(len(mc.retryMetrics.OperationMetrics))
		highFailurePenalty := highFailureRatio * 20.0 // Max 20 point penalty
		score -= highFailurePenalty
	}
	
	// Penalize stale metrics (no recent activity)
	if !mc.retryMetrics.LastMetricsUpdate.IsZero() {
		hoursSinceUpdate := time.Since(mc.retryMetrics.LastMetricsUpdate).Hours()
		if hoursSinceUpdate > 1 {
			stalePenalty := (hoursSinceUpdate - 1) * 2 // 2 points per hour after 1h
			if stalePenalty > 10 {
				stalePenalty = 10 // Max 10 point penalty
			}
			score -= stalePenalty
		}
	}
	
	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	
	return score
}